package upgrade

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type daemonApplyExecutor struct {
	store       *TaskStore
	started     chan struct{}
	release     chan struct{}
	err         error
	markSuccess bool
	calls       atomic.Int32
}

var daemonSocketCounter atomic.Uint64

func daemonSocketPath() string {
	return filepath.Join(os.TempDir(), "uvp-u-"+strconv.FormatUint(daemonSocketCounter.Add(1), 10)+".sock")
}

func (f *daemonApplyExecutor) Apply(_ context.Context, record TaskRecord) error {
	f.calls.Add(1)
	select {
	case f.started <- struct{}{}:
	default:
	}
	if f.release != nil {
		<-f.release
	}
	if f.err != nil {
		return f.err
	}
	if f.markSuccess {
		for _, phase := range []TaskPhase{PhaseBackingUp, PhaseMigrating, PhaseActivating, PhaseValidating, PhaseCommitting, PhaseSucceeded} {
			if err := f.store.MarkPhase(record.TaskID, phase); err != nil {
				return err
			}
		}
	}
	return nil
}

func (f *daemonApplyExecutor) Rollback(context.Context, TaskRecord) error { return nil }

type terminalDaemonExecutor struct {
	store  *TaskStore
	err    error
	phase  TaskPhase
	detail string
}

func (f *terminalDaemonExecutor) Apply(_ context.Context, record TaskRecord) error {
	if err := f.store.UpdateMetadata(record.TaskID, func(task *TaskRecord) error {
		task.Error = f.detail
		return nil
	}); err != nil {
		return err
	}
	phase := f.phase
	if phase == "" {
		phase = PhaseRolledBack
	}
	if err := f.store.MarkPhase(record.TaskID, phase); err != nil {
		return err
	}
	return f.err
}

func (f *terminalDaemonExecutor) Rollback(context.Context, TaskRecord) error { return nil }

type daemonRollbackExecutor struct {
	store       *TaskStore
	started     chan struct{}
	release     chan struct{}
	err         error
	markSuccess bool
	calls       atomic.Int32
}

func (f *daemonRollbackExecutor) Apply(context.Context, TaskRecord) error { return nil }

func (f *daemonRollbackExecutor) Rollback(_ context.Context, record TaskRecord) error {
	f.calls.Add(1)
	select {
	case f.started <- struct{}{}:
	default:
	}
	if f.release != nil {
		<-f.release
	}
	if f.err != nil {
		return f.err
	}
	if f.markSuccess {
		return f.store.MarkPhase(record.TaskID, PhaseRolledBack)
	}
	return nil
}

type daemonEvents struct {
	mu     sync.Mutex
	events []UpgradeEvent
}

func (s *daemonEvents) Append(_ context.Context, event UpgradeEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
	return nil
}

func (s *daemonEvents) Snapshot() []UpgradeEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]UpgradeEvent(nil), s.events...)
}

func daemonTask(t *testing.T, store *TaskStore) TaskRecord {
	t.Helper()
	record, err := store.Submit(TaskRequest{
		IdempotencyKey: "daemon-test",
		Actor:          "admin",
		TargetDigest:   strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []TaskPhase{PhaseVerifying, PhasePreparing, PhaseAwaitingConfirmation} {
		if err := store.MarkPhase(record.TaskID, phase); err != nil {
			t.Fatal(err)
		}
	}
	record.Phase = PhaseAwaitingConfirmation
	return record
}

func failedDaemonTask(t *testing.T, store *TaskStore) TaskRecord {
	t.Helper()
	record := daemonTask(t, store)
	if err := store.MarkPhase(record.TaskID, PhaseStopping); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkPhase(record.TaskID, PhaseFailed); err != nil {
		t.Fatal(err)
	}
	record.Phase = PhaseFailed
	// A daemon rollback request is only safe after a durable service-switch
	// boundary. This fixture models a failure while stopping the old service;
	// preparation failures are intentionally rejected by the rollback gate.
	if err := store.UpdateMetadata(record.TaskID, func(task *TaskRecord) error {
		task.FailurePhase = PhaseStopping
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	record.FailurePhase = PhaseStopping
	return record
}

func startUnixServer(t *testing.T, server *UnixServer) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()
	deadline := time.Now().Add(time.Second)
	for {
		if conn, err := net.DialTimeout("unix", server.SocketPath, 10*time.Millisecond); err == nil {
			_ = conn.Close()
			return cancel, done
		}
		select {
		case serveErr := <-done:
			t.Fatalf("updater server stopped before socket started: %v", serveErr)
		default:
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("updater socket did not start")
		}
		time.Sleep(time.Millisecond)
	}
}

func waitForTask(t *testing.T, store *TaskStore, taskID string, want func(TaskRecord) bool) TaskRecord {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		record, err := store.Get(taskID)
		if err == nil && want(record) {
			return record
		}
		if time.Now().After(deadline) {
			t.Fatalf("task did not reach expected state: last=%#v err=%v", record, err)
		}
		time.Sleep(time.Millisecond)
	}
}

func waitForGate(t *testing.T, gate *GateStore, want func(GateState) bool) GateState {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		state, err := gate.Load()
		if err == nil && want(state) {
			return state
		}
		if time.Now().After(deadline) {
			t.Fatalf("gate did not reach expected state: last=%#v err=%v", state, err)
		}
		time.Sleep(time.Millisecond)
	}
}

func applyRequest(record TaskRecord) ProtocolRequest {
	return ProtocolRequest{Protocol: UpdaterProtocolVersion, Action: ActionApply, TaskID: record.TaskID, TargetDigest: record.TargetDigest, Confirmation: true}
}

func rollbackRequest(record TaskRecord) ProtocolRequest {
	return ProtocolRequest{Protocol: UpdaterProtocolVersion, Action: ActionRollback, TaskID: record.TaskID}
}

func readUnixResponse(t *testing.T, socket string, request ProtocolRequest) ProtocolResponse {
	t.Helper()
	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		t.Fatal(err)
	}
	var response ProtocolResponse
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&response); err != nil {
		t.Fatal(err)
	}
	return response
}

func TestUnixServerRejectsApplyWithoutAdapter(t *testing.T) {
	root := t.TempDir()
	store, err := OpenTaskStore(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.Submit(TaskRequest{IdempotencyKey: "k", Actor: "admin", TargetDigest: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"})
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []TaskPhase{PhaseVerifying, PhasePreparing, PhaseAwaitingConfirmation} {
		if err := store.MarkPhase(record.TaskID, phase); err != nil {
			t.Fatal(err)
		}
	}
	socket := filepath.Join(os.TempDir(), "uvp-updater-test.sock")
	_ = os.Remove(socket)
	defer os.Remove(socket)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := &UnixServer{SocketPath: socket, Store: store}
	errCh := make(chan error, 1)
	go func() { errCh <- server.Serve(ctx) }()
	deadline := time.Now().Add(time.Second)
	for {
		conn, dialErr := net.Dial("unix", socket)
		if dialErr == nil {
			request := ProtocolRequest{Protocol: UpdaterProtocolVersion, Action: ActionApply, TaskID: record.TaskID, Confirmation: true}
			if err := json.NewEncoder(conn).Encode(request); err != nil {
				t.Fatal(err)
			}
			response := ProtocolResponse{}
			if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&response); err != nil {
				t.Fatal(err)
			}
			_ = conn.Close()
			if response.OK || response.Error == "" {
				t.Fatalf("expected safe apply rejection: %#v", response)
			}
			break
		}
		if time.Now().After(deadline) {
			select {
			case serveErr := <-errCh:
				t.Fatalf("updater server stopped: %v", serveErr)
			default:
			}
			t.Fatal("updater socket did not start")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-errCh:
	case <-time.After(time.Second):
		t.Fatal("updater server did not stop")
	}
}

func TestUnixServerRejectsApplyDuringShutdownBeforeStoppingBoundary(t *testing.T) {
	root := t.TempDir()
	store, err := OpenTaskStore(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	record := daemonTask(t, store)
	executor := &daemonApplyExecutor{store: store, started: make(chan struct{}, 1)}
	server := &UnixServer{Store: store, Executor: executor}
	server.mu.Lock()
	server.closing = true
	server.mu.Unlock()

	response := server.dispatch(context.Background(), applyRequest(record))
	if response.OK || !strings.Contains(response.Error, "shutting down") {
		t.Fatalf("apply response = %#v", response)
	}
	updated, err := store.Get(record.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Phase != PhaseAwaitingConfirmation {
		t.Fatalf("shutdown rejection crossed stopping boundary: %#v", updated)
	}
	if calls := executor.calls.Load(); calls != 0 {
		t.Fatalf("shutdown rejection started %d apply workers", calls)
	}
}

func TestUnixServerRejectsUnknownProtocolFields(t *testing.T) {
	root := t.TempDir()
	store, err := OpenTaskStore(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(os.TempDir(), "uvp-updater-test.sock")
	_ = os.Remove(socket)
	defer os.Remove(socket)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := &UnixServer{SocketPath: socket, Store: store}
	errCh := make(chan error, 1)
	go func() { errCh <- server.Serve(ctx) }()
	deadline := time.Now().Add(time.Second)
	for {
		conn, dialErr := net.Dial("unix", socket)
		if dialErr == nil {
			_, _ = conn.Write([]byte(`{"protocol":1,"action":"status","shell":"id"}` + "\n"))
			response := ProtocolResponse{}
			if err := json.NewDecoder(conn).Decode(&response); err != nil {
				t.Fatal(err)
			}
			_ = conn.Close()
			if response.OK || response.Error == "" {
				t.Fatalf("expected unknown field rejection: %#v", response)
			}
			return
		}
		if time.Now().After(deadline) {
			select {
			case serveErr := <-errCh:
				t.Fatalf("updater server stopped: %v", serveErr)
			default:
			}
			t.Fatal("updater socket did not start")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestUnixServerApplyAcknowledgesBeforeLongExecutionAndSurvivesDisconnect(t *testing.T) {
	root := t.TempDir()
	store, err := OpenTaskStore(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	record := daemonTask(t, store)
	executor := &daemonApplyExecutor{store: store, started: make(chan struct{}, 1), release: make(chan struct{}), markSuccess: true}
	server := &UnixServer{SocketPath: daemonSocketPath(), Store: store, Executor: executor}
	cancel, done := startUnixServer(t, server)

	conn, err := net.Dial("unix", server.SocketPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(conn).Encode(applyRequest(record)); err != nil {
		t.Fatal(err)
	}
	var response ProtocolResponse
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&response); err != nil {
		t.Fatal("apply acknowledgement was not returned promptly:", err)
	}
	if !response.OK || response.Task == nil || response.Task.Phase != PhaseStopping {
		t.Fatalf("apply response = %#v", response)
	}
	_ = conn.Close()
	select {
	case <-executor.started:
	case <-time.After(time.Second):
		t.Fatal("background apply did not start")
	}

	close(executor.release)
	waitForTask(t, store, record.TaskID, func(task TaskRecord) bool { return task.Phase == PhaseSucceeded })
	if got := executor.calls.Load(); got != 1 {
		t.Fatalf("apply calls = %d, want 1", got)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("updater server did not stop")
	}
}

func TestUnixServerApplyRetryIsIdempotentWhileRunningAndAfterSuccess(t *testing.T) {
	root := t.TempDir()
	store, err := OpenTaskStore(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	record := daemonTask(t, store)
	executor := &daemonApplyExecutor{store: store, started: make(chan struct{}, 1), release: make(chan struct{}), markSuccess: true}
	server := &UnixServer{SocketPath: daemonSocketPath(), Store: store, Executor: executor}
	cancel, done := startUnixServer(t, server)
	first := readUnixResponse(t, server.SocketPath, applyRequest(record))
	if !first.OK || first.Task == nil || first.Task.Phase != PhaseStopping {
		t.Fatalf("first apply response = %#v", first)
	}
	select {
	case <-executor.started:
	case <-time.After(time.Second):
		t.Fatal("background apply did not start")
	}
	second := readUnixResponse(t, server.SocketPath, applyRequest(record))
	if !second.OK || second.Task == nil || second.Task.Phase != PhaseStopping {
		t.Fatalf("retry apply response = %#v", second)
	}
	if got := executor.calls.Load(); got != 1 {
		t.Fatalf("retry started %d apply calls, want 1", got)
	}
	close(executor.release)
	waitForTask(t, store, record.TaskID, func(task TaskRecord) bool { return task.Phase == PhaseSucceeded })
	third := readUnixResponse(t, server.SocketPath, applyRequest(record))
	if !third.OK || third.Task == nil || third.Task.Phase != PhaseSucceeded {
		t.Fatalf("terminal retry response = %#v", third)
	}
	if got := executor.calls.Load(); got != 1 {
		t.Fatalf("terminal retry started %d apply calls, want 1", got)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("updater server did not stop")
	}
}

func TestUnixServerApplyPersistsBackgroundExecutionError(t *testing.T) {
	root := t.TempDir()
	store, err := OpenTaskStore(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	record := daemonTask(t, store)
	executor := &daemonApplyExecutor{store: store, started: make(chan struct{}, 1), err: errors.New("migration failed")}
	gate, err := OpenGateStore(filepath.Join(root, "state", "gate.json"))
	if err != nil {
		t.Fatal(err)
	}
	events := &daemonEvents{}
	server := &UnixServer{SocketPath: daemonSocketPath(), Store: store, Executor: executor, Gate: gate, Events: events}
	cancel, done := startUnixServer(t, server)
	response := readUnixResponse(t, server.SocketPath, applyRequest(record))
	if !response.OK || response.Task == nil || response.Task.Phase != PhaseStopping {
		t.Fatalf("apply response = %#v", response)
	}
	// The outcome event is persisted after the recovery phase. Wait for that
	// boundary before asserting event delivery and its durable marker.
	waitForTask(t, store, record.TaskID, func(task TaskRecord) bool {
		return task.Phase == PhaseRecoveryRequired && task.OutcomeReported == PhaseRecoveryRequired
	})
	failed, err := store.Get(record.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(failed.Error, "migration failed") {
		t.Fatalf("task error = %q", failed.Error)
	}
	if failed.OutcomeReported != PhaseRecoveryRequired {
		t.Fatalf("recovery outcome marker = %s", failed.OutcomeReported)
	}
	gateState, err := gate.Load()
	if err != nil {
		t.Fatal(err)
	}
	if gateState.Mode != GateRecovery || gateState.TaskID != record.TaskID || !strings.Contains(gateState.Reason, "migration failed") {
		t.Fatalf("recovery gate = %#v", gateState)
	}
	recoveryEvents := events.Snapshot()
	if len(recoveryEvents) != 1 || recoveryEvents[0].Event != "recovery_required" || recoveryEvents[0].TaskID != record.TaskID {
		t.Fatalf("recovery events = %#v", recoveryEvents)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("updater server did not stop")
	}
}

func TestUnixServerApplyKeepsWorkflowTerminalError(t *testing.T) {
	root := t.TempDir()
	store, err := OpenTaskStore(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	record := daemonTask(t, store)
	executor := &terminalDaemonExecutor{store: store, err: errors.New("outer executor error"), detail: "workflow rollback detail"}
	server := &UnixServer{SocketPath: daemonSocketPath(), Store: store, Executor: executor}
	cancel, done := startUnixServer(t, server)
	response := readUnixResponse(t, server.SocketPath, applyRequest(record))
	if !response.OK {
		t.Fatalf("apply response = %#v", response)
	}
	finished := waitForTask(t, store, record.TaskID, func(task TaskRecord) bool { return task.Phase == PhaseRolledBack })
	if finished.Error != "workflow rollback detail" {
		t.Fatalf("terminal error = %q", finished.Error)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("updater server did not stop")
	}
}

func TestUnixServerApplyRestoresRecoveryGateForTerminalExecutorOutcome(t *testing.T) {
	root := t.TempDir()
	store, err := OpenTaskStore(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	record := daemonTask(t, store)
	gate, err := OpenGateStore(filepath.Join(root, "state", "gate.json"))
	if err != nil {
		t.Fatal(err)
	}
	executor := &terminalDaemonExecutor{store: store, err: errors.New("executor returned"), phase: PhaseRecoveryRequired, detail: "workflow recovery detail"}
	server := &UnixServer{SocketPath: daemonSocketPath(), Store: store, Executor: executor, Gate: gate}
	cancel, done := startUnixServer(t, server)
	response := readUnixResponse(t, server.SocketPath, applyRequest(record))
	if !response.OK {
		t.Fatalf("apply response = %#v", response)
	}
	waitForTask(t, store, record.TaskID, func(task TaskRecord) bool { return task.Phase == PhaseRecoveryRequired })
	state := waitForGate(t, gate, func(state GateState) bool { return state.Mode == GateRecovery && state.TaskID == record.TaskID })
	if state.Mode != GateRecovery || state.TaskID != record.TaskID || state.Reason != "workflow recovery detail" {
		t.Fatalf("recovery gate = %#v", state)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("updater server did not stop")
	}
}

func TestUnixServerApplyRejectsMismatchedDigest(t *testing.T) {
	root := t.TempDir()
	store, err := OpenTaskStore(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	record := daemonTask(t, store)
	executor := &daemonApplyExecutor{store: store, started: make(chan struct{}, 1)}
	server := &UnixServer{SocketPath: daemonSocketPath(), Store: store, Executor: executor}
	cancel, done := startUnixServer(t, server)
	request := applyRequest(record)
	request.TargetDigest = strings.Repeat("b", 64)
	response := readUnixResponse(t, server.SocketPath, request)
	if response.OK || !strings.Contains(response.Error, "digest") {
		t.Fatalf("mismatched digest response = %#v", response)
	}
	if got := executor.calls.Load(); got != 0 {
		t.Fatalf("mismatched digest launched %d calls", got)
	}
	current, err := store.Get(record.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Phase != PhaseAwaitingConfirmation {
		t.Fatalf("mismatched digest changed phase to %s", current.Phase)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("updater server did not stop")
	}
}

func TestUnixServerRollbackAcknowledgesBeforeLongExecutionAndIsIdempotent(t *testing.T) {
	root := t.TempDir()
	store, err := OpenTaskStore(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	record := failedDaemonTask(t, store)
	executor := &daemonRollbackExecutor{store: store, started: make(chan struct{}, 1), release: make(chan struct{}), markSuccess: true}
	server := &UnixServer{SocketPath: daemonSocketPath(), Store: store, Executor: executor}
	cancel, done := startUnixServer(t, server)
	first := readUnixResponse(t, server.SocketPath, rollbackRequest(record))
	if !first.OK || first.Task == nil || first.Task.Phase != PhaseFailed {
		t.Fatalf("rollback response = %#v", first)
	}
	select {
	case <-executor.started:
	case <-time.After(time.Second):
		t.Fatal("background rollback did not start")
	}
	second := readUnixResponse(t, server.SocketPath, rollbackRequest(record))
	if !second.OK || second.Task == nil || second.Task.Phase != PhaseFailed {
		t.Fatalf("retry rollback response = %#v", second)
	}
	if got := executor.calls.Load(); got != 1 {
		t.Fatalf("retry started %d rollback calls, want 1", got)
	}
	close(executor.release)
	waitForTask(t, store, record.TaskID, func(task TaskRecord) bool { return task.Phase == PhaseRolledBack })
	third := readUnixResponse(t, server.SocketPath, rollbackRequest(record))
	if !third.OK || third.Task == nil || third.Task.Phase != PhaseRolledBack {
		t.Fatalf("terminal rollback response = %#v", third)
	}
	if got := executor.calls.Load(); got != 1 {
		t.Fatalf("terminal retry started %d rollback calls, want 1", got)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("updater server did not stop")
	}
}

func TestUnixServerRollbackRejectsIneligibleOrCommittedTask(t *testing.T) {
	root := t.TempDir()
	store, err := OpenTaskStore(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	record := daemonTask(t, store)
	executor := &daemonRollbackExecutor{store: store, started: make(chan struct{}, 1)}
	server := &UnixServer{SocketPath: daemonSocketPath(), Store: store, Executor: executor}
	cancel, done := startUnixServer(t, server)
	response := readUnixResponse(t, server.SocketPath, rollbackRequest(record))
	if response.OK || !strings.Contains(response.Error, "not eligible") {
		t.Fatalf("awaiting-confirmation rollback response = %#v", response)
	}
	if err := store.MarkPhase(record.TaskID, PhaseStopping); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkPhase(record.TaskID, PhaseBackingUp); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkPhase(record.TaskID, PhaseMigrating); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkPhase(record.TaskID, PhaseActivating); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkPhase(record.TaskID, PhaseValidating); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkPhase(record.TaskID, PhaseCommitting); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkPhase(record.TaskID, PhaseFailed); err != nil {
		t.Fatal(err)
	}
	response = readUnixResponse(t, server.SocketPath, rollbackRequest(record))
	if response.OK || !strings.Contains(response.Error, "committed") {
		t.Fatalf("committed rollback response = %#v", response)
	}
	if got := executor.calls.Load(); got != 0 {
		t.Fatalf("ineligible rollback launched %d calls", got)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("updater server did not stop")
	}
}

func TestUnixServerRollbackPersistsBackgroundFailure(t *testing.T) {
	root := t.TempDir()
	store, err := OpenTaskStore(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	record := failedDaemonTask(t, store)
	gate, err := OpenGateStore(filepath.Join(root, "state", "gate.json"))
	if err != nil {
		t.Fatal(err)
	}
	executor := &daemonRollbackExecutor{store: store, started: make(chan struct{}, 1), err: errors.New("restore unavailable")}
	server := &UnixServer{SocketPath: daemonSocketPath(), Store: store, Executor: executor, Gate: gate}
	cancel, done := startUnixServer(t, server)
	response := readUnixResponse(t, server.SocketPath, rollbackRequest(record))
	if !response.OK {
		t.Fatalf("rollback response = %#v", response)
	}
	updated := waitForTask(t, store, record.TaskID, func(task TaskRecord) bool {
		return task.Phase == PhaseRecoveryRequired && strings.Contains(task.Error, "restore unavailable")
	})
	if !strings.Contains(updated.Error, "restore unavailable") {
		t.Fatalf("rollback error = %q", updated.Error)
	}
	state, err := gate.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.Mode != GateRecovery || state.TaskID != record.TaskID {
		t.Fatalf("rollback recovery gate = %#v", state)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("updater server did not stop")
	}
}

func TestUnixServerShutdownWaitsForApplyWorker(t *testing.T) {
	root := t.TempDir()
	store, err := OpenTaskStore(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	record := daemonTask(t, store)
	executor := &daemonApplyExecutor{store: store, started: make(chan struct{}, 1), release: make(chan struct{}), markSuccess: true}
	server := &UnixServer{SocketPath: daemonSocketPath(), Store: store, Executor: executor, ShutdownTimeout: time.Second}
	cancel, done := startUnixServer(t, server)
	response := readUnixResponse(t, server.SocketPath, applyRequest(record))
	if !response.OK {
		t.Fatalf("apply response = %#v", response)
	}
	select {
	case <-executor.started:
	case <-time.After(time.Second):
		t.Fatal("background apply did not start")
	}
	cancel()
	select {
	case <-done:
		t.Fatal("server returned before apply worker finished")
	case <-time.After(25 * time.Millisecond):
	}
	close(executor.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not wait for apply worker")
	}
	updated, err := store.Get(record.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Phase != PhaseSucceeded {
		t.Fatalf("shutdown task phase = %s", updated.Phase)
	}
}
