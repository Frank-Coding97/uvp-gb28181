package ptz

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/openapi/auth"
)

type rootDispatcher struct {
	ready bool
}

func (d rootDispatcher) Ready() bool { return d.ready }

func (d rootDispatcher) Handle(context.Context, auth.PTZRequest) (any, error) {
	return map[string]string{"generation": "new"}, nil
}

func TestRuntimeRootReplaceAndClearFailClosed(t *testing.T) {
	root := NewRuntimeRoot()
	require.False(t, root.Ready())
	require.ErrorIs(t, root.Replace(rootDispatcher{}), auth.ErrPTZUnavailable)
	require.NoError(t, root.Replace(rootDispatcher{ready: true}))
	require.True(t, root.Ready())
	data, err := root.Handle(context.Background(), auth.PTZRequest{Scope: auth.PTZOperationReadScope})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"generation": "new"}, data)
	root.Clear()
	require.False(t, root.Ready())
	_, err = root.Handle(context.Background(), auth.PTZRequest{Scope: auth.PTZOperationReadScope})
	require.ErrorIs(t, err, auth.ErrPTZUnavailable)
}
