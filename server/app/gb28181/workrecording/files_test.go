package workrecording

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

const (
	fileJobA                 = "8d9b6784-df9a-4486-84ce-3e6eedb18280"
	fileJobB                 = "4d1f6c2a-7e83-4a9b-9f10-2c0e5b6a7d81"
	fileJobC                 = "c1a2b3c4-d5e6-4789-8abc-def012345678"
	isolatedHookNodeID int64 = 17
)

type isolatedRecordingHookFixture struct {
	ReceivedAt float64 `json:"received_at"`
	Body       struct {
		App       string  `json:"app"`
		FileName  string  `json:"file_name"`
		FilePath  string  `json:"file_path"`
		StartTime int64   `json:"start_time"`
		Stream    string  `json:"stream"`
		TimeLen   float64 `json:"time_len"`
		VHost     string  `json:"vhost"`
	} `json:"body"`
}

func fileAttributionDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "file-attribution.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.AutoMigrate(&models.GbWorkRecording{}))
	return db
}

func fileAttributionJob(id string, nodeID int64, vhost, appName, stream, root, state string) models.GbWorkRecording {
	now := time.Date(2026, 9, 9, 12, 42, 52, 0, time.UTC)
	return models.GbWorkRecording{
		ID: id, ChannelID: uint(len(id)), CreatedBy: 1, RequestID: "request-" + id,
		State: state, DesiredAction: "stop", Version: 3, NodeID: nodeID,
		VHost: vhost, App: appName, Stream: stream, RecordingRoot: root,
		Generation: 1, StartedAt: &now, StoppedAt: &now,
		FileState: FilePending, FormState: FormSubmitted, SchemaVersion: 1,
		DeviceID: "00000000000000000001", FormJSON: "{}",
	}
}

func TestFileAttributionFindsStoppedJobWhenNextJobAlreadyStarted(t *testing.T) {
	db := fileAttributionDB(t)
	jobs := []models.GbWorkRecording{
		fileAttributionJob(fileJobA, 7, "__defaultVhost__", "rtp", "stream-1", "/opt/zlm/work-recordings/"+fileJobA, StateStopped),
		fileAttributionJob(fileJobB, 7, "__defaultVhost__", "rtp", "stream-1", "/opt/zlm/work-recordings/"+fileJobB, StateRecording),
	}
	// The jobs intentionally have the same second and the same media tuple.
	require.NoError(t, db.Create(&jobs).Error)
	attributor := NewFileAttributionRepository(db)
	input := FileAttributionInput{
		NodeID: 7, VHost: "__defaultVhost__", App: "rtp", Stream: "stream-1",
		FilePath: "/opt/zlm/work-recordings/" + fileJobA + "/record/rtp/stream-1/2026-09-09/2026-09-09-12-42-52-0.mp4",
	}
	got, err := attributor.Resolve(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, fileJobA, got.ID)

	// A repeated callback for the same path remains the same attribution.
	repeated, err := attributor.Resolve(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, got.ID, repeated.ID)
}

func TestFileAttributionSupportsWindowsAndUNCPaths(t *testing.T) {
	db := fileAttributionDB(t)
	jobs := []models.GbWorkRecording{
		fileAttributionJob(fileJobA, 8, "v", "app", "stream", `C:\zlm\work-recordings\`+fileJobA, StateStopped),
		fileAttributionJob(fileJobB, 9, "v", "app", "stream", `\\server\share\zlm\work-recordings\`+fileJobB, StateStopped),
	}
	require.NoError(t, db.Create(&jobs).Error)
	attributor := NewFileAttributionRepository(db)
	win, err := attributor.Resolve(context.Background(), FileAttributionInput{
		NodeID: 8, VHost: "v", App: "app", Stream: "stream",
		FilePath: `C:\zlm\work-recordings\` + fileJobA + `\record\app\stream\clip.mp4`,
	})
	require.NoError(t, err)
	require.Equal(t, fileJobA, win.ID)
	unc, err := attributor.Resolve(context.Background(), FileAttributionInput{
		NodeID: 9, VHost: "v", App: "app", Stream: "stream",
		FilePath: `\\server\share\zlm\work-recordings\` + fileJobB + `\record\app\stream\clip.mp4`,
	})
	require.NoError(t, err)
	require.Equal(t, fileJobB, unc.ID)
}

func TestFileAttributionRejectsWrongMediaOrInvalidPath(t *testing.T) {
	db := fileAttributionDB(t)
	job := fileAttributionJob(fileJobA, 7, "v", "app", "stream", "/opt/zlm/work-recordings/"+fileJobA, StateStopped)
	require.NoError(t, db.Create(&job).Error)
	attributor := NewFileAttributionRepository(db)
	validFilePath := "/opt/zlm/work-recordings/" + fileJobA + "/clip.mp4"
	cases := []FileAttributionInput{
		{NodeID: 8, VHost: "v", App: "app", Stream: "stream", FilePath: validFilePath},
		{NodeID: 7, VHost: "other", App: "app", Stream: "stream", FilePath: validFilePath},
		{NodeID: 7, VHost: "v", App: "other", Stream: "stream", FilePath: validFilePath},
		{NodeID: 7, VHost: "v", App: "app", Stream: "other", FilePath: validFilePath},
		{NodeID: 7, VHost: "v", App: "app", Stream: "stream", FilePath: "relative/work-recordings/" + fileJobA + "/clip.mp4"},
		{NodeID: 7, VHost: "v", App: "app", Stream: "stream", FilePath: "/opt/zlm/work-recordings/" + fileJobA + "/clip\x00.mp4"},
		{NodeID: 7, VHost: "v", App: "app", Stream: "stream", FilePath: ""},
	}
	for _, input := range cases {
		got, err := attributor.Resolve(context.Background(), input)
		require.Nil(t, got)
		require.ErrorIs(t, err, ErrAttributionUnknown, input)
	}
}

func TestFileAttributionRejectsPrefixCollisionAndTraversal(t *testing.T) {
	db := fileAttributionDB(t)
	job := fileAttributionJob(fileJobA, 7, "v", "app", "stream", "/opt/zlm/work-recordings/"+fileJobA, StateStopped)
	require.NoError(t, db.Create(&job).Error)
	attributor := NewFileAttributionRepository(db)
	for _, filePath := range []string{
		"/opt/zlm/work-recordings/" + fileJobB + "/record/clip.mp4",
		"/opt/zlm/work-recordings/" + fileJobA + "/../" + fileJobB + "/record/clip.mp4",
		"/opt/zlm/work-recordings/" + fileJobA + "/../../outside/clip.mp4",
	} {
		got, err := attributor.Resolve(context.Background(), FileAttributionInput{NodeID: 7, VHost: "v", App: "app", Stream: "stream", FilePath: filePath})
		require.Nil(t, got)
		require.ErrorIs(t, err, ErrAttributionUnknown, filePath)
	}
}

func TestFileAttributionRejectsMissingOrAmbiguousRoots(t *testing.T) {
	db := fileAttributionDB(t)
	jobs := []models.GbWorkRecording{
		fileAttributionJob(fileJobA, 7, "v", "app", "stream", "", StateStopped),
		fileAttributionJob(fileJobB, 7, "v", "app", "stream", "relative/work-recordings/"+fileJobB, StateStopped),
	}
	require.NoError(t, db.Create(&jobs).Error)
	attributor := NewFileAttributionRepository(db)
	filePath := "/opt/zlm/work-recordings/" + fileJobA + "/record/clip.mp4"
	got, err := attributor.Resolve(context.Background(), FileAttributionInput{NodeID: 7, VHost: "v", App: "app", Stream: "stream", FilePath: filePath})
	require.Nil(t, got)
	require.ErrorIs(t, err, ErrAttributionUnknown)

	job := fileAttributionJob(fileJobC, 7, "v", "app", "stream", "/opt/zlm/work-recordings/"+fileJobC, StateStopped)
	require.NoError(t, db.Create(&job).Error)
	got, err = attributor.Resolve(context.Background(), FileAttributionInput{NodeID: 7, VHost: "v", App: "app", Stream: "stream", FilePath: "/opt/zlm/work-recordings/" + fileJobC + "/record/clip.mp4"})
	require.NoError(t, err)
	require.Equal(t, fileJobC, got.ID)

	uppercaseID := strings.ToUpper(fileJobC)
	got, err = attributor.Resolve(context.Background(), FileAttributionInput{NodeID: 7, VHost: "v", App: "app", Stream: "stream", FilePath: "/opt/zlm/work-recordings/" + uppercaseID + "/record/clip.mp4"})
	require.Nil(t, got)
	require.ErrorIs(t, err, ErrAttributionUnknown)
	got, err = attributor.Resolve(context.Background(), FileAttributionInput{NodeID: 7, VHost: "v", App: "app", Stream: "stream", FilePath: "/opt/zlm/work-recordings/" + fileJobC + "/record/work-recordings/" + fileJobC + "/clip.mp4"})
	require.Nil(t, got)
	require.ErrorIs(t, err, ErrAttributionUnknown)
}

func TestFileAttributionChecksExactTupleUnderCaseInsensitiveCollation(t *testing.T) {
	db := claimDB(t)
	require.NoError(t, db.Exec(`CREATE TABLE gb_work_recording (id TEXT PRIMARY KEY, node_id INTEGER, v_host TEXT COLLATE NOCASE, app TEXT COLLATE NOCASE, stream TEXT COLLATE NOCASE, recording_root TEXT)`).Error)
	root := "/srv/work-recordings/" + fileJobA
	require.NoError(t, db.Exec("INSERT INTO gb_work_recording(id,node_id,v_host,app,stream,recording_root) VALUES (?,1,'v','rtp','Camera',?)", fileJobA, root).Error)
	job, err := NewFileAttributionRepository(db).Resolve(context.Background(), FileAttributionInput{NodeID: 1, VHost: "v", App: "rtp", Stream: "camera", FilePath: root + "/clip.mp4"})
	require.Nil(t, job)
	require.ErrorIs(t, err, ErrAttributionUnknown)
}

func TestFileAttributionReplaysIsolatedRealHooksOutOfOrder(t *testing.T) {
	fixtureData, err := os.ReadFile(filepath.Join("testdata", "isolated_recording_hooks.json"))
	require.NoError(t, err)
	var fixtures []isolatedRecordingHookFixture
	require.NoError(t, json.Unmarshal(fixtureData, &fixtures))
	require.Len(t, fixtures, 2)
	require.Less(t, fixtures[0].ReceivedAt, fixtures[1].ReceivedAt)
	require.Equal(t, fixtures[0].Body.StartTime, fixtures[1].Body.StartTime)
	require.Equal(t, fixtures[0].Body.FileName, fixtures[1].Body.FileName)
	require.NotEqual(t, fixtures[0].Body.FilePath, fixtures[1].Body.FilePath)

	db := fileAttributionDB(t)
	jobs := make([]models.GbWorkRecording, 0, len(fixtures))
	for i, fixture := range fixtures {
		path := cleanNodePath(fixture.Body.FilePath)
		jobID, ok := workJobIDFromPath(path)
		require.Truef(t, ok, "fixture %d has no canonical job id", i)
		recordIndex := strings.Index(path, "/record/")
		require.Greater(t, recordIndex, 0)
		state := StateRecording
		if i == 0 {
			state = StateStopped
		}
		job := fileAttributionJob(jobID, isolatedHookNodeID, fixture.Body.VHost, fixture.Body.App, fixture.Body.Stream, path[:recordIndex], state)
		startedAt := time.Unix(fixture.Body.StartTime, 0).UTC()
		job.StartedAt = &startedAt
		if state == StateRecording {
			job.StoppedAt = nil
		}
		jobs = append(jobs, job)
	}
	require.NoError(t, db.Create(&jobs).Error)
	attributor := NewFileAttributionRepository(db)

	// Replay in reverse order twice, as delayed/duplicated callbacks can arrive
	// after the next same-second job has already started.
	for pass := 0; pass < 2; pass++ {
		for i := len(fixtures) - 1; i >= 0; i-- {
			fixture := fixtures[i]
			jobID, ok := workJobIDFromPath(cleanNodePath(fixture.Body.FilePath))
			require.True(t, ok)
			job, resolveErr := attributor.Resolve(context.Background(), FileAttributionInput{
				NodeID: isolatedHookNodeID, VHost: fixture.Body.VHost, App: fixture.Body.App,
				Stream: fixture.Body.Stream, FilePath: fixture.Body.FilePath,
			})
			require.NoError(t, resolveErr)
			require.Equal(t, jobID, job.ID)
		}
	}
}
