package play

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/openapi/auth"
)

type runtimeRootDispatcher struct {
	reconcileErr error
	called       int
}

func (*runtimeRootDispatcher) Ready() bool { return true }
func (*runtimeRootDispatcher) Start(context.Context, auth.PlayRequest) (any, error) {
	return nil, nil
}
func (d *runtimeRootDispatcher) ReconcileViewers(context.Context) error {
	d.called++
	return d.reconcileErr
}

func TestRuntimeRootForwardsViewerReconciliation(t *testing.T) {
	root := NewRuntimeRoot()
	require.NoError(t, root.ReconcileViewers(context.Background()))

	dispatcher := &runtimeRootDispatcher{reconcileErr: errors.New("snapshot unavailable")}
	require.NoError(t, root.Replace(dispatcher))
	require.ErrorIs(t, root.ReconcileViewers(context.Background()), dispatcher.reconcileErr)
	require.Equal(t, 1, dispatcher.called)
}
