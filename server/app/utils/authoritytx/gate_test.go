package authoritytx

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGateSerializesIndependentConnectionsUntilCommit(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "authority.db") + "?_pragma=busy_timeout(3000)"
	first, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	second, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	for _, db := range []*gorm.DB{first, second} {
		raw, e := db.DB()
		require.NoError(t, e)
		t.Cleanup(func() { _ = raw.Close() })
	}
	require.NoError(t, first.AutoMigrate(&Gate{}))
	require.NoError(t, first.Create(&Gate{ID: 1}).Error)
	tx := first.Begin()
	require.NoError(t, Acquire(context.Background(), tx))
	entered, acquired := make(chan struct{}), make(chan error, 1)
	go func() {
		acquired <- second.Transaction(func(other *gorm.DB) error { close(entered); return Acquire(context.Background(), other) })
	}()
	<-entered
	select {
	case e := <-acquired:
		t.Fatalf("second authority change passed uncommitted gate: %v", e)
	case <-time.After(50 * time.Millisecond):
	}
	require.NoError(t, tx.Commit().Error)
	select {
	case e := <-acquired:
		require.NoError(t, e)
	case <-time.After(2 * time.Second):
		t.Fatal("gate was not released on commit")
	}
	var gate Gate
	require.NoError(t, first.Take(&gate, 1).Error)
	require.EqualValues(t, 2, gate.Revision)
}

func TestGateFailsClosedAndRollsBack(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	defer raw.Close()
	require.NoError(t, db.AutoMigrate(&Gate{}))
	require.Error(t, Acquire(context.Background(), db), "autocommit cannot protect later queries")
	require.Error(t, db.Transaction(func(tx *gorm.DB) error { return Acquire(context.Background(), tx) }), "missing singleton must not silently authorize")
	require.NoError(t, db.Create(&Gate{ID: 1}).Error)
	failed := errors.New("authority write failed")
	require.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error { require.NoError(t, Acquire(context.Background(), tx)); return failed }), failed)
	var gate Gate
	require.NoError(t, db.Take(&gate, 1).Error)
	require.Zero(t, gate.Revision)
}
