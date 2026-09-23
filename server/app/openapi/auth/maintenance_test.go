package auth

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

func TestOpenAPIMaintenanceRetentionAndClockFreeze(t *testing.T) {
	gate, db, secret := gatewayFixture(t)
	start := time.Now().UTC().Truncate(time.Second)
	var clock atomic.Int64
	clock.Store(start.Unix())
	gate.admission = NewAdmission(db, func() time.Time { return time.Unix(clock.Load(), 0) })
	old := start.Add(-31 * 24 * time.Hour)
	require.NoError(t, db.Create(&models.Nonce{ClientID: 1, Value: strings.Repeat("a", 32), AcceptedAt: start, ExpiresAt: start.Add(660 * time.Second)}).Error)
	require.NoError(t, db.Create(&[]models.Audit{{RequestID: "old-terminal", Result: "success", CompletedAt: &old}, {RequestID: "old-started", Result: "started", CreatedAt: old}}).Error)
	clock.Store(start.Add(659 * time.Second).Unix())
	require.NoError(t, gate.maintain(context.Background()))
	var count int64
	require.NoError(t, db.Model(&models.Nonce{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, db.Model(&models.Audit{}).Where("result = ?", "started").Count(&count).Error)
	require.EqualValues(t, 1, count, "maintenance must never recover another active request")
	require.NoError(t, db.Model(&models.Audit{}).Where("request_id = ?", "old-terminal").Count(&count).Error)
	require.Zero(t, count)
	clock.Store(start.Add(660 * time.Second).Unix())
	require.NoError(t, gate.maintain(context.Background()))
	require.NoError(t, db.Model(&models.Nonce{}).Count(&count).Error)
	require.Zero(t, count)
	clock.Store(start.Unix())
	require.ErrorIs(t, gate.maintain(context.Background()), ErrFrozen)
	out := gatewayCall(t, gate, secret, strings.Repeat("b", 32), nil)
	require.Equal(t, 503, out.Code, out.Body.String())
}

func TestOpenAPIMaintenanceLoopStopsAndReportsFailure(t *testing.T) {
	gate, db, _ := gatewayFixture(t)
	require.NoError(t, db.Migrator().DropTable(&models.Audit{}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	errors := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		gate.runMaintenance(ctx, ticks, func(err error) { errors <- err })
	}()
	ticks <- time.Now()
	select {
	case err := <-errors:
		require.ErrorIs(t, err, ErrUnavailable)
	case <-time.After(time.Second):
		t.Fatal("cleanup failure was not reported")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("maintenance did not stop on cancellation")
	}
	// Disabled startup does not allocate an immortal maintenance goroutine.
	var disabled *Gateway
	disabled.RunMaintenance(context.Background(), nil)
}
