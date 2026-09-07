package dashboard

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func newLayoutTestService(t *testing.T) (*LayoutService, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDashboardLayout{}))
	return NewLayoutService(db), db
}

func TestLayoutServiceDefaultsIsolationCASAndReset(t *testing.T) {
	service, db := newLayoutTestService(t)
	ctx := context.Background()

	initial, err := service.Get(ctx, 7)
	require.NoError(t, err)
	require.Zero(t, initial.Revision)
	require.Len(t, initial.Layout.Widgets, 12)

	aLayout := initial.Layout
	aLayout.Widgets[0].X = 3
	savedA, err := service.Save(ctx, 7, 0, aLayout)
	require.NoError(t, err)
	require.EqualValues(t, 1, savedA.Revision)

	bLayout := initial.Layout
	bLayout.Widgets[0].Visible = false
	savedB, err := service.Save(ctx, 8, 0, bLayout)
	require.NoError(t, err)
	require.EqualValues(t, 1, savedB.Revision)

	loadedA, err := service.Get(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, 3, loadedA.Layout.Widgets[0].X)
	loadedB, err := service.Get(ctx, 8)
	require.NoError(t, err)
	require.False(t, loadedB.Layout.Widgets[0].Visible)

	_, err = service.Save(ctx, 7, 0, initial.Layout)
	require.ErrorIs(t, err, ErrLayoutRevisionConflict)

	reset, err := service.Reset(ctx, 7)
	require.NoError(t, err)
	require.Zero(t, reset.Revision)
	require.Equal(t, DefaultLayout(), reset.Layout)
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbDashboardLayout{}).Where("user_id = ?", 7).Count(&count).Error)
	require.Zero(t, count)
}

func TestLayoutServiceRejectsInvalidLayoutWithoutChangingStoredRevision(t *testing.T) {
	service, _ := newLayoutTestService(t)
	ctx := context.Background()
	valid, err := service.Save(ctx, 7, 0, DefaultLayout())
	require.NoError(t, err)

	invalid := valid.Layout
	invalid.Widgets[0].ID = "unknown"
	_, err = service.Save(ctx, 7, valid.Revision, invalid)
	require.Error(t, err)

	loaded, err := service.Get(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, valid.Revision, loaded.Revision)
}
