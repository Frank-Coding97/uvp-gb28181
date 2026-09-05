package audit

import (
	"context"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"testing"
	"time"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

func TestOpenAPIAuditOutcomesAndRetention(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, _ := db.DB()
	raw.SetMaxOpenConns(1)
	defer raw.Close()
	require.NoError(t, db.AutoMigrate(&models.Audit{}))
	now := time.Unix(1790000000, 0)
	store := New(db, func() time.Time { return now })
	ctx := context.Background()
	for _, id := range []string{"done", "interrupted", "old"} {
		created := now
		if id == "old" {
			created = now.Add(-31 * 24 * time.Hour)
		}
		require.NoError(t, db.Create(&models.Audit{RequestID: id, Result: "started", CreatedAt: created}).Error)
	}
	require.NoError(t, store.Complete(ctx, "done", "OK", 20*time.Millisecond))
	require.Error(t, store.Complete(ctx, "done", "OK", time.Millisecond))
	require.Error(t, store.Complete(ctx, "interrupted", "a raw secret must never become a reason", time.Second))
	var row models.Audit
	require.NoError(t, db.Where("request_id = ?", "interrupted").First(&row).Error)
	require.Equal(t, "started", row.Result)
	n, err := store.Cleanup(ctx)
	require.NoError(t, err)
	require.Zero(t, n, "never delete started even if old")
	require.NoError(t, store.RecoverInterrupted(ctx))
	row = models.Audit{}
	require.NoError(t, db.Where("request_id = ?", "interrupted").First(&row).Error)
	require.Equal(t, "outcome_unknown", row.Result)
	// Unknown results retain 30 days after completion/recovery, not old start time.
	n, err = store.Cleanup(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
	now = now.Add(30*24*time.Hour + time.Second)
	n, err = store.Cleanup(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 3, n)
}
