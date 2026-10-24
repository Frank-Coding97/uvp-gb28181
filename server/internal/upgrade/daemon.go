package upgrade

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"uvplatform.com/uvp-gb28181/app/utils/logging"
)

const maxProtocolRequestBytes = 64 << 10

// ApplyExecutor is implemented by a deployment adapter (standalone,
// systemd, or compose). The updater daemon deliberately does not accept
// commands or paths from the wire; adapters receive only a persisted task.
type ApplyExecutor interface {
	Apply(context.Context, TaskRecord) error
	Rollback(context.Context, TaskRecord) error
}

// UpdaterControl keeps every operation over helper-owned state behind the
// local socket. Its implementation is assembled only by the privileged helper
// after it has loaded an administrator-owned registration.
type UpdaterControl interface {
	ListTasks(context.Context) ([]TaskRecord, error)
	RecentEvents(context.Context, int) ([]UpgradeEvent, error)
	PrepareTask(context.Context, PrepareTaskRequest) (TaskRecord, error)
	LoadPolicy(context.Context) (AutoUpgradePolicy, error)
	SavePolicy(context.Context, AutoUpgradePolicy, string) (AutoUpgradePolicy, error)
	Status(context.Context) (ControlStatus, error)
	Check(context.Context) (ControlCheckResult, error)
	Import(context.Context, string) (StagedArtifact, error)
	SubmitImport(context.Context, string) (ImportRecord, error)
	GetImport(context.Context, string) (ImportRecord, error)
	CreateTask(context.Context, ControlCreateTaskRequest) (TaskRecord, error)
	GetTask(context.Context, string) (TaskRecord, error)
}

// PeerAuthorizer derives the Unix peer identity from the accepted socket. A
// socket mode is only a coarse admission control; privileged helpers must
// validate the kernel-reported peer before decoding a request.
type PeerAuthorizer func(net.Conn) error

// UnixServer serves the fixed updater protocol on a local Unix socket.
type UnixServer struct {
	SocketPath string
	// SocketGroupID changes the socket's group after it is created. A negative
	// value leaves the platform default untouched, which keeps the in-process
	// test server and legacy callers compatible. Registered helpers set this to
	// the business service group so peer UID admission and filesystem group
	// permissions describe the same caller identity.
	SocketGroupID   int
	SocketGroupSet  bool
	Store           *TaskStore
	Executor        ApplyExecutor
	Gate            *GateStore
	Events          UpgradeEventSink
	Listener        net.Listener
	ShutdownTimeout time.Duration
	Control         UpdaterControl
	PeerAuthorizer  PeerAuthorizer

	mu      sync.Mutex
	running map[string]struct{}
	workers sync.WaitGroup
	closing bool
}

// CallUnix sends exactly one newline-delimited protocol request to the local
// updater and waits for its bounded response.
func CallUnix(ctx context.Context, socketPath string, request ProtocolRequest) (ProtocolResponse, error) {
	if err := request.Validate(); err != nil {
		return ProtocolResponse{}, err
	}
	if strings.TrimSpace(socketPath) == "" {
		return ProtocolResponse{}, errors.New("updater socket path is required")
	}
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return ProtocolResponse{}, fmt.Errorf("connect updater socket: %w", err)
	}
	defer conn.Close()
	deadline := time.Now().Add(10 * time.Second)
	if err := conn.SetDeadline(deadline); err != nil {
		return ProtocolResponse{}, err
	}
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return ProtocolResponse{}, fmt.Errorf("write updater request: %w", err)
	}
	var response ProtocolResponse
	decoder := json.NewDecoder(io.LimitReader(conn, maxProtocolRequestBytes+1))
	if err := decoder.Decode(&response); err != nil {
		return ProtocolResponse{}, fmt.Errorf("read updater response: %w", err)
	}
	if response.Protocol != UpdaterProtocolVersion {
		return ProtocolResponse{}, fmt.Errorf("unsupported updater response protocol %d", response.Protocol)
	}
	return response, nil
}

func (s *UnixServer) Serve(ctx context.Context) error {
	if s == nil || s.Store == nil {
		return errors.New("updater store is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(s.SocketPath) == "" {
		return errors.New("updater socket path is required")
	}
	s.mu.Lock()
	s.closing = false
	if s.running == nil {
		s.running = make(map[string]struct{})
	}
	s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.SocketPath), 0750); err != nil {
		return fmt.Errorf("create updater socket directory: %w", err)
	}
	_ = os.Remove(s.SocketPath)
	listener, err := net.Listen("unix", s.SocketPath)
	if err != nil {
		return fmt.Errorf("listen updater socket: %w", err)
	}
	s.Listener = listener
	if err := os.Chmod(s.SocketPath, 0660); err != nil {
		_ = listener.Close()
		_ = os.Remove(s.SocketPath)
		return fmt.Errorf("set updater socket permissions: %w", err)
	}
	if s.SocketGroupSet {
		if err := setSocketGroup(s.SocketPath, s.SocketGroupID); err != nil {
			_ = listener.Close()
			_ = os.Remove(s.SocketPath)
			return fmt.Errorf("set updater socket group: %w", err)
		}
	}
	stopListenerWatcher := make(chan struct{})
	defer func() {
		close(stopListenerWatcher)
		_ = listener.Close()
		_ = os.Remove(s.SocketPath)
		s.waitForWorkers()
	}()
	go func() {
		select {
		case <-ctx.Done():
			_ = listener.Close()
		case <-stopListenerWatcher:
		}
	}()
	for {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			if ctx.Err() != nil {
				return nil
			}
			return acceptErr
		}
		go s.handle(conn)
	}
}

// waitForWorkers prevents a normal helper shutdown from abandoning a worker
// in the middle of a deployment boundary. If a deployment does not finish in
// the grace period, its durable phase remains available for Recover on the
// next helper start.
func (s *UnixServer) waitForWorkers() {
	s.mu.Lock()
	s.closing = true
	s.mu.Unlock()
	done := make(chan struct{})
	go func() {
		s.workers.Wait()
		close(done)
	}()
	timeout := s.ShutdownTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	}
}

func (s *UnixServer) handle(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if s.PeerAuthorizer != nil {
		if err := s.PeerAuthorizer(conn); err != nil {
			s.writeResponse(conn, ProtocolResponse{Protocol: UpdaterProtocolVersion, Error: err.Error()})
			return
		}
	}
	reader := bufio.NewReader(io.LimitReader(conn, maxProtocolRequestBytes+1))
	line, err := reader.ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		s.writeResponse(conn, ProtocolResponse{Protocol: UpdaterProtocolVersion, Error: err.Error()})
		return
	}
	if len(line) > maxProtocolRequestBytes {
		s.writeResponse(conn, ProtocolResponse{Protocol: UpdaterProtocolVersion, Error: "protocol request exceeds size limit"})
		return
	}
	request, err := DecodeProtocolRequest(bytesTrimSpace(line))
	if err != nil {
		s.writeResponse(conn, ProtocolResponse{Protocol: UpdaterProtocolVersion, Error: err.Error()})
		return
	}
	response := s.dispatch(context.Background(), request)
	s.writeResponse(conn, response)
}

func (s *UnixServer) writeResponse(conn net.Conn, response ProtocolResponse) {
	response.Error = logging.SanitizeErrorDetail(response.Error)
	if response.Task != nil {
		response.Task.Error = logging.SanitizeErrorDetail(response.Task.Error)
	}
	for i := range response.Tasks {
		response.Tasks[i].Error = logging.SanitizeErrorDetail(response.Tasks[i].Error)
	}
	for i := range response.Events {
		response.Events[i].Reason = logging.SanitizeErrorDetail(response.Events[i].Reason)
	}
	if response.Protocol == 0 {
		response.Protocol = UpdaterProtocolVersion
	}
	_ = json.NewEncoder(conn).Encode(response)
}

func (s *UnixServer) dispatch(ctx context.Context, request ProtocolRequest) ProtocolResponse {
	response := ProtocolResponse{Protocol: UpdaterProtocolVersion}
	switch request.Action {
	case ActionStatus:
		if s.Control != nil {
			status, err := s.Control.Status(ctx)
			if err != nil {
				response.Error = err.Error()
				return response
			}
			response.OK, response.Status, response.Task = true, &status, status.ActiveTask
			return response
		}
		records, err := s.Store.List()
		if err != nil {
			response.Error = err.Error()
			return response
		}
		response.OK = true
		response.Task = activeTaskRecord(records)
		return response
	case ActionCheck:
		control, err := s.control()
		if err != nil {
			response.Error = err.Error()
			return response
		}
		checked, err := control.Check(ctx)
		if err != nil {
			response.Error = err.Error()
			return response
		}
		response.OK, response.Check = true, &checked
		return response
	case ActionImport:
		control, err := s.control()
		if err != nil {
			response.Error = err.Error()
			return response
		}
		record, err := control.SubmitImport(ctx, request.InboxToken)
		if err != nil {
			response.Error = err.Error()
			return response
		}
		response.OK, response.Import = true, &record
		return response
	case ActionGetImport:
		control, err := s.control()
		if err != nil {
			response.Error = err.Error()
			return response
		}
		record, err := control.GetImport(ctx, request.ImportID)
		if err != nil {
			response.Error = err.Error()
			return response
		}
		response.OK, response.Import = true, &record
		return response
	case ActionCreateTask:
		control, err := s.control()
		if err != nil {
			response.Error = err.Error()
			return response
		}
		record, err := control.CreateTask(ctx, ControlCreateTaskRequest{
			IdempotencyKey: request.Create.IdempotencyKey,
			Actor:          request.Create.Actor,
			TargetDigest:   request.Create.TargetDigest,
		})
		if err != nil {
			response.Error = err.Error()
			return response
		}
		response.OK, response.Task = true, &record
		return response
	case ActionGetTask:
		control, err := s.control()
		if err != nil {
			response.Error = err.Error()
			return response
		}
		record, err := control.GetTask(ctx, request.TaskID)
		if err != nil {
			response.Error = err.Error()
			return response
		}
		response.OK, response.Task = true, &record
		return response
	case ActionPrepare:
		record, err := s.Store.Get(request.TaskID)
		if err != nil {
			response.Error = err.Error()
			return response
		}
		if !strings.EqualFold(record.TargetDigest, request.TargetDigest) {
			response.Error = "task digest does not match request"
			return response
		}
		response.OK, response.Task = true, &record
		return response
	case ActionTaskList:
		control, err := s.control()
		if err != nil {
			response.Error = err.Error()
			return response
		}
		records, err := control.ListTasks(ctx)
		if err != nil {
			response.Error = err.Error()
			return response
		}
		response.OK, response.Tasks = true, records
		return response
	case ActionEvents:
		control, err := s.control()
		if err != nil {
			response.Error = err.Error()
			return response
		}
		events, err := control.RecentEvents(ctx, request.EventLimit)
		if err != nil {
			response.Error = err.Error()
			return response
		}
		response.OK, response.Events = true, events
		return response
	case ActionPrepareTask:
		control, err := s.control()
		if err != nil {
			response.Error = err.Error()
			return response
		}
		staged, err := control.Import(ctx, request.Prepare.InboxToken)
		if err != nil {
			response.Error = err.Error()
			return response
		}
		if !strings.EqualFold(staged.SHA256, request.Prepare.TargetDigest) {
			response.Error = "imported artifact digest does not match request"
			return response
		}
		record, err := control.CreateTask(ctx, ControlCreateTaskRequest{
			IdempotencyKey: request.Prepare.IdempotencyKey,
			Actor:          request.Prepare.Actor,
			TargetDigest:   request.Prepare.TargetDigest,
		})
		if err != nil {
			response.Error = err.Error()
			return response
		}
		response.OK, response.Task = true, &record
		return response
	case ActionPolicyGet:
		control, err := s.control()
		if err != nil {
			response.Error = err.Error()
			return response
		}
		policy, err := control.LoadPolicy(ctx)
		if err != nil {
			response.Error = err.Error()
			return response
		}
		response.OK, response.Policy = true, &policy
		return response
	case ActionPolicySet:
		control, err := s.control()
		if err != nil {
			response.Error = err.Error()
			return response
		}
		policy, err := control.SavePolicy(ctx, *request.Policy, request.Actor)
		if err != nil {
			response.Error = err.Error()
			return response
		}
		response.OK, response.Policy = true, &policy
		return response
	case ActionApply:
		if !request.Confirmation {
			response.Error = "apply request requires confirmation"
			return response
		}
		record, err := s.acceptApply(request.TaskID, request.TargetDigest)
		if err != nil {
			response.Error = err.Error()
			return response
		}
		response.OK = true
		updated, getErr := s.Store.Get(record.TaskID)
		if getErr == nil {
			response.Task = &updated
		} else {
			response.Task = &record
		}
		return response
	case ActionCancel:
		if s.Executor == nil {
			response.Error = "no deployment adapter is registered"
			return response
		}
		if err := s.Store.Cancel(request.TaskID); err != nil {
			response.Error = err.Error()
			return response
		}
		record, _ := s.Store.Get(request.TaskID)
		response.OK, response.Task = true, &record
		return response
	case ActionRollback:
		record, err := s.Store.Get(request.TaskID)
		if err != nil {
			response.Error = err.Error()
			return response
		}
		if record.Phase == PhaseRolledBack {
			response.OK, response.Task = true, &record
			return response
		}
		if record.Committed {
			response.Error = "committed task requires a new compatibility review before rollback"
			return response
		}
		if record.Phase != PhaseFailed && record.Phase != PhaseRecoveryRequired {
			response.Error = fmt.Sprintf("task %q is not eligible for rollback from phase %s", record.TaskID, record.Phase)
			return response
		}
		if !rollbackExecutionBoundary(record) {
			response.Error = "task has no persisted service-switch boundary; preparation failures cannot be rolled back"
			return response
		}
		if s.Executor == nil {
			response.Error = "no deployment adapter is registered"
			return response
		}
		if _, err := s.startRollback(record); err != nil {
			response.Error = err.Error()
			return response
		}
		response.OK = true
		updated, getErr := s.Store.Get(record.TaskID)
		if getErr == nil {
			response.Task = &updated
		}
		return response
	default:
		response.Error = "unsupported updater action"
		return response
	}
}

func (s *UnixServer) control() (UpdaterControl, error) {
	if s == nil || s.Control == nil {
		return nil, errors.New("privileged updater control is not registered")
	}
	return s.Control, nil
}

// acceptApply makes the durable stopping transition and worker handoff one
// admission boundary. Shutdown marks closing with the same mutex before it
// waits for workers, so it cannot leave a newly accepted task in stopping
// without either a running worker or a future recovery path.
func (s *UnixServer) acceptApply(taskID, targetDigest string) (TaskRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, err := s.Store.Get(taskID)
	if err != nil {
		return TaskRecord{}, err
	}
	if targetDigest != "" {
		if !validDigest(targetDigest) || !strings.EqualFold(record.TargetDigest, targetDigest) {
			return TaskRecord{}, errors.New("task digest does not match request")
		}
	}
	if record.Phase == PhaseSucceeded || record.Phase == PhaseRolledBack {
		return record, nil
	}
	if s.Executor == nil {
		return TaskRecord{}, errors.New("no deployment adapter is registered")
	}
	if s.closing {
		return TaskRecord{}, errors.New("updater is shutting down")
	}
	if record.Phase == PhaseAwaitingConfirmation {
		if err := s.Store.MarkPhase(record.TaskID, PhaseStopping); err != nil {
			// The helper scheduler or a concurrent request may have crossed the
			// durable boundary first. Reload it while the admission mutex is held.
			latest, getErr := s.Store.Get(record.TaskID)
			if getErr != nil || !isResumableApplyPhase(latest.Phase) {
				return TaskRecord{}, err
			}
			record = latest
		} else {
			record.Phase = PhaseStopping
		}
	} else if !isResumableApplyPhase(record.Phase) {
		return TaskRecord{}, fmt.Errorf("task %q is not awaiting confirmation or applying: %s", record.TaskID, record.Phase)
	}
	if s.running == nil {
		s.running = make(map[string]struct{})
	}
	if _, running := s.running[record.TaskID]; running {
		return record, nil
	}
	if _, err := s.startWorkerLocked(record.TaskID, func() { s.runApply(record) }); err != nil {
		return TaskRecord{}, err
	}
	return record, nil
}

func (s *UnixServer) startRollback(record TaskRecord) (bool, error) {
	return s.startWorker(record.TaskID, func() { s.runRollback(record) })
}

func (s *UnixServer) startWorker(taskID string, run func()) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startWorkerLocked(taskID, run)
}

// startWorkerLocked registers a worker before releasing the same mutex used
// by waitForWorkers to close admission. Callers must hold s.mu.
func (s *UnixServer) startWorkerLocked(taskID string, run func()) (bool, error) {
	if s.closing {
		return false, errors.New("updater is shutting down")
	}
	if s.running == nil {
		s.running = make(map[string]struct{})
	}
	if _, ok := s.running[taskID]; ok {
		return false, nil
	}
	if s.Executor == nil {
		return false, errors.New("no deployment adapter is registered")
	}
	s.running[taskID] = struct{}{}
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		defer func() {
			s.mu.Lock()
			delete(s.running, taskID)
			s.mu.Unlock()
		}()
		run()
	}()
	return true, nil
}

func (s *UnixServer) runApply(record TaskRecord) {
	var applyErr error
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				applyErr = fmt.Errorf("upgrade executor panic: %v", recovered)
			}
		}()
		// Deliberately use a process-lifetime context. The caller is only
		// asking the helper to accept the work; a closed socket must not cancel
		// a deployment after its stopping boundary has been persisted.
		applyErr = s.Executor.Apply(context.Background(), record)
	}()
	s.persistApplyResult(record.TaskID, applyErr)
}

func (s *UnixServer) runRollback(record TaskRecord) {
	var rollbackErr error
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				rollbackErr = fmt.Errorf("upgrade rollback executor panic: %v", recovered)
			}
		}()
		// Like Apply, rollback may restart a service or restore a release and
		// must remain independent from the socket that accepted the request.
		rollbackErr = s.Executor.Rollback(context.Background(), record)
	}()
	s.persistRollbackResult(record.TaskID, rollbackErr)
}

func (s *UnixServer) persistApplyResult(taskID string, applyErr error) {
	record, err := s.Store.Get(taskID)
	if err != nil {
		return
	}
	if applyErr == nil && isTerminalPhase(record.Phase) {
		return
	}
	if isTerminalPhase(record.Phase) {
		// WorkflowExecutor owns terminal diagnostics and outcome events. A
		// daemon-level error may be the only evidence for a panic in a custom
		// executor, but never overwrite an existing workflow explanation.
		if applyErr != nil && record.Error == "" {
			_ = s.Store.UpdateMetadata(taskID, func(task *TaskRecord) error {
				if task.Error == "" {
					task.Error = applyErr.Error()
				}
				return nil
			})
		}
		if record.Phase == PhaseRecoveryRequired && s.Gate != nil {
			reason := record.Error
			if reason == "" && applyErr != nil {
				reason = applyErr.Error()
			}
			_ = s.Gate.Save(GateState{Mode: GateRecovery, TaskID: taskID, BuildID: record.BuildID, Reason: reason})
		}
		return
	}
	if applyErr == nil {
		applyErr = fmt.Errorf("upgrade executor returned before terminal phase %s", record.Phase)
	}
	message := applyErr.Error()
	_ = s.Store.UpdateMetadata(taskID, func(task *TaskRecord) error {
		if task.Error == "" {
			task.Error = message
		} else if task.Error != message && !strings.Contains(task.Error, message) {
			task.Error = task.Error + "; daemon: " + message
		}
		return nil
	})
	// The executor may have stopped the service or changed data before
	// returning an error (or panicking). Marking this as ordinary failed would
	// allow a new task to start against an unknown deployment state. Keep the
	// recovery gate closed until an operator or the workflow explicitly repairs
	// it.
	if s.Gate != nil {
		if gateErr := s.Gate.Save(GateState{Mode: GateRecovery, TaskID: taskID, BuildID: record.BuildID, Reason: message}); gateErr != nil {
			_ = s.Store.UpdateMetadata(taskID, func(task *TaskRecord) error {
				if task.Error == "" {
					task.Error = fmt.Sprintf("%s; recovery gate: %v", message, gateErr)
				} else if !strings.Contains(task.Error, gateErr.Error()) {
					task.Error += "; recovery gate: " + gateErr.Error()
				}
				return nil
			})
		}
	}
	if markErr := s.Store.MarkPhase(taskID, PhaseRecoveryRequired); markErr != nil {
		_ = s.Store.UpdateMetadata(taskID, func(task *TaskRecord) error {
			if !strings.Contains(task.Error, markErr.Error()) {
				task.Error += "; persist recovery phase: " + markErr.Error()
			}
			return nil
		})
		return
	}
	record.Phase = PhaseRecoveryRequired
	s.emitRecoveryOutcome(record, message)
}

func (s *UnixServer) persistRollbackResult(taskID string, rollbackErr error) {
	record, err := s.Store.Get(taskID)
	if err != nil || record.Phase == PhaseRolledBack {
		return
	}
	if rollbackErr == nil {
		rollbackErr = fmt.Errorf("rollback executor returned before rolled back phase %s", record.Phase)
	}
	message := rollbackErr.Error()
	_ = s.Store.UpdateMetadata(taskID, func(task *TaskRecord) error {
		if task.Error == "" {
			task.Error = message
		} else if !strings.Contains(task.Error, message) {
			task.Error += "; daemon rollback: " + message
		}
		return nil
	})
	if s.Gate != nil {
		if gateErr := s.Gate.Save(GateState{Mode: GateRecovery, TaskID: taskID, BuildID: record.BuildID, Reason: message}); gateErr != nil {
			_ = s.Store.UpdateMetadata(taskID, func(task *TaskRecord) error {
				if !strings.Contains(task.Error, gateErr.Error()) {
					task.Error += "; recovery gate: " + gateErr.Error()
				}
				return nil
			})
		}
	}
	if record.Phase != PhaseRecoveryRequired {
		if markErr := s.Store.MarkPhase(taskID, PhaseRecoveryRequired); markErr != nil {
			_ = s.Store.UpdateMetadata(taskID, func(task *TaskRecord) error {
				if !strings.Contains(task.Error, markErr.Error()) {
					task.Error += "; persist recovery phase: " + markErr.Error()
				}
				return nil
			})
			return
		}
	}
	record.Phase = PhaseRecoveryRequired
	s.emitRecoveryOutcome(record, message)
}

func (s *UnixServer) emitRecoveryOutcome(record TaskRecord, reason string) {
	if s.Events == nil || record.OutcomeReported == PhaseRecoveryRequired {
		return
	}
	if err := s.Events.Append(context.Background(), UpgradeEvent{Event: "recovery_required", TaskID: record.TaskID, PolicyVersion: record.PolicyVersion, BuildID: record.BuildID, Reason: reason, At: time.Now().UTC()}); err != nil {
		_ = s.Store.UpdateMetadata(record.TaskID, func(task *TaskRecord) error {
			if !strings.Contains(task.Error, err.Error()) {
				task.Error += "; recovery event: " + err.Error()
			}
			return nil
		})
		return
	}
	if err := s.Store.UpdateMetadata(record.TaskID, func(task *TaskRecord) error {
		task.OutcomeReported = PhaseRecoveryRequired
		return nil
	}); err != nil {
		_ = s.Store.UpdateMetadata(record.TaskID, func(task *TaskRecord) error {
			if !strings.Contains(task.Error, err.Error()) {
				task.Error += "; persist recovery outcome: " + err.Error()
			}
			return nil
		})
	}
}

func activeTaskRecord(records []TaskRecord) *TaskRecord {
	for i := range records {
		if !isTerminalPhase(records[i].Phase) || records[i].Phase == PhaseRecoveryRequired {
			return &records[i]
		}
	}
	return nil
}

func bytesTrimSpace(data []byte) []byte {
	return []byte(strings.TrimSpace(string(data)))
}
