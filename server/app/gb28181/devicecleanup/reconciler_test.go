package devicecleanup

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/playauth"
)

const gateDeviceCode = "34020000001320000041"

var reconcilerFixtureDBID atomic.Int64

type fixture struct {
	db      *gorm.DB
	devices *playauth.DeviceCleanupStore
	intents *playauth.DeviceOperationIntentStore
	live    *fakeLive
	streams *fakeStreams
}

type fakeLive struct {
	err    error
	onCall func(deviceID string, targetEpoch int64) error
	calls  []string
}

func (f *fakeLive) SettleDeviceBefore(_ context.Context, deviceID string, targetEpoch int64) error {
	if deviceID == "" || targetEpoch <= 0 {
		return errors.New("fixture: invalid live cleanup request")
	}
	f.calls = append(f.calls, fmt.Sprintf("%s@%d", deviceID, targetEpoch))
	if f.onCall != nil {
		if err := f.onCall(deviceID, targetEpoch); err != nil {
			return err
		}
	}
	return f.err
}

type fakeStreams struct {
	list models.GbChannelList
	err  error
}

func (f *fakeStreams) ListPlayingChannels(context.Context) (models.GbChannelList, error) {
	return f.list, f.err
}

func newFixture(t *testing.T, rows ...string) *fixture {
	t.Helper()
	dsn := fmt.Sprintf("file:devicecleanup_%d?mode=memory&cache=shared&_busy_timeout=5000", reconcilerFixtureDBID.Add(1))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&playauth.DeviceOperationIntent{}))
	require.NoError(t, db.Exec(`CREATE TABLE gb_device (
		id INTEGER PRIMARY KEY, device_id TEXT NULL, access_epoch INTEGER NULL,
		cleanup_completed_epoch INTEGER NULL, deleted_at DATETIME NULL)`).Error)
	// rows are "id|device_id|access|completed".
	for _, row := range rows {
		var id, access, completed int64
		var code string
		_, err := fmt.Sscanf(row, "%d|%20s|%d|%d", &id, &code, &access, &completed)
		require.NoError(t, err, "bad fixture row %q", row)
		require.NoError(t, db.Exec(`INSERT INTO gb_device(id, device_id, access_epoch, cleanup_completed_epoch)
			VALUES (?, ?, ?, ?)`, id, code, access, completed).Error)
	}
	return &fixture{
		db:      db,
		devices: playauth.NewDeviceCleanupStore(db),
		intents: playauth.NewDeviceOperationIntentStore(db),
		live:    &fakeLive{},
		streams: &fakeStreams{},
	}
}

func (f *fixture) reconciler(t *testing.T, options ...Option) *Reconciler {
	t.Helper()
	r, err := New(f.devices, f.intents, f.live, f.streams, options...)
	require.NoError(t, err)
	return r
}

func (f *fixture) watermark(t *testing.T, deviceID string) int64 {
	t.Helper()
	state, err := f.devices.Load(context.Background(), deviceID)
	require.NoError(t, err)
	return state.CleanupCompletedEpoch
}

func (f *fixture) insertOlderIntent(t *testing.T, id string, epoch int64, state string) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	version := int64(1)
	var dispatchAt, cancelledAt any
	switch state {
	case playauth.IntentDispatched:
		version, dispatchAt = 2, now
	case playauth.IntentCancelled:
		version, cancelledAt = 2, now
	}
	require.NoError(t, f.db.Exec(`INSERT INTO gb_device_operation_intent
		(operation_id, device_pk, device_code, target_scope, target_pk, target_code,
		 contract_version, device_epoch, kind, state, row_version, created_at, updated_at,
		 dispatch_started_at, cancelled_at)
		VALUES (?, 1, ?, 'device', 1, ?, 1, ?, 'live', ?, ?, ?, ?, ?, ?)`,
		id, gateDeviceCode, gateDeviceCode, epoch, state, version, now, now, dispatchAt, cancelledAt).Error)
}

// A transferred device must not stay gated forever. Before this reconciler
// existed nothing advanced cleanup_completed_epoch, so the fail-closed media
// gate refused every play for the device until an operator edited the database.
func TestReconcilerOpensGateForTransferredDevice(t *testing.T) {
	f := newFixture(t, "1|"+gateDeviceCode+"|2|1")
	stats, err := f.reconciler(t).Tick(context.Background())
	require.NoError(t, err)
	require.Equal(t, Stats{Examined: 1, Completed: 1}, stats)
	require.EqualValues(t, 2, f.watermark(t, gateDeviceCode))
	require.Equal(t, []string{gateDeviceCode + "@2"}, f.live.calls, "the live drain targets the transferred epoch")
}

func TestReconcilerKeepsGateClosedWhileDurableWorkIsUnsettled(t *testing.T) {
	for _, state := range []string{playauth.IntentReserved, playauth.IntentDispatched} {
		t.Run(state, func(t *testing.T) {
			f := newFixture(t, "1|"+gateDeviceCode+"|2|1")
			f.insertOlderIntent(t, fmt.Sprintf("%032d", 1), 1, state)
			stats, err := f.reconciler(t).Tick(context.Background())
			require.NoError(t, err)
			require.Equal(t, Stats{Examined: 1, Pending: 1}, stats)
			require.EqualValues(t, 1, f.watermark(t, gateDeviceCode))
		})
	}
}

func TestReconcilerKeepsGateClosedWhenLiveMediaCannotSettle(t *testing.T) {
	f := newFixture(t, "1|"+gateDeviceCode+"|2|1")
	f.live.err = fmt.Errorf("%w: fixture still stopping", playauth.ErrDeviceCleanupPending)
	stats, err := f.reconciler(t).Tick(context.Background())
	require.NoError(t, err)
	require.Equal(t, Stats{Examined: 1, Pending: 1}, stats)
	require.EqualValues(t, 1, f.watermark(t, gateDeviceCode))
}

// A process-local drain cannot speak for a session another instance still owns,
// so the durable stream projection has to agree before the gate opens.
func TestReconcilerKeepsGateClosedWhileStreamProjectionListsDevice(t *testing.T) {
	f := newFixture(t, "1|"+gateDeviceCode+"|2|1")
	f.streams.list = models.GbChannelList{&models.GbChannel{DeviceID: gateDeviceCode, ChannelID: "34020000001310000099"}}
	stats, err := f.reconciler(t).Tick(context.Background())
	require.NoError(t, err)
	require.Equal(t, Stats{Examined: 1, Pending: 1}, stats)
	require.EqualValues(t, 1, f.watermark(t, gateDeviceCode))
}

// Evidence gathered before a long drain must not be reused after it: a transfer
// or a late intent that lands during the drain keeps the device gated.
func TestReconcilerRechecksDurableEvidenceAfterTheDrain(t *testing.T) {
	f := newFixture(t, "1|"+gateDeviceCode+"|2|1")
	f.live.onCall = func(string, int64) error {
		f.insertOlderIntent(t, fmt.Sprintf("%032d", 2), 1, playauth.IntentReserved)
		return nil
	}
	stats, err := f.reconciler(t).Tick(context.Background())
	require.NoError(t, err)
	require.Equal(t, Stats{Examined: 1, Pending: 1}, stats)
	require.EqualValues(t, 1, f.watermark(t, gateDeviceCode))
}

func TestReconcilerYieldsToANewerTransfer(t *testing.T) {
	f := newFixture(t, "1|"+gateDeviceCode+"|2|1")
	f.live.onCall = func(string, int64) error {
		// The operator reassigns the device while its old epoch is draining.
		return f.db.Exec(`UPDATE gb_device SET access_epoch = 3 WHERE id = 1`).Error
	}
	stats, err := f.reconciler(t).Tick(context.Background())
	require.NoError(t, err, "losing the race to a newer transfer is not a failure")
	require.Equal(t, Stats{Examined: 1, Pending: 1}, stats)
	require.EqualValues(t, 1, f.watermark(t, gateDeviceCode), "the newer epoch must not be completed by the older pass")
	require.EqualValues(t, 3, f.loadAccessEpoch(t))
}

func TestReconcilerReportsDependencyFailureWithoutStarvingThePage(t *testing.T) {
	second := "34020000001320000042"
	f := newFixture(t, "1|"+gateDeviceCode+"|2|1", "2|"+second+"|2|1")
	f.live.onCall = func(deviceID string, _ int64) error {
		if deviceID == gateDeviceCode {
			return errors.New("fixture: media authority unreachable")
		}
		return nil
	}
	stats, err := f.reconciler(t).Tick(context.Background())
	require.Error(t, err, "a real dependency failure is reported, not masked as pending")
	require.Equal(t, Stats{Examined: 2, Completed: 1, Failed: 1}, stats)
	require.EqualValues(t, 1, f.watermark(t, gateDeviceCode))
	require.EqualValues(t, 2, f.watermark(t, second))
}

func TestReconcilerIgnoresAlreadyCompletedDevices(t *testing.T) {
	f := newFixture(t, "1|"+gateDeviceCode+"|2|2")
	stats, err := f.reconciler(t).Tick(context.Background())
	require.NoError(t, err)
	require.Equal(t, Stats{}, stats)
	require.Empty(t, f.live.calls)
}

func TestReconcilerFailsClosedWithoutEveryEvidenceSource(t *testing.T) {
	f := newFixture(t, "1|"+gateDeviceCode+"|2|1")
	for name, build := range map[string]func() (*Reconciler, error){
		"no devices": func() (*Reconciler, error) { return New(nil, f.intents, f.live, f.streams) },
		"no intents": func() (*Reconciler, error) { return New(f.devices, nil, f.live, f.streams) },
		"no live":    func() (*Reconciler, error) { return New(f.devices, f.intents, nil, f.streams) },
		"nil live":   func() (*Reconciler, error) { return New(f.devices, f.intents, (*fakeLive)(nil), f.streams) },
		"no streams": func() (*Reconciler, error) { return New(f.devices, f.intents, f.live, nil) },
	} {
		t.Run(name, func(t *testing.T) {
			reconciler, err := build()
			require.ErrorIs(t, err, ErrReconcilerUnavailable)
			require.Nil(t, reconciler)
		})
	}
	require.EqualValues(t, 1, f.watermark(t, gateDeviceCode))
}

func TestReconcilerTicksUntilStopped(t *testing.T) {
	f := newFixture(t, "1|"+gateDeviceCode+"|2|1")
	passed := make(chan Stats, 4)
	r := f.reconciler(t, WithInterval(time.Millisecond), WithReport(func(stats Stats, err error) {
		require.NoError(t, err)
		passed <- stats
	}))
	require.NoError(t, r.Start(context.Background()))
	select {
	case stats := <-passed:
		require.Equal(t, Stats{Examined: 1, Completed: 1}, stats)
	case <-time.After(5 * time.Second):
		t.Fatal("reconciler never reported a pass")
	}
	require.NoError(t, r.Stop(context.Background()))
	require.NoError(t, r.Stop(context.Background()), "stopping twice joins the same loop")
	require.EqualValues(t, 2, f.watermark(t, gateDeviceCode))
}

func TestReconcilerRefusesASecondStart(t *testing.T) {
	f := newFixture(t, "1|"+gateDeviceCode+"|2|2")
	r := f.reconciler(t, WithInterval(time.Hour))
	require.NoError(t, r.Start(context.Background()))
	t.Cleanup(func() { _ = r.Stop(context.Background()) })
	require.ErrorIs(t, r.Start(context.Background()), ErrReconcilerUnavailable)
}

func (f *fixture) loadAccessEpoch(t *testing.T) int64 {
	t.Helper()
	var state struct{ AccessEpoch int64 }
	require.NoError(t, f.db.Table("gb_device").Select("access_epoch").Where("id = 1").Take(&state).Error)
	return state.AccessEpoch
}
