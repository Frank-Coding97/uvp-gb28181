package trace

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestModuleReadFilterReturnsUnchangedBytesAndEmitsFrames(t *testing.T) {
	module := &Module{framer: NewFrameAssembler(4096)}
	var got []Frame
	module.onFrame = func(frame Frame) {
		got = append(got, frame)
	}
	props := testReadProps("TCP", 5060, 15060)
	raw := sipMessage("MESSAGE", "runtime", []byte("body"))

	first := append([]byte(nil), raw[:len(raw)/2]...)
	filtered, err := module.ReadFilter(props, first)
	require.NoError(t, err)
	require.Equal(t, first, filtered)
	require.Empty(t, got)

	second := append([]byte(nil), raw[len(raw)/2:]...)
	filtered, err = module.ReadFilter(props, second)
	require.NoError(t, err)
	require.Equal(t, second, filtered)
	require.Len(t, got, 1)
	require.Equal(t, raw, got[0].Data)
}

func TestRelationalRuntimeUsesInjectedBusinessDBAndDoesNotCloseIt(t *testing.T) {
	db := newRelationalStoreTestDB(t)
	require.True(t, db.Migrator().HasTable(&gbmodels.GbSipTraceMessage{}))
	t.Setenv("UVP_TRACE_RUNTIME_TEST_KEY", strings.Repeat("k", 32))
	cfg := gbconfig.TraceConfig{
		Enabled: true, QueueCapacity: 8, BatchSize: 1, FlushIntervalMS: 5,
		EncryptionKeyEnv: "UVP_TRACE_RUNTIME_TEST_KEY", RetentionDays: 30,
	}
	runtime := NewRuntimeWithDB(cfg, db)
	module := runtime.(*Module)
	require.NotNil(t, module.QueryService())
	require.Eventually(t, func() bool { return module.Health().State == HealthReady }, time.Second, 10*time.Millisecond)

	module.WriteObserver(testWriteProps(), []byte("MESSAGE sip:test@example SIP/2.0\r\nCall-ID: runtime-db\r\nCSeq: 1 MESSAGE\r\nContent-Length: 0\r\n\r\n"))
	require.Eventually(t, func() bool {
		var count int64
		return db.Model(&gbmodels.GbSipTraceMessage{}).Count(&count).Error == nil && count == 1
	}, time.Second, 10*time.Millisecond)
	require.NoError(t, module.Shutdown(context.Background()))

	var one int
	require.NoError(t, db.Raw("SELECT 1").Scan(&one).Error)
	require.Equal(t, 1, one)
}

func TestRelationalRuntimeDegradesWhenBusinessDBIsUnavailable(t *testing.T) {
	t.Setenv("UVP_TRACE_RUNTIME_TEST_KEY", strings.Repeat("k", 32))
	runtime := NewRuntimeWithDB(gbconfig.TraceConfig{Enabled: true, EncryptionKeyEnv: "UVP_TRACE_RUNTIME_TEST_KEY"}, nil)
	require.Equal(t, HealthDegraded, runtime.Health().State)
	require.NoError(t, runtime.Shutdown(context.Background()))
}
