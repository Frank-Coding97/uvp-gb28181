package ptz

import (
	"context"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"testing"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

func TestLoggingGBHTTPPTZPersistenceFailure(t *testing.T) {
	// No preset table: the accepted SIP command still remains sent, while the
	// local optimistic-cache failure must be diagnosable with its request ID.
	svc := newPTZTestService(t, &fakeTrackedSender{})
	core, entries := observer.New(zap.WarnLevel)
	ctx := logging.WithContext(context.Background(), logging.WithIdentity(zap.New(core), zap.String("request_id", "ptz-persist-request")))
	cmd := Command{CmdType: "DeviceControl", Action: "preset_set", IdempotencyKey: "preset-log", Payload: map[string]interface{}{"action": "preset_set", "id": 3, "name": "fixture"}, Build: func(int) ([]byte, error) { return []byte("<Control/>"), nil }}
	op, err := svc.Execute(ctx, testTarget(), cmd)
	require.NoError(t, err)
	require.Equal(t, gbmodels.PTZOperationSent, op.Status)
	require.Len(t, entries.All(), 1)
	fields := entries.All()[0].ContextMap()
	require.Equal(t, "ptz-persist-request", fields["request_id"])
	require.Equal(t, "ptz.preset_cache_failed", fields["event"])
	require.Equal(t, op.OperationID, fields["operationId"])
}
