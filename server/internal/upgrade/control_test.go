package upgrade

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestControlImportStagesOnlySignedInboxArtifact(t *testing.T) {
	fixture := newControlFixture(t)
	manifest, archive, digest := controlSignedArchive(t, fixture.private, fixture.target, "1.2.0")
	token := strings.Repeat("a", 32)
	require.NoError(t, os.WriteFile(filepath.Join(fixture.inbox, token), archive, 0600))

	staged, err := fixture.service.Import(context.Background(), token)

	require.NoError(t, err)
	require.Equal(t, digest, staged.SHA256)
	require.Equal(t, manifest.Version, staged.Version)
	require.Equal(t, manifest.ReleaseNotes, staged.ReleaseNotes)
	require.Equal(t, manifest.PublishedAt, staged.PublishedAt)
	staged.ReleaseNotes = "tampered unsigned notes"
	verified, err := fixture.service.verifyStagedArtifact(context.Background(), staged, digest, fixture.staging, PreflightSnapshot{CurrentVersion: "1.0.0"})
	require.NoError(t, err)
	require.Equal(t, manifest.ReleaseNotes, verified.ReleaseNotes)
	require.True(t, isWithin(fixture.staging, staged.Path))
	require.FileExists(t, staged.Path)
	require.FileExists(t, staged.Path+".json")
	require.NoFileExists(t, filepath.Join(fixture.inbox, token))
}

func TestControlCreateTaskPreparesVerifiedImportedArtifact(t *testing.T) {
	fixture := newControlFixture(t)
	require.NoError(t, fixture.service.Start(context.Background()))
	t.Cleanup(fixture.service.Close)
	_, archive, digest := controlSignedArchive(t, fixture.private, fixture.target, "1.2.0")
	token := strings.Repeat("b", 32)
	require.NoError(t, os.WriteFile(filepath.Join(fixture.inbox, token), archive, 0600))
	_, err := fixture.service.Import(context.Background(), token)
	require.NoError(t, err)

	record, err := fixture.service.CreateTask(context.Background(), ControlCreateTaskRequest{
		IdempotencyKey: "operator-request-1",
		Actor:          "admin",
		TargetDigest:   digest,
	})

	require.NoError(t, err)
	prepared := waitForControlTask(t, fixture.store, record.TaskID, func(task TaskRecord) bool {
		return task.Phase == PhaseAwaitingConfirmation || task.Phase == PhaseFailed
	})
	require.Equal(t, PhaseAwaitingConfirmation, prepared.Phase)
	require.Equal(t, "offline", prepared.Source)
	require.Equal(t, "1.2.0", prepared.Version)
	require.NotNil(t, prepared.Preflight)
	require.NotEmpty(t, prepared.StagingPath)
	reopened, err := OpenTaskStore(fixture.store.root)
	require.NoError(t, err)
	saved, err := reopened.Get(record.TaskID)
	require.NoError(t, err)
	require.Equal(t, "新增：离线升级\n修复：服务启动", saved.ReleaseNotes)
	require.Equal(t, "2026-10-18T07:00:00Z", saved.PublishedAt)
}

func TestControlStartResumesInterruptedPreparation(t *testing.T) {
	fixture := newControlFixture(t)
	_, archive, digest := controlSignedArchive(t, fixture.private, fixture.target, "1.2.0")
	token := strings.Repeat("d", 32)
	require.NoError(t, os.WriteFile(filepath.Join(fixture.inbox, token), archive, 0600))
	_, err := fixture.service.Import(context.Background(), token)
	require.NoError(t, err)

	record, err := fixture.store.Submit(TaskRequest{
		IdempotencyKey: "interrupted-preparation",
		Actor:          "admin",
		TargetDigest:   digest,
	})
	require.NoError(t, err)

	started := make(chan struct{})
	firstContext, cancelFirst := context.WithCancel(context.Background())
	fixture.service.Preflight = func(ctx context.Context) (PreflightSnapshot, error) {
		select {
		case <-started:
		default:
			close(started)
		}
		<-ctx.Done()
		return PreflightSnapshot{}, ctx.Err()
	}
	require.NoError(t, fixture.service.Start(firstContext))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first helper did not begin preparation")
	}
	cancelFirst()
	fixture.service.Close()

	interrupted, err := fixture.store.Get(record.TaskID)
	require.NoError(t, err)
	require.Equal(t, PhaseFetching, interrupted.Phase)
	require.Empty(t, interrupted.Error)

	restarted := newControlServiceForFixture(fixture)
	require.NoError(t, restarted.Start(context.Background()))
	t.Cleanup(restarted.Close)
	prepared := waitForControlTask(t, fixture.store, record.TaskID, func(task TaskRecord) bool {
		return task.Phase == PhaseAwaitingConfirmation || task.Phase == PhaseFailed
	})
	require.Equal(t, PhaseAwaitingConfirmation, prepared.Phase)
	require.Equal(t, "offline", prepared.Source)
}

func TestControlStartResumesEveryPreparationPhase(t *testing.T) {
	for _, interruptedAt := range []TaskPhase{PhaseFetching, PhaseVerifying, PhasePreparing} {
		t.Run(string(interruptedAt), func(t *testing.T) {
			fixture := newControlFixture(t)
			_, archive, digest := controlSignedArchive(t, fixture.private, fixture.target, "1.2.0")
			token := strings.Repeat("e", 32)
			require.NoError(t, os.WriteFile(filepath.Join(fixture.inbox, token), archive, 0600))
			_, err := fixture.service.Import(context.Background(), token)
			require.NoError(t, err)

			record, err := fixture.store.Submit(TaskRequest{
				IdempotencyKey: "resume-" + string(interruptedAt),
				Actor:          "admin",
				TargetDigest:   digest,
			})
			require.NoError(t, err)
			for _, phase := range []TaskPhase{PhaseVerifying, PhasePreparing} {
				if interruptedAt == PhaseFetching {
					break
				}
				require.NoError(t, fixture.store.MarkPhase(record.TaskID, phase))
				if phase == interruptedAt {
					break
				}
			}

			require.NoError(t, fixture.service.Start(context.Background()))
			t.Cleanup(fixture.service.Close)
			prepared := waitForControlTask(t, fixture.store, record.TaskID, func(task TaskRecord) bool {
				return task.Phase == PhaseAwaitingConfirmation || task.Phase == PhaseFailed
			})
			require.Equal(t, PhaseAwaitingConfirmation, prepared.Phase)
			require.NotNil(t, prepared.Preflight)
		})
	}
}

func TestControlCheckRejectsUnchangedAndDowngradeRelease(t *testing.T) {
	fixture := newControlFixture(t)
	for _, version := range []string{"1.1.0", "1.0.9"} {
		t.Run(version, func(t *testing.T) {
			manifest, _, _ := controlSignedArchive(t, fixture.private, fixture.target, version)
			fixture.service.CheckRelease = func(context.Context) (Manifest, error) { return manifest, nil }

			result, err := fixture.service.Check(context.Background())

			require.NoError(t, err)
			require.False(t, result.HasUpdate)
			require.Equal(t, version, result.Manifest.Version)
		})
	}
}

type controlFixture struct {
	service *ControlService
	store   *TaskStore
	private ed25519.PrivateKey
	inbox   string
	staging string
	target  Target
}

func newControlFixture(t *testing.T) controlFixture {
	t.Helper()
	root := t.TempDir()
	store, err := OpenTaskStore(filepath.Join(root, "state"))
	require.NoError(t, err)
	events, err := OpenFileEventLog(filepath.Join(root, "state", "events.jsonl"))
	require.NoError(t, err)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	target := Target{Deployment: "standalone", OS: runtime.GOOS, Arch: runtime.GOARCH, UpdaterProtocol: UpdaterProtocolVersion}
	preflight := PreflightSnapshot{
		CurrentVersion: "1.1.0", CurrentBuildID: "build-1", ConfigDigest: strings.Repeat("a", 64),
		DatabaseDialect: "sqlite", MigrationDigest: strings.Repeat("b", 64),
	}
	inbox := filepath.Join(root, "inbox")
	staging := filepath.Join(root, "state", "staging")
	require.NoError(t, os.MkdirAll(inbox, 0700))
	service := &ControlService{
		Store:       store,
		EventLog:    events,
		InboxRoot:   inbox,
		StagingRoot: staging,
		Target:      target,
		TrustedKeys: map[string]ed25519.PublicKey{"release-key": public},
		Preflight: func(context.Context) (PreflightSnapshot, error) {
			return preflight, nil
		},
	}
	return controlFixture{service: service, store: store, private: private, inbox: inbox, staging: staging, target: target}
}

func newControlServiceForFixture(fixture controlFixture) *ControlService {
	return &ControlService{
		Store:       fixture.store,
		EventLog:    fixture.service.EventLog,
		InboxRoot:   fixture.inbox,
		StagingRoot: fixture.staging,
		Target:      fixture.target,
		TrustedKeys: fixture.service.TrustedKeys,
		Preflight:   fixture.servicePreflight(),
	}
}

func TestControlStatusReportsRegisteredOnlineCapabilities(t *testing.T) {
	fixture := newControlFixture(t)
	status, err := fixture.service.Status(context.Background())
	require.NoError(t, err)
	require.False(t, status.ReleaseSourceConfigured)
	require.False(t, status.OnlineCheck)
	require.True(t, status.OfflineImport)

	fixture.service.ReleaseSource = "https://updates.example.test/channels/stable/manifest.json"
	status, err = fixture.service.Status(context.Background())
	require.NoError(t, err)
	require.True(t, status.ReleaseSourceConfigured)
	require.True(t, status.OnlineCheck)
	require.True(t, status.OfflineImport)

	fixture.service.TrustedKeys = nil
	status, err = fixture.service.Status(context.Background())
	require.NoError(t, err)
	require.True(t, status.ReleaseSourceConfigured)
	require.False(t, status.OnlineCheck)
	require.False(t, status.OfflineImport)
}

func (fixture controlFixture) servicePreflight() func(context.Context) (PreflightSnapshot, error) {
	return func(context.Context) (PreflightSnapshot, error) {
		return PreflightSnapshot{
			CurrentVersion: "1.1.0", CurrentBuildID: "build-1", ConfigDigest: strings.Repeat("a", 64),
			DatabaseDialect: "sqlite", MigrationDigest: strings.Repeat("b", 64),
		}, nil
	}
}

func controlSignedArchive(t *testing.T, private ed25519.PrivateKey, target Target, version string) (Manifest, []byte, string) {
	t.Helper()
	body := []byte("trusted release package")
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	manifest := Manifest{
		SchemaVersion: 1, Channel: "stable", Sequence: 2, ReleaseID: "release-2", Version: version, BuildID: "build-2", MinUpdaterProtocol: UpdaterProtocolVersion,
		ReleaseNotes: "新增：离线升级\n修复：服务启动", PublishedAt: "2026-10-18T07:00:00Z",
		Artifacts: []Artifact{{Deployment: target.Deployment, OS: target.OS, Arch: target.Arch, Filename: "release.tar", URL: "https://updates.example.test/releases/release-2/release.tar", Size: int64(len(body)), SHA256: digest}},
	}
	payload, err := json.Marshal(manifest)
	require.NoError(t, err)
	envelope, err := json.Marshal(SignedManifest{Payload: base64.StdEncoding.EncodeToString(payload), Signatures: []ManifestSignature{{KeyID: "release-key", Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(private, payload))}}})
	require.NoError(t, err)
	manifest.Envelope = envelope
	return manifest, buildTar(t, map[string][]byte{"manifest.json": envelope, "release.tar": body}), digest
}

func waitForControlTask(t *testing.T, store *TaskStore, taskID string, done func(TaskRecord) bool) TaskRecord {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		record, err := store.Get(taskID)
		if err == nil && done(record) {
			return record
		}
		time.Sleep(10 * time.Millisecond)
	}
	record, err := store.Get(taskID)
	require.NoError(t, err)
	t.Fatalf("task %s did not reach terminal preparation state: %s", taskID, record.Phase)
	return TaskRecord{}
}
