package ginhelper

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
	"uvplatform.cn/uvp-gb28181/app/utils/ymlconfig"
)

type logValues map[string]interface{}

func (v logValues) Get(k string) interface{} { return v[k] }

type lockedLogBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}
func (b *lockedLogBuffer) Sync() error { return nil }
func (b *lockedLogBuffer) rows(t *testing.T) []map[string]interface{} {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	var rows []map[string]interface{}
	for _, line := range bytes.Split(bytes.TrimSpace(b.b.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var r map[string]interface{}
		if err := json.Unmarshal(line, &r); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, r)
	}
	return rows
}
func loggingRouter(t *testing.T) (*gin.Engine, *lockedLogBuffer) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg, e := logging.ParseConfig(logValues{"logs.outputs": []string{"stdout"}, "logs.stdoutformat": "json", "logs.level": "warn"}, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	b := &lockedLogBuffer{}
	r, e := logging.NewRuntime(logging.Options{Config: cfg, Sinks: map[string]zapcore.WriteSyncer{"stdout": b}})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := r.Close(); e != nil {
			t.Error(e)
		}
	})
	g := gin.New()
	g.Use(RequestLogging(r.Root), CustomRecovery())
	return g, b
}
func TestLoggingHTTPResults(t *testing.T) {
	g, b := loggingRouter(t)
	g.GET("/ok", func(c *gin.Context) { response.Success(c, nil) })
	g.GET("/failure", func(c *gin.Context) { response.ReturnJson(c, 200, 42, "business failure", nil) })
	g.GET("/bad", func(c *gin.Context) { c.Status(400) })
	g.GET("/content", func(c *gin.Context) { c.String(200, "hello") })
	g.GET("/error", func(c *gin.Context) { c.Status(500) })
	for _, path := range []string{"/ok", "/failure", "/bad", "/content", "/error", "/missing-secret?token=fixture-secret"} {
		w := httptest.NewRecorder()
		g.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Header().Get("X-Request-ID") == "" {
			t.Error("missing response ID")
		}
	}
	rows := b.rows(t)
	if len(rows) != 6 {
		t.Fatalf("access count %d", len(rows))
	}
	for _, r := range rows {
		if r["event"] != "http.access" || r["duration_ms"] == nil || r["response_bytes"] == nil {
			t.Errorf("invalid summary %v", r)
		}
		if strings.Contains(r["route"].(string), "secret") {
			t.Fatal("raw unmatched route leaked")
		}
	}
	if rows[1]["business_code"] != float64(42) || rows[1]["business_success"] != false || rows[1]["http_status"] != float64(200) {
		t.Errorf("business result %v", rows[1])
	}
	if _, ok := rows[3]["business_code"]; ok {
		t.Fatal("invented business code")
	}
	if rows[3]["response_bytes"] != float64(5) {
		t.Error("wrong bytes")
	}
}
func TestLoggingHTTPConcurrentIdentity(t *testing.T) {
	g, b := loggingRouter(t)
	g.GET("/items/:id", func(c *gin.Context) {
		if c.Query("token") != "fixture-secret" {
			t.Error("request mutated")
		}
		app.Log(c.Request.Context()).Warn("request operation", zap.String("event", "operation.test"))
		c.Status(204)
	})
	var wg sync.WaitGroup
	ids := sync.Map{}
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/items/fixture-secret?token=fixture-secret", nil)
			r.Header.Set("X-Request-ID", "external.same")
			r.Header.Set("Authorization", "fixture-secret")
			r.AddCookie(&http.Cookie{Name: "secret", Value: "fixture-secret"})
			g.ServeHTTP(w, r)
			id := w.Header().Get("X-Request-ID")
			if id == "" || id == "external.same" {
				t.Error("untrusted ID used")
			}
			if _, loaded := ids.LoadOrStore(id, true); loaded {
				t.Error("duplicate ID")
			}
		}()
	}
	wg.Wait()
	rows := b.rows(t)
	if len(rows) != 128 {
		t.Fatalf("records %d", len(rows))
	}
	counts := map[string]int{}
	for _, r := range rows {
		if r["client_request_id"] != "external.same" {
			t.Error("missing external ID")
		}
		id, _ := r["request_id"].(string)
		counts[id]++
		data, _ := json.Marshal(r)
		if bytes.Contains(data, []byte("fixture-secret")) {
			t.Fatal("credentials leaked")
		}
	}
	for id, count := range counts {
		if id == "" || count != 2 {
			t.Errorf("crossed request %q: %d", id, count)
		}
	}
}
func TestLoggingHTTPInvalidIdentity(t *testing.T) {
	for _, id := range []string{"", strings.Repeat("x", 65), "bad\nvalue", "坏id"} {
		g, b := loggingRouter(t)
		g.GET("/", func(c *gin.Context) { c.Status(204) })
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("X-Request-ID", id)
		g.ServeHTTP(httptest.NewRecorder(), r)
		rows := b.rows(t)
		if len(rows) != 1 {
			t.Fatal("missing access")
		}
		if _, ok := rows[0]["client_request_id"]; ok {
			t.Error("invalid identity retained")
		}
	}
}
func loggingPanicSite() { panic("fixture-secret") }
func TestLoggingRecovery(t *testing.T) {
	for _, kind := range []string{"abort", "panic", "written", "broken_pipe", "uncomparable"} {
		t.Run(kind, func(t *testing.T) {
			g, b := loggingRouter(t)
			g.GET("/panic", func(c *gin.Context) {
				switch kind {
				case "abort":
					response.Fail(c, "expected")
					panic(consts.RequestAborted)
				case "written":
					c.String(202, "already")
					loggingPanicSite()
				case "broken_pipe":
					panic(&net.OpError{Op: "write", Err: &os.SyscallError{Syscall: "write", Err: syscall.EPIPE}})
				case "uncomparable":
					panic(map[string]string{"secret": "fixture-secret"})
				default:
					loggingPanicSite()
				}
			})
			w := httptest.NewRecorder()
			g.ServeHTTP(w, httptest.NewRequest("GET", "/panic", nil))
			rows := b.rows(t)
			want := 2
			if kind == "abort" {
				want = 1
			}
			if len(rows) != want {
				t.Fatalf("events %d want %d", len(rows), want)
			}
			if strings.Contains(w.Body.String(), "fixture-secret") {
				t.Fatal("panic reflected")
			}
			if kind == "written" && (w.Code != 202 || w.Body.String() != "already") {
				t.Fatal("rewrote response")
			}
			if kind == "broken_pipe" && w.Body.Len() != 0 {
				t.Fatal("wrote broken connection")
			}
			if kind == "panic" {
				if w.Code != 500 {
					t.Error("missing 500")
				}
				if !strings.Contains(rows[0]["stack"].(string), "loggingPanicSite") {
					t.Error("original stack missing")
				}
			}
			for _, r := range rows {
				if r["request_id"] == nil {
					t.Error("panic correlation missing")
				}
			}
		})
	}
}

// The middleware must preserve the exact writer and streaming interfaces.
type streamWriter struct {
	header  http.Header
	conn    net.Conn
	reader  *bufio.ReadWriter
	flushed bool
	status  int
	body    bytes.Buffer
}

func (w *streamWriter) Header() http.Header                          { return w.header }
func (w *streamWriter) WriteHeader(n int)                            { w.status = n }
func (w *streamWriter) Write(p []byte) (int, error)                  { return w.body.Write(p) }
func (w *streamWriter) Flush()                                       { w.flushed = true }
func (w *streamWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) { return w.conn, w.reader, nil }
func TestLoggingHTTPStreaming(t *testing.T) {
	g, b := loggingRouter(t)
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	rw := bufio.NewReadWriter(bufio.NewReader(left), bufio.NewWriter(left))
	w := &streamWriter{header: http.Header{}, conn: left, reader: rw}
	g.GET("/stream", func(c *gin.Context) {
		before := c.Writer
		c.Writer.WriteString("chunk")
		c.Writer.Flush()
		if !w.flushed || w.body.String() != "chunk" {
			t.Error("stream was buffered")
		}
		if c.Writer != before {
			t.Error("writer replaced")
		}
		conn, reader, err := c.Writer.Hijack()
		if err == nil || conn != nil || reader != nil {
			t.Error("Gin must reject Hijack after writing")
		}

	})
	g.ServeHTTP(w, httptest.NewRequest("GET", "/stream", nil))
	g.GET("/hijack", func(c *gin.Context) {
		conn, reader, err := c.Writer.Hijack()
		if err != nil || conn != left || reader != rw {
			t.Error("Hijack capability changed")
		}
	})
	g.ServeHTTP(&streamWriter{header: http.Header{}, conn: left, reader: rw}, httptest.NewRequest("GET", "/hijack", nil))
	if len(b.rows(t)) != 2 {
		t.Error("stream access count")
	}
}
func TestLoggingHTTPDebugRoutes(t *testing.T) {
	oldConfig, oldLog := app.ConfigYml, app.ZapLog
	oldMode, oldPrint, oldRoutes := gin.Mode(), gin.DebugPrintFunc, gin.DebugPrintRouteFunc
	oldWriter, oldError := gin.DefaultWriter, gin.DefaultErrorWriter
	defer func() {
		app.ConfigYml, app.ZapLog = oldConfig, oldLog
		gin.SetMode(oldMode)
		gin.DebugPrintFunc, gin.DebugPrintRouteFunc = oldPrint, oldRoutes
		gin.DefaultWriter, gin.DefaultErrorWriter = oldWriter, oldError
	}()
	for _, debug := range []bool{false, true} {
		for _, routes := range []bool{false, true} {
			t.Run(fmt.Sprintf("debug=%v/routes=%v", debug, routes), func(t *testing.T) {
				dir := t.TempDir()
				content := fmt.Sprintf("server:\n  appdebug: %v\nlogs:\n  routes: %v\n", debug, routes)
				if err := os.WriteFile(filepath.Join(dir, "config.yml"), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
				cfg, err := ymlconfig.LoadYamlFactory(dir)
				if err != nil {
					t.Fatal(err)
				}
				app.ConfigYml = cfg
				lc, err := logging.ParseConfig(logValues{"logs.outputs": []string{"stdout"}, "logs.stdoutformat": "json", "logs.level": "warn", "logs.modules": map[string]interface{}{"routes": "info"}}, dir)
				if err != nil {
					t.Fatal(err)
				}
				b := &lockedLogBuffer{}
				rt, err := logging.NewRuntime(logging.Options{Config: lc, Sinks: map[string]zapcore.WriteSyncer{"stdout": b}})
				if err != nil {
					t.Fatal(err)
				}
				defer rt.Close()
				app.ZapLog = rt.Root
				g := GetEngine()
				g.GET("/check", func(c *gin.Context) { c.Status(204) })
				logRoutes(g)
				w := httptest.NewRecorder()
				g.ServeHTTP(w, httptest.NewRequest("GET", "/check", nil))
				hasPprof := false
				for _, r := range g.Routes() {
					if strings.HasPrefix(r.Path, "/debug/pprof") {
						hasPprof = true
					}
				}
				if hasPprof != debug {
					t.Error("logging altered debug exposure")
				}
				accessCount, routeCount := 0, 0
				for _, r := range b.rows(t) {
					if r["event"] == "http.access" {
						accessCount++
					}
					if r["event"] == "http.route_registered" {
						routeCount++
					}
				}
				if accessCount != 1 {
					t.Error("warn root suppressed access")
				}
				if routes && routeCount == 0 {
					t.Error("requested route listing missing")
				}
				if !routes && routeCount != 0 {
					t.Error("route switch ignored")
				}
			})
		}
	}
}

func TestLoggingGBHTTPStringCode(t *testing.T) {
	g, b := loggingRouter(t)
	g.GET("/management", func(c *gin.Context) {
		// Existing management responses use symbolic codes rather than numeric codes.
		response.SetBusinessStringResult(c, "INVALID_REQUEST", false)
		c.JSON(400, gin.H{"code": "INVALID_REQUEST", "message": "invalid"})
	})
	w := httptest.NewRecorder()
	g.ServeHTTP(w, httptest.NewRequest("GET", "/management", nil))
	rows := b.rows(t)
	if len(rows) != 1 || rows[0]["business_code"] != "INVALID_REQUEST" || rows[0]["business_success"] != false {
		t.Fatalf("symbolic business code lost: %v", rows)
	}
}
