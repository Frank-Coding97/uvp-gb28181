package upgrade

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHelperAcceptedTaskFinishesAfterRequestAndPersistsOneOutcome(t *testing.T) {
	root := t.TempDir()
	store, err := OpenTaskStore(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	record := daemonTask(t, store)
	events, err := OpenFileEventLog(filepath.Join(root, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	adapter := &blockingWorkflowAdapter{entered: make(chan struct{}, 1), release: make(chan struct{})}
	workflow := &WorkflowExecutor{Store: store, Adapter: adapter, Events: events}
	socketRoot, err := os.MkdirTemp("", "uvp-int-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(socketRoot)
	server := &UnixServer{SocketPath: filepath.Join(socketRoot, "helper.sock"), Store: store, Executor: workflow}
	cancel, done := startUnixServer(t, server)
	defer func() { cancel(); <-done }()
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	response, err := CallUnix(requestCtx, server.SocketPath, ProtocolRequest{Protocol: 1, Action: ActionApply, TaskID: record.TaskID, Confirmation: true})
	if err != nil || !response.OK {
		t.Fatalf("accept: %#v %v", response, err)
	}
	cancelRequest()
	select {
	case <-adapter.entered:
	case <-time.After(time.Second):
		t.Fatal("workflow not started")
	}
	if adapter.stopCalls.Load() != 1 {
		t.Fatal("task not started exactly once")
	}
	close(adapter.release)
	waitForTask(t, store, record.TaskID, func(task TaskRecord) bool {
		return task.Phase == PhaseSucceeded && task.OutcomeReported == PhaseSucceeded
	})
	// Retrying the same accepted request and restarting recovery must not execute
	// again or emit duplicate terminal events.
	response, err = CallUnix(context.Background(), server.SocketPath, ProtocolRequest{Protocol: 1, Action: ActionApply, TaskID: record.TaskID, Confirmation: true})
	if err != nil || !response.OK {
		t.Fatalf("retry: %#v %v", response, err)
	}
	if err := workflow.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	outcomes, err := events.Recent(100)
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 1 || outcomes[0].Event != "succeeded" {
		t.Fatalf("outcomes: %#v", outcomes)
	}
	if adapter.stopCalls.Load() != 1 {
		t.Fatal("duplicate service stop")
	}
}

func TestHelperRechecksRevokedAutomaticPolicyAfterAcceptingTask(t *testing.T) {
	root := t.TempDir()
	store, record := prepareWorkflowTask(t)
	policy := AutoUpgradePolicy{Enabled: true, PolicyVersion: 1, Timezone: "UTC", WindowStart: "02:00", WindowEnd: "04:00", Channel: "stable", AllowRestart: true}
	preflight := automaticPreflightForTest()
	if err := store.UpdateMetadata(record.TaskID, func(task *TaskRecord) error {
		task.Actor, task.PolicySnapshot, task.PolicyVersion = "policy", &policy, 1
		task.Version = "1.2.0"
		task.BuildID = "build-1"
		task.Preflight = &preflight
		task.Compatibility = automaticCompatibilityForTest()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	policies, err := OpenPolicyStore(filepath.Join(root, "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	policy.Enabled = false
	if err := policies.Save(policy); err != nil {
		t.Fatal(err)
	}
	adapter := &fakeWorkflowAdapter{}
	events, err := OpenFileEventLog(filepath.Join(root, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := &WorkflowExecutor{
		Store: store, Adapter: adapter, PolicyStore: policies, Events: events,
		Preflight: func(context.Context) (PreflightSnapshot, error) { return preflight, nil },
	}
	if err := workflow.Recover(context.Background()); err == nil {
		t.Fatal("revoked automatic approval recovered as valid")
	}
	updated, err := store.Get(record.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Phase != PhaseFailed || !strings.Contains(updated.Error, "expired") || len(adapter.steps) != 0 {
		t.Fatalf("task: %#v, steps %v", updated, adapter.steps)
	}
	outcomes, err := events.Recent(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 1 || outcomes[0].Reason == "" {
		t.Fatalf("failure audit missing reason: %#v", outcomes)
	}
}
