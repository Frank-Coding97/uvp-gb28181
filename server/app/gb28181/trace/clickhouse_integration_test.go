//go:build integration

package trace

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
)

func TestClickHouseIntegrationWriteListReadDelete(t *testing.T) {
	required := []string{
		"UVP_TRACE_TEST_CLICKHOUSE_ADDR",
		"UVP_TRACE_TEST_CLICKHOUSE_USER",
		"UVP_TRACE_TEST_CLICKHOUSE_PASSWORD",
		"UVP_TRACE_TEST_CLICKHOUSE_DATABASE",
		"UVP_TRACE_TEST_ENCRYPTION_KEY",
	}
	for _, name := range required {
		if os.Getenv(name) == "" {
			t.Skipf("%s is not configured", name)
		}
	}
	cfg := gbconfig.TraceConfig{
		Enabled:          true,
		Address:          os.Getenv("UVP_TRACE_TEST_CLICKHOUSE_ADDR"),
		Database:         os.Getenv("UVP_TRACE_TEST_CLICKHOUSE_DATABASE"),
		Username:         os.Getenv("UVP_TRACE_TEST_CLICKHOUSE_USER"),
		PasswordEnv:      "UVP_TRACE_TEST_CLICKHOUSE_PASSWORD",
		EncryptionKeyEnv: "UVP_TRACE_TEST_ENCRYPTION_KEY",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	store, err := OpenClickHouseStore(ctx, cfg)
	require.NoError(t, err)
	defer store.Close()
	cipher, err := LoadCipherFromEnv(cfg.EncryptionKeyEnv, "v1")
	require.NoError(t, err)

	eventID := uuid.NewString()
	callID := "integration-" + eventID
	raw := []byte("MESSAGE sip:34020000002000000001@3402000000 SIP/2.0\r\n" +
		"From: <sip:34020000001320000001@3402000000>;tag=1\r\n" +
		"To: <sip:34020000002000000001@3402000000>\r\n" +
		"Call-ID: " + callID + "\r\nCSeq: 9 MESSAGE\r\nContent-Length: 4\r\n\r\nbody")
	payload, err := cipher.Encrypt(raw)
	require.NoError(t, err)
	now := time.Now().UTC()
	event := StoredEvent{
		EventID: eventID, OccurredAt: now, Direction: DirectionInbound, Transport: "UDP",
		LocalAddr: "192.168.10.220:5061", RemoteAddr: "192.168.10.10:5060",
		DeviceID: "34020000001320000001", Method: "MESSAGE", CallID: callID,
		CSeq: 9, CSeqMethod: "MESSAGE", Payload: payload,
	}
	require.NoError(t, store.InsertBatch(ctx, []StoredEvent{event}))
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		require.NoError(t, store.conn.Exec(cleanupCtx,
			fmt.Sprintf("ALTER TABLE %s DELETE WHERE event_id = toUUID(?) SETTINGS mutations_sync = 1", store.fullTable),
			eventID,
		))
	}()

	page, err := store.ListMessages(ctx, MessageFilter{From: now.Add(-time.Minute), To: now.Add(time.Minute), CallID: callID, Limit: 10})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, eventID, page.Items[0].EventID)
	detail, err := store.GetMessage(ctx, eventID)
	require.NoError(t, err)
	decrypted, err := cipher.Decrypt(detail.Payload)
	require.NoError(t, err)
	require.Equal(t, raw, decrypted)
}
