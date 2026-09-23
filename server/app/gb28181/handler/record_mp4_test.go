package handler_test

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/handler"
	"uvplatform.cn/uvp-gb28181/app/gb28181/recording"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
)

type recordMP4Resolver map[string]int64

func (r recordMP4Resolver) IDForUUID(uuid string) (int64, bool) {
	id, ok := r[uuid]
	return id, ok
}

type fakeRecordMP4Indexer struct {
	calls atomic.Int32
	event recording.RecordMP4Event
	err   error
}

func (f *fakeRecordMP4Indexer) IndexRecordMP4(_ context.Context, _ int64, event recording.RecordMP4Event) (bool, error) {
	f.calls.Add(1)
	f.event = event
	return true, f.err
}

func newRecordMP4Engine(h *handler.HookController) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/index/hook/on_record_mp4", h.OnRecordMP4)
	return engine
}

func validRecordMP4Payload() gin.H {
	return gin.H{
		"mediaServerId": "node-uuid", "vhost": "__defaultVhost__", "app": "rtp", "stream": "stream-7",
		"start_time": 1784710800, "file_size": 1024, "time_len": 60.5,
		"file_path": "/record/stream-7/file.mp4", "file_name": "file.mp4",
		"folder": "/record/stream-7", "url": "record/stream-7/file.mp4",
	}
}

func TestOnRecordMP4IndexesKnownNode(t *testing.T) {
	indexer := &fakeRecordMP4Indexer{}
	h := handler.NewHookController(stream.NewNotifier())
	h.SetRecordMP4Indexer(recordMP4Resolver{"node-uuid": 2}, indexer)

	rr := postJSON(t, newRecordMP4Engine(h), "/index/hook/on_record_mp4", validRecordMP4Payload())
	require.Equal(t, http.StatusOK, rr.Code)
	require.EqualValues(t, 1, indexer.calls.Load())
	require.Equal(t, "stream-7", indexer.event.Stream)
	require.Equal(t, 60.5, indexer.event.TimeLen)
}

func TestOnRecordMP4IgnoresUnknownNode(t *testing.T) {
	indexer := &fakeRecordMP4Indexer{}
	h := handler.NewHookController(stream.NewNotifier())
	h.SetRecordMP4Indexer(recordMP4Resolver{}, indexer)

	rr := postJSON(t, newRecordMP4Engine(h), "/index/hook/on_record_mp4", validRecordMP4Payload())
	require.Equal(t, http.StatusOK, rr.Code)
	require.Zero(t, indexer.calls.Load())
}

func TestOnRecordMP4Returns5xxOnPersistenceFailure(t *testing.T) {
	indexer := &fakeRecordMP4Indexer{err: errors.New("database unavailable")}
	h := handler.NewHookController(stream.NewNotifier())
	h.SetRecordMP4Indexer(recordMP4Resolver{"node-uuid": 2}, indexer)

	rr := postJSON(t, newRecordMP4Engine(h), "/index/hook/on_record_mp4", validRecordMP4Payload())
	require.Equal(t, http.StatusInternalServerError, rr.Code)
}

func TestOnRecordMP4RejectsInvalidTiming(t *testing.T) {
	h := handler.NewHookController(stream.NewNotifier())
	h.SetRecordMP4Indexer(recordMP4Resolver{"node-uuid": 2}, &fakeRecordMP4Indexer{})
	payload := validRecordMP4Payload()
	payload["start_time"] = 0
	payload["time_len"] = -1

	rr := postJSON(t, newRecordMP4Engine(h), "/index/hook/on_record_mp4", payload)
	require.Equal(t, http.StatusBadRequest, rr.Code)
}
