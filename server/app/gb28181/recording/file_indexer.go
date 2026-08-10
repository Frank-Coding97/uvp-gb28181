package recording

import (
	"context"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
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

func (i *FileIndexer) IndexRecordMP4(ctx context.Context, nodeID int64, event RecordMP4Event) (bool, error) {
	session, err := i.repo.FindSessionByMedia(ctx, nodeID, event.VHost, event.App, event.Stream)
	if err != nil {
		return false, err
	}
	attribution := RecordingFileAttribution{NodeID: nodeID}
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
		channel, findErr := i.repo.FindChannelByStream(ctx, event.Stream)
		if findErr != nil {
			return false, findErr
		}
		if channel == nil {
			return false, nil
		}
		attribution = RecordingFileAttribution{NodeID: nodeID, ChannelID: channel.ID, DeviceID: channel.DeviceID}
	}
	if err := i.repo.UpsertCompleteFile(ctx, attribution, event); err != nil {
		return false, err
	}
	return true, nil
}
