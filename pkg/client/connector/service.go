package connector

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh/terminal"
	"google.golang.org/grpc"

	"github.com/datawire/ambassador/pkg/metriton"
	"github.com/datawire/dlib/dgroup"
	"github.com/datawire/dlib/dlog"
	"github.com/datawire/dlib/dutil"
	"github.com/datawire/telepresence2/pkg/client"
	"github.com/datawire/telepresence2/pkg/rpc/common"
	rpc "github.com/datawire/telepresence2/pkg/rpc/connector"
	"github.com/datawire/telepresence2/pkg/rpc/daemon"
	"github.com/datawire/telepresence2/pkg/rpc/manager"
)

var help = `The Telepresence Connect is a background component that manages a connection. It
requires that a daemon is already running.

Launch the Telepresence Connector:
    telepresence connect

The Connector uses the Daemon's log so its output can be found in
    ` + client.Logfile + `
to troubleshoot problems.
`

// service represents the state of the Telepresence Connector
type service struct {
	rpc.UnimplementedConnectorServer
	daemon       daemon.DaemonClient
	daemonLogger daemonLogger
	cluster      *k8sCluster
	bridge       *bridge
	trafficMgr   *trafficManager
	ctx          context.Context
	cancel       func()
}

// Command returns the CLI sub-command for "connector-foreground"
func Command() *cobra.Command {
	var init bool
	c := &cobra.Command{
		Use:    "connector-foreground",
		Short:  "Launch Telepresence Connector in the foreground (debug)",
		Args:   cobra.ExactArgs(0),
		Hidden: true,
		Long:   help,
		RunE: func(_ *cobra.Command, args []string) error {
			return run(init)
		},
	}
	flags := c.Flags()
	flags.BoolVar(&init, "init", false, "initialize running connector (for debugging)")
	return c
}

type callCtx struct {
	context.Context
	caller context.Context
}

func (c callCtx) Deadline() (deadline time.Time, ok bool) {
	if dl, ok := c.Context.Deadline(); ok {
		return dl, true
	}
	return c.caller.Deadline()
}

func (c callCtx) Done() <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		select {
		case <-c.Context.Done():
			close(ch)
		case <-c.caller.Done():
			close(ch)
		}
	}()
	return ch
}

func (c callCtx) Err() error {
	err := c.Context.Err()
	if err == nil {
		err = c.caller.Err()
	}
	return err
}

func (c callCtx) Value(key interface{}) interface{} {
	return c.Context.Value(key)
}

func callRecovery(c context.Context, r interface{}, err error) error {
	perr := dutil.PanicToError(r)
	if perr != nil {
		if err == nil {
			err = perr
		} else {
			dlog.Errorf(c, "%+v", perr)
		}
	}
	if err != nil {
		dlog.Errorf(c, "%+v", err)
	}
	return err
}

var ucn int64 = 0

func nextUcn() int {
	return int(atomic.AddInt64(&ucn, 1))
}

func callName(s string) string {
	return fmt.Sprintf("%s-%d", s, nextUcn())
}

func (s *service) callCtx(c context.Context, name string) context.Context {
	return dgroup.WithGoroutineName(&callCtx{Context: s.ctx, caller: c}, callName(name))
}

func (s *service) Version(_ context.Context, _ *empty.Empty) (*common.VersionInfo, error) {
	return &common.VersionInfo{
		ApiVersion: client.APIVersion,
		Version:    client.Version(),
	}, nil
}

func (s *service) Connect(c context.Context, cr *rpc.ConnectRequest) (ci *rpc.ConnectInfo, err error) {
	c = s.callCtx(c, "Connect")
	defer func() { err = callRecovery(c, recover(), err) }()
	return s.connect(c, cr), nil
}

func (s *service) CreateIntercept(c context.Context, ir *manager.CreateInterceptRequest) (result *rpc.InterceptResult, err error) {
	ie, is := s.interceptStatus()
	if ie != rpc.InterceptError_UNSPECIFIED {
		return &rpc.InterceptResult{Error: ie, ErrorText: is}, nil
	}
	c = s.callCtx(c, "CreateIntercept")
	defer func() { err = callRecovery(c, recover(), err) }()
	return s.trafficMgr.addIntercept(c, s.ctx, ir)
}

func (s *service) RemoveIntercept(c context.Context, rr *manager.RemoveInterceptRequest2) (result *rpc.InterceptResult, err error) {
	ie, is := s.interceptStatus()
	if ie != rpc.InterceptError_UNSPECIFIED {
		return &rpc.InterceptResult{Error: ie, ErrorText: is}, nil
	}
	c = s.callCtx(c, "RemoveIntercept")
	defer func() { err = callRecovery(c, recover(), err) }()
	err = s.trafficMgr.removeIntercept(c, rr.Name)
	return &rpc.InterceptResult{}, err
}

func (s *service) List(_ context.Context, lr *rpc.ListRequest) (*rpc.DeploymentInfoSnapshot, error) {
	if s.trafficMgr.grpc == nil {
		return &rpc.DeploymentInfoSnapshot{}, nil
	}
	return s.trafficMgr.deploymentInfoSnapshot(lr.Filter), nil
}

func (s *service) Uninstall(c context.Context, ur *rpc.UninstallRequest) (result *rpc.UninstallResult, err error) {
	c = s.callCtx(c, "Uninstall")
	defer func() { err = callRecovery(c, recover(), err) }()
	return s.trafficMgr.uninstall(c, ur)
}

func (s *service) Quit(_ context.Context, _ *empty.Empty) (*empty.Empty, error) {
	s.cancel()
	return &empty.Empty{}, nil
}

// daemonLogger is an io.Writer implementation that sends data to the daemon logger
type daemonLogger struct {
	stream daemon.Daemon_LoggerClient
}

func (d *daemonLogger) Write(data []byte) (n int, err error) {
	err = d.stream.Send(&daemon.LogMessage{Text: data})
	return len(data), err
}

// connect the connector to a cluster
func (s *service) connect(c context.Context, cr *rpc.ConnectRequest) *rpc.ConnectInfo {
	r := &rpc.ConnectInfo{}
	setStatus := func() {
		r.ClusterOk = true
		r.ClusterContext = s.cluster.Context
		r.ClusterServer = s.cluster.server()
		if s.bridge != nil {
			r.BridgeOk = s.bridge.check(c)
		}
		if s.trafficMgr != nil {
			s.trafficMgr.setStatus(r)
		}
	}

	// Sanity checks
	if s.cluster != nil {
		setStatus()
		r.Error = rpc.ConnectInfo_ALREADY_CONNECTED
		return r
	}

	if s.bridge != nil {
		r.Error = rpc.ConnectInfo_DISCONNECTING
		return r
	}

	dgroup.ParentGroup(s.ctx).Go(callName("metriton"), func(c context.Context) error {
		reporter := &metriton.Reporter{
			Application:  "telepresence2",
			Version:      client.Version(),
			GetInstallID: func(_ *metriton.Reporter) (string, error) { return cr.InstallId, nil },
			BaseMetadata: map[string]interface{}{"mode": "daemon"},
		}

		if _, err := reporter.Report(c, map[string]interface{}{"action": "connect"}); err != nil {
			dlog.Errorf(c, "report failed: %+v", err)
		}
		return nil // error is logged and is not fatal
	})

	dlog.Info(c, "Connecting to traffic manager...")
	cluster, err := trackKCluster(s.ctx, cr.Context, cr.Namespace, s.daemon, cr.Args)
	if err != nil {
		dlog.Errorf(c, "unable to track k8s cluster: %+v", err)
		r.Error = rpc.ConnectInfo_CLUSTER_FAILED
		r.ErrorText = err.Error()
		s.cancel()
		return r
	}
	s.cluster = cluster

	/*
		previewHost, err := cluster.getClusterPreviewHostname(p)
		if err != nil {
			p.Logf("get preview URL hostname: %+v", err)
			previewHost = ""
		}
	*/

	dlog.Infof(c, "Connected to context %s (%s)", s.cluster.Context, s.cluster.server())

	tmgr, err := newTrafficManager(s.ctx, s.cluster, cr.InstallId, cr.IsCi)
	if err != nil {
		dlog.Errorf(c, "Unable to connect to TrafficManager: %s", err)
		r.Error = rpc.ConnectInfo_TRAFFIC_MANAGER_FAILED
		r.ErrorText = err.Error()
		// No point in continuing without a traffic manager
		s.cancel()
		return r
	}

	s.trafficMgr = tmgr
	// Wait for traffic manager to connect
	dlog.Info(c, "Waiting for TrafficManager to connect")
	if err := tmgr.waitUntilStarted(); err != nil {
		dlog.Errorf(c, "Failed to start traffic-manager: %v", err)
		r.Error = rpc.ConnectInfo_TRAFFIC_MANAGER_FAILED
		r.ErrorText = err.Error()
		// No point in continuing without a traffic manager
		s.cancel()
		return r
	}

	dlog.Infof(c, "Starting traffic-manager bridge in context %s, namespace %s", cluster.Context, cluster.Namespace)
	br := newBridge(cluster.Namespace, s.daemon, tmgr.sshPort)
	err = br.start(s.ctx)
	if err != nil {
		dlog.Errorf(c, "Failed to start traffic-manager bridge: %v", err)
		r.Error = rpc.ConnectInfo_BRIDGE_FAILED
		r.ErrorText = err.Error()
		// No point in continuing without a bridge
		s.cancel()
		return r
	}

	s.bridge = br
	setStatus()
	return r
}

// setUpLogging connects to the daemon logger
func (s *service) setUpLogging(c context.Context) (context.Context, error) {
	var err error
	s.daemonLogger.stream, err = s.daemon.Logger(c)
	if err != nil {
		return nil, err
	}

	logger := logrus.StandardLogger()
	logger.Out = &s.daemonLogger
	loggingToTerminal := terminal.IsTerminal(int(os.Stdout.Fd()))
	if loggingToTerminal {
		logger.Formatter = client.NewFormatter("15:04:05")
	} else {
		logger.Formatter = client.NewFormatter("2006/01/02 15:04:05")
	}
	logger.Level = logrus.DebugLevel
	return dlog.WithLogger(c, dlog.WrapLogrus(logger)), nil
}

// run is the main function when executing as the connector
func run(init bool) error {
	// establish a connection to the daemon gRPC service
	conn, err := client.DialSocket(client.DaemonSocketName)
	if err != nil {
		return err
	}
	defer conn.Close()
	s := &service{daemon: daemon.NewDaemonClient(conn)}

	c, err := s.setUpLogging(context.Background())
	if err != nil {
		return err
	}

	c = dgroup.WithGoroutineName(c, "connector")

	var cancel context.CancelFunc
	c, cancel = context.WithCancel(c)
	s.cancel = func() {
		dlog.Debug(s.ctx, "cancelling connector context")
		cancel()
	}

	g := dgroup.NewGroup(c, dgroup.GroupConfig{
		SoftShutdownTimeout:  2 * time.Second,
		EnableSignalHandling: true})

	dlog.Info(c, "---")
	dlog.Infof(c, "Telepresence Connector %s starting...", client.DisplayVersion())
	dlog.Infof(c, "PID is %d", os.Getpid())
	dlog.Info(c, "")

	svcStarted := make(chan bool)
	if init {
		g.Go("debug-init", func(c context.Context) error {
			<-svcStarted
			_, _ = s.Connect(c, &rpc.ConnectRequest{InstallId: "dummy-id"})
			return nil
		})
	}

	g.Go("service", func(c context.Context) (err error) {
		var listener net.Listener
		defer func() {
			if perr := dutil.PanicToError(recover()); perr != nil {
				dlog.Error(c, perr)
				if listener != nil {
					_ = listener.Close()
				}
				_ = os.Remove(client.ConnectorSocketName)
			}
			if err != nil {
				dlog.Errorf(c, "Server ended with: %v", err)
			} else {
				dlog.Debug(c, "Server ended")
			}
		}()

		// Listen on unix domain socket
		dlog.Debug(c, "Server starting")
		s.ctx = c
		listener, err = net.Listen("unix", client.ConnectorSocketName)
		if err != nil {
			return err
		}

		svc := grpc.NewServer()
		rpc.RegisterConnectorServer(svc, s)

		go func() {
			<-c.Done()
			svc.GracefulStop()
		}()

		close(svcStarted)
		return svc.Serve(listener)
	})

	g.Go("teardown", s.handleShutdown)

	err = g.Wait()
	if err != nil {
		dlog.Error(c, err)
	}
	return err
}

// handleShutdown ensures that the connector quits gracefully when receiving a signal
// or when the context is cancelled.
func (s *service) handleShutdown(c context.Context) error {
	<-c.Done()
	dlog.Info(c, "Shutting down")

	cluster := s.cluster
	if cluster == nil {
		return nil
	}
	s.cluster = nil

	trafficMgr := s.trafficMgr
	s.trafficMgr = nil
	if trafficMgr != nil {
		_ = trafficMgr.clearIntercepts(context.Background())
		_ = trafficMgr.Close()
	}
	s.bridge = nil
	return nil
}