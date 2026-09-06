package executors

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
	"uvplatform.cn/uvp-gb28181/app/utils/schedulerhelper"
)

func TestLoggingBackgroundEventsDemoExecutor(t *testing.T) {
	var output bytes.Buffer
	cfg, err := logging.ParseConfig(nil, "/app")
	require.NoError(t, err)
	cfg.Outputs, cfg.StdoutFormat = []string{"stdout"}, "json"
	runtime, err := logging.NewRuntime(logging.Options{Config: cfg, Sinks: map[string]zapcore.WriteSyncer{"stdout": zapcore.Lock(zapcore.AddSync(&output))}})
	require.NoError(t, err)
	previous := app.ZapLog
	previousWriter := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previousWriter) })
	app.ZapLog = runtime.Root
	t.Cleanup(func() { app.ZapLog = previous; require.NoError(t, runtime.Close()) })
	ctx := logging.WithContext(context.Background(), logging.WithIdentity(runtime.Root, zap.String("execution_id", "demo-execution")))
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	job := &schedulerhelper.Job{ID: "demo-job", Name: "demo", Parameters: map[string]interface{}{"password": "demo-private-secret"}}
	require.ErrorIs(t, (&DemoExecutor{}).Execute(ctx, job), context.Canceled)
	require.NoError(t, runtime.Close())
	require.NotContains(t, output.String(), "demo-private-secret")
	rows := bytes.Split(bytes.TrimSpace(output.Bytes()), []byte("\n"))
	require.Len(t, rows, 2)
	for i, line := range rows {
		var row map[string]interface{}
		require.NoError(t, json.Unmarshal(line, &row))
		require.Equal(t, "demo-execution", row["execution_id"])
		require.Equal(t, "demo-job", row["job_id"])
		require.Equal(t, "scheduler.demo", row["component"])
		if i == 0 {
			require.Equal(t, "scheduler.demo.started", row["event"])
			require.Equal(t, float64(1), row["parameter_count"])
		} else {
			require.Equal(t, "scheduler.demo.canceled", row["event"])
		}
	}
}
