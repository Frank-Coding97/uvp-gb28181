package devicesessions

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"uvplatform.com/uvp-gb28181/app/gb28181/devicecapture"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	runtimeptz "uvplatform.com/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

var (
	ErrNotFound               = errors.New("device session not found")
	ErrInvalidRequest         = errors.New("device session request is invalid")
	ErrConflict               = errors.New("device session request conflicts with an existing idempotency key")
	ErrDeviceOffline          = errors.New("device session target is offline")
	ErrCapabilityNotSupported = errors.New("device snapshot capture capability is not supported")
	ErrInvalidUploadBaseURL   = errors.New("device snapshot upload base URL is invalid")
	ErrUnavailable            = errors.New("device session service unavailable")
	ErrStopUnsupported        = errors.New("device snapshot stop is not supported by the domain service")
)

const (
	CaptureStateCreating = "creating"
	CaptureStateWaiting  = "waiting"
	CaptureStateFailed   = "failed"
	CaptureStateUnknown  = "unknown"
	maxCaptureBody       = 4096
)

// CaptureSessionRecord is the durable ownership and idempotency boundary for
// OpenAPI-created device captures. User identities and OpenAPI client IDs live
// in different columns and are never converted into one another.
type CaptureSessionRecord struct {
	ID                uint64    `gorm:"primaryKey"`
	ClientID          int64     `gorm:"column:client_id;not null;uniqueIndex:uk_openapi_capture_idem,priority:1;index:idx_openapi_capture_session_client,priority:2"`
	IdempotencyDigest string    `gorm:"column:idempotency_digest;size:64;not null;uniqueIndex:uk_openapi_capture_idem,priority:2"`
	RequestDigest     string    `gorm:"column:request_digest;size:64;not null"`
	SessionID         string    `gorm:"column:session_id;size:36;not null;uniqueIndex:uk_openapi_capture_session"`
	RegistrySessionID string    `gorm:"column:registry_session_id;size:36;not null;default:'';index:idx_openapi_capture_registry_session"`
	OperationID       string    `gorm:"column:operation_id;size:64;not null;default:''"`
	DeviceID          string    `gorm:"column:device_id;size:20;not null;index:idx_openapi_capture_session_client,priority:3"`
	ChannelID         string    `gorm:"column:channel_id;size:20;not null;index:idx_openapi_capture_session_client,priority:4"`
	InternalDeviceID  uint      `gorm:"column:internal_device_id;not null"`
	InternalChannelID uint      `gorm:"column:internal_channel_id;not null"`
	DepartmentID      uint      `gorm:"column:department_id;not null"`
	SnapNum           int       `gorm:"column:snap_num;not null"`
	Interval          int       `gorm:"column:interval_seconds;not null"`
	State             string    `gorm:"column:state;size:24;not null;index:idx_openapi_capture_state_updated,priority:1"`
	CreatedAt         time.Time `gorm:"column:created_at;not null;index:idx_openapi_capture_state_updated,priority:2"`
	UpdatedAt         time.Time `gorm:"column:updated_at;not null"`
}

func (CaptureSessionRecord) TableName() string { return "sys_openapi_device_capture_session" }

type CaptureRequest struct {
	ClientID       int64
	Scope          resource.DepartmentScope
	DeviceID       string
	ChannelID      string
	IdempotencyKey string
	SnapNum        int
	Interval       int
}

type CaptureImage struct {
	FileName   string    `json:"fileName"`
	Size       int64     `json:"size"`
	MD5        string    `json:"md5"`
	CapturedAt time.Time `json:"capturedAt"`
	ContentURL string    `json:"contentUrl"`
}

type CaptureSession struct {
	SessionID         string         `json:"sessionId"`
	DeviceID          string         `json:"deviceId"`
	ChannelID         string         `json:"channelId"`
	State             string         `json:"state"`
	CommandStatus     string         `json:"commandStatus"`
	SnapNum           int            `json:"snapNum"`
	Interval          int            `json:"interval"`
	ReceivedCount     int            `json:"receivedCount"`
	NotifiedCount     int            `json:"notifiedCount"`
	Images            []CaptureImage `json:"images"`
	CreatedAt         time.Time      `json:"createdAt"`
	UpdatedAt         time.Time      `json:"updatedAt"`
	registrySessionID string
}

type ImageContent struct {
	FileName    string
	ContentType string
	MD5         string
	Bytes       []byte
}

type CaptureRegistry interface {
	Create(devicecapture.CreateRequest) devicecapture.Session
	GetForOwner(string, string) (devicecapture.Session, bool)
	MarkWaiting(string)
	MarkFailed(string, error)
	FilePath(string) (string, error)
}

type DeviceCommandService interface {
	Execute(context.Context, runtimeptz.Target, runtimeptz.Command) (gbmodels.GbPTZOperation, error)
	GetOperation(context.Context, string) (gbmodels.GbPTZOperation, error)
}

type UploadURLBuilder func(context.Context, string) (string, error)

type Config struct {
	DB        *gorm.DB
	Captures  CaptureRegistry
	Commands  DeviceCommandService
	UploadURL UploadURLBuilder
	Now       func() time.Time
}

type Service struct {
	db        *gorm.DB
	captures  CaptureRegistry
	commands  DeviceCommandService
	uploadURL UploadURLBuilder
	now       func() time.Time
}

func NewService(config Config) *Service {
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &Service{db: config.DB, captures: config.Captures, commands: config.Commands, uploadURL: config.UploadURL, now: now}
}

func (s *Service) Ready() bool {
	return s != nil && s.db != nil && s.captures != nil && s.commands != nil && s.uploadURL != nil
}

func (s *Service) CreateCapture(ctx context.Context, request CaptureRequest) (CaptureSession, error) {
	if !s.Ready() || ctx == nil {
		return CaptureSession{}, ErrUnavailable
	}
	if request.ClientID <= 0 || !validGBID(request.DeviceID) || !validGBID(request.ChannelID) ||
		request.SnapNum < 1 || request.SnapNum > 10 || request.Interval < 1 || request.Interval > 3600 || !validIdempotencyKey(request.IdempotencyKey) {
		return CaptureSession{}, ErrInvalidRequest
	}
	target, channel, err := s.resolveTarget(ctx, request.Scope, request.DeviceID, request.ChannelID)
	if err != nil {
		return CaptureSession{}, err
	}
	if target.Profile.Version != protocol.Version2022 {
		return CaptureSession{}, ErrCapabilityNotSupported
	}
	record, created, err := s.reserve(ctx, request, target, channel)
	if err != nil {
		return CaptureSession{}, err
	}
	if !created {
		if record.RequestDigest != requestDigest(request) {
			return CaptureSession{}, ErrConflict
		}
		return s.captureView(ctx, record)
	}

	owner := clientOwner(request.ClientID)
	registrySession := s.captures.Create(devicecapture.CreateRequest{
		OwnerID: owner, ChannelID: strconv.FormatUint(uint64(channel.ID), 10),
		ChannelCode: channel.ChannelID, DeviceCode: target.DeviceCode,
		SnapNum: request.SnapNum, Interval: request.Interval,
	})
	if err := s.updateRecord(ctx, record, map[string]any{
		"registry_session_id": registrySession.ID,
		"updated_at":          s.now().UTC(),
	}); err != nil {
		s.captures.MarkFailed(registrySession.ID, err)
		return CaptureSession{}, ErrUnavailable
	}
	record.RegistrySessionID = registrySession.ID

	uploadURL, err := s.uploadURL(ctx, registrySession.UploadToken)
	if err != nil {
		s.captures.MarkFailed(registrySession.ID, err)
		_ = s.updateRecord(ctx, record, map[string]any{"state": CaptureStateFailed, "updated_at": s.now().UTC()})
		return CaptureSession{}, ErrUnavailable
	}
	key := deviceIdempotencyKey(request.ClientID, request.IdempotencyKey)
	snapNum, interval := request.SnapNum, request.Interval
	blocks := manscdp.DeviceConfigBlocks{SnapShot: &manscdp.SnapShotBlock{
		SnapNum: &snapNum, Interval: &interval, UploadURL: &uploadURL, SessionID: &registrySession.ID,
	}}
	operation, err := s.commands.Execute(ctx, target, runtimeptz.Command{
		CmdType: manscdp.CmdDeviceConfig, Action: runtimeptz.ActionSnapshotConfig, IdempotencyKey: key,
		Profile: target.Profile, TargetScope: gbmodels.ControlTargetScopeChannel, TargetCode: channel.ChannelID,
		Payload: map[string]any{"configTypes": []string{manscdp.ConfigTypeSnapShotConfig}, "blocks": blocks,
			"sessionId": registrySession.ID, "snapNum": snapNum, "interval": interval},
		ResponseRequired: true, MaxAttempts: 1,
		Build: func(sn int) ([]byte, error) {
			return manscdp.BuildDeviceConfigBlocksWithProfile(target.Profile, channel.ChannelID, sn, blocks)
		},
	})
	if err != nil {
		s.captures.MarkFailed(registrySession.ID, err)
		_ = s.updateRecord(ctx, record, map[string]any{"state": CaptureStateFailed, "updated_at": s.now().UTC()})
		return CaptureSession{}, mapCommandError(err)
	}
	s.captures.MarkWaiting(registrySession.ID)
	if err := s.updateRecord(ctx, record, map[string]any{
		"operation_id": operation.OperationID,
		"state":        CaptureStateWaiting,
		"updated_at":   s.now().UTC(),
	}); err != nil {
		return CaptureSession{}, ErrUnavailable
	}
	record.OperationID, record.State = operation.OperationID, CaptureStateWaiting
	return s.captureView(ctx, record)
}

func (s *Service) GetCapture(ctx context.Context, clientID int64, scope resource.DepartmentScope, deviceID, channelID, sessionID string) (CaptureSession, error) {
	if !s.Ready() || ctx == nil || clientID <= 0 || strings.TrimSpace(sessionID) == "" {
		return CaptureSession{}, ErrNotFound
	}
	var record CaptureSessionRecord
	result := s.db.WithContext(ctx).Where("client_id = ? AND session_id = ?", clientID, sessionID).Limit(1).Find(&record)
	if result.Error != nil {
		return CaptureSession{}, ErrUnavailable
	}
	if result.RowsAffected != 1 || record.DeviceID != deviceID || record.ChannelID != channelID {
		return CaptureSession{}, ErrNotFound
	}
	target, channel, err := s.resolveTarget(ctx, scope, deviceID, channelID)
	if err != nil || target.DeviceID != record.InternalDeviceID || channel.ID != record.InternalChannelID {
		return CaptureSession{}, ErrNotFound
	}
	return s.captureView(ctx, record)
}

// ReadImage resolves a persisted image only through an OpenAPI-owned capture
// record and rechecks the caller's current resource scope on every read.
func (s *Service) ReadImage(ctx context.Context, clientID int64, scope resource.DepartmentScope, deviceID, channelID, sessionID, fileName string) (ImageContent, error) {
	if !s.Ready() || ctx == nil || clientID <= 0 || !validFileName(fileName) {
		return ImageContent{}, ErrNotFound
	}
	var record CaptureSessionRecord
	result := s.db.WithContext(ctx).Where("client_id = ? AND session_id = ?", clientID, sessionID).Limit(1).Find(&record)
	if result.Error != nil {
		return ImageContent{}, ErrUnavailable
	}
	if result.RowsAffected != 1 || record.DeviceID != deviceID || record.ChannelID != channelID || record.RegistrySessionID == "" {
		return ImageContent{}, ErrNotFound
	}
	target, channel, err := s.resolveTarget(ctx, scope, deviceID, channelID)
	if err != nil || target.DeviceID != record.InternalDeviceID || channel.ID != record.InternalChannelID {
		return ImageContent{}, ErrNotFound
	}
	var snapshot gbmodels.GbChannelSnapshot
	result = s.db.WithContext(ctx).Where("session_id = ? AND channel_code = ? AND device_id = ? AND channel_id = ? AND file_name = ? AND source = ?",
		record.RegistrySessionID, record.ChannelID, record.InternalDeviceID, record.InternalChannelID, fileName, gbmodels.SnapshotSourceDevice).
		Limit(1).Find(&snapshot)
	if result.Error != nil {
		return ImageContent{}, ErrUnavailable
	}
	if result.RowsAffected != 1 {
		return ImageContent{}, ErrNotFound
	}
	path, err := s.captures.FilePath(snapshot.RelPath)
	if err != nil {
		return ImageContent{}, ErrNotFound
	}
	payload, err := os.ReadFile(path)
	if err != nil || int64(len(payload)) != snapshot.Size {
		return ImageContent{}, ErrUnavailable
	}
	digest := md5.Sum(payload)
	if !strings.EqualFold(hex.EncodeToString(digest[:]), snapshot.MD5) {
		return ImageContent{}, ErrUnavailable
	}
	return ImageContent{FileName: snapshot.FileName, ContentType: "image/jpeg", MD5: snapshot.MD5, Bytes: payload}, nil
}

// StopCapture intentionally does not claim that the device stopped taking
// images. The existing SnapShotConfig domain flow has no verified stop method.
func (s *Service) StopCapture(ctx context.Context, clientID int64, scope resource.DepartmentScope, deviceID, channelID, sessionID string) (CaptureSession, error) {
	_, err := s.GetCapture(ctx, clientID, scope, deviceID, channelID, sessionID)
	if err != nil {
		return CaptureSession{}, err
	}
	return CaptureSession{}, ErrStopUnsupported
}

func (s *Service) reserve(ctx context.Context, request CaptureRequest, target runtimeptz.Target, channel gbmodels.GbChannel) (CaptureSessionRecord, bool, error) {
	digest := idempotencyDigest(request.IdempotencyKey)
	bodyDigest := requestDigest(request)
	var record CaptureSessionRecord
	created := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("client_id = ? AND idempotency_digest = ?", request.ClientID, digest).Limit(1).Find(&record)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 1 {
			return nil
		}
		now := s.now().UTC()
		record = CaptureSessionRecord{
			ClientID: request.ClientID, IdempotencyDigest: digest, RequestDigest: bodyDigest,
			SessionID: uuid.NewString(), DeviceID: request.DeviceID, ChannelID: request.ChannelID,
			InternalDeviceID: target.DeviceID, InternalChannelID: channel.ID, DepartmentID: channel.OwnerDeptID,
			SnapNum: request.SnapNum, Interval: request.Interval, State: CaptureStateCreating, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		var existing CaptureSessionRecord
		result := s.db.WithContext(ctx).Where("client_id = ? AND idempotency_digest = ?", request.ClientID, digest).Limit(1).Find(&existing)
		if result.Error == nil && result.RowsAffected == 1 {
			record = existing
		} else {
			return CaptureSessionRecord{}, false, ErrUnavailable
		}
	}
	if record.RequestDigest != bodyDigest {
		return record, false, ErrConflict
	}
	return record, created, nil
}

func (s *Service) updateRecord(ctx context.Context, record CaptureSessionRecord, updates map[string]any) error {
	return s.db.WithContext(ctx).Model(&CaptureSessionRecord{}).
		Where("client_id = ? AND session_id = ?", record.ClientID, record.SessionID).Updates(updates).Error
}

func (s *Service) captureView(ctx context.Context, record CaptureSessionRecord) (CaptureSession, error) {
	view := CaptureSession{
		SessionID: record.SessionID, DeviceID: record.DeviceID, ChannelID: record.ChannelID,
		State: record.State, CommandStatus: "unknown", SnapNum: record.SnapNum, Interval: record.Interval,
		Images: []CaptureImage{}, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
		registrySessionID: record.RegistrySessionID,
	}
	if record.RegistrySessionID != "" {
		if session, ok := s.captures.GetForOwner(record.RegistrySessionID, clientOwner(record.ClientID)); ok {
			view.State = string(session.State)
			view.ReceivedCount, view.NotifiedCount = len(session.Files), session.NotifiedCount
		} else if record.State != CaptureStateFailed {
			// Registry state is volatile. After a process restart, unknown is more
			// honest than reconstructing a completed state from uploaded files alone.
			view.State = CaptureStateUnknown
		}
	}
	if record.OperationID != "" {
		operation, err := s.commands.GetOperation(ctx, record.OperationID)
		if err == nil {
			view.CommandStatus = string(operation.Status)
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return CaptureSession{}, ErrUnavailable
		}
	}
	var rows []gbmodels.GbChannelSnapshot
	result := s.db.WithContext(ctx).Where("session_id = ? AND channel_code = ? AND device_id = ? AND channel_id = ? AND source = ?",
		record.RegistrySessionID, record.ChannelID, record.InternalDeviceID, record.InternalChannelID, gbmodels.SnapshotSourceDevice).
		Order("captured_at ASC, id ASC").Limit(10).Find(&rows)
	if result.Error != nil {
		return CaptureSession{}, ErrUnavailable
	}
	for _, row := range rows {
		view.Images = append(view.Images, CaptureImage{
			FileName: row.FileName, Size: row.Size, MD5: row.MD5, CapturedAt: row.CapturedAt,
			ContentURL: fmt.Sprintf("/openapi/v1/devices/%s/channels/%s/snapshot-sessions/%s/images/%s/content",
				url.PathEscape(record.DeviceID), url.PathEscape(record.ChannelID), url.PathEscape(record.SessionID), url.PathEscape(row.FileName)),
		})
	}
	if len(rows) > view.ReceivedCount {
		view.ReceivedCount = len(rows)
	}
	return view, nil
}

func (s *Service) resolveTarget(ctx context.Context, scope resource.DepartmentScope, deviceID, channelID string) (runtimeptz.Target, gbmodels.GbChannel, error) {
	departmentIDs, err := resource.ResolveDepartmentIDs(ctx, s.db, scope)
	if err != nil {
		if errors.Is(err, resource.ErrResourceNotFound) || errors.Is(err, resource.ErrInvalidDepartmentScope) {
			return runtimeptz.Target{}, gbmodels.GbChannel{}, ErrNotFound
		}
		return runtimeptz.Target{}, gbmodels.GbChannel{}, ErrUnavailable
	}
	var device gbmodels.GbDevice
	result := s.db.WithContext(ctx).Where("device_id = ? AND owner_dept_id IN ? AND deleted_at IS NULL", deviceID, departmentIDs).
		Where("NOT EXISTS (SELECT 1 FROM gb_device d2 WHERE d2.device_id = gb_device.device_id AND d2.deleted_at IS NULL AND d2.id <> gb_device.id)").
		Limit(1).Find(&device)
	if result.Error != nil {
		return runtimeptz.Target{}, gbmodels.GbChannel{}, ErrUnavailable
	}
	if result.RowsAffected != 1 {
		return runtimeptz.Target{}, gbmodels.GbChannel{}, ErrNotFound
	}
	var channel gbmodels.GbChannel
	result = s.db.WithContext(ctx).Where("device_id = ? AND channel_id = ? AND owner_dept_id = ? AND owner_dept_id IN ? AND deleted_at IS NULL",
		deviceID, channelID, device.OwnerDeptID, departmentIDs).
		Where("NOT EXISTS (SELECT 1 FROM gb_channel c2 WHERE c2.device_id = gb_channel.device_id AND c2.channel_id = gb_channel.channel_id AND c2.deleted_at IS NULL AND c2.id <> gb_channel.id)").
		Limit(1).Find(&channel)
	if result.Error != nil {
		return runtimeptz.Target{}, gbmodels.GbChannel{}, ErrUnavailable
	}
	if result.RowsAffected != 1 {
		return runtimeptz.Target{}, gbmodels.GbChannel{}, ErrNotFound
	}
	if device.Status != gbmodels.DeviceStatusOnline || channel.Status != gbmodels.ChannelStatusOnline {
		return runtimeptz.Target{}, gbmodels.GbChannel{}, ErrDeviceOffline
	}
	profile := protocol.ProfileFor(protocol.Version2016)
	if strings.TrimSpace(device.EffectiveVersion) == protocol.Version2022 {
		profile = protocol.ProfileFor(protocol.Version2022)
	}
	return runtimeptz.Target{
		DeviceID: uint(device.ID), DeviceCode: device.DeviceID,
		ChannelID: channel.ID, ChannelCode: channel.ChannelID,
		IP: device.IP, Port: device.Port, Transport: device.Transport,
		DeviceOnline: true, ChannelOnline: true, Profile: profile,
	}, channel, nil
}

func validGBID(value string) bool {
	if len(value) != 20 {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func validIdempotencyKey(value string) bool {
	if !utf8.ValidString(value) || strings.TrimSpace(value) != value || len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if unicode.IsControl(char) {
			return false
		}
	}
	return true
}

func validFileName(value string) bool {
	if value == "" || filepath.Base(value) != value || strings.ContainsAny(value, `/\\`) || len(value) > 64 {
		return false
	}
	lower := strings.ToLower(value)
	return strings.HasSuffix(lower, ".jpg") || strings.HasSuffix(lower, ".jpeg")
}

func clientOwner(clientID int64) string { return fmt.Sprintf("openapi-client:%d", clientID) }

func idempotencyDigest(key string) string {
	digest := sha256.Sum256([]byte(key))
	return hex.EncodeToString(digest[:])
}

func requestDigest(request CaptureRequest) string {
	canonical, _ := json.Marshal(struct {
		DeviceID  string `json:"deviceId"`
		ChannelID string `json:"channelId"`
		SnapNum   int    `json:"snapNum"`
		Interval  int    `json:"interval"`
	}{request.DeviceID, request.ChannelID, request.SnapNum, request.Interval})
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:])
}

func deviceIdempotencyKey(clientID int64, key string) string {
	return "openapi-capture:" + strconv.FormatInt(clientID, 10) + ":" + idempotencyDigest(key)
}

func mapCommandError(err error) error {
	if err == nil {
		return nil
	}
	var operationErr *runtimeptz.OperationError
	if errors.As(err, &operationErr) {
		switch operationErr.Code {
		case runtimeptz.ErrorCodeHomePositionInvalidArgument:
			return ErrInvalidRequest
		case runtimeptz.ErrorCodeHomePositionNotFound:
			return ErrNotFound
		case runtimeptz.ErrorCodeHomePositionDeviceOffline:
			return ErrDeviceOffline
		case runtimeptz.ErrorCodeHomePositionIdempotencyConflict:
			return ErrConflict
		default:
			return ErrUnavailable
		}
	}
	return ErrUnavailable
}

func readBounded(reader io.Reader, max int64) ([]byte, error) {
	if reader == nil || max <= 0 {
		return nil, ErrInvalidRequest
	}
	payload, err := io.ReadAll(io.LimitReader(reader, max+1))
	if err != nil || int64(len(payload)) > max {
		return nil, ErrInvalidRequest
	}
	return payload, nil
}
