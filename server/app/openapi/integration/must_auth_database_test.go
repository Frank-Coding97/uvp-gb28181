package integration

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	openapiconfig "uvplatform.cn/uvp-gb28181/app/openapi/config"
	"gorm.io/gorm"
)

func checkNativeMustAuthLatch(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(db.Statement.Context, 15*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	var failures atomic.Int32
	for i := 0; i < 11; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			state, err := openapiconfig.NewMustAuthStore(db, time.Now).Latch(ctx)
			if err != nil || !state.MustAuthLocked || state.LockVersion != 1 {
				failures.Add(1)
			}
		}()
	}
	wg.Wait()
	if failures.Load() != 0 {
		t.Fatalf("concurrent must-auth latch had %d failures", failures.Load())
	}
	state, err := openapiconfig.NewMustAuthStore(db, time.Now).Load(ctx)
	if err != nil {
		t.Fatalf("reloaded must-auth state: %v", err)
	}
	if !state.MustAuthLocked || state.LockVersion != 1 {
		t.Fatalf("unexpected latched state: locked=%t version=%d", state.MustAuthLocked, state.LockVersion)
	}
	t.Log("native must-auth latch: concurrent requests preserve exactly one locked row/version1; new store restores state")
}
