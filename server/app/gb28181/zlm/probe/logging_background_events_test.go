package probe_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/probe"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

func TestLoggingBackgroundEventsProbe(t *testing.T) {
	const secret = "probe-private-credential"
	var output bytes.Buffer
	cfg, err := logging.ParseConfig(nil, "/app")
	require.NoError(t, err)
	cfg.Outputs, cfg.StdoutFormat = []string{"stdout"}, "json"
	runtime, err := logging.NewRuntime(logging.Options{Config: cfg, Sinks: map[string]zapcore.WriteSyncer{"stdout": zapcore.Lock(zapcore.AddSync(&output))}})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, runtime.Close()) })
	reg := newFakeRegistry(&node.Node{ID: 7, State: node.StateOffline}, &node.Node{ID: 8, State: node.StateOffline})
	p := probe.New(reg, func(n *node.Node) probe.Client {
		if n.ID == 7 {
			return &fakeClient{err: errors.New(secret)}
		}
		return &fakeClient{}
	}, time.Second, runtime.Root)
	results := p.Run(context.Background())
	require.Len(t, results, 2)
	require.False(t, results[0].Pass)
	require.True(t, results[1].Pass)
	require.Equal(t, 0, reg.markCalls[7])
	require.Equal(t, 1, reg.markCalls[8])
	require.NoError(t, runtime.Close())
	require.NotContains(t, output.String(), secret)
	events := map[string]map[string]interface{}{}
	for _, line := range bytes.Split(bytes.TrimSpace(output.Bytes()), []byte("\n")) {
		var row map[string]interface{}
		require.NoError(t, json.Unmarshal(line, &row))
		require.Equal(t, "zlm.probe", row["component"])
		events[row["event"].(string)] = row
	}
	require.Contains(t, events, "zlm.probe.failed")
	require.Equal(t, float64(7), events["zlm.probe.failed"]["node_id"])
	require.Contains(t, events, "zlm.probe.node_active")
	require.Equal(t, float64(8), events["zlm.probe.node_active"]["node_id"])
	require.Contains(t, events, "zlm.probe.completed")
}
