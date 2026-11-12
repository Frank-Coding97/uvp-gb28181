package upgrade

import (
	"context"
	"path/filepath"
	"testing"
)

func TestFileEventLogPersistsAndLimitsEvents(t *testing.T) {
	log, err := OpenFileEventLog(filepath.Join(t.TempDir(), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := log.Append(context.Background(), UpgradeEvent{Event: "blocked", Reason: string(rune('a' + i))}); err != nil {
			t.Fatal(err)
		}
	}
	events, err := log.Recent(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Reason != "b" || events[1].Reason != "c" {
		t.Fatalf("events = %#v", events)
	}
}

func TestFileEventLogDeduplicatesRetriedTerminalOutcomes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	first, err := OpenFileEventLog(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := OpenFileEventLog(path)
	if err != nil {
		t.Fatal(err)
	}
	event := UpgradeEvent{Event: "succeeded", TaskID: "task-1"}
	if err := first.Append(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if err := second.Append(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	events, err := first.Recent(100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("duplicate outcomes: %#v", events)
	}
}
