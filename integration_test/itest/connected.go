package itest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type connected struct {
	NamespacePair
}

func WithConnection(np NamespacePair, f func(ctx context.Context, ch NamespacePair)) {
	np.HarnessT().Run("Test_Connected", func(t *testing.T) {
		ctx := withT(np.HarnessContext(), t)
		require.NoError(t, np.GeneralError())
		ch := &connected{NamespacePair: np}
		ch.PushHarness(ctx, ch.setup, nil)
		defer ch.PopHarness()
		f(ctx, ch)
	})
}

func (ch *connected) setup(ctx context.Context) bool {
	t := getT(ctx)
	TelepresenceQuitOk(ctx) //nolint:dogsled
	err := ch.TelepresenceHelmInstall(ctx, false, nil)
	assert.NoError(t, err)
	if t.Failed() {
		return false
	}

	// Connect using telepresence-test-developer user
	stdout, _, err := Telepresence(ctx, "connect", "--manager-namespace", ch.ManagerNamespace())
	assert.NoError(t, err)
	assert.Contains(t, stdout, "Connected to context default")
	if t.Failed() {
		return false
	}
	stdout, _, err = Telepresence(ctx, "loglevel", "-d30m", "debug")
	assert.NoError(t, err)
	if t.Failed() {
		return false
	}
	ch.CapturePodLogs(ctx, "app=traffic-manager", "", ch.ManagerNamespace())
	return true
}
