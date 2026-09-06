package client

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

func TestOpenAPIAdminScopeBatchAtomicValidation(t *testing.T) {
	svc, _ := newClientTestService(t, &recordingRevocationStore{})
	ctx := context.Background()
	view, _, err := svc.Create(ctx, CreateRequest{Name: "batch", OwnerDeptID: 10, CreatedBy: 7})
	require.NoError(t, err)
	for _, scopes := range [][]string{{"device:list", "unknown"}, {"device:list", "device:list"}} {
		_, err = svc.SetScopes(ctx, view.ID, scopes, view.RowVersion, 7)
		require.Error(t, err)
		got, getErr := svc.Get(ctx, view.ID)
		require.NoError(t, getErr)
		require.Equal(t, view.RowVersion, got.RowVersion)
		current, scopeErr := svc.ListScopes(ctx, view.ID)
		require.NoError(t, scopeErr)
		require.Empty(t, current)
	}
}

func TestOpenAPIAdminScopeBatchRollbackAndEpochIsolation(t *testing.T) {
	store := &recordingRevocationStore{}
	svc, db := newClientTestService(t, store)
	ctx := context.Background()
	view, _, err := svc.Create(ctx, CreateRequest{Name: "batch", OwnerDeptID: 10, CreatedBy: 7})
	require.NoError(t, err)
	view, err = svc.SetScopes(ctx, view.ID, []string{"device:list", "play:live:apply"}, view.RowVersion, 7)
	require.NoError(t, err)
	original := view
	store.err = errors.New("intent unavailable")
	_, err = svc.SetScopes(ctx, view.ID, []string{"channel:list"}, view.RowVersion, 7)
	require.Error(t, err)
	got, err := svc.Get(ctx, view.ID)
	require.NoError(t, err)
	require.Equal(t, original.RowVersion, got.RowVersion)
	var rows []models.ClientScope
	require.NoError(t, db.Where("client_id = ?", view.ID).Find(&rows).Error)
	require.Len(t, rows, 2)
	for _, row := range rows {
		require.True(t, row.Enabled)
	}
	store.err = nil
	view, err = svc.SetScopes(ctx, view.ID, []string{"device:list"}, view.RowVersion, 7)
	require.NoError(t, err)
	require.Equal(t, original.AuthEpoch, view.AuthEpoch)
	query, err := svc.GetScope(ctx, view.ID, "device:list")
	require.NoError(t, err)
	require.EqualValues(t, 1, query.ScopeEpoch)
	play, err := svc.GetScope(ctx, view.ID, "play:live:apply")
	require.NoError(t, err)
	require.False(t, play.Enabled)
	require.EqualValues(t, 2, play.ScopeEpoch)
	_, err = svc.SetScopes(ctx, view.ID, []string{"device:list"}, original.RowVersion, 7)
	require.ErrorIs(t, err, ErrConflict)
	_, err = svc.SetScopes(ctx, view.ID, nil, view.RowVersion, 0)
	require.ErrorIs(t, err, ErrAuthorizationUnavailable)
}
