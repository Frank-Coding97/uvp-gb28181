package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"uvplatform.cn/uvp-gb28181/app/gb28181/handler"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/ginhelper"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

func TestLoggingHookResult(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("authentication rejection records failure", func(t *testing.T) {
		root, observed := observer.New(zap.DebugLevel)
		previous := app.ZapLog
		app.ZapLog = zap.New(root)
		t.Cleanup(func() { app.ZapLog = previous })

		auth := handler.NewHookAuthenticator()
		engine := gin.New()
		engine.Use(ginhelper.RequestLogging(app.ZapLog))
		engine.POST("/hook", auth.Middleware(playauth.HookOnFlowReport, handler.HookRejectAdmission), func(c *gin.Context) {
			t.Fatal("rejected hook must not reach handler")
		})

		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/hook", nil))

		var body struct {
			Code int `json:"code"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
		require.Equal(t, -1, body.Code)
		entry := findHookAccessLog(t, observed)
		require.Equal(t, int64(-1), entry.ContextMap()["business_code"])
		require.Equal(t, false, entry.ContextMap()["business_success"])
	})

	t.Run("success records result and hook scope", func(t *testing.T) {
		root, observed := observer.New(zap.DebugLevel)
		previous := app.ZapLog
		app.ZapLog = zap.New(root)
		t.Cleanup(func() { app.ZapLog = previous })

		engine := gin.New()
		engine.Use(ginhelper.RequestLogging(app.ZapLog))
		engine.POST("/hook", handler.NewHookController(nil).OnStreamChanged)
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewBufferString(`{"app":"rtp","stream":"success-stream","schema":"fmp4","regist":true}`))
		request.Header.Set("Content-Type", "application/json")
		engine.ServeHTTP(response, request)

		var body struct {
			Code int `json:"code"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
		require.Equal(t, 0, body.Code)
		entry := findHookAccessLog(t, observed)
		require.Equal(t, int64(0), entry.ContextMap()["business_code"])
		require.Equal(t, true, entry.ContextMap()["business_success"])
		require.NotEmpty(t, entry.ContextMap()["request_id"])
	})
}

func TestLoggingGBHTTPConcurrentStreamCorrelation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root, observed := observer.New(zap.InfoLevel)
	previous := app.ZapLog
	app.ZapLog = zap.New(root)
	t.Cleanup(func() { app.ZapLog = previous })

	engine := gin.New()
	engine.Use(ginhelper.RequestLogging(app.ZapLog))
	engine.POST("/hook", handler.NewHookController(nil).OnStreamChanged)
	const requests = 32
	var wait sync.WaitGroup
	errorsCh := make(chan error, requests)
	for i := 0; i < requests; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewBufferString(`{"app":"rtp","stream":"same-stream","schema":"fmp4","regist":true}`))
			request.Header.Set("Content-Type", "application/json")
			engine.ServeHTTP(response, request)
			var body struct {
				Code int `json:"code"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				errorsCh <- err
				return
			}
			if body.Code != 0 {
				errorsCh <- errors.New("hook success code changed")
			}
		}()
	}
	wait.Wait()
	close(errorsCh)
	for err := range errorsCh {
		require.NoError(t, err)
	}

	requestIDs := make(map[string]struct{}, requests)
	var hookLogs int
	for _, entry := range observed.All() {
		if entry.Message != "ZLM Hook on_stream_changed" {
			continue
		}
		hookLogs++
		fields := entry.ContextMap()
		require.Equal(t, "hook", entry.LoggerName)
		require.Equal(t, "same-stream", fields["stream"])
		require.Equal(t, "gb28181.hook.stream.changed", fields["event"])
		requestID, ok := fields["request_id"].(string)
		require.True(t, ok)
		require.NotEmpty(t, requestID)
		requestIDs[requestID] = struct{}{}
	}
	require.Equal(t, requests, hookLogs)
	require.Len(t, requestIDs, requests)
}

type loggingHookObserver struct {
	started  chan context.Context
	release  chan struct{}
	finished chan struct{}
}

func (o *loggingHookObserver) ObserveStream(ctx context.Context, _ string, _ bool) error {
	o.started <- ctx
	logging.FromContext(ctx, nil).Info("test async hook callback", zap.String("event", "test.hook.async"))
	<-o.release
	close(o.finished)
	return nil
}

func TestLoggingAsyncContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root, observed := observer.New(zap.InfoLevel)
	previous := app.ZapLog
	app.ZapLog = zap.New(root)
	t.Cleanup(func() { app.ZapLog = previous })

	streamObserver := &loggingHookObserver{
		started:  make(chan context.Context, 1),
		release:  make(chan struct{}),
		finished: make(chan struct{}),
	}
	hook := handler.NewHookController(nil)
	hook.SetStreamObserver(streamObserver)
	engine := gin.New()
	engine.Use(ginhelper.RequestLogging(app.ZapLog))
	engine.POST("/hook", hook.OnStreamChanged)

	requestContext, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewBufferString(`{"app":"rtp","stream":"async-stream","schema":"fmp4","regist":true}`)).WithContext(requestContext)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	served := make(chan struct{})
	go func() {
		engine.ServeHTTP(response, request)
		close(served)
	}()

	var observerContext context.Context
	select {
	case observerContext = <-streamObserver.started:
	case <-time.After(time.Second):
		t.Fatal("stream observer was not started")
	}
	cancel()
	select {
	case <-served:
	case <-time.After(time.Second):
		t.Fatal("hook did not return before async observer completed")
	}
	require.NoError(t, observerContext.Err())
	close(streamObserver.release)
	select {
	case <-streamObserver.finished:
	case <-time.After(time.Second):
		t.Fatal("async observer did not finish")
	}

	var asyncLogFound bool
	for _, entry := range observed.All() {
		if entry.ContextMap()["event"] == "test.hook.async" {
			asyncLogFound = true
			require.Equal(t, response.Header().Get("X-Request-ID"), entry.ContextMap()["request_id"])
		}
	}
	require.True(t, asyncLogFound)
}

func findHookAccessLog(t *testing.T, observed *observer.ObservedLogs) observer.LoggedEntry {
	t.Helper()
	for _, entry := range observed.All() {
		if entry.ContextMap()["event"] == "http.access" {
			return entry
		}
	}
	t.Fatal("HTTP access log not found")
	return observer.LoggedEntry{}
}
