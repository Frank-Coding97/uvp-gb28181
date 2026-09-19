// Package resource contains the machine-client resource boundary. It does not
// use the backend user's visibility or shared-device permissions.
package resource

import (
	"context"
	"errors"
	"unicode/utf8"

	"gorm.io/gorm"
)

var (
	ErrResourceNotFound    = errors.New("resource not found")
	ErrInvalidListOptions  = errors.New("invalid resource list options")
	ErrInvalidDepartmentScope = errors.New("invalid department data scope")
	ErrResourceUnavailable = errors.New("resource database unavailable")
)

// The values intentionally match the role data-scope contract used by the
// administration UI: 3 is the current department and 4 includes all active
// descendants. OpenAPI clients do not support the role-level "all" or
// "custom" modes.
const (
	DataScopeDepartment           int8 = 3
	DataScopeDepartmentAndChildren int8 = 4
)

// DepartmentScope is the immutable department boundary attached to an
// authenticated OpenAPI client. OwnerDeptID is trusted from the client row,
// never from an HTTP request.
type DepartmentScope struct {
	OwnerDeptID uint
	DataScope   int8
}

func (scope DepartmentScope) validate() error {
	if scope.OwnerDeptID == 0 {
		return ErrResourceNotFound
	}
	if scope.DataScope != DataScopeDepartment && scope.DataScope != DataScopeDepartmentAndChildren {
		return ErrInvalidDepartmentScope
	}
	return nil
}

// Service performs resource queries using a trusted, exact owner department.
// ownerDeptID is supplied by the OpenAPI authentication chain; it is never
// read from a request query or body.
type Service struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Service { return &Service{db: db} }

type Device struct {
	DeviceID     string `json:"deviceId"`
	Name         string `json:"name"`
	Alias        string `json:"alias"`
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	Status       string `json:"status"`
}

type Channel struct {
	DeviceID     string `json:"deviceId"`
	ChannelID    string `json:"channelId"`
	Name         string `json:"name"`
	Alias        string `json:"alias"`
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	Status       string `json:"status"`
	PTZType      int8   `json:"ptzType"`
}

type DeviceStatus struct {
	DeviceID string `json:"deviceId"`
	Status   string `json:"status"`
}

type ChannelStatus struct {
	DeviceID  string `json:"deviceId"`
	ChannelID string `json:"channelId"`
	Status    string `json:"status"`
}

type DevicePage struct {
	Items    []Device `json:"items"`
	Page     int      `json:"page"`
	PageSize int      `json:"pageSize"`
	Total    int64    `json:"total"`
}

type ChannelPage struct {
	Items    []Channel `json:"items"`
	Page     int       `json:"page"`
	PageSize int       `json:"pageSize"`
	Total    int64     `json:"total"`
}

type DeviceListOptions struct {
	Page     int
	PageSize int
	Keyword  string
	Status   string
}

type ChannelListOptions struct {
	Page     int
	PageSize int
	Keyword  string
	Status   string
}

type deviceRow struct {
	DeviceID     string
	Name         string
	Alias        string
	Manufacturer string
	Model        string
	Status       *int8
}

type channelRow struct {
	DeviceID     string
	ChannelID    string
	Name         string
	Alias        string
	Manufacturer string
	Model        string
	Status       *int8
	PTZType      int8
}

func (s *Service) ListDevices(ctx context.Context, ownerDeptID uint, options DeviceListOptions) (DevicePage, error) {
	options, err := normalizeDeviceOptions(options)
	if err != nil {
		return DevicePage{}, err
	}
	if err := s.checkOwnerDepartment(ctx, ownerDeptID); err != nil {
		return DevicePage{}, err
	}
	query, err := s.deviceQuery(ctx, ownerDeptID, options)
	if err != nil {
		return DevicePage{}, err
	}
	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return DevicePage{}, err
	}
	rows := make([]deviceRow, 0)
	if err := query.Select("d.device_id, d.name, d.alias, d.manufacturer, d.model, d.status").
		Order("d.device_id ASC").
		Offset((options.Page - 1) * options.PageSize).
		Limit(options.PageSize).
		Find(&rows).Error; err != nil {
		return DevicePage{}, err
	}
	items := make([]Device, 0, len(rows))
	for _, row := range rows {
		items = append(items, deviceFromRow(row))
	}
	return DevicePage{Items: items, Page: options.Page, PageSize: options.PageSize, Total: total}, nil
}

func (s *Service) GetDevice(ctx context.Context, ownerDeptID uint, deviceID string) (Device, error) {
	if err := s.checkOwnerDepartment(ctx, ownerDeptID); err != nil {
		return Device{}, err
	}
	if !validGBCode(deviceID) {
		return Device{}, ErrResourceNotFound
	}
	query, err := s.deviceQuery(ctx, ownerDeptID, DeviceListOptions{})
	if err != nil {
		return Device{}, err
	}
	rows := make([]deviceRow, 0, 1)
	if err := query.Where("d.device_id = ?", deviceID).
		Select("d.device_id, d.name, d.alias, d.manufacturer, d.model, d.status").
		Find(&rows).Error; err != nil {
		return Device{}, err
	}
	if len(rows) != 1 {
		return Device{}, ErrResourceNotFound
	}
	return deviceFromRow(rows[0]), nil
}

func (s *Service) GetDeviceStatus(ctx context.Context, ownerDeptID uint, deviceID string) (DeviceStatus, error) {
	device, err := s.GetDevice(ctx, ownerDeptID, deviceID)
	if err != nil {
		return DeviceStatus{}, err
	}
	return DeviceStatus{DeviceID: device.DeviceID, Status: device.Status}, nil
}

func (s *Service) ListChannels(ctx context.Context, ownerDeptID uint, deviceID string, options ChannelListOptions) (ChannelPage, error) {
	options, err := normalizeChannelOptions(options)
	if err != nil {
		return ChannelPage{}, err
	}
	if err := s.checkOwnerDepartment(ctx, ownerDeptID); err != nil {
		return ChannelPage{}, err
	}
	if !validGBCode(deviceID) {
		return ChannelPage{}, ErrResourceNotFound
	}
	if _, err := s.GetDevice(ctx, ownerDeptID, deviceID); err != nil {
		return ChannelPage{}, err
	}
	query, err := s.channelQuery(ctx, ownerDeptID, deviceID, options)
	if err != nil {
		return ChannelPage{}, err
	}
	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return ChannelPage{}, err
	}
	rows := make([]channelRow, 0)
	if err := query.Select("c.device_id, c.channel_id, c.name, c.alias, c.manufacturer, c.model, c.status, c.ptz_type").
		Order("c.channel_id ASC").
		Offset((options.Page - 1) * options.PageSize).
		Limit(options.PageSize).
		Find(&rows).Error; err != nil {
		return ChannelPage{}, err
	}
	items := make([]Channel, 0, len(rows))
	for _, row := range rows {
		items = append(items, channelFromRow(row))
	}
	return ChannelPage{Items: items, Page: options.Page, PageSize: options.PageSize, Total: total}, nil
}

func (s *Service) GetChannel(ctx context.Context, ownerDeptID uint, deviceID, channelID string) (Channel, error) {
	if err := s.checkOwnerDepartment(ctx, ownerDeptID); err != nil {
		return Channel{}, err
	}
	if !validGBCode(deviceID) || !validGBCode(channelID) {
		return Channel{}, ErrResourceNotFound
	}
	query, err := s.channelQuery(ctx, ownerDeptID, deviceID, ChannelListOptions{})
	if err != nil {
		return Channel{}, err
	}
	rows := make([]channelRow, 0, 1)
	if err := query.Where("c.channel_id = ?", channelID).
		Select("c.device_id, c.channel_id, c.name, c.alias, c.manufacturer, c.model, c.status, c.ptz_type").
		Find(&rows).Error; err != nil {
		return Channel{}, err
	}
	if len(rows) != 1 {
		return Channel{}, ErrResourceNotFound
	}
	return channelFromRow(rows[0]), nil
}

func (s *Service) GetChannelStatus(ctx context.Context, ownerDeptID uint, deviceID, channelID string) (ChannelStatus, error) {
	channel, err := s.GetChannel(ctx, ownerDeptID, deviceID, channelID)
	if err != nil {
		return ChannelStatus{}, err
	}
	return ChannelStatus{DeviceID: channel.DeviceID, ChannelID: channel.ChannelID, Status: channel.Status}, nil
}

func (s *Service) checkOwnerDepartment(ctx context.Context, ownerDeptID uint) error {
	if s == nil || s.db == nil {
		return ErrResourceUnavailable
	}
	if ownerDeptID == 0 {
		return ErrResourceNotFound
	}
	var ids []uint
	result := s.db.WithContext(ctx).Table("sys_department").
		Select("id").
		Where("id = ? AND status = ? AND deleted_at IS NULL", ownerDeptID, 1).
		Find(&ids)
	if result.Error != nil {
		return result.Error
	}
	if len(ids) != 1 {
		return ErrResourceNotFound
	}
	return nil
}

func (s *Service) deviceQuery(ctx context.Context, ownerDeptID uint, options DeviceListOptions) (*gorm.DB, error) {
	if s == nil || s.db == nil {
		return nil, ErrResourceUnavailable
	}
	query := s.db.WithContext(ctx).Table("gb_device AS d").
		Joins("JOIN sys_department AS dept ON dept.id = d.owner_dept_id AND dept.status = ? AND dept.deleted_at IS NULL", 1).
		Where("d.owner_dept_id = ? AND d.owner_dept_id <> 0 AND d.deleted_at IS NULL", ownerDeptID).
		Where("NOT EXISTS (SELECT 1 FROM gb_device AS d2 WHERE d2.device_id = d.device_id AND d2.deleted_at IS NULL AND d2.id <> d.id)")
	if options.Keyword != "" {
		keyword := "%" + options.Keyword + "%"
		query = query.Where("(d.device_id LIKE ? OR d.name LIKE ? OR d.alias LIKE ? OR d.manufacturer LIKE ? OR d.model LIKE ?)", keyword, keyword, keyword, keyword, keyword)
	}
	return applyStatusFilter(query, "d.status", options.Status), nil
}

func (s *Service) channelQuery(ctx context.Context, ownerDeptID uint, deviceID string, options ChannelListOptions) (*gorm.DB, error) {
	if s == nil || s.db == nil {
		return nil, ErrResourceUnavailable
	}
	query := s.db.WithContext(ctx).Table("gb_channel AS c").
		Joins("JOIN gb_device AS d ON d.device_id = c.device_id AND d.deleted_at IS NULL").
		Joins("JOIN sys_department AS dept ON dept.id = d.owner_dept_id AND dept.status = ? AND dept.deleted_at IS NULL", 1).
		Where("d.device_id = ? AND d.owner_dept_id = ? AND d.owner_dept_id <> 0", deviceID, ownerDeptID).
		Where("c.owner_dept_id = d.owner_dept_id AND c.deleted_at IS NULL").
		Where("NOT EXISTS (SELECT 1 FROM gb_device AS d2 WHERE d2.device_id = d.device_id AND d2.deleted_at IS NULL AND d2.id <> d.id)").
		Where("NOT EXISTS (SELECT 1 FROM gb_channel AS c2 WHERE c2.device_id = c.device_id AND c2.channel_id = c.channel_id AND c2.deleted_at IS NULL AND c2.id <> c.id)")
	if options.Keyword != "" {
		keyword := "%" + options.Keyword + "%"
		query = query.Where("(c.channel_id LIKE ? OR c.name LIKE ? OR c.alias LIKE ? OR c.manufacturer LIKE ? OR c.model LIKE ?)", keyword, keyword, keyword, keyword, keyword)
	}
	return applyStatusFilter(query, "c.status", options.Status), nil
}

func applyStatusFilter(query *gorm.DB, column, status string) *gorm.DB {
	switch status {
	case "":
		return query
	case "online":
		return query.Where(column+" = ?", 1)
	case "offline":
		return query.Where(column+" = ?", 0)
	case "unknown":
		return query.Where("("+column+" IS NULL OR "+column+" NOT IN (?, ?))", 0, 1)
	default:
		return query
	}
}

func normalizeDeviceOptions(options DeviceListOptions) (DeviceListOptions, error) {
	if options.Page < 0 || options.PageSize < 0 || options.PageSize > 100 || options.Status != "" && !validStatus(options.Status) || utf8.RuneCountInString(options.Keyword) > 100 || !utf8.ValidString(options.Keyword) {
		return DeviceListOptions{}, ErrInvalidListOptions
	}
	if options.Page == 0 {
		options.Page = 1
	}
	if options.PageSize == 0 {
		options.PageSize = 20
	}
	if !validPageOffset(options.Page, options.PageSize) {
		return DeviceListOptions{}, ErrInvalidListOptions
	}
	return options, nil
}

func normalizeChannelOptions(options ChannelListOptions) (ChannelListOptions, error) {
	if options.Page < 0 || options.PageSize < 0 || options.PageSize > 100 || options.Status != "" && !validStatus(options.Status) || utf8.RuneCountInString(options.Keyword) > 100 || !utf8.ValidString(options.Keyword) {
		return ChannelListOptions{}, ErrInvalidListOptions
	}
	if options.Page == 0 {
		options.Page = 1
	}
	if options.PageSize == 0 {
		options.PageSize = 20
	}
	if !validPageOffset(options.Page, options.PageSize) {
		return ChannelListOptions{}, ErrInvalidListOptions
	}
	return options, nil
}

func validPageOffset(page, pageSize int) bool {
	if page < 1 || pageSize < 1 {
		return false
	}
	maxInt := int(^uint(0) >> 1)
	return page-1 <= maxInt/pageSize
}

func validStatus(status string) bool {
	return status == "online" || status == "offline" || status == "unknown"
}

func validGBCode(value string) bool {
	if len(value) != 20 || !utf8.ValidString(value) {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

func deviceFromRow(row deviceRow) Device {
	return Device{DeviceID: row.DeviceID, Name: row.Name, Alias: row.Alias, Manufacturer: row.Manufacturer, Model: row.Model, Status: normalizedStatus(row.Status)}
}

func channelFromRow(row channelRow) Channel {
	return Channel{DeviceID: row.DeviceID, ChannelID: row.ChannelID, Name: row.Name, Alias: row.Alias, Manufacturer: row.Manufacturer, Model: row.Model, Status: normalizedStatus(row.Status), PTZType: row.PTZType}
}

func normalizedStatus(status *int8) string {
	if status == nil {
		return "unknown"
	}
	switch *status {
	case 1:
		return "online"
	case 0:
		return "offline"
	default:
		return "unknown"
	}
}
