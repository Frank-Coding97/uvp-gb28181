package processauthority

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestProcessAuthorityConcurrentFenceDoesNotInvertTransactionLocks(t *testing.T) {
	db, lock := authorityDB(t), authorityLock(t)
	var root registrationLatch
	authority, err := root.register(context.Background(), db, lock)
	require.NoError(t, err)
	first := db.Begin()
	require.NoError(t, first.Error)
	defer first.Rollback()
	require.NoError(t, authority.CheckTx(first))
	entered, release := make(chan struct{}), make(chan struct{})
	var intercepted atomic.Bool
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("authority-test:hold-second", func(tx *gorm.DB) {
		if intercepted.CompareAndSwap(false, true) {
			close(entered)
			<-release
			tx.AddError(context.Canceled)
		}
	}))
	defer db.Callback().Update().Remove("authority-test:hold-second")
	second := make(chan error, 1)
	go func() { second <- db.Transaction(func(tx *gorm.DB) error { return authority.CheckTx(tx) }) }()
	<-entered
	recheck := make(chan error, 1)
	go func() { recheck <- authority.CheckTx(first) }()
	progressed := false
	select {
	case err := <-recheck:
		require.NoError(t, err)
		progressed = true
	case <-time.After(500 * time.Millisecond):
	}
	close(release)
	require.Error(t, <-second)
	if !progressed {
		require.NoError(t, <-recheck)
	}
	require.True(t, progressed, "a competing transaction cannot hold a Go mutex needed by the SQL row-lock owner")
}
