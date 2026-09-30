package client

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	appmodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

func TestOpenAPIAdminScopeBatchAtomicValidation(t *testing.T) {
	svc, _ := newClientTestService(t)
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

func TestOpenAPIAdminScopeBatchAndEpochIsolation(t *testing.T) {
	svc, db := newClientTestService(t)
	ctx := context.Background()
	view, _, err := svc.Create(ctx, CreateRequest{Name: "batch", OwnerDeptID: 10, CreatedBy: 7})
	require.NoError(t, err)
	view, err = svc.SetScopes(ctx, view.ID, []string{"device:list", "channel:list"}, view.RowVersion, 7)
	require.NoError(t, err)
	original := view
	_, err = svc.SetScopes(ctx, view.ID, []string{"channel:list"}, view.RowVersion, 7)
	require.NoError(t, err)
	got, err := svc.Get(ctx, view.ID)
	require.NoError(t, err)
	require.Equal(t, original.RowVersion+1, got.RowVersion)
	var rows []models.ClientScope
	require.NoError(t, db.Where("client_id = ?", view.ID).Find(&rows).Error)
	require.Len(t, rows, 2)
	for _, row := range rows {
		if row.Scope == "channel:list" {
			require.True(t, row.Enabled)
		} else {
			require.False(t, row.Enabled)
		}
	}
	view = got
	view, err = svc.SetScopes(ctx, view.ID, []string{"device:list"}, view.RowVersion, 7)
	require.NoError(t, err)
	require.Equal(t, original.AuthEpoch, view.AuthEpoch)
	query, err := svc.GetScope(ctx, view.ID, "device:list")
	require.NoError(t, err)
	require.EqualValues(t, 3, query.ScopeEpoch)
	play, err := svc.GetScope(ctx, view.ID, "channel:list")
	require.NoError(t, err)
	require.False(t, play.Enabled)
	require.EqualValues(t, 2, play.ScopeEpoch)
	_, err = svc.SetScopes(ctx, view.ID, []string{"device:list"}, original.RowVersion, 7)
	require.ErrorIs(t, err, ErrConflict)
	_, err = svc.SetScopes(ctx, view.ID, nil, view.RowVersion, 0)
	require.ErrorIs(t, err, ErrAuthorizationUnavailable)
}

var scopeBatchTestDatabaseID int64

func newClientTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	scopeBatchTestDatabaseID++
	dsn := fmt.Sprintf("file:openapi_scope_batch_%d?mode=memory&cache=shared", scopeBatchTestDatabaseID)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Client{}, &models.ClientScope{}, &models.Audit{}, &appmodels.SysOperationLog{}))
	secrets, err := NewSecretManager(bytes.Repeat([]byte{0xA5}, 32), "scope-batch-test")
	require.NoError(t, err)
	service, err := NewService(db, secrets,
		WithClock(func() time.Time { return time.Unix(1790000000, 0).UTC() }),
		WithManagementBoundary(scopeBatchAllowAllBoundary{}),
	)
	require.NoError(t, err)
	return service, db
}

type scopeBatchAllowAllBoundary struct{}

func (scopeBatchAllowAllBoundary) AuthorizeCreate(context.Context, uint, uint) error { return nil }
func (scopeBatchAllowAllBoundary) AuthorizeClient(context.Context, uint, string, uint) error {
	return nil
}
