package upgrade

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func capacityArchive(t *testing.T, headers []*tar.Header) string {
	t.Helper()
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	for _, h := range headers {
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			if _, err := tw.Write(bytes.Repeat([]byte("x"), int(h.Size))); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "package.tar")
	if err := os.WriteFile(p, b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPackagePreflightRejectsUnsafeArchiveBeforeStop(t *testing.T) {
	for _, h := range []*tar.Header{
		{Name: "../escape", Typeflag: tar.TypeReg, Size: 1},
		{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"},
	} {
		p := capacityArchive(t, []*tar.Header{h})
		if _, err := inspectPackage(context.Background(), p); err == nil {
			t.Fatalf("accepted unsafe header: %#v", h)
		}
	}
}

func TestPackagePreflightRejectsTruncatedArchive(t *testing.T) {
	p := capacityArchive(t, []*tar.Header{{Name: "backend", Typeflag: tar.TypeReg, Size: 2000}})
	if err := os.Truncate(p, 600); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectPackage(context.Background(), p); err == nil {
		t.Fatal("accepted truncated package")
	}
}

func TestPackagePreflightRejectsFileAsParent(t *testing.T) {
	for _, headers := range [][]*tar.Header{
		{{Name: "backend", Typeflag: tar.TypeReg, Size: 1}, {Name: "backend/config", Typeflag: tar.TypeReg, Size: 1}},
		{{Name: "backend/config", Typeflag: tar.TypeReg, Size: 1}, {Name: "backend", Typeflag: tar.TypeReg, Size: 1}},
	} {
		if _, err := inspectPackage(context.Background(), capacityArchive(t, headers)); err == nil {
			t.Fatal("accepted file used as a parent directory")
		}
	}
}

func TestCapacityAggregatesRequirementsOnSameFilesystem(t *testing.T) {
	var calls int
	probe := func(string) (filesystemCapacity, error) {
		calls++
		return filesystemCapacity{Device: 1, Available: 100}, nil
	}
	err := checkCapacity([]spaceRequirement{{Path: t.TempDir(), Bytes: 60}, {Path: t.TempDir(), Bytes: 60}}, probe)
	if err == nil || !strings.Contains(err.Error(), "insufficient") {
		t.Fatalf("aggregate capacity: %v", err)
	}
	if calls != 2 {
		t.Fatalf("probed %d paths", calls)
	}
}

func TestReserveComposeBackupCapacityRejectsInsufficientSpaceBeforeStop(t *testing.T) {
	composeFile := filepath.Join(t.TempDir(), "compose.yml")
	contents := []byte("services:\n  backend:\n    image: uvp:old\n")
	if err := os.WriteFile(composeFile, contents, 0600); err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	want := allocatedStorage(uint64(len(contents))) + capacityReserve
	err := reserveComposeBackupCapacity(composeFile, state, func(path string) (filesystemCapacity, error) {
		if path != state {
			t.Fatalf("capacity path = %q, want %q", path, state)
		}
		return filesystemCapacity{Device: 1, Available: want - 1, BlockSize: capacityBlockSize}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "insufficient disk space") {
		t.Fatalf("compose backup capacity error = %v", err)
	}
	if err := reserveComposeBackupCapacity(composeFile, state, func(string) (filesystemCapacity, error) {
		return filesystemCapacity{Device: 1, Available: want, BlockSize: capacityBlockSize}, nil
	}); err != nil {
		t.Fatalf("compose backup capacity with exact budget = %v", err)
	}
}

func TestWorkflowCapacityFailureDoesNotStopOrChangeGate(t *testing.T) {
	store, record := prepareWorkflowTask(t)
	adapter := &fakeWorkflowAdapter{}
	gate, err := OpenGateStore(filepath.Join(t.TempDir(), "gate.json"))
	if err != nil {
		t.Fatal(err)
	}
	w := WorkflowExecutor{Store: store, Adapter: adapter, Gate: gate, Prepare: func(context.Context, TaskRecord) error { return errors.New("insufficient disk space") }}
	if err := w.Apply(context.Background(), record); err == nil {
		t.Fatal("capacity failure accepted")
	}
	if len(adapter.steps) != 0 {
		t.Fatalf("disrupted service: %v", adapter.steps)
	}
	state, err := gate.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.Mode != GateNormal {
		t.Fatalf("changed gate: %#v", state)
	}
}

func TestWorkflowRechecksWindowAfterSlowPackageAndDatabasePreflight(t *testing.T) {
	for _, slowStep := range []string{"package", "database"} {
		t.Run(slowStep, func(t *testing.T) {
			f := newHelperSchedulerFixture(t)
			if err := f.store.MarkPhase(f.record.TaskID, PhaseStopping); err != nil {
				t.Fatal(err)
			}
			f.record.Phase = PhaseStopping
			now := f.scheduler.Now()
			outside := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, time.UTC)
			adapter := &fakeWorkflowAdapter{}
			w := WorkflowExecutor{Store: f.store, Adapter: adapter, PolicyStore: f.policies, Now: func() time.Time { return now }}
			w.Prepare = func(context.Context, TaskRecord) error {
				if slowStep == "package" {
					now = outside
				}
				return nil
			}
			w.Preflight = func(context.Context) (PreflightSnapshot, error) {
				if slowStep == "database" {
					now = outside
				}
				return *f.record.Preflight, nil
			}
			if err := w.Apply(context.Background(), f.record); err == nil {
				t.Fatal("started outside maintenance window")
			}
			if len(adapter.steps) != 0 {
				t.Fatalf("disrupted service: %v", adapter.steps)
			}
		})
	}
}

func TestPackagePreflightBudgetsImplicitDirectories(t *testing.T) {
	path := capacityArchive(t, []*tar.Header{{Name: "one/two/three/backend", Typeflag: tar.TypeReg, Size: 1}})
	size, err := inspectPackage(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if size < 5*capacityBlockSize {
		t.Fatalf("implicit directories were not budgeted: %d", size)
	}
}

func TestWorkflowWindowExpiryAfterGateRestoresNormalWithoutStopping(t *testing.T) {
	f := newHelperSchedulerFixture(t)
	if err := f.store.MarkPhase(f.record.TaskID, PhaseStopping); err != nil {
		t.Fatal(err)
	}
	f.record.Phase = PhaseStopping
	gate, err := OpenGateStore(filepath.Join(t.TempDir(), "gate.json"))
	if err != nil {
		t.Fatal(err)
	}
	adapter := &fakeWorkflowAdapter{}
	inside := f.scheduler.Now()
	calls := 0
	w := WorkflowExecutor{Store: f.store, Adapter: adapter, Gate: gate, PolicyStore: f.policies, Preflight: func(context.Context) (PreflightSnapshot, error) { return *f.record.Preflight, nil }}
	w.Now = func() time.Time {
		calls++
		if calls > 1 {
			return time.Date(inside.Year(), inside.Month(), inside.Day(), 12, 0, 0, 0, time.UTC)
		}
		return inside
	}
	if err := w.Apply(context.Background(), f.record); err == nil {
		t.Fatal("started after gate crossed window")
	}
	if len(adapter.steps) > 0 {
		t.Fatalf("service changed: %v", adapter.steps)
	}
	state, err := gate.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.Mode != GateNormal {
		t.Fatalf("gate left closed: %#v", state)
	}
	r, err := f.store.Get(f.record.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Phase != PhaseFailed {
		t.Fatalf("unexpected failure phase %s", r.Phase)
	}
}

type preparingWorkflowAdapter struct {
	fakeWorkflowAdapter
	prepareErr error
}

func (a *preparingWorkflowAdapter) PreparePackage(context.Context, TaskRecord) error {
	return a.prepareErr
}

func TestRuntimePackagePreparationFailureDoesNotStop(t *testing.T) {
	store, record := prepareWorkflowTask(t)
	adapter := &preparingWorkflowAdapter{prepareErr: errors.New("Docker image import failed")}
	w := WorkflowExecutor{Store: store, Adapter: adapter}
	if err := w.Apply(context.Background(), record); err == nil {
		t.Fatal("accepted failed runtime preparation")
	}
	if len(adapter.steps) > 0 {
		t.Fatalf("disrupted service: %v", adapter.steps)
	}
}
