package controllers_test

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/scheduler"
)

type schedulerSwitchRound struct {
	firstEntered  chan struct{}
	secondEntered chan struct{}
	releaseFirst  chan struct{}
	releaseSecond chan struct{}
}

type barrierSettingWriter struct {
	mu        sync.Mutex
	call      int
	algorithm string
	round     *schedulerSwitchRound
}

func (w *barrierSettingWriter) beginRound(round *schedulerSwitchRound) {
	w.mu.Lock()
	w.call = 0
	w.round = round
	w.mu.Unlock()
}

func (w *barrierSettingWriter) UpdateAlgorithm(_ context.Context, name string) error {
	w.mu.Lock()
	w.call++
	call := w.call
	round := w.round
	w.mu.Unlock()

	switch call {
	case 1:
		close(round.firstEntered)
		<-round.releaseFirst
		w.mu.Lock()
		w.algorithm = name
		w.mu.Unlock()
		return errors.New("first write failed")
	case 2:
		close(round.secondEntered)
		<-round.releaseSecond
		w.mu.Lock()
		w.algorithm = name
		w.mu.Unlock()
		return nil
	default:
		w.mu.Lock()
		w.algorithm = name
		w.mu.Unlock()
		return nil
	}
}

func (w *barrierSettingWriter) snapshot() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.algorithm
}

func TestSchedulerControllerT13_SwitchReturnsNextInviteAndCompensates(t *testing.T) {
	r, mgr, setting, _ := setupSchedulerRouter(t)
	w, resp := do(t, r, "PUT", "/api/gb28181/zlm/scheduler", map[string]any{"algorithm": "weighted"})
	require.Equal(t, http.StatusOK, w.Code)
	data := resp["data"].(map[string]any)
	require.Equal(t, "weighted", data["algorithm"])
	require.Equal(t, "next_invite", data["effectiveFrom"])

	setting.err = errors.New("db unavailable")
	w, _ = do(t, r, "PUT", "/api/gb28181/zlm/scheduler", map[string]any{"algorithm": "leastload"})
	require.NotEqual(t, http.StatusInternalServerError, w.Code)
	require.Equal(t, "weighted", mgr.CurrentName(), "DB failure must compensate the in-memory switch")
	require.Equal(t, 2, setting.calls)
}

func TestSchedulerControllerT13_RejectsInvalidLogFilters(t *testing.T) {
	r, _, _, _ := setupSchedulerRouter(t)
	for _, path := range []string{
		"/api/gb28181/zlm/scheduler/logs?from=not-time",
		"/api/gb28181/zlm/scheduler/logs?result=unknown",
		"/api/gb28181/zlm/scheduler/logs?limit=0",
		"/api/gb28181/zlm/scheduler/logs?limit=1001",
		"/api/gb28181/zlm/scheduler/logs?limit=",
		"/api/gb28181/zlm/scheduler/logs?from=",
		"/api/gb28181/zlm/scheduler/logs?algorithm=unknown",
		"/api/gb28181/zlm/scheduler/logs?unknown=x",
	} {
		w, resp := do(t, r, "GET", path, nil)
		require.NotEqual(t, http.StatusInternalServerError, w.Code, path)
		require.Equal(t, float64(1), resp["code"], path)
	}
}

func TestSchedulerControllerT13_FiltersLogsByTypedPredicates(t *testing.T) {
	r, _, _, logRepo := setupSchedulerRouter(t)
	base := time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC)
	logRepo.rows = []scheduler.SchedulerLog{
		{ID: 1, HappenedAt: base, Algorithm: "roundrobin", NodeID: 1, StreamID: "s1"},
		{ID: 2, HappenedAt: base.Add(time.Minute), Algorithm: "roundrobin", NodeID: 1, StreamID: "s2", ErrorMessage: "no active"},
		{ID: 3, HappenedAt: base.Add(2 * time.Minute), Algorithm: "weighted", NodeID: 2, StreamID: "s1"},
	}
	from := base.Add(-time.Second).Format(time.RFC3339)
	to := base.Add(time.Minute + time.Second).Format(time.RFC3339)
	w, resp := do(t, r, "GET", "/api/gb28181/zlm/scheduler/logs?from="+from+"&to="+to+"&nodeId=1&algorithm=roundrobin&result=success&streamId=s1&limit=1", nil)
	require.Equal(t, http.StatusOK, w.Code)
	data := resp["data"].(map[string]any)
	require.Equal(t, float64(1), data["limit"])
	rows := data["list"].([]any)
	require.Len(t, rows, 1)
	require.Equal(t, float64(1), rows[0].(map[string]any)["id"])
}

func TestSchedulerControllerT13_SwitchBarrierPreventsStaleCompensation(t *testing.T) {
	setting := &barrierSettingWriter{}
	r, mgr, _ := setupSchedulerRouterWithSetting(t, setting)

	for roundNumber := 0; roundNumber < 100; roundNumber++ {
		require.NoError(t, mgr.Switch("roundrobin"))
		round := &schedulerSwitchRound{
			firstEntered:  make(chan struct{}),
			secondEntered: make(chan struct{}),
			releaseFirst:  make(chan struct{}),
			releaseSecond: make(chan struct{}),
		}
		setting.beginRound(round)

		firstCode := make(chan int, 1)
		secondCode := make(chan int, 1)
		go func() {
			w, _ := do(t, r, "PUT", "/api/gb28181/zlm/scheduler", map[string]any{"algorithm": "weighted"})
			firstCode <- w.Code
		}()
		select {
		case <-round.firstEntered:
		case <-time.After(2 * time.Second):
			t.Fatalf("round %d: first DB write did not reach barrier", roundNumber)
		}

		go func() {
			w, _ := do(t, r, "PUT", "/api/gb28181/zlm/scheduler", map[string]any{"algorithm": "leastload"})
			secondCode <- w.Code
		}()

		secondAlreadyEntered := false
		select {
		case <-round.secondEntered:
			secondAlreadyEntered = true
		case <-time.After(20 * time.Millisecond):
		}
		close(round.releaseFirst)
		if !secondAlreadyEntered {
			select {
			case <-round.secondEntered:
			case <-time.After(2 * time.Second):
				t.Fatalf("round %d: second DB write did not reach barrier", roundNumber)
			}
		}
		close(round.releaseSecond)

		require.NotEqual(t, http.StatusInternalServerError, <-firstCode, "round %d", roundNumber)
		require.Equal(t, http.StatusOK, <-secondCode, "round %d", roundNumber)
		require.Equal(t, "leastload", mgr.CurrentName(), "round %d: stale compensation overwrote newer switch", roundNumber)
		require.Equal(t, "leastload", setting.snapshot(), "round %d: DB algorithm must match Manager", roundNumber)
	}
}
