package gormhelper

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
)

type createHookRow struct {
	ID        uint `gorm:"primaryKey"`
	CreatedBy uint `gorm:"column:created_by"`
}

func TestCreateBeforeHookBatches(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	previous := app.ZapLog
	app.ZapLog = zap.New(core)
	t.Cleanup(func() { app.ZapLog = previous })
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&createHookRow{}))
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:create", CreateBeforeHook))
	claims := &app.Claims{}
	claims.UserID = 42
	ctx := context.WithValue(context.Background(), consts.BindContextKeyName, claims)
	rows := make([]createHookRow, 5)
	require.NoError(t, db.WithContext(ctx).CreateInBatches(&rows, 2).Error)
	require.Zero(t, logs.Len(), "GORM batch slices must not trigger pointer warnings")
	var saved []createHookRow
	require.NoError(t, db.Find(&saved).Error)
	require.Len(t, saved, 5)
	for _, row := range saved {
		require.Equal(t, uint(42), row.CreatedBy)
		require.NotZero(t, row.ID)
	}
}

func TestCreateBeforeHookWarnsForStructValue(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	previous := app.ZapLog
	app.ZapLog = zap.New(core)
	t.Cleanup(func() { app.ZapLog = previous })
	CreateBeforeHook(&gorm.DB{Statement: &gorm.Statement{Dest: createHookRow{}}})
	require.Equal(t, 1, logs.Len())
}
