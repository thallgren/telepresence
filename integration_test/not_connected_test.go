package integration_test

import (
	"runtime"

	"github.com/stretchr/testify/suite"

	"github.com/telepresenceio/telepresence/v2/integration_test/itest"
	"github.com/telepresenceio/telepresence/v2/pkg/client"
)

type notConnectedSuite struct {
	itest.Suite
	itest.NamespacePair
}

func init() {
	itest.AddNamespacePairSuite("", func(h itest.NamespacePair) suite.TestingSuite {
		return &notConnectedSuite{Suite: itest.Suite{Harness: h}, NamespacePair: h}
	})
}

func (s *notConnectedSuite) SetupSuite() {
	s.Suite.SetupSuite()
	ctx := s.Context()
	require := s.Require()
	require.NoError(s.TelepresenceHelmInstall(ctx, false, nil))
	stdout := itest.TelepresenceOk(ctx, "connect", "--manager-namespace", s.ManagerNamespace())
	s.Contains(stdout, "Connected to context")
	s.CapturePodLogs(ctx, "app=traffic-manager", "", s.ManagerNamespace())
	itest.TelepresenceDisconnectOk(ctx)
}

func (s *notConnectedSuite) TearDownSuite() {
	ctx := itest.WithUser(s.Context(), "default")
	itest.TelepresenceOk(ctx, "helm", "uninstall")
}

func (s *notConnectedSuite) Test_ConnectWithCommand() {
	ctx := s.Context()
	stdout := itest.TelepresenceOk(ctx, "connect", "--manager-namespace", s.ManagerNamespace(), "--", s.Executable(), "status")
	s.Contains(stdout, "Connected to context")
	s.Contains(stdout, "Kubernetes context:")
	itest.TelepresenceDisconnectOk(ctx)
}

func (s *notConnectedSuite) Test_InvalidKubeconfig() {
	ctx := s.Context()
	itest.TelepresenceOk(ctx, "quit", "-s")
	path := "/dev/null"
	if runtime.GOOS == "windows" {
		path = "C:\\NUL"
	}
	badEnvCtx := itest.WithEnv(ctx, map[string]string{"KUBECONFIG": path})
	_, stderr, err := itest.Telepresence(badEnvCtx, "connect")
	s.Contains(stderr, "kubeconfig has no context definition")
	itest.TelepresenceQuitOk(ctx) // process is started with bad env, so get rid of it
	s.Error(err)
}

func (s *notConnectedSuite) Test_NonExistentContext() {
	ctx := s.Context()

	_, stderr, err := itest.Telepresence(ctx, "connect", "--context", "not-likely-to-exist")
	s.Error(err)
	s.Contains(stderr, `"not-likely-to-exist" does not exist`)
	itest.TelepresenceDisconnectOk(ctx)
}

func (s *notConnectedSuite) Test_ConnectingToOtherNamespace() {
	ctx := s.Context()

	suffix := itest.GetGlobalHarness(s.HarnessContext()).Suffix()
	appSpace2, mgrSpace2 := itest.AppAndMgrNSName(suffix + "-2")
	itest.CreateNamespaces(ctx, appSpace2, mgrSpace2)
	defer itest.DeleteNamespaces(ctx, appSpace2, mgrSpace2)

	s.Run("Installs Successfully", func() {
		ctx := itest.WithNamespaces(s.Context(), &itest.Namespaces{
			Namespace:         mgrSpace2,
			ManagedNamespaces: []string{appSpace2},
		})
		s.NoError(s.TelepresenceHelmInstall(ctx, false, nil))
	})

	s.Run("Can be connected to with --manager-namespace-flag", func() {
		ctx := s.Context()
		itest.TelepresenceQuitOk(ctx)

		// Set the config to some nonsense to verify that the flag wins
		ctx = itest.WithConfig(ctx, func(cfg *client.Config) {
			cfg.Cluster.DefaultManagerNamespace = "daffy-duck"
		})
		ctx = itest.WithUser(ctx, mgrSpace2+":"+itest.TestUser)
		stdout := itest.TelepresenceOk(ctx, "connect", "--manager-namespace="+mgrSpace2)
		s.Contains(stdout, "Connected to context")
		stdout = itest.TelepresenceOk(ctx, "status")
		s.Regexp(`Manager namespace\s+: `+mgrSpace2, stdout)
	})

	s.Run("Can be connected to with defaultManagerNamespace config", func() {
		ctx := s.Context()
		itest.TelepresenceQuitOk(ctx)
		ctx = itest.WithConfig(ctx, func(cfg *client.Config) {
			cfg.Cluster.DefaultManagerNamespace = mgrSpace2
		})
		stdout := itest.TelepresenceOk(itest.WithUser(ctx, "default"), "connect")
		s.Contains(stdout, "Connected to context")
		stdout = itest.TelepresenceOk(ctx, "status")
		s.Regexp(`Manager namespace\s+: `+mgrSpace2, stdout)
	})

	s.Run("Uninstalls Successfully", func() {
		s.UninstallTrafficManager(s.Context(), mgrSpace2)
	})
}
