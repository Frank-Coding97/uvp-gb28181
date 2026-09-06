package ginhelper

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	goruntime "runtime"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
	"uvplatform.cn/uvp-gb28181/app/utils/response"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	budgetWarmup   = 200
	budgetBatches  = 12
	budgetBatchN   = 500
	budgetP95Limit = 200 * time.Microsecond
	budgetReport   = "/tmp/uvp-logging-t15-http-budget.json"
)

type budgetSink struct {
	count   atomic.Uint64
	mu      sync.Mutex
	samples [][]byte
}

func (s *budgetSink) Write(p []byte) (int, error) {
	n, err := io.Discard.Write(p)
	s.count.Add(1)
	s.mu.Lock()
	if len(s.samples) < 4 {
		s.samples = append(s.samples, append([]byte(nil), p...))
	}
	s.mu.Unlock()
	return n, err
}
func (s *budgetSink) Sync() error  { return nil }
func (s *budgetSink) Close() error { return nil }

func budgetRuntime(t *testing.T) (*logging.Runtime, *budgetSink) {
	t.Helper()
	cfg := logging.Config{
		Outputs:      []string{"stdout"},
		Level:        zapcore.InfoLevel,
		Modules:      map[string]zapcore.Level{"access": zapcore.InfoLevel},
		FileFormat:   "json",
		StdoutFormat: "json",
		MaxSizeMB:    1,
		MaxBackups:   1,
		MaxAgeDays:   1,
	}
	sink := &budgetSink{}
	rt, err := logging.NewRuntime(logging.Options{
		Config: cfg,
		Sinks:  map[string]zapcore.WriteSyncer{"stdout": sink},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rt.Close(); err != nil {
			t.Error(err)
		}
	})
	return rt, sink
}

func budgetRouter(t *testing.T, root *zap.Logger, native bool) *gin.Engine {
	t.Helper()
	g := gin.New()
	if native {
		g.Use(nativeBudgetLogging(root))
	} else {
		g.Use(RequestLogging(root))
	}
	g.GET("/budget", func(c *gin.Context) {
		if !native {
			response.SetBusinessResult(c, 0, true)
		}
		c.Status(http.StatusNoContent)
	})
	return g
}

// nativeBudgetLogging is the fixed-ID control. It emits the same access
// schema, while avoiding the request context and response metadata lookup that
// RequestLogging adds. A fixed ID keeps request_id present and comparable.
func nativeBudgetLogging(root *zap.Logger) gin.HandlerFunc {
	logger := logging.WithIdentity(root, zap.String("request_id", "baseline-fixed")).Named("access")
	return func(c *gin.Context) {
		started := time.Now()
		c.Header("X-Request-ID", "baseline-fixed")
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = "<unmatched>"
		}
		size := c.Writer.Size()
		if size < 0 {
			size = 0
		}
		logger.Info("HTTP request completed",
			zap.String("event", "http.access"),
			zap.String("method", http.MethodGet),
			zap.String("route", route),
			zap.Int("http_status", c.Writer.Status()),
			zap.Float64("duration_ms", float64(time.Since(started))/float64(time.Millisecond)),
			zap.Int("response_bytes", size),
			zap.Int("business_code", 0),
			zap.Bool("business_success", true),
		)
	}
}

type budgetStats struct {
	Requests  int                      `json:"requests"`
	Emitted   uint64                   `json:"emitted"`
	P50NS     int64                    `json:"p50_ns"`
	P95NS     int64                    `json:"p95_ns"`
	Durations []int64                  `json:"durations_ns"`
	Samples   []map[string]interface{} `json:"samples"`
}

type budgetReportData struct {
	Warmup         int         `json:"warmup_per_group"`
	Batches        int         `json:"batches"`
	BatchSize      int         `json:"batch_size"`
	P95BudgetNS    int64       `json:"p95_budget_ns"`
	P95DeltaNS     int64       `json:"p95_delta_ns"`
	Baseline       budgetStats `json:"baseline"`
	RequestLogging budgetStats `json:"request_logging"`
}

func budgetServe(t *testing.T, g *gin.Engine, path string) (time.Duration, string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	started := time.Now()
	g.ServeHTTP(w, req)
	elapsed := time.Since(started)
	if w.Code != http.StatusNoContent {
		t.Fatalf("HTTP status = %d, want %d", w.Code, http.StatusNoContent)
	}
	return elapsed, w.Header().Get("X-Request-ID")
}

func budgetRows(t *testing.T, sink *budgetSink) []map[string]interface{} {
	t.Helper()
	sink.mu.Lock()
	raw := append([][]byte(nil), sink.samples...)
	sink.mu.Unlock()
	rows := make([]map[string]interface{}, 0, len(raw))
	for _, line := range raw {
		var row map[string]interface{}
		if err := json.Unmarshal(line, &row); err != nil {
			t.Fatalf("decode JSON sink row: %v; raw=%q", err, line)
		}
		rows = append(rows, row)
	}
	return rows
}

func budgetPercentile(values []time.Duration, fraction float64) time.Duration {
	ordered := append([]time.Duration(nil), values...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	index := int(math.Ceil(fraction*float64(len(ordered)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(ordered) {
		index = len(ordered) - 1
	}
	return ordered[index]
}

func budgetStat(t *testing.T, values []time.Duration, sink *budgetSink, emittedBefore int) budgetStats {
	t.Helper()
	durations := make([]int64, len(values))
	for i, value := range values {
		durations[i] = value.Nanoseconds()
	}
	return budgetStats{
		Requests:  len(values),
		Emitted:   sink.count.Load() - uint64(emittedBefore),
		P50NS:     budgetPercentile(values, 0.50).Nanoseconds(),
		P95NS:     budgetPercentile(values, 0.95).Nanoseconds(),
		Durations: durations,
		Samples:   budgetRows(t, sink),
	}
}

func assertBudgetRow(t *testing.T, row map[string]interface{}, wantID string) {
	t.Helper()
	if row["event"] != "http.access" || row["method"] != http.MethodGet || row["route"] != "/budget" {
		t.Fatalf("access identity fields = %v", row)
	}
	if row["http_status"] != float64(http.StatusNoContent) || row["response_bytes"] != float64(0) {
		t.Fatalf("access response fields = %v", row)
	}
	if row["business_code"] != float64(0) || row["business_success"] != true {
		t.Fatalf("access business fields = %v", row)
	}
	if row["request_id"] != wantID {
		t.Fatalf("request_id = %v, want %q", row["request_id"], wantID)
	}
	if _, ok := row["duration_ms"].(float64); !ok {
		t.Fatalf("duration_ms missing or not numeric: %v", row)
	}
}

func TestLoggingHTTPBudget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	baselineRuntime, baselineSink := budgetRuntime(t)
	requestRuntime, requestSink := budgetRuntime(t)
	baseline := budgetRouter(t, baselineRuntime.Root, true)
	requestLogging := budgetRouter(t, requestRuntime.Root, false)

	for i := 0; i < budgetWarmup; i++ {
		if i%2 == 0 {
			budgetServe(t, baseline, "/budget")
			budgetServe(t, requestLogging, "/budget")
		} else {
			budgetServe(t, requestLogging, "/budget")
			budgetServe(t, baseline, "/budget")
		}
	}
	baselineEmittedBefore := baselineSink.count.Load()
	requestEmittedBefore := requestSink.count.Load()
	if baselineEmittedBefore != budgetWarmup || requestEmittedBefore != budgetWarmup {
		t.Fatalf("warmup request/log count mismatch: baseline=%d/%d request_logging=%d/%d", budgetWarmup, baselineEmittedBefore, budgetWarmup, requestEmittedBefore)
	}
	goruntime.GC()

	baselineDurations := make([]time.Duration, 0, budgetBatches*budgetBatchN)
	requestDurations := make([]time.Duration, 0, budgetBatches*budgetBatchN)
	for batch := 0; batch < budgetBatches; batch++ {
		for i := 0; i < budgetBatchN; i++ {
			if batch%2 == 0 {
				elapsed, _ := budgetServe(t, baseline, "/budget")
				baselineDurations = append(baselineDurations, elapsed)
				elapsed, _ = budgetServe(t, requestLogging, "/budget")
				requestDurations = append(requestDurations, elapsed)
			} else {
				elapsed, _ := budgetServe(t, requestLogging, "/budget")
				requestDurations = append(requestDurations, elapsed)
				elapsed, _ = budgetServe(t, baseline, "/budget")
				baselineDurations = append(baselineDurations, elapsed)
			}
		}
	}

	baselineStats := budgetStat(t, baselineDurations, baselineSink, int(baselineEmittedBefore))
	requestStats := budgetStat(t, requestDurations, requestSink, int(requestEmittedBefore))
	if baselineStats.Emitted != uint64(baselineStats.Requests) || requestStats.Emitted != uint64(requestStats.Requests) {
		t.Fatalf("request/log count mismatch: baseline=%d/%d request_logging=%d/%d", baselineStats.Requests, baselineStats.Emitted, requestStats.Requests, requestStats.Emitted)
	}
	if len(budgetRows(t, baselineSink)) == 0 || len(budgetRows(t, requestSink)) == 0 {
		t.Fatal("JSON sink did not retain validation rows")
	}
	assertBudgetRow(t, baselineStats.Samples[0], "baseline-fixed")
	requestID, _ := requestStats.Samples[0]["request_id"].(string)
	if requestID == "" || requestID == "baseline-fixed" {
		t.Fatalf("production request ID missing or fixed: %q", requestID)
	}
	assertBudgetRow(t, requestStats.Samples[0], requestID)
	if got := requestStats.Samples[0]["request_id"]; got != requestID {
		t.Fatalf("request correlation changed: %v", got)
	}

	delta := time.Duration(requestStats.P95NS - baselineStats.P95NS)
	report := budgetReportData{
		Warmup:         budgetWarmup,
		Batches:        budgetBatches,
		BatchSize:      budgetBatchN,
		P95BudgetNS:    budgetP95Limit.Nanoseconds(),
		P95DeltaNS:     delta.Nanoseconds(),
		Baseline:       baselineStats,
		RequestLogging: requestStats,
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(budgetReport, encoded, 0600); err != nil {
		t.Fatalf("write raw measurement report %s: %v", budgetReport, err)
	}
	t.Logf("HTTP budget report=%s baseline p50=%s p95=%s emitted=%d request_logging p50=%s p95=%s emitted=%d p95_delta=%s budget=%s", budgetReport, time.Duration(baselineStats.P50NS), time.Duration(baselineStats.P95NS), baselineStats.Emitted, time.Duration(requestStats.P50NS), time.Duration(requestStats.P95NS), requestStats.Emitted, delta, budgetP95Limit)
	if delta > budgetP95Limit {
		t.Fatalf("RequestLogging p95 overhead %s exceeds budget %s", delta, budgetP95Limit)
	}
}
