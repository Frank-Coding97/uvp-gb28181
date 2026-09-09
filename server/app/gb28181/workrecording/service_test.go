package workrecording

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

type serviceMP4Client struct {
	mu          sync.Mutex
	active      bool
	isCalls     int
	starts      int
	stops       int
	directories []string
	startErr    error
	stopErr     error
	isErr       error
	beforeStart func() error
	beforeStop  func()
}

func (c *serviceMP4Client) IsRecording(context.Context, string, string, string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.isCalls++
	return c.active, c.isErr
}

func (c *serviceMP4Client) StartRecord(_ context.Context, vhost, app, stream string, maxSecond int) error {
	_ = vhost
	_ = app
	_ = stream
	_ = maxSecond
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.beforeStart != nil {
		if err := c.beforeStart(); err != nil {
			return err
		}
	}
	c.starts++
	if c.startErr == nil {
		c.active = true
	}
	return c.startErr
}

func (c *serviceMP4Client) StartMP4RecordInDirectory(ctx context.Context, vhost, app, stream string, maxSecond int, directory string) error {
	c.mu.Lock()
	c.directories = append(c.directories, directory)
	c.mu.Unlock()
	return c.StartRecord(ctx, vhost, app, stream, maxSecond)
}

func (c *serviceMP4Client) StopRecord(_ context.Context, vhost, app, stream string) error {
	_ = vhost
	_ = app
	_ = stream
	c.mu.Lock()
	c.stops++
	if c.stopErr == nil {
		c.active = false
	}
	beforeStop := c.beforeStop
	c.beforeStop = nil
	stopErr := c.stopErr
	c.mu.Unlock()
	if beforeStop != nil {
		beforeStop()
	}
	return stopErr
}

type servicePrepare struct {
	mu         sync.Mutex
	root       string
	prepared   []string
	releases   atomic.Int32
	prepareErr error
	onError    PreparedRecording
}

func (p *servicePrepare) Prepare(_ context.Context, channelID uint, jobID string) (PreparedRecording, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.prepareErr != nil {
		return p.onError, p.prepareErr
	}
	p.prepared = append(p.prepared, jobID)
	root := p.root
	if root == "" {
		root = "/var/lib/uvp"
	}
	return PreparedRecording{
		Target: MediaTarget{
			NodeID: 1, VHost: "__defaultVhost__", App: "rtp", Stream: "channel-" + strconv.FormatUint(uint64(channelID), 10), Generation: 1,
			RecordingRoot: root + "/work-recordings/" + jobID,
		},
		Release: func() { p.releases.Add(1) },
	}, nil
}

func serviceDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "work-recording-service.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.AutoMigrate(&models.GbWorkRecording{}, &models.GbRecorderClaim{}))
	return db
}

func serviceFixture(t *testing.T) (*Service, *serviceMP4Client, *servicePrepare, *gorm.DB) {
	t.Helper()
	db := serviceDB(t)
	client := &serviceMP4Client{}
	prepare := &servicePrepare{}
	recorder := NewRecorder(NewClaims(db), func(context.Context, MediaTarget) (func(), error) {
		return func() {}, nil
	}, func(MediaTarget) (MP4Client, error) { return client, nil })
	service := NewService(db, recorder, prepare.Prepare)
	return service, client, prepare, db
}

func serviceStartRequest(channelID uint, requestID string) StartRequest {
	return StartRequest{ChannelID: channelID, RequestID: requestID}
}

func TestWorkServiceStartIsIdempotentByActorAndRequest(t *testing.T) {
	service, client, prepare, _ := serviceFixture(t)
	ctx := context.Background()
	first, err := service.Start(ctx, 7, serviceStartRequest(1, "request-1"), 30)
	require.NoError(t, err)
	second, err := service.Start(ctx, 7, serviceStartRequest(1, "request-1"), 30)
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	require.Equal(t, StateRecording, second.State)
	require.Equal(t, 1, client.starts)
	require.Equal(t, []string{first.ID}, prepare.prepared)

	_, err = service.Start(ctx, 7, serviceStartRequest(2, "request-1"), 30)
	require.ErrorIs(t, err, ErrRequestConflict)
	require.Equal(t, 1, client.starts)

	stopped, err := service.Stop(ctx, first.ID)
	require.NoError(t, err)
	require.Equal(t, StateStopped, stopped.State)
}

func TestWorkServiceSerializesConcurrentStartsForOneChannel(t *testing.T) {
	service, client, _, _ := serviceFixture(t)
	ctx := context.Background()
	ready := make(chan struct{})
	results := make(chan struct {
		snapshot Snapshot
		err      error
	}, 2)
	for _, requestID := range []string{"request-a", "request-b"} {
		go func(requestID string) {
			<-ready
			snapshot, err := service.Start(ctx, 7, serviceStartRequest(1, requestID), 30)
			results <- struct {
				snapshot Snapshot
				err      error
			}{snapshot: snapshot, err: err}
		}(requestID)
	}
	close(ready)
	var successful Snapshot
	for range 2 {
		result := <-results
		if result.err == nil {
			successful = result.snapshot
			continue
		}
		require.ErrorIs(t, result.err, ErrOwnerConflict)
	}
	require.NotEmpty(t, successful.ID)
	require.Equal(t, 1, client.starts)
	_, err := service.Stop(ctx, successful.ID)
	require.NoError(t, err)
}

func TestWorkServicePersistsDirectoryAndBindingBeforeExternalStart(t *testing.T) {
	service, client, _, db := serviceFixture(t)
	client.beforeStart = func() error {
		var job models.GbWorkRecording
		if err := db.First(&job, "request_id = ?", "request-1").Error; err != nil {
			return err
		}
		if job.NodeID != 1 || job.VHost != "__defaultVhost__" || job.App != "rtp" || job.Generation != 1 || job.RecordingRoot == "" || job.RecorderClaimVersion == 0 {
			return errors.New("job binding was not durable before StartRecord")
		}
		return nil
	}
	result, err := service.Start(context.Background(), 7, serviceStartRequest(1, "request-1"), 30)
	require.NoError(t, err)
	require.Equal(t, StateRecording, result.State)
	require.Equal(t, 1, client.starts)
	_, err = service.Stop(context.Background(), result.ID)
	require.NoError(t, err)
}

func TestWorkServicePrepareFailureAbortsOnlyUnboundReservation(t *testing.T) {
	service, client, prepare, db := serviceFixture(t)
	prepare.prepareErr = errors.New("live source unavailable")

	result, err := service.Start(context.Background(), 7, serviceStartRequest(1, "request-1"), 30)
	require.ErrorIs(t, err, prepare.prepareErr)
	require.Equal(t, StateFailed, result.State)
	require.Zero(t, client.starts)
	claim, err := NewClaims(db).Get(context.Background(), ChannelResource(1))
	require.NoError(t, err)
	require.Equal(t, StateIdle, claim.State)
	require.Equal(t, uint64(2), mustServiceJob(t, db, result.ID).RecorderClaimVersion)

	prepare.prepareErr = nil
	next, err := service.Start(context.Background(), 7, serviceStartRequest(1, "request-2"), 30)
	require.NoError(t, err)
	require.Equal(t, StateRecording, next.State)
	require.Equal(t, 1, client.starts)
	_, err = service.Stop(context.Background(), next.ID)
	require.NoError(t, err)
}

func TestWorkServicePrepareErrorWithLeaseRetainsUnknownOwnership(t *testing.T) {
	service, client, prepare, db := serviceFixture(t)
	prepare.prepareErr = errors.New("source cleanup pending")
	prepare.onError = PreparedRecording{
		Target: MediaTarget{
			NodeID: 1, VHost: "__defaultVhost__", App: "rtp", Stream: "channel-\x01", Generation: 1,
			RecordingRoot: "/var/lib/uvp/work-recordings/placeholder",
		},
		Release: func() { prepare.releases.Add(1) },
	}

	result, err := service.Start(context.Background(), 7, serviceStartRequest(1, "request-1"), 30)
	require.ErrorIs(t, err, prepare.prepareErr)
	require.Equal(t, StateUnknown, result.State)
	require.Zero(t, client.starts)
	claim, err := NewClaims(db).Get(context.Background(), ChannelResource(1))
	require.NoError(t, err)
	require.Equal(t, StateStarting, claim.State)
	require.Equal(t, "source cleanup pending", mustServiceJob(t, db, result.ID).LastError)
	require.Zero(t, prepare.releases.Load())
}

func TestWorkServiceGetKeepsUnknownWhenClaimIsStillStarting(t *testing.T) {
	service, client, prepare, db := serviceFixture(t)
	prepare.prepareErr = errors.New("source cleanup pending")
	prepare.onError = PreparedRecording{
		Target: MediaTarget{
			NodeID: 1, VHost: "__defaultVhost__", App: "rtp", Stream: "channel-\x01", Generation: 1,
			RecordingRoot: "/var/lib/uvp/work-recordings/placeholder",
		},
		Release: func() { prepare.releases.Add(1) },
	}
	ctx := context.Background()

	started, err := service.Start(ctx, 7, serviceStartRequest(1, "request-get-starting"), 30)
	require.ErrorIs(t, err, prepare.prepareErr)
	require.Equal(t, StateUnknown, started.State)
	checked, err := service.Get(ctx, started.ID)
	require.NoError(t, err)
	require.Equal(t, StateUnknown, checked.State)
	require.NotNil(t, checked.LastCheckedAt)
	claim, err := NewClaims(db).Get(ctx, ChannelResource(1))
	require.NoError(t, err)
	require.Equal(t, StateStarting, claim.State)
	require.Zero(t, client.starts)

	_, err = service.Stop(ctx, started.ID)
	require.NoError(t, err)
	require.Equal(t, int32(1), prepare.releases.Load())
}

func TestWorkServiceUsesLegacyHandoffWithOnePrepare(t *testing.T) {
	service, client, prepare, db := serviceFixture(t)
	ctx := context.Background()
	require.NoError(t, db.AutoMigrate(&models.GbChannel{}, &models.GbRecordingSession{}))
	require.NoError(t, db.Create(&models.GbChannel{
		ID: 1, DeviceID: "device", ChannelID: "channel", CloudRecordingEnabled: false,
		CloudRecordingState: models.CloudRecordingStateDisabled,
	}).Error)
	legacyTarget := MediaTarget{NodeID: 1, VHost: "__defaultVhost__", App: "rtp", Stream: "legacy-stream", Generation: 1}
	require.NoError(t, db.Create(&models.GbRecorderClaim{
		ResourceKey: ChannelResource(1), ChannelID: 1, OwnerKind: OwnerLegacy, OwnerID: "channel:1", State: StateUnknown, Version: 7,
	}).Error)
	require.NoError(t, db.Create(&models.GbRecorderClaim{
		ResourceKey: legacyTarget.key(), ChannelID: 1, NodeID: legacyTarget.NodeID, VHost: legacyTarget.VHost,
		App: legacyTarget.App, Stream: legacyTarget.Stream, Generation: legacyTarget.Generation,
		OwnerKind: OwnerLegacy, OwnerID: "media:legacy", State: StateUnknown, Version: 7,
	}).Error)
	prepareCalls := 0
	service.prepare = func(_ context.Context, _ uint, jobID string) (PreparedRecording, error) {
		prepareCalls++
		return PreparedRecording{
			Target: MediaTarget{
				NodeID: legacyTarget.NodeID, VHost: legacyTarget.VHost, App: legacyTarget.App, Stream: legacyTarget.Stream,
				Generation: legacyTarget.Generation, RecordingRoot: "/var/lib/uvp/work-recordings/" + jobID,
			},
			Release: func() { prepare.releases.Add(1) },
		}, nil
	}

	started, err := service.Start(ctx, 7, serviceStartRequest(1, "request-legacy"), 30)
	require.NoError(t, err)
	require.Equal(t, StateRecording, started.State)
	require.Equal(t, 1, prepareCalls)
	require.Equal(t, 1, client.starts)
	claim, err := NewClaims(db).Get(ctx, ChannelResource(1))
	require.NoError(t, err)
	require.Equal(t, OwnerWork, claim.OwnerKind)
	require.Equal(t, started.ID, claim.OwnerID)

	_, err = service.Stop(ctx, started.ID)
	require.NoError(t, err)
	require.Equal(t, int32(1), prepare.releases.Load())
}

func TestWorkServiceLegacyHandoffFailureReleasesPrepareLeaseAndKeepsLegacyClaims(t *testing.T) {
	service, client, prepare, db := serviceFixture(t)
	ctx := context.Background()
	require.NoError(t, db.AutoMigrate(&models.GbChannel{}, &models.GbRecordingSession{}))
	require.NoError(t, db.Create(&models.GbChannel{
		ID: 1, DeviceID: "device", ChannelID: "channel", CloudRecordingEnabled: false,
		CloudRecordingState: models.CloudRecordingStateDisabled,
	}).Error)
	legacyTarget := MediaTarget{NodeID: 1, VHost: "__defaultVhost__", App: "rtp", Stream: "legacy-stream", Generation: 1}
	require.NoError(t, db.Create(&models.GbRecorderClaim{
		ResourceKey: ChannelResource(1), ChannelID: 1, OwnerKind: OwnerLegacy, OwnerID: "channel:1", State: StateUnknown, Version: 7,
	}).Error)
	require.NoError(t, db.Create(&models.GbRecorderClaim{
		ResourceKey: legacyTarget.key(), ChannelID: 1, NodeID: legacyTarget.NodeID, VHost: legacyTarget.VHost,
		App: legacyTarget.App, Stream: legacyTarget.Stream, Generation: legacyTarget.Generation,
		OwnerKind: OwnerLegacy, OwnerID: "media:legacy", State: StateUnknown, Version: 7,
	}).Error)
	service.prepare = func(_ context.Context, _ uint, jobID string) (PreparedRecording, error) {
		return PreparedRecording{
			Target: MediaTarget{
				NodeID: legacyTarget.NodeID, VHost: legacyTarget.VHost, App: legacyTarget.App, Stream: legacyTarget.Stream,
				Generation: legacyTarget.Generation, RecordingRoot: "/var/lib/uvp/work-recordings/" + jobID,
			},
			Release: func() { prepare.releases.Add(1) },
		}, nil
	}
	client.active = true

	result, err := service.Start(ctx, 7, serviceStartRequest(1, "request-legacy-rejected"), 30)
	require.ErrorIs(t, err, ErrAttributionUnknown)
	require.Equal(t, StateFailed, result.State)
	require.Zero(t, client.starts)
	require.Equal(t, int32(1), prepare.releases.Load())
	channelClaim, err := NewClaims(db).Get(ctx, ChannelResource(1))
	require.NoError(t, err)
	require.Equal(t, OwnerLegacy, channelClaim.OwnerKind)
	require.Equal(t, "channel:1", channelClaim.OwnerID)
	require.Equal(t, StateUnknown, channelClaim.State)
	mediaClaim, err := NewClaims(db).Get(ctx, legacyTarget.key())
	require.NoError(t, err)
	require.Equal(t, OwnerLegacy, mediaClaim.OwnerKind)
	require.Equal(t, "media:legacy", mediaClaim.OwnerID)
	require.Equal(t, StateUnknown, mediaClaim.State)
}

func TestWorkServiceStartConfirmationWriteFailureLeavesUnknownAndDoesNotRetry(t *testing.T) {
	service, client, _, db := serviceFixture(t)
	require.NoError(t, db.Exec("CREATE TRIGGER reject_job_recording BEFORE UPDATE ON gb_work_recording WHEN NEW.state = 'recording' BEGIN SELECT RAISE(ABORT, 'unavailable'); END").Error)

	result, err := service.Start(context.Background(), 7, serviceStartRequest(1, "request-1"), 30)
	require.Error(t, err)
	require.Equal(t, StateUnknown, result.State)
	require.Equal(t, 1, client.starts)
	require.Equal(t, StateUnknown, mustServiceJob(t, db, result.ID).State)

	retry, err := service.Start(context.Background(), 7, serviceStartRequest(1, "request-1"), 30)
	require.Error(t, err)
	require.Equal(t, result.ID, retry.ID)
	require.Equal(t, 1, client.starts)
}

func TestWorkServiceUncertainStartKeepsLeaseUntilConfirmedStop(t *testing.T) {
	service, client, prepare, _ := serviceFixture(t)
	client.startErr = errors.New("start timeout")

	started, err := service.Start(context.Background(), 7, serviceStartRequest(1, "request-1"), 30)
	require.ErrorIs(t, err, client.startErr)
	require.Equal(t, StateUnknown, started.State)
	require.Zero(t, prepare.releases.Load())
	require.Equal(t, 1, client.starts)

	client.startErr = nil
	stopped, err := service.Stop(context.Background(), started.ID)
	require.NoError(t, err)
	require.Equal(t, StateStopped, stopped.State)
	require.Equal(t, int32(1), prepare.releases.Load())
	require.Equal(t, 1, client.stops)
}

func TestWorkServiceStopAbortsExactStartingClaimWithoutExternalStop(t *testing.T) {
	service, client, _, db := serviceFixture(t)
	ctx := context.Background()
	jobID := uuid.NewString()
	job := &models.GbWorkRecording{
		ID: jobID, ChannelID: 1, CreatedBy: 7, RequestID: "request-1", State: StateStarting,
		DesiredAction: DesiredActionStart, Version: 1, FileState: FilePending, FormState: FormDraft,
		SchemaVersion: 1, FormJSON: "{}",
	}
	require.NoError(t, db.Create(job).Error)
	_, err := service.recorder.Reserve(ctx, 1, Owner{Kind: OwnerWork, ID: jobID})
	require.NoError(t, err)

	stopped, err := service.Stop(ctx, jobID)
	require.NoError(t, err)
	require.Equal(t, StateStopped, stopped.State)
	require.Equal(t, FileFinalizing, stopped.FileState)
	require.Zero(t, client.stops)
	claim, err := NewClaims(db).Get(ctx, ChannelResource(1))
	require.NoError(t, err)
	require.Equal(t, StateIdle, claim.State)
}

func TestWorkServiceStopWriteFailureDoesNotReleaseOrCallExternalStop(t *testing.T) {
	service, client, _, db := serviceFixture(t)
	result, err := service.Start(context.Background(), 7, serviceStartRequest(1, "request-1"), 30)
	require.NoError(t, err)
	require.NoError(t, db.Exec("CREATE TRIGGER reject_job_stop BEFORE UPDATE ON gb_work_recording WHEN NEW.desired_action = 'stop' BEGIN SELECT RAISE(ABORT, 'unavailable'); END").Error)

	_, err = service.Stop(context.Background(), result.ID)
	require.Error(t, err)
	require.Zero(t, client.stops)
	job := mustServiceJob(t, db, result.ID)
	require.Equal(t, StateRecording, job.State)
	require.Equal(t, "start", job.DesiredAction)
	claim, err := NewClaims(db).Get(context.Background(), ChannelResource(1))
	require.NoError(t, err)
	require.Equal(t, StateRecording, claim.State)
}

func TestWorkServiceConfirmedStopWriteFailureRetainsClaimUntilRetry(t *testing.T) {
	service, client, prepare, db := serviceFixture(t)
	started, err := service.Start(context.Background(), 7, serviceStartRequest(1, "request-1"), 30)
	require.NoError(t, err)
	require.NoError(t, db.Exec("CREATE TRIGGER reject_job_stopped BEFORE UPDATE ON gb_work_recording WHEN NEW.state = 'stopped' BEGIN SELECT RAISE(ABORT, 'unavailable'); END").Error)

	_, err = service.Stop(context.Background(), started.ID)
	require.Error(t, err)
	require.Equal(t, 1, client.stops)
	require.Zero(t, prepare.releases.Load())
	job := mustServiceJob(t, db, started.ID)
	require.Equal(t, StateStopping, job.State)
	claim, err := NewClaims(db).Get(context.Background(), ChannelResource(1))
	require.NoError(t, err)
	require.Equal(t, StateStopped, claim.State)
	_, err = service.Start(context.Background(), 7, serviceStartRequest(1, "request-2"), 30)
	require.ErrorIs(t, err, ErrOwnerConflict)

	require.NoError(t, db.Exec("DROP TRIGGER reject_job_stopped").Error)
	stopped, err := service.Stop(context.Background(), started.ID)
	require.NoError(t, err)
	require.Equal(t, StateStopped, stopped.State)
	require.Equal(t, int32(1), prepare.releases.Load())
}

func TestWorkServiceStopReleasesAfterStopAndAllowsNextJob(t *testing.T) {
	service, client, prepare, _ := serviceFixture(t)
	ctx := context.Background()
	first, err := service.Start(ctx, 7, serviceStartRequest(1, "request-a"), 30)
	require.NoError(t, err)
	stopped, err := service.Stop(ctx, first.ID)
	require.NoError(t, err)
	require.Equal(t, StateStopped, stopped.State)
	require.Equal(t, FileFinalizing, stopped.FileState)
	require.Equal(t, 1, client.stops)
	require.Equal(t, int32(1), prepare.releases.Load())

	second, err := service.Start(ctx, 7, serviceStartRequest(1, "request-b"), 30)
	require.NoError(t, err)
	require.NotEqual(t, first.ID, second.ID)
	require.Equal(t, 2, client.starts)
	require.Len(t, client.directories, 2)
	require.NotEqual(t, client.directories[0], client.directories[1])
	_, err = service.Stop(ctx, second.ID)
	require.NoError(t, err)
	require.Equal(t, int32(2), prepare.releases.Load())
}

func TestWorkServiceStopOldStoppedJobDoesNotTouchNewOwner(t *testing.T) {
	service, client, _, _ := serviceFixture(t)
	ctx := context.Background()
	first, err := service.Start(ctx, 7, serviceStartRequest(1, "request-old"), 30)
	require.NoError(t, err)
	_, err = service.Stop(ctx, first.ID)
	require.NoError(t, err)

	second, err := service.Start(ctx, 7, serviceStartRequest(1, "request-new"), 30)
	require.NoError(t, err)
	client.stopErr = errors.New("old job must not stop current owner")

	repeated, err := service.Stop(ctx, first.ID)
	require.NoError(t, err)
	require.Equal(t, StateStopped, repeated.State)
	require.Equal(t, 1, client.stops)

	client.stopErr = nil
	_, err = service.Stop(ctx, second.ID)
	require.NoError(t, err)
}

func TestWorkServiceGetRefreshesExactOwnerAndPersistsCheckTime(t *testing.T) {
	service, client, _, db := serviceFixture(t)
	ctx := context.Background()
	started, err := service.Start(ctx, 7, serviceStartRequest(1, "request-get-refresh"), 30)
	require.NoError(t, err)
	beforeCalls := client.isCalls
	claimVersion := mustServiceJob(t, db, started.ID).RecorderClaimVersion

	refreshed, err := service.Get(ctx, started.ID)
	require.NoError(t, err)
	require.Equal(t, StateRecording, refreshed.State)
	require.NotNil(t, refreshed.LastCheckedAt)
	require.Equal(t, beforeCalls+1, client.isCalls)
	job := mustServiceJob(t, db, started.ID)
	require.Equal(t, StateRecording, job.State)
	require.NotNil(t, job.LastCheckedAt)
	require.Equal(t, claimVersion, job.RecorderClaimVersion)

	_, err = service.Stop(ctx, started.ID)
	require.NoError(t, err)
}

func TestWorkServiceGetDisconnectStopsAndReleasesWithoutExternalStop(t *testing.T) {
	service, client, prepare, db := serviceFixture(t)
	ctx := context.Background()
	started, err := service.Start(ctx, 7, serviceStartRequest(1, "request-get-disconnect"), 30)
	require.NoError(t, err)
	client.active = false

	refreshed, err := service.Get(ctx, started.ID)
	require.NoError(t, err)
	require.Equal(t, StateStopped, refreshed.State)
	require.Equal(t, FileFinalizing, refreshed.FileState)
	require.NotNil(t, refreshed.LastCheckedAt)
	require.Zero(t, client.stops)
	require.Equal(t, int32(1), prepare.releases.Load())
	claim, err := NewClaims(db).Get(ctx, ChannelResource(1))
	require.NoError(t, err)
	require.Equal(t, StateIdle, claim.State)
}

func TestWorkServiceGetReadErrorPersistsUnknownAndRetainsClaim(t *testing.T) {
	service, client, prepare, db := serviceFixture(t)
	ctx := context.Background()
	started, err := service.Start(ctx, 7, serviceStartRequest(1, "request-get-error"), 30)
	require.NoError(t, err)
	readErr := errors.New("zlm unavailable")
	client.isErr = readErr

	refreshed, err := service.Get(ctx, started.ID)
	require.ErrorIs(t, err, readErr)
	require.Equal(t, StateUnknown, refreshed.State)
	require.NotNil(t, refreshed.LastCheckedAt)
	require.Zero(t, prepare.releases.Load())
	job := mustServiceJob(t, db, started.ID)
	require.Equal(t, StateUnknown, job.State)
	require.NotNil(t, job.LastCheckedAt)
	claim, err := NewClaims(db).Get(ctx, ChannelResource(1))
	require.NoError(t, err)
	require.Equal(t, StateRecording, claim.State)
	require.Equal(t, OwnerWork, claim.OwnerKind)
	require.Equal(t, started.ID, claim.OwnerID)

	client.isErr = nil
	_, err = service.Stop(ctx, started.ID)
	require.NoError(t, err)
}

func TestWorkServiceGetGenerationMismatchPersistsUnknownAndRetainsLease(t *testing.T) {
	service, client, prepare, db := serviceFixture(t)
	ctx := context.Background()
	started, err := service.Start(ctx, 7, serviceStartRequest(1, "request-get-generation"), 30)
	require.NoError(t, err)
	beforeCalls := client.isCalls
	generationErr := errors.New("live generation changed")
	service.recorder.pin = func(context.Context, MediaTarget) (func(), error) {
		return nil, generationErr
	}

	refreshed, err := service.Get(ctx, started.ID)
	require.ErrorIs(t, err, generationErr)
	require.Equal(t, StateUnknown, refreshed.State)
	require.NotNil(t, refreshed.LastCheckedAt)
	require.Equal(t, beforeCalls, client.isCalls)
	require.Zero(t, prepare.releases.Load())
	job := mustServiceJob(t, db, started.ID)
	require.Equal(t, StateUnknown, job.State)
	claim, err := NewClaims(db).Get(ctx, ChannelResource(1))
	require.NoError(t, err)
	require.Equal(t, StateRecording, claim.State)
	require.Equal(t, OwnerWork, claim.OwnerKind)
	require.Equal(t, started.ID, claim.OwnerID)

	service.recorder.pin = func(context.Context, MediaTarget) (func(), error) { return func() {}, nil }
	_, err = service.Stop(ctx, started.ID)
	require.NoError(t, err)
}

func TestWorkServiceGetDoesNotOverwriteStopIntentWithRecordingClaim(t *testing.T) {
	service, client, _, db := serviceFixture(t)
	ctx := context.Background()
	started, err := service.Start(ctx, 7, serviceStartRequest(1, "request-get-stop-intent"), 30)
	require.NoError(t, err)
	require.NoError(t, db.Model(&models.GbWorkRecording{}).Where("id = ?", started.ID).Updates(map[string]any{
		"state": StateStopping, "desired_action": DesiredActionStop,
	}).Error)
	beforeCalls := client.isCalls

	refreshed, err := service.Get(ctx, started.ID)
	require.NoError(t, err)
	require.Equal(t, StateStopping, refreshed.State)
	require.Equal(t, beforeCalls+1, client.isCalls)
	require.Equal(t, DesiredActionStop, mustServiceJob(t, db, started.ID).DesiredAction)

	_, err = service.Stop(ctx, started.ID)
	require.NoError(t, err)
}

func TestWorkServiceGetOldGenerationDoesNotObserveNewOwner(t *testing.T) {
	service, client, _, db := serviceFixture(t)
	ctx := context.Background()
	first, err := service.Start(ctx, 7, serviceStartRequest(1, "request-get-old"), 30)
	require.NoError(t, err)
	_, err = service.Stop(ctx, first.ID)
	require.NoError(t, err)
	// Simulate a durable old result that still needs reconciliation after its
	// channel claim has already been yielded to a later job.
	require.NoError(t, db.Model(&models.GbWorkRecording{}).Where("id = ?", first.ID).Updates(map[string]any{
		"state": StateUnknown, "file_state": FileFinalizing,
	}).Error)
	second, err := service.Start(ctx, 7, serviceStartRequest(1, "request-get-new"), 30)
	require.NoError(t, err)
	beforeCalls := client.isCalls

	_, err = service.Get(ctx, first.ID)
	require.ErrorIs(t, err, ErrOwnerConflict)
	require.Equal(t, beforeCalls, client.isCalls)
	current := mustServiceJob(t, db, second.ID)
	require.Equal(t, StateRecording, current.State)
	claim, err := NewClaims(db).Get(ctx, ChannelResource(1))
	require.NoError(t, err)
	require.Equal(t, OwnerWork, claim.OwnerKind)
	require.Equal(t, second.ID, claim.OwnerID)

	_, err = service.Stop(ctx, second.ID)
	require.NoError(t, err)
}

func TestWorkServiceCanceledStopDoesNotReverseIntoExternalStop(t *testing.T) {
	service, client, _, _ := serviceFixture(t)
	ctx := context.Background()
	started, err := service.Start(ctx, 7, serviceStartRequest(1, "request-1"), 30)
	require.NoError(t, err)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = service.Stop(canceled, started.ID)
	require.Error(t, err)
	require.Zero(t, client.stops)
	job, err := service.Get(ctx, started.ID)
	require.NoError(t, err)
	require.Equal(t, StateRecording, job.State)

	_, err = service.Stop(ctx, started.ID)
	require.NoError(t, err)
}

func TestWorkServiceStartContinuesAfterRequestCancellation(t *testing.T) {
	service, client, prepare, _ := serviceFixture(t)
	requestCtx, cancel := context.WithCancel(context.Background())
	service.prepare = func(ctx context.Context, channelID uint, jobID string) (PreparedRecording, error) {
		prepared, err := prepare.Prepare(ctx, channelID, jobID)
		cancel()
		return prepared, err
	}

	started, err := service.Start(requestCtx, 7, serviceStartRequest(1, "request-canceled-start"), 30)
	require.NoError(t, err)
	require.Equal(t, StateRecording, started.State)
	require.Equal(t, 1, client.starts)
	_, err = service.Stop(context.Background(), started.ID)
	require.NoError(t, err)
}

func TestWorkServiceStopContinuesAfterRequestCancellation(t *testing.T) {
	service, client, _, _ := serviceFixture(t)
	ctx := context.Background()
	started, err := service.Start(ctx, 7, serviceStartRequest(1, "request-canceled-stop"), 30)
	require.NoError(t, err)
	stopCtx, cancel := context.WithCancel(ctx)
	client.beforeStop = cancel

	stopped, err := service.Stop(stopCtx, started.ID)
	require.NoError(t, err)
	require.Equal(t, StateStopped, stopped.State)
	require.Equal(t, 1, client.stops)
}

func TestWorkServiceStopRejectsForeignOrLegacyClaim(t *testing.T) {
	service, client, _, db := serviceFixture(t)
	started, err := service.Start(context.Background(), 7, serviceStartRequest(1, "request-1"), 30)
	require.NoError(t, err)
	require.NoError(t, db.Model(&models.GbRecorderClaim{}).Where("resource_key = ?", ChannelResource(1)).Updates(map[string]any{"owner_kind": OwnerLegacy, "owner_id": "legacy"}).Error)

	_, err = service.Stop(context.Background(), started.ID)
	require.ErrorIs(t, err, ErrOwnerConflict)
	require.Zero(t, client.stops)
}

func mustServiceJob(t *testing.T, db *gorm.DB, id string) models.GbWorkRecording {
	t.Helper()
	var job models.GbWorkRecording
	require.NoError(t, db.First(&job, "id = ?", id).Error)
	return job
}
