package recording

import (
	"context"
	"time"

	"uvplatform.com/uvp-gb28181/app/gb28181/models"
)

type RecordMP4Event struct {
	VHost     string
	App       string
	Stream    string
	FileName  string
	FilePath  string
	Folder    string
	URL       string
	StartTime time.Time
	TimeLen   float64
	FileSize  uint64
}

type FileIndexer struct {
	repo      FileIndexRepository
	locations LocationLookup
}

type FileIndexRepository interface {
	FindSessionByMedia(context.Context, int64, string, string, string) (*models.GbRecordingSession, error)
	FindChannelByStream(context.Context, string) (*models.GbChannel, error)
	UpsertCompleteFile(context.Context, RecordingFileAttribution, RecordMP4Event) error
}

type RecordingFileAttribution struct {
	NodeID    int64
	SessionID *uint64
	ChannelID uint
	DeviceID  string
}

func NewFileIndexer(repo FileIndexRepository, locations ...LocationLookup) *FileIndexer {
	indexer := &FileIndexer{repo: repo}
	if len(locations) > 0 {
		indexer.locations = locations[0]
	}
	return indexer
}

// 归属解析顺序：录像会话（计划录像）→ 当前在推流通道（点播/实况）。
// 两条路都走不通（流在平台上、但不属于任何通道）时**仍然入库**，归属置零：
// 这类行是录像缓存任务（设备录像下载）的分片产物 —— recordcache 靠本表拿
// 时长/大小推进度（见 recordcache/index.go），不能不写。
// ⛔ 它们不进云端录像列表的职责由列表查询承担（catalog_repo 里排除
// channel_id=0 的行），不在这里靠"拒写"实现 —— 拒写会让缓存任务退化成
// 按墙钟推进（2026-10-20 定位，老板实测云端录像列表混入下载分片）。
func (i *FileIndexer) IndexRecordMP4(ctx context.Context, nodeID int64, event RecordMP4Event) (bool, error) {
	session, err := i.repo.FindSessionByMedia(ctx, nodeID, event.VHost, event.App, event.Stream)
	if err != nil {
		return false, err
	}
	var attribution RecordingFileAttribution
	if session != nil {
		attribution = RecordingFileAttribution{NodeID: nodeID, SessionID: &session.ID, ChannelID: session.ChannelID, DeviceID: session.DeviceID}
	} else {
		if i.locations == nil {
			return false, nil
		}
		locatedNodeID, ok := i.locations.Lookup(event.Stream)
		if !ok || locatedNodeID != nodeID {
			return false, nil
		}
		// ⭐ NodeID 必须无条件带上：零归属行也要能按节点定位文件
		attribution = RecordingFileAttribution{NodeID: nodeID}
		channel, findErr := i.repo.FindChannelByStream(ctx, event.Stream)
		if findErr != nil {
			return false, findErr
		}
		if channel != nil {
			attribution.ChannelID = channel.ID
			attribution.DeviceID = channel.DeviceID
		}
		// channel == nil：流在平台 RTP 注册表里但查不到通道 —— 缓存/回放流，零归属入库
	}
	if err := i.repo.UpsertCompleteFile(ctx, attribution, event); err != nil {
		return false, err
	}
	return true, nil
}
