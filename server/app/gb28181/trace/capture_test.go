package trace

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func newCaptureTestService(t *testing.T, now *time.Time, allowed bool) (*CaptureService, *gorm.DB, *gbmodels.GbDevice) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbSIPTraceCapture{}))
	device := &gbmodels.GbDevice{DeviceID: "34020000001320000001", Name: "test", OwnerDeptID: 10}
	require.NoError(t, db.Create(device).Error)
	service := NewCaptureService(db, CaptureAuthorizerFunc(func(context.Context, uint, *gbmodels.GbDevice) bool {
		return allowed
	}), func() time.Time { return *now })
	return service, db, device
}

func TestCaptureModelMigrationAndDefaultWindow(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	service, db, device := newCaptureTestService(t, &now, true)
	require.True(t, db.Migrator().HasTable(&gbmodels.GbSIPTraceCapture{}))
	require.True(t, db.Migrator().HasIndex(&gbmodels.GbSIPTraceCapture{}, "uk_sip_trace_capture_active"))

	result, err := service.Start(context.Background(), device.ID, 7)
	require.NoError(t, err)
	require.False(t, result.Reused)
	require.Equal(t, CaptureStatusActive, result.Capture.StatusAt(now))
	require.Equal(t, now.Add(DefaultCaptureDuration), result.Capture.PlannedEndAt)
	require.Equal(t, device.DeviceID, result.Capture.DeviceCode)
	require.EqualValues(t, 7, result.Capture.CreatedBy)
}

func TestCaptureStartReusesActiveWindow(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	service, db, device := newCaptureTestService(t, &now, true)
	first, err := service.Start(context.Background(), device.ID, 7)
	require.NoError(t, err)
	second, err := service.Start(context.Background(), device.ID, 8)
	require.NoError(t, err)
	require.True(t, second.Reused)
	require.Equal(t, first.Capture.ID, second.Capture.ID)
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbSIPTraceCapture{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	active, err := service.Active(context.Background(), device.ID, 7)
	require.NoError(t, err)
	require.NotNil(t, active)
	require.Equal(t, first.Capture.ID, active.ID)
}

func TestCaptureTimeoutCreatesNewWindowAndMaterializesOldEnd(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	service, db, device := newCaptureTestService(t, &now, true)
	first, err := service.Start(context.Background(), device.ID, 7)
	require.NoError(t, err)
	now = now.Add(DefaultCaptureDuration + time.Second)
	second, err := service.Start(context.Background(), device.ID, 7)
	require.NoError(t, err)
	require.NotEqual(t, first.Capture.ID, second.Capture.ID)

	var expired gbmodels.GbSIPTraceCapture
	require.NoError(t, db.First(&expired, "id = ?", first.Capture.ID).Error)
	require.NotNil(t, expired.EndedAt)
	require.Equal(t, CaptureEndTimeout, expired.EndReason)
	require.Nil(t, expired.ActiveKey)
}

func TestCaptureStopIsIdempotentAndReturnsWorkbenchFilter(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	service, _, device := newCaptureTestService(t, &now, true)
	started, err := service.Start(context.Background(), device.ID, 7)
	require.NoError(t, err)
	now = now.Add(5 * time.Minute)
	stopped, err := service.Stop(context.Background(), started.Capture.ID, 7)
	require.NoError(t, err)
	require.Equal(t, CaptureStatusEnded, stopped.StatusAt(now))
	require.Equal(t, CaptureEndManual, stopped.EndReason)

	again, err := service.Stop(context.Background(), started.Capture.ID, 7)
	require.NoError(t, err)
	require.NotNil(t, stopped.EndedAt)
	require.NotNil(t, again.EndedAt)
	require.True(t, stopped.EndedAt.Equal(*again.EndedAt))
	filter := again.WorkbenchFilter(now)
	require.Equal(t, device.DeviceID, filter.DeviceID)
	require.Equal(t, again.StartedAt, filter.From)
	require.Equal(t, *again.EndedAt, filter.To)
	active, err := service.Active(context.Background(), device.ID, 7)
	require.NoError(t, err)
	require.Nil(t, active)
}

func TestCaptureRejectsMissingDeletedAndUnauthorizedDevices(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	allowedService, db, device := newCaptureTestService(t, &now, true)
	_, err := allowedService.Start(context.Background(), 999999, 7)
	require.ErrorIs(t, err, ErrCaptureDeviceNotFound)
	require.NoError(t, db.Delete(device).Error)
	_, err = allowedService.Start(context.Background(), device.ID, 7)
	require.ErrorIs(t, err, ErrCaptureDeviceNotFound)

	deniedService := NewCaptureService(db, CaptureAuthorizerFunc(func(context.Context, uint, *gbmodels.GbDevice) bool {
		return false
	}), func() time.Time { return now })
	other := &gbmodels.GbDevice{DeviceID: "34020000001320000002", OwnerDeptID: 20}
	require.NoError(t, db.Create(other).Error)
	_, err = deniedService.Start(context.Background(), other.ID, 7)
	require.ErrorIs(t, err, ErrCaptureForbidden)
}
