package recording

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/workrecording"
)

// RecordMP4Indexer is the hook-facing contract implemented by both the
// legacy indexer and this work-aware wrapper.
type RecordMP4Indexer interface {
	IndexRecordMP4(context.Context, int64, RecordMP4Event) (bool, error)
}

// WorkFileIndexer first resolves an isolated work-recording path against its
// durable job identity. Ordinary legacy paths retain the existing indexer
// behavior; a path that claims to be under a work directory fails closed when
// no exact job can be proved.
type WorkFileIndexer struct {
	db     *gorm.DB
	legacy RecordMP4Indexer
}

func NewWorkFileIndexer(db *gorm.DB, legacy RecordMP4Indexer) *WorkFileIndexer {
	return &WorkFileIndexer{db: db, legacy: legacy}
}

// IndexRecordMP4 indexes one authenticated on_record_mp4 callback. A work
// callback writes the catalog row and its job relation in one transaction so
// a relation failure cannot leave a file that appears indexed but is not
// attributable to the job. This records observed files only; it does not
// infer that all tail files have completed.
func (i *WorkFileIndexer) IndexRecordMP4(ctx context.Context, nodeID int64, event RecordMP4Event) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if i == nil {
		return false, workrecording.ErrAttributionUnknown
	}
	input := workrecording.FileAttributionInput{
		NodeID: nodeID, VHost: event.VHost, App: event.App,
		Stream: event.Stream, FilePath: event.FilePath,
	}
	job, err := workrecording.NewFileAttributionRepository(i.db).Resolve(ctx, input)
	if err != nil {
		if errors.Is(err, workrecording.ErrAttributionUnknown) {
			if workRecordingPath(event) {
				return false, err
			}
			if i == nil || i.legacy == nil {
				return false, err
			}
			return i.legacy.IndexRecordMP4(ctx, nodeID, event)
		}
		return false, err
	}
	if job == nil {
		return false, workrecording.ErrAttributionUnknown
	}
	if i == nil || i.db == nil {
		return false, workrecording.ErrAttributionUnknown
	}

	var indexed bool
	err = i.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Re-resolve inside the write transaction. The initial lookup selects
		// the work path; this second lookup prevents a concurrent job edit from
		// changing the attribution between the read and the catalog write.
		resolved, resolveErr := workrecording.NewFileAttributionRepository(tx).Resolve(ctx, input)
		if resolveErr != nil {
			return resolveErr
		}
		if resolved == nil || resolved.ID != job.ID {
			return workrecording.ErrAttributionUnknown
		}

		file := workRecordingFile(resolved, nodeID, event)
		if err := loadRecordingSnapshots(tx, file); err != nil {
			return err
		}
		now := time.Now()
		if _, _, err := upsertCatalogFile(tx, file, now); err != nil {
			return err
		}

		link := &models.GbWorkRecordingFile{
			FileID: file.ID, WorkRecordingID: resolved.ID,
			Evidence: "isolated-directory:" + resolved.ID,
		}
		result := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "file_id"}},
			DoNothing: true,
		}).Create(link)
		if result.Error != nil {
			return result.Error
		}

		var stored models.GbWorkRecordingFile
		if err := tx.Where("file_id = ?", file.ID).First(&stored).Error; err != nil {
			return err
		}
		if stored.WorkRecordingID != resolved.ID {
			return errors.New("recording file is already linked to another work recording")
		}
		indexed = true
		return nil
	})
	return indexed, err
}

func workRecordingFile(job *models.GbWorkRecording, nodeID int64, event RecordMP4Event) *models.GbRecordingFile {
	startTime := event.StartTime
	timeLen := event.TimeLen
	fileSize := event.FileSize
	return &models.GbRecordingFile{
		// GbWorkRecording.DeviceID is the customer form's business identifier,
		// not necessarily the GB device ID. Leave it empty so
		// loadRecordingSnapshots fills the catalog snapshot from the channel.
		ChannelID: job.ChannelID,
		NodeID:    nodeID, VHost: event.VHost, App: event.App, Stream: event.Stream,
		FileName: event.FileName, FilePath: event.FilePath,
		Folder: event.Folder, URL: event.URL, StartTime: &startTime,
		TimeLen: &timeLen, FileSize: &fileSize,
		Source:        models.RecordingFileSourceHook,
		MetadataState: models.RecordingMetadataComplete,
	}
}

func workRecordingPath(event RecordMP4Event) bool {
	for _, value := range []string{event.FilePath, event.Folder, event.URL} {
		value = strings.ReplaceAll(value, `\`, "/")
		value = "/" + strings.TrimLeft(value, "/")
		if strings.Contains(strings.ToLower(value), "/work-recordings/") {
			return true
		}
	}
	return false
}
