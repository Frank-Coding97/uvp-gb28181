package gb28181

import (
	"context"
	"errors"
	"fmt"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"strings"
	"sync/atomic"
	"time"
	gbroutes "uvplatform.cn/uvp-gb28181/app/gb28181/routes"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	gbrecording "uvplatform.cn/uvp-gb28181/app/gb28181/recording"
	"uvplatform.cn/uvp-gb28181/app/gb28181/workrecording"
	gbzlm "uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	gbzlmmanagement "uvplatform.cn/uvp-gb28181/app/gb28181/zlm/management"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// The gate lives for the process, across SIP runtime reloads. Old and new
// adapters must never operate the same MP4 resources using different locks.
var recordingMutationGate atomic.Pointer[workrecording.MP4MutationGate]

func recoverRecordingProtection(ctx context.Context) error {
	if app.DB() == nil {
		return fmt.Errorf("录像占用恢复缺少数据库")
	}
	gate := recordingMutationGate.Load()
	if gate == nil {
		candidate := workrecording.NewMP4MutationGate(workrecording.NewClaims(app.DB()))
		recordingMutationGate.CompareAndSwap(nil, candidate)
		gate = recordingMutationGate.Load()
	}
	gbroutes.SetWorkRecordingSourceLeaseChecker(persistedWorkLeases{})
	gate.SetWorkStartCheck(checkLegacyRecordingIdle)
	if err := gate.RecoverLegacy(ctx); err != nil {
		return fmt.Errorf("恢复录像占用失败: %w", err)
	}
	return nil
}

func guardRecordingSourceClose(ctx context.Context, streamID string, fn func(context.Context) error) error {
	gate := recordingMutationGate.Load()
	if gate == nil || app.DB() == nil {
		return workrecording.ErrInvalidRequest
	}
	channel, err := gbrecording.NewGormRepo(app.DB()).FindChannelByStream(ctx, streamID)
	if err != nil {
		return err
	}
	var channelID uint
	if channel != nil {
		channelID = channel.ID
	}
	// A zero node searches all nodes for this stream, protecting persisted
	// work claims before the in-memory location map has been recovered.
	err = gate.GuardSourceClose(ctx, channelID, 0, streamID, fn)
	if errors.Is(err, workrecording.ErrOwnerConflict) {
		return errors.Join(play.ErrSourceProtected, err)
	}
	return err
}

func newWorkRecorder(live *play.Service) *workrecording.Recorder {
	recorder := workrecording.NewRecorder(workrecording.NewClaims(app.DB()), func(ctx context.Context, target workrecording.MediaTarget) (func(), error) {
		if live == nil {
			return nil, workrecording.ErrInvalidRequest
		}
		ref, ok := live.CurrentLiveRef(target.Stream)
		if !ok || ref.NodeID != target.NodeID || ref.Generation != target.Generation {
			return nil, workrecording.ErrVersionConflict
		}
		return live.PinLiveGeneration(ref)
	}, func(target workrecording.MediaTarget) (workrecording.MP4Client, error) {
		if zlmRegistry == nil {
			return nil, workrecording.ErrInvalidRequest
		}
		n, ok := zlmRegistry.Get(target.NodeID)
		if !ok {
			return nil, fmt.Errorf("录像节点不可用")
		}
		return gbzlm.NewClientForNode(n), nil
	})
	recorder.SetMutationGate(recordingMutationGate.Load())
	return recorder
}

func checkLegacyRecordingIdle(ctx context.Context, channelID uint, target workrecording.MediaTarget) error {
	var channel models.GbChannel
	result := app.DB().WithContext(ctx).Select("id", "cloud_recording_enabled").First(&channel, channelID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	if channel.CloudRecordingEnabled {
		return workrecording.ErrOwnerConflict
	}
	var active int64
	err := app.DB().WithContext(ctx).Model(&models.GbRecordingSession{}).
		Where("state <> ?", models.RecordingSessionStateStopped).
		Where("channel_id = ? OR (node_id = ? AND vhost = ? AND app = ? AND stream = ?)", channelID, target.NodeID, target.VHost, target.App, target.Stream).Count(&active).Error
	if err != nil {
		return err
	}
	if active != 0 {
		return workrecording.ErrOwnerConflict
	}
	return nil
}

func guardLegacyRecordingOperation(ctx context.Context, channelID uint, streamID string, fn func(context.Context) error) error {
	gate := recordingMutationGate.Load()
	if gate == nil {
		return workrecording.ErrInvalidRequest
	}
	return gate.GuardSourceClose(ctx, channelID, 0, streamID, fn)
}

func guardManagementMutation(ctx context.Context, target gbzlmmanagement.OwnershipTarget, closeSource bool, fn func(context.Context) error) error {
	gate := recordingMutationGate.Load()
	if gate == nil || app.DB() == nil {
		return workrecording.ErrInvalidRequest
	}
	channel, err := gbrecording.NewGormRepo(app.DB()).FindChannelByStream(ctx, target.Media.Stream)
	if err != nil {
		return err
	}
	var channelID uint
	if channel != nil {
		channelID = channel.ID
	}
	if closeSource {
		err = gate.GuardSourceClose(ctx, channelID, target.NodeID, target.Media.Stream, fn)
	} else {
		err = gate.LegacyMutation(ctx, channelID, workrecording.MediaTarget{NodeID: target.NodeID, VHost: target.Media.Vhost, App: target.Media.App, Stream: target.Media.Stream}, fn)
	}
	if errors.Is(err, workrecording.ErrOwnerConflict) {
		return gbzlmmanagement.NewOwnershipConflictError(fmt.Sprint(target.NodeID), "媒体正由作业录像占用", err)
	}
	return err
}
func guardManagementMP4(ctx context.Context, target gbzlmmanagement.OwnershipTarget, fn func(context.Context) error) error {
	return guardManagementMutation(ctx, target, false, fn)
}
func guardManagementSource(ctx context.Context, target gbzlmmanagement.OwnershipTarget, fn func(context.Context) error) error {
	return guardManagementMutation(ctx, target, true, fn)
}

type workDirectoryAPI interface {
	GetServerConfig(context.Context) (map[string]string, error)
	GetMP4RecordFilesInDirectory(context.Context, string, string, string, string, string) (*gbzlm.MP4RecordListing, error)
	ProbeMP4RecordRoot(context.Context, string, string, string) (*gbzlm.MP4RecordListing, error)
}

func prepareWorkDirectory(ctx context.Context, client workDirectoryAPI, target workrecording.MediaTarget, jobID string) (string, error) {
	config, err := client.GetServerConfig(ctx)
	if err != nil {
		return "", err
	}
	root, err := resolveWorkRecordRoot(ctx, client, config, target)
	if err != nil {
		return "", err
	}
	candidate, err := workrecording.WorkDirectory(root, jobID)
	if err != nil {
		return "", err
	}
	listing, err := client.GetMP4RecordFilesInDirectory(ctx, target.VHost, target.App, target.Stream, "", candidate)
	if err != nil {
		return "", err
	}
	if listing == nil {
		return "", workrecording.ErrAttributionUnknown
	}
	return workrecording.ResolvedWorkDirectory(listing.RootPath, jobID)
}

// resolveWorkRecordRoot returns the absolute node directory under which per-job
// work directories are created.
//
// An explicitly configured absolute record.filePath wins, because the operator's
// declaration is authoritative. When it is absent the node's own root is used
// instead: ZLM keeps its default in code, so a node that never had the key
// written into config.ini reports nothing for it, and demanding a human copy a
// value the node already knows is what made every fresh node fail to record.
//
// A configured value that is not absolute is deliberately ignored rather than
// used: the node echoes a relative custom path back relative, and
// ResolvedWorkDirectory refuses anything that is not absolute, so trusting it
// would fail later with a message that points at the wrong thing.
func resolveWorkRecordRoot(ctx context.Context, client workDirectoryAPI, config map[string]string, target workrecording.MediaTarget) (string, error) {
	configured := strings.TrimSpace(config["record.filePath"])
	if configured != "" && !workrecording.AbsoluteNodePath(configured) && app.ZapLog != nil {
		// Do not fail here: the node's own root is usable and the configuration
		// mistake is worth reporting without blocking recording.
		app.ZapLog.Warn("节点录像根目录配置为相对路径，已改用节点默认根目录",
			zap.String("recordFilePath", configured),
			zap.String("app", target.App),
			zap.String("stream", target.Stream))
	}
	if workrecording.AbsoluteNodePath(configured) {
		return configured, nil
	}
	listing, err := client.ProbeMP4RecordRoot(ctx, target.VHost, target.App, target.Stream)
	if err != nil {
		return "", err
	}
	if listing == nil {
		return "", workrecording.ErrAttributionUnknown
	}
	root, err := workrecording.RecordRootFromProbe(listing.RootPath, config["record.appName"], target.App, target.Stream)
	if err != nil {
		return "", fmt.Errorf("%w: 无法确定节点录像根目录(record.filePath 未配置且探测结果不可用)，请在「媒体管理」中设置录像文件路径", err)
	}
	return root, nil
}

func prepareWorkRecording(live *play.Service, leases *play.SourceLeaseRegistry) workrecording.PrepareFunc {
	return func(ctx context.Context, channelID uint, jobID string) (workrecording.PreparedRecording, error) {
		var prepared workrecording.PreparedRecording
		var channel models.GbChannel
		queryResult := app.DB().WithContext(ctx).First(&channel, channelID)
		if queryResult.Error != nil {
			return prepared, queryResult.Error
		}
		if queryResult.RowsAffected == 0 {
			return prepared, gorm.ErrRecordNotFound
		}
		result, err := live.EnsureLive(ctx, play.Request{DeviceID: channel.DeviceID, ChannelID: channel.ChannelID, Trigger: "work-recording"})
		if err != nil {
			return prepared, err
		}
		if result == nil || result.Node == nil || result.Generation == 0 || result.StreamID == "" {
			return prepared, workrecording.ErrAttributionUnknown
		}
		lease := leases.Acquire(result.StreamID, result.Generation, "work-recording:"+jobID)
		prepared.Release = func() { _ = lease.Release() }
		prepared.Target = workrecording.MediaTarget{NodeID: result.Node.ID, VHost: models.DefaultRecordingVHost, App: result.App, Stream: result.StreamID, Generation: result.Generation}
		n, ok := zlmRegistry.Get(result.Node.ID)
		if !ok {
			return prepared, workrecording.ErrAttributionUnknown
		}
		root, err := prepareWorkDirectory(ctx, gbzlm.NewClientForNode(n), prepared.Target, jobID)
		if err != nil {
			return prepared, err
		}
		prepared.Target.RecordingRoot = root
		return prepared, nil
	}
}

func restoreWorkGenerationFloor(ctx context.Context, live *play.Service) error {
	var jobs, claims uint64
	if err := app.DB().WithContext(ctx).Model(&models.GbWorkRecording{}).Select("COALESCE(MAX(generation), 0)").Scan(&jobs).Error; err != nil {
		return err
	}
	if err := app.DB().WithContext(ctx).Model(&models.GbRecorderClaim{}).Select("COALESCE(MAX(generation), 0)").Scan(&claims).Error; err != nil {
		return err
	}
	if claims > jobs {
		jobs = claims
	}
	live.SetGenerationFloor(jobs)
	return nil
}

// Persistent claims also protect hooks while a new runtime is booting, before
// its in-memory leases and playback stopper have been published.
type persistedWorkLeases struct{}

func (persistedWorkLeases) HasLease(streamID string) bool {
	if streamID == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return hasPersistedWorkLease(ctx, app.DB(), streamID)
}

func hasPersistedWorkLease(ctx context.Context, db *gorm.DB, streamID string) bool {
	if db == nil {
		return true
	}
	var count int64
	channels := db.WithContext(ctx).Model(&models.GbChannel{}).Select("id").Where("stream_id = ?", streamID)
	err := db.WithContext(ctx).Model(&models.GbRecorderClaim{}).
		Where("owner_kind = ? AND state <> ?", workrecording.OwnerWork, workrecording.StateIdle).
		Where("stream = ? OR channel_id IN (?)", streamID, channels).Count(&count).Error
	return err != nil || count != 0
}
