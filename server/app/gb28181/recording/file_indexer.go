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
	repo Repository
}

func NewFileIndexer(repo Repository) *FileIndexer {
	return &FileIndexer{repo: repo}
}

func (i *FileIndexer) IndexRecordMP4(ctx context.Context, nodeID int64, event RecordMP4Event) (bool, error) {
	session, err := i.repo.FindSessionByMedia(ctx, nodeID, event.VHost, event.App, event.Stream)
	if err != nil || session == nil {
		return false, err
	}
	return i.repo.InsertFile(ctx, &models.GbRecordingFile{
		SessionID: &session.ID, ChannelID: session.ChannelID, DeviceID: session.DeviceID,
		NodeID: nodeID, VHost: event.VHost, App: event.App, Stream: event.Stream,
		FileName: event.FileName, FilePath: event.FilePath, Folder: event.Folder, URL: event.URL,
		StartTime: event.StartTime, TimeLen: event.TimeLen, FileSize: event.FileSize,
	})
}
