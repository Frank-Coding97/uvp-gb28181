package handler_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/handler"
	"uvplatform.com/uvp-gb28181/app/gb28181/stream"
)

func TestHookDynamicNoneReaderUsesGenerationAndConsumerAwareStop(t *testing.T) {
	stopper := &mockGenerationStopper{current: stream.LiveRef{StreamID: "0200000007", SSRC: "0200000007", Generation: 7, NodeID: 22}}
	h := handler.NewHookController(stream.NewNotifier())
	h.SetPlayStopper(stopper)
	h.SetNoneReaderPolicy(&mockNoneReaderPolicy{close: true})
	response := postJSON(t, newHookEngine(t, h), "/index/hook/on_stream_none_reader", gin.H{"app": "rtp", "stream": "0200000007"})
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Equal(t, false, body["close"])
	require.Eventually(t, func() bool { return stopper.noneReaderCalls.Load() == 1 }, time.Second, time.Millisecond)
	require.Zero(t, stopper.calls.Load(), "dynamic sources must not bypass the consumer-aware conditional stop")
}

func TestHookDynamicNoneReaderDuringStartDoesNotQueueBareStop(t *testing.T) {
	stopper := &mockGenerationStopper{}
	h := handler.NewHookController(stream.NewNotifier())
	h.SetPlayStopper(stopper)
	h.SetNoneReaderPolicy(&mockNoneReaderPolicy{close: true})
	response := postJSON(t, newHookEngine(t, h), "/index/hook/on_stream_none_reader", gin.H{"app": "rtp", "stream": "0200000007"})
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Equal(t, false, body["close"])
	require.Never(t, func() bool { return stopper.calls.Load() != 0 }, 50*time.Millisecond, time.Millisecond)
	require.Zero(t, stopper.calls.Load())
	require.Zero(t, stopper.noneReaderCalls.Load())
}

func TestHookPlaybackNoneReaderKeepsExistingPlaybackCleanup(t *testing.T) {
	stopper := &mockGenerationStopper{}
	h := handler.NewHookController(stream.NewNotifier())
	h.SetPlayStopper(stopper)
	h.SetNoneReaderPolicy(&mockNoneReaderPolicy{close: true})
	postJSON(t, newHookEngine(t, h), "/index/hook/on_stream_none_reader", gin.H{"app": "rtp", "stream": "pb-device-channel"})
	require.Eventually(t, func() bool { return stopper.calls.Load() == 1 }, time.Second, time.Millisecond)
	require.Zero(t, stopper.noneReaderCalls.Load())
}
