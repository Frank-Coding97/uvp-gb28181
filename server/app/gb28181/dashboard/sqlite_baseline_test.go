package dashboard

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/internal/sqlitebootstrap"
)

func newDashboardSQLiteBaselineDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gormhelper.NewSQLiteClient(filepath.Join(t.TempDir(), "dashboard.db"))
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	_, err = sqlitebootstrap.Initialize(context.Background(), db)
	require.NoError(t, err)
	return db
}

func TestLayoutServiceSQLiteBaselineEmptyAndRevisionFlow(t *testing.T) {
	db := newDashboardSQLiteBaselineDB(t)
	service := NewLayoutService(db)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	empty, err := service.Get(ctx, 9901)
	require.NoError(t, err)
	require.Equal(t, DefaultLayout(), empty.Layout)

	saved, err := service.Save(ctx, 9901, 0, DefaultLayout())
	require.NoError(t, err)
	require.EqualValues(t, 1, saved.Revision)
	loaded, err := service.Get(ctx, 9901)
	require.NoError(t, err)
	require.EqualValues(t, 1, loaded.Revision)
	require.Equal(t, saved.Layout, loaded.Layout)
	_, err = service.Save(ctx, 9901, 1, DefaultLayout())
	require.NoError(t, err)
	_, err = service.Save(ctx, 9901, 1, DefaultLayout())
	require.ErrorIs(t, err, ErrLayoutRevisionConflict)
}
