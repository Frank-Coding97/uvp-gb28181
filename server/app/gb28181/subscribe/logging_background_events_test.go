package subscribe_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/subscribe"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

func TestLoggingSubscribeStateEventsUseRequestScope(t *testing.T) {
	const (
		deviceID = "34020000002000000001"
		secret   = "subscribe-token=must-not-leak"
	)

	var output bytes.Buffer
	cfg := logging.Config{
		Outputs:      []string{"stdout"},
		Level:        zapcore.DebugLevel,
		Modules:      map[string]zapcore.Level{"subscribe": zapcore.DebugLevel},
		FileFormat:   "json",
		StdoutFormat: "json",
		MaxSizeMB:    1,
		MaxBackups:   1,
		MaxAgeDays:   1,
	}
	runtime, err := logging.NewRuntime(logging.Options{
		Config: cfg,
		Sinks:  map[string]zapcore.WriteSyncer{"stdout": zapcore.Lock(zapcore.AddSync(&output))},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, runtime.Close()) })

	db := newTestDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	seedDevice(t, db, deviceID, gbmodels.SubscribeUnknown)

	sm := subscribe.NewStateMachine(db, runtime.Root)
	call := func(requestID string) context.Context {
		return logging.WithContext(context.Background(), logging.WithIdentity(runtime.Root, zap.String("request_id", requestID)))
	}

	sm.OnRegister(call("subscribe-register"), deviceID)
	assertSubscribeCapability(t, db, deviceID, gbmodels.SubscribeFallback)

	old := time.Now().Add(-25 * time.Hour)
	require.NoError(t, db.Model(&gbmodels.GbDevice{}).Where("device_id = ?", deviceID).Updates(map[string]interface{}{
		"subscribe_capability": gbmodels.SubscribeFallback,
		"subscribe_last_test":  old,
	}).Error)
	sm.OnRegister(call("subscribe-retry"), deviceID)
	assertSubscribeCapability(t, db, deviceID, gbmodels.SubscribeFallback)

	sm.OnNotify(call("subscribe-notify"), deviceID)
	assertSubscribeCapability(t, db, deviceID, gbmodels.SubscribeSubscribed)

	sm.DegradeToFallback(call("subscribe-degrade"), deviceID, secret)
	assertSubscribeCapability(t, db, deviceID, gbmodels.SubscribeFallback)

	require.NotContains(t, output.String(), secret)
	records := subscribeLogRecords(t, output.String())
	want := map[string]string{
		"subscribe.capability.probe":    "subscribe-register",
		"subscribe.capability.retry":    "subscribe-retry",
		"subscribe.capability.upgraded": "subscribe-notify",
		"subscribe.capability.degraded": "subscribe-degrade",
	}
	for event, requestID := range want {
		record := subscribeLogRecord(t, records, event)
		require.Equal(t, "subscribe", record["component"])
		require.Equal(t, deviceID, record["device_id"])
		require.Equal(t, requestID, record["request_id"])
		if event == "subscribe.capability.probe" || event == "subscribe.capability.retry" {
			require.Equal(t, true, record["simulated"], "legacy state machine does not send real SUBSCRIBE")
		}
	}
	degraded := subscribeLogRecord(t, records, "subscribe.capability.degraded")
	require.Equal(t, true, degraded["reason_present"])
}

func assertSubscribeCapability(t *testing.T, db *gorm.DB, deviceID string, want gbmodels.SubscribeCapability) {
	t.Helper()
	var dev gbmodels.GbDevice
	require.NoError(t, db.Where("device_id = ?", deviceID).First(&dev).Error)
	require.Equal(t, want, dev.SubscribeCapability)
}

func subscribeLogRecords(t *testing.T, output string) []map[string]interface{} {
	t.Helper()
	var records []map[string]interface{}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		var record map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(line), &record))
		records = append(records, record)
	}
	return records
}

func subscribeLogRecord(t *testing.T, records []map[string]interface{}, event string) map[string]interface{} {
	t.Helper()
	for _, record := range records {
		if record["event"] == event {
			return record
		}
	}
	t.Fatalf("subscribe log event not found: %s; records=%v", event, records)
	return nil
}
