package itest

import (
	"context"
	"testing"

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
		s.PushHarness(ctx, s.setup, nil)
		defer h.PopHarness()
		f(s)
	})
}

func (h *helmAndService) setup(ctx context.Context) bool {
	t := getT(ctx)
	TelepresenceQuitOk(ctx)
	require.NoError(t, h.TelepresenceHelmInstall(ctx, false, nil))

	stdout := TelepresenceOk(ctx, "connect", "--manager-namespace", h.ManagerNamespace())
	require.Contains(t, stdout, "Connected to context")
	return true
}
