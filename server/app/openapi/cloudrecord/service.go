// Package cloudrecord reads the platform's durable gb_recording_file index.
// It does not borrow a background user's identity or issue node credentials.
package cloudrecord

import (
	"context"
	"errors"
	"math"
	"path"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/openapi/adapters"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

const maxPageSize = 100
const maxDateRangeDays = 366

type Service struct {
	db        *gorm.DB
	resources *resource.Service
}

func NewService(db *gorm.DB) *Service { return &Service{db: db, resources: resource.New(db)} }

type Options struct {
	Page          int
	PageSize      int
	StartTime     *time.Time
	EndTime       *time.Time
	MetadataState string
}
type DateOptions struct{ StartDate, EndDate string }

type File struct {
	ID              string     `json:"id"`
	DeviceID        string     `json:"deviceId"`
	ChannelID       string     `json:"channelId"`
	FileName        string     `json:"fileName"`
	StartTime       *time.Time `json:"startTime"`
	DurationSeconds *float64   `json:"durationSeconds"`
	SizeBytes       *uint64    `json:"sizeBytes"`
	RecordDate      *string    `json:"recordDate"`
	Source          string     `json:"source"`
	MetadataState   string     `json:"metadataState"`
	Missing         bool       `json:"missing"`
	DiscoveredAt    time.Time  `json:"discoveredAt"`
	LastSeenAt      *time.Time `json:"lastSeenAt"`
}
type Page struct {
	Items    []File `json:"items"`
	Page     int    `json:"page"`
	PageSize int    `json:"pageSize"`
	Total    int64  `json:"total"`
}
type Date struct {
	Date      string `json:"date"`
	FileCount int64  `json:"fileCount"`
}
type Dates struct {
	Items     []Date `json:"items"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
}

func (s *Service) query(ctx context.Context, scope resource.DepartmentScope, deviceCode, channelCode string) (*gorm.DB, error) {
	if s == nil || s.db == nil || s.resources == nil || ctx == nil {
		return nil, adapters.ErrUnavailable
	}
	// The current device/channel boundary must still allow this client; an old
	// file's saved department alone cannot authorize a moved/deleted resource.
	if _, err := s.resources.GetChannelInScope(ctx, scope, deviceCode, channelCode); err != nil {
		return nil, normalizeError(err)
	}
	departments, err := resource.ResolveDepartmentIDs(ctx, s.db, scope)
	if err != nil {
		return nil, normalizeError(err)
	}
	query := s.db.WithContext(ctx).Table("gb_recording_file AS f").
		Joins("JOIN gb_channel AS c ON c.id = f.channel_id AND c.device_id = f.device_id AND c.deleted_at IS NULL").
		Joins("JOIN gb_device AS d ON d.device_id = c.device_id AND d.owner_dept_id = c.owner_dept_id AND d.deleted_at IS NULL").
		Where("f.channel_id <> 0 AND f.device_id = ? AND c.channel_id = ? AND f.owner_dept_id IN ? AND d.owner_dept_id IN ?", deviceCode, channelCode, departments, departments)
	return query, nil
}
func (s *Service) List(ctx context.Context, scope resource.DepartmentScope, deviceCode, channelCode string, options Options) (Page, error) {
	page, size := options.Page, options.PageSize
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = 20
	}
	if page < 1 || size < 1 || size > maxPageSize || page-1 > math.MaxInt/size || options.StartTime != nil && options.EndTime != nil && !options.StartTime.Before(*options.EndTime) {
		return Page{}, adapters.ErrInvalidRequest
	}
	if options.MetadataState != "" && options.MetadataState != gbmodels.RecordingMetadataComplete && options.MetadataState != gbmodels.RecordingMetadataPartial {
		return Page{}, adapters.ErrInvalidRequest
	}
	query, err := s.query(ctx, scope, deviceCode, channelCode)
	if err != nil {
		return Page{}, err
	}
	if options.StartTime != nil {
		query = query.Where("f.start_time >= ?", *options.StartTime)
	}
	if options.EndTime != nil {
		query = query.Where("f.start_time < ?", *options.EndTime)
	}
	if options.MetadataState != "" {
		query = query.Where("f.metadata_state = ?", options.MetadataState)
	}
	var total int64
	if err = query.Count(&total).Error; err != nil {
		return Page{}, adapters.ErrUnavailable
	}
	rows := make([]gbmodels.GbRecordingFile, 0)
	err = query.Select(fileColumns).Order("CASE WHEN f.start_time IS NULL THEN 1 ELSE 0 END").Order("f.start_time DESC, f.id DESC").Offset((page - 1) * size).Limit(size).Scan(&rows).Error
	if err != nil {
		return Page{}, adapters.ErrUnavailable
	}
	items := make([]File, 0, len(rows))
	for _, row := range rows {
		items = append(items, fileView(row, channelCode))
	}
	return Page{Items: items, Page: page, PageSize: size, Total: total}, nil
}

const fileColumns = "f.id, f.device_id, f.file_name, f.start_time, f.time_len, f.file_size, f.record_date, f.source, f.metadata_state, f.missing_at, f.discovered_at, f.last_seen_at"

func (s *Service) Info(ctx context.Context, scope resource.DepartmentScope, deviceCode, channelCode, fileID string) (File, error) {
	id, err := strconv.ParseUint(fileID, 10, 64)
	if err != nil || id == 0 || strconv.FormatUint(id, 10) != fileID {
		return File{}, adapters.ErrInvalidRequest
	}
	query, err := s.query(ctx, scope, deviceCode, channelCode)
	if err != nil {
		return File{}, err
	}
	var row gbmodels.GbRecordingFile
	result := query.Select(fileColumns).Where("f.id = ?", id).Limit(1).Scan(&row)
	if result.Error != nil {
		return File{}, adapters.ErrUnavailable
	}
	if result.RowsAffected != 1 {
		return File{}, adapters.ErrNotFound
	}
	return fileView(row, channelCode), nil
}

func (s *Service) Dates(ctx context.Context, scope resource.DepartmentScope, deviceCode, channelCode string, options DateOptions) (Dates, error) {
	start, err := time.Parse("2006-01-02", options.StartDate)
	if err != nil {
		return Dates{}, adapters.ErrInvalidRequest
	}
	end, err := time.Parse("2006-01-02", options.EndDate)
	if err != nil || !start.Before(end) || end.Sub(start) > maxDateRangeDays*24*time.Hour {
		return Dates{}, adapters.ErrInvalidRequest
	}
	query, err := s.query(ctx, scope, deviceCode, channelCode)
	if err != nil {
		return Dates{}, err
	}
	items := make([]Date, 0)
	err = query.Where("f.record_date >= ? AND f.record_date < ?", start, end).Select("DATE(f.record_date) AS date, COUNT(*) AS file_count").Group("DATE(f.record_date)").Order("DATE(f.record_date) ASC").Scan(&items).Error
	if err != nil {
		return Dates{}, adapters.ErrUnavailable
	}
	return Dates{Items: items, StartDate: options.StartDate, EndDate: options.EndDate}, nil
}

func fileView(row gbmodels.GbRecordingFile, channelCode string) File {
	var date *string
	if row.RecordDate != nil {
		value := row.RecordDate.Format("2006-01-02")
		date = &value
	}
	return File{ID: strconv.FormatUint(row.ID, 10), DeviceID: row.DeviceID, ChannelID: channelCode, FileName: path.Base(strings.ReplaceAll(row.FileName, "\\", "/")), StartTime: row.StartTime, DurationSeconds: row.TimeLen, SizeBytes: row.FileSize, RecordDate: date, Source: row.Source, MetadataState: row.MetadataState, Missing: row.MissingAt != nil, DiscoveredAt: row.DiscoveredAt, LastSeenAt: row.LastSeenAt}
}
func normalizeError(err error) error {
	switch {
	case errors.Is(err, resource.ErrResourceNotFound):
		return adapters.ErrNotFound
	case errors.Is(err, resource.ErrInvalidDepartmentScope), errors.Is(err, resource.ErrInvalidListOptions):
		return adapters.ErrInvalidRequest
	default:
		return adapters.ErrUnavailable
	}
}
