package itest

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type HelmAndService interface {
	SingleService
}

type helmAndService struct {
	SingleService
}

func WithHelmAndService(h SingleService, f func(HelmAndService)) {
	h.HarnessT().Run("Test_Helm", func(t *testing.T) {
		ctx := withT(h.HarnessContext(), t)
		s := &helmAndService{SingleService: h}
		s.PushHarness(ctx, s.setup, s.tearDown)
		defer h.PopHarness()
		f(s)
	})
}

func (h *helmAndService) setup(ctx context.Context) bool {
	t := getT(ctx)
	TelepresenceQuitOk(ctx)

	// Destroy the telepresence-test-developer binding so that we actually test the RBAC set up in the helm chart
	require.NoError(t, Kubectl(ctx, "", "delete", "rolebinding", "telepresence-test-developer"))
	require.NoError(t, h.InstallTrafficManager(ctx, nil, h.ManagerNamespace(), h.AppNamespace()))

	stdout := TelepresenceOk(ctx, "connect")
	require.Contains(t, stdout, "Connected to context")
	return true
}

func (h *helmAndService) tearDown(ctx context.Context) {
	TelepresenceQuitOk(ctx)
	h.UninstallTrafficManager(ctx, h.ManagerNamespace())

	// Helm uninstall does deletions asynchronously, which means the rbac might not be cleaned
	// up immediately.
	// Helm currently has no method to wait for deletions to be finished before returning, but
	// I think this should be sufficient since rbac is cleaned up super quickly.
	t := getT(ctx)
	assert.Eventually(t, func() bool {
		return Kubectl(ctx, "", "--as", TestUserAccount, "get", "namespaces") != nil
	}, 20*time.Second, 2*time.Second, "User still has permissions to get namespaces")

	// Restore the rbac we blew up in the setup
	ctx = WithWorkingDir(ctx, filepath.Join(GetOSSRoot(ctx), "integration_test"))
	require.NoError(t, Kubectl(ctx, "", "apply", "-f", filepath.Join("testdata", "k8s", "client_rbac.yaml")))
	require.NoError(t, Run(ctx, "kubectl", "label", "rolebinding", TestUser, "purpose="+purposeLabel))
}
