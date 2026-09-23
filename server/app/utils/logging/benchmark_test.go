package logging

import (
	"io"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Both implementations use the same encoder, fields, clock, and destination.
// Discard measures CPU independently of filesystem latency; File measures the
// managed synchronous writer. Complex sanitization is reported separately.
func BenchmarkLogging(b *testing.B) {
	for _, target := range []string{"Discard", "File"} {
		b.Run(target, func(b *testing.B) {
			for _, impl := range []string{"Native", "Safe"} {
				b.Run(impl, func(b *testing.B) {
					benchmarkLogging(b, target, impl, false)
				})
			}
		})
	}
	b.Run("ComplexSafe", func(b *testing.B) { benchmarkLogging(b, "Discard", "Safe", true) })
}

func benchmarkLogging(b *testing.B, target, impl string, complex bool) {
	cfg, err := ParseConfig(nil, b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	cfg.Outputs = []string{"stdout"}
	cfg.StdoutFormat = "json"
	var sink zapcore.WriteSyncer = zapcore.AddSync(io.Discard)
	if target == "File" {
		writer, err := openFileWriter(writerOptions{path: filepath.Join(b.TempDir(), "runtime.log"), maxBytes: 5 << 20, maxBackups: 7, maxAge: 15 * 24 * time.Hour})
		if err != nil {
			b.Fatal(err)
		}
		sink = writer
		b.Cleanup(func() {
			if err := writer.Close(); err != nil {
				b.Error(err)
			}
		})
	}
	var logger *zap.Logger
	if impl == "Safe" {
		runtime, err := NewRuntime(Options{Config: cfg, Service: "uvp-gb28181", Version: "benchmark", Instance: "local", Sinks: map[string]zapcore.WriteSyncer{"stdout": sink}, ErrorOutput: zapcore.AddSync(io.Discard)})
		if err != nil {
			b.Fatal(err)
		}
		logger = runtime.Root
		b.Cleanup(func() {
			if err := runtime.Close(); err != nil {
				b.Error(err)
			}
		})
	} else {
		enc := zap.NewProductionEncoderConfig()
		enc.TimeKey = "created_at"
		enc.MessageKey = "message"
		enc.NameKey = "component"
		enc.StacktraceKey = "stack"
		enc.EncodeTime = func(t time.Time, a zapcore.PrimitiveArrayEncoder) {
			a.AppendString(t.Format("2006-01-02T15:04:05.000Z07:00"))
		}
		enc.EncodeDuration = zapcore.MillisDurationEncoder
		core := zapcore.NewCore(zapcore.NewJSONEncoder(enc), sink, zapcore.DebugLevel).With([]zap.Field{zap.String("service", "uvp-gb28181"), zap.String("version", "benchmark"), zap.String("instance", "local")})
		logger = zap.New(core, zap.ErrorOutput(zapcore.AddSync(io.Discard)))
	}
	logger = logger.Named("device")
	fields := []zap.Field{zap.String("event", "device.scan_completed"), zap.String("device_id", "34020000001320000001"), zap.Int("scanned", 32), zap.Bool("changed", false)}
	if complex {
		fields = append(fields, zap.Any("payload", map[string]any{"password": "fixture-secret"}), zap.String("endpoint", "https://user:fixture-secret@example.test/private?token=fixture-secret"))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("device scan completed", fields...)
	}
	b.StopTimer()
	if err := logger.Sync(); err != nil {
		b.Fatal(err)
	}
}
