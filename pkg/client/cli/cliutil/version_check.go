package cliutil

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	empty "google.golang.org/protobuf/types/known/emptypb"

	"github.com/telepresenceio/telepresence/rpc/v2/common"
	"github.com/telepresenceio/telepresence/v2/pkg/client/errcat"
	"github.com/telepresenceio/telepresence/v2/pkg/version"
)

type daemonClient interface {
	Version(context.Context, *empty.Empty, ...grpc.CallOption) (*common.VersionInfo, error)
}

func versionCheck(ctx context.Context, daemonType string, daemonBinary string, configuredDaemon bool, daemon daemonClient) error {
	if IsQuitting(ctx) {
		return nil
	}
	var quitFlag string
	switch {
	case daemonType == "Root":
		quitFlag = "-r"
	case daemonType == "User":
		quitFlag = "-u"
	default:
		return fmt.Errorf("unknown daemonType: %s", daemonType)
	}
	// Ensure that the already running daemon has the correct version
	vi, err := daemon.Version(ctx, &empty.Empty{})
	if err != nil {
		return fmt.Errorf("unable to retrieve version of %s Daemon: %w", daemonType, err)
	}
	if version.Version != vi.Version {
		// OSS Version mismatch. We never allow this
		if !configuredDaemon {
			return errcat.User.Newf("version mismatch. Client %s != %s Daemon %s, please run 'telepresence quit %s' and reconnect",
				version.Version, daemonType, vi.Version, quitFlag)
		}
		return GetTelepresencePro(ctx)
	}
	if daemonBinary != "" {
		if vi.Executable != daemonBinary {
			return errcat.User.Newf("executable mismatch. Connector using %s, configured to use %s, please run 'telepresence quit %s' and reconnect",
				vi.Executable, daemonBinary, quitFlag)
		}
	}
	return nil
}
