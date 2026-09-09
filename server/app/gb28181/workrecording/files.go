package workrecording

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// FileAttributionInput is the server-observed identity of one MP4 file. The
// media tuple is queried exactly; it is never inferred from the current claim
// or the reusable recording session.
type FileAttributionInput struct {
	NodeID   int64
	VHost    string
	App      string
	Stream   string
	FilePath string
}

// FileAttributionRepository resolves a completed MP4 path to one durable work
// recording. It is read-only so a caller can decide separately how to persist
// the file association or an unknown-attribution record.
type FileAttributionRepository struct {
	db *gorm.DB
}

func NewFileAttributionRepository(db *gorm.DB) *FileAttributionRepository {
	return &FileAttributionRepository{db: db}
}

// Resolve returns a job only when exactly one historical job with the supplied
// node/media tuple owns the normalized path. A missing or ambiguous match is
// deliberately indistinguishable from an invalid path to prevent guessing.
func (r *FileAttributionRepository) Resolve(ctx context.Context, input FileAttributionInput) (*models.GbWorkRecording, error) {
	filePath, ok := attributionFilePath(input.FilePath)
	if r == nil || r.db == nil || !validAttributionMedia(input) || !ok {
		return nil, ErrAttributionUnknown
	}
	jobID, ok := workJobIDFromPath(filePath)
	if !ok {
		return nil, ErrAttributionUnknown
	}

	var job models.GbWorkRecording
	result := r.db.WithContext(ctx).
		Where("id = ? AND node_id = ? AND v_host = ? AND app = ? AND stream = ?", jobID, input.NodeID, input.VHost, input.App, input.Stream).
		First(&job)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrAttributionUnknown
		}
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrAttributionUnknown
	}
	// Some business databases use case-insensitive text collations. Verify
	// the exact media tuple again before accepting the directory evidence.
	if job.NodeID != input.NodeID || job.VHost != input.VHost || job.App != input.App || job.Stream != input.Stream || !workRootContainsFile(job.RecordingRoot, job.ID, filePath) {
		return nil, ErrAttributionUnknown
	}
	return &job, nil
}

func validAttributionMedia(input FileAttributionInput) bool {
	if input.NodeID <= 0 || input.VHost == "" || input.App == "" || input.Stream == "" {
		return false
	}
	if len(input.VHost) > 128 || len(input.App) > 64 || len(input.Stream) > 64 {
		return false
	}
	return !strings.ContainsRune(input.VHost, '\x00') &&
		!strings.ContainsRune(input.App, '\x00') &&
		!strings.ContainsRune(input.Stream, '\x00')
}

func attributionFilePath(value string) (string, bool) {
	if value == "" || strings.ContainsRune(value, '\x00') {
		return "", false
	}
	cleaned := cleanNodePath(value)
	if !absoluteNodePath(cleaned) {
		return "", false
	}
	return cleaned, true
}

func workJobIDFromPath(filePath string) (string, bool) {
	const marker = "/work-recordings/"
	if strings.Count(filePath, marker) != 1 {
		return "", false
	}
	segment := filePath[strings.Index(filePath, marker)+len(marker):]
	separator := strings.IndexByte(segment, '/')
	if separator <= 0 {
		return "", false
	}
	jobID := segment[:separator]
	parsed, err := uuid.Parse(jobID)
	if err != nil || parsed.String() != jobID {
		return "", false
	}
	return jobID, true
}

func workRootContainsFile(root, jobID, filePath string) bool {
	if !ownsWorkDirectory(root, jobID) {
		return false
	}
	root = cleanNodePath(root)
	return filePath != root && strings.HasPrefix(filePath, root+"/")
}
