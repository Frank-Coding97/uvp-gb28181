package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/model"
)

var (
	ErrPlatformNotFound       = errors.New("cascade platform not found")
	ErrRevisionConflict       = errors.New("cascade platform revision conflict")
	ErrInvalidProjection      = errors.New("invalid cascade projection")
	ErrInvalidMediaTransition = errors.New("invalid cascade media transition")
	// ErrRetiredPlatformConflict 两条已删除的平台分别占着"名称"与"接入关系"两个
	// 唯一键位,新建的这条平台无法同时接手两行。详见 findRetiredPlatform。
	ErrRetiredPlatformConflict = errors.New("cascade platform identity claimed by retired rows")
)

type PlatformStore interface {
	CreatePlatform(ctx context.Context, platform *model.GbCascadePlatform) error
	FindPlatform(ctx context.Context, platformID uint64) (*model.GbCascadePlatform, error)
	ListPlatforms(ctx context.Context) ([]model.GbCascadePlatform, error)
	ListEnabledPlatforms(ctx context.Context) ([]model.GbCascadePlatform, error)
	UpdatePlatformConfig(ctx context.Context, platform *model.GbCascadePlatform, expectedRevision uint64) (*model.GbCascadePlatform, error)
	SoftDeletePlatform(ctx context.Context, platformID uint64) error
	RecordRegistrationSuccess(ctx context.Context, platformID uint64, registeredAt, expiresAt time.Time) error
	RecordRegistrationExpired(ctx context.Context, platformID uint64, at time.Time) error
	RecordRegistrationFailure(ctx context.Context, platformID uint64, code, message string, at time.Time) error
	RecordHeartbeatSuccess(ctx context.Context, platformID uint64, at time.Time) error
	RecordHeartbeatFailure(ctx context.Context, platformID uint64, code, message string, at time.Time) error
}

type ProjectionStore interface {
	ReplaceProjection(ctx context.Context, platformID uint64, expectedProjectionRevision uint64, devices []DeviceProjectionInput, channels []ChannelProjectionInput) error
	ProjectionSnapshot(ctx context.Context, platformID uint64) (*ProjectionSnapshot, error)
}

type MediaSessionStore interface {
	CreateMediaSession(ctx context.Context, session *model.GbCascadeMediaSession) error
	FindMediaSessionByDialog(ctx context.Context, dialogKey string) (*model.GbCascadeMediaSession, error)
	TransitionMediaSession(ctx context.Context, dialogKey string, from, to model.CascadeMediaSessionState, at time.Time) (bool, error)
	ListNonterminalMediaSessions(ctx context.Context, platformID uint64) ([]model.GbCascadeMediaSession, error)
}

// Repository is the persistence boundary for cascade controllers and actors.
// Callers update configuration, runtime facts, projections, and media sessions through separate methods.
type Repository interface {
	PlatformStore
	ProjectionStore
	MediaSessionStore
}

type GormRepository struct {
	db *gorm.DB
}

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

// CreatePlatform 新建一个上级平台。
//
// 名称与接入关系都命中"已软删的遗留行"时接手那一行而不是 INSERT —— 否则用户
// 删掉平台再建一个同名(或同上级地址)的平台会撞 1062。理由见 platform_resurrect.go。
func (r *GormRepository) CreatePlatform(ctx context.Context, platform *model.GbCascadePlatform) error {
	row := *platform
	row.ID = 0
	if row.ConfigRevision == 0 {
		row.ConfigRevision = 1
	}
	if row.EffectiveVersion == "" {
		row.EffectiveVersion = "2016"
	}
	if row.EffectiveVersionFrom == "" {
		row.EffectiveVersionFrom = "default"
	}
	if row.ProfileOverride == "" {
		row.ProfileOverride = model.CascadeProfileOverrideAuto
	}
	if row.Transport == "" {
		row.Transport = "UDP"
	}
	if row.RegisterExpires == 0 {
		row.RegisterExpires = 3600
	}
	if row.KeepaliveInterval == 0 {
		row.KeepaliveInterval = 60
	}
	if row.CatalogBatchSize == 0 {
		row.CatalogBatchSize = 100
	}
	if row.MaxStreams == 0 {
		row.MaxStreams = 1
	}
	now := time.Now().UTC()
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	row.UpdatedAt = now
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 两个唯一索引都不含 deleted_at,软删的平台行会继续占着"名称"和"接入关系"
		// 两个键位。先认领遗留行,否则删掉再建同名平台会撞 1062(见 platform_resurrect.go)。
		retired, err := findRetiredPlatform(tx, &row)
		if err != nil {
			return err
		}
		if retired != nil {
			if err := resurrectPlatform(tx, &row, retired); err != nil {
				return err
			}
		} else if err := tx.Create(&row).Error; err != nil {
			return err
		}
		*platform = row
		return nil
	})
}

func (r *GormRepository) FindPlatform(ctx context.Context, platformID uint64) (*model.GbCascadePlatform, error) {
	var platform model.GbCascadePlatform
	// 全局 gorm hook 会吞掉 ErrRecordNotFound,判空必须看 RowsAffected
	result := r.db.WithContext(ctx).Limit(1).Find(&platform, platformID)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrPlatformNotFound
	}
	return &platform, nil
}

func (r *GormRepository) ListPlatforms(ctx context.Context) ([]model.GbCascadePlatform, error) {
	var platforms []model.GbCascadePlatform
	err := r.db.WithContext(ctx).Order("id").Find(&platforms).Error
	return platforms, err
}

func (r *GormRepository) ListEnabledPlatforms(ctx context.Context) ([]model.GbCascadePlatform, error) {
	var platforms []model.GbCascadePlatform
	err := r.db.WithContext(ctx).Where("enabled = ?", true).Order("id").Find(&platforms).Error
	return platforms, err
}

// UpdatePlatformConfig intentionally excludes runtime facts. Actor events cannot be overwritten by a stale form post.
//
// 改名前先确认目标名称/接入关系没有被一条"已删除"的平台占着:那种 UPDATE 会撞
// 唯一键,接口只回一句"已存在",而用户翻遍平台列表也找不到那个平台(它已经删了)。
func (r *GormRepository) UpdatePlatformConfig(ctx context.Context, platform *model.GbCascadePlatform, expectedRevision uint64) (*model.GbCascadePlatform, error) {
	if platform == nil || platform.ID == 0 || expectedRevision == 0 {
		return nil, ErrRevisionConflict
	}
	retired, err := findRetiredPlatform(r.db.WithContext(ctx), platform)
	if err != nil {
		return nil, err
	}
	if retired != nil {
		return nil, fmt.Errorf("%w: 目标名称或接入关系仍被已删除的平台 id=%d 占用", ErrRetiredPlatformConflict, retired.ID)
	}
	updates := platformConfigUpdates(platform)
	updates["config_revision"] = gorm.Expr("config_revision + ?", 1)
	updates["updated_at"] = time.Now().UTC()
	result := r.db.WithContext(ctx).Model(&model.GbCascadePlatform{}).
		Where("id = ? AND config_revision = ?", platform.ID, expectedRevision).
		Updates(updates)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		if _, err := r.FindPlatform(ctx, platform.ID); err != nil {
			return nil, err
		}
		return nil, ErrRevisionConflict
	}
	return r.FindPlatform(ctx, platform.ID)
}

func platformConfigUpdates(platform *model.GbCascadePlatform) map[string]any {
	return map[string]any{
		"name":               platform.Name,
		"upstream_server_id": platform.UpstreamServerID,
		"upstream_domain":    platform.UpstreamDomain,
		"host":               platform.Host,
		"port":               platform.Port,
		"local_device_id":    platform.LocalDeviceID,
		"local_domain":       platform.LocalDomain,
		"local_sip_ip":       platform.LocalSIPIP,
		"local_sip_port":     platform.LocalSIPPort,
		"media_advertise_ip": platform.MediaAdvertiseIP,
		"auth_username":      platform.AuthUsername,
		"secret_nonce":       platform.SecretNonce,
		"secret_ciphertext":  platform.SecretCiphertext,
		"secret_alg":         platform.SecretAlg,
		"secret_key_version": platform.SecretKeyVersion,
		"profile_override":   platform.ProfileOverride,
		"charset_override":   platform.CharsetOverride,
		"register_expires":   platform.RegisterExpires,
		"keepalive_interval": platform.KeepaliveInterval,
		"retry_policy":       platform.RetryPolicy,
		"transport":          platform.Transport,
		"catalog_batch_size": platform.CatalogBatchSize,
		"publish_platform":   platform.PublishPlatform,
		"publish_civil":      platform.PublishCivil,
		"publish_group":      platform.PublishGroup,
		"max_streams":        platform.MaxStreams,
		"ptz_enabled":        platform.PTZEnabled,
		"enabled":            platform.Enabled,
	}
}

// SoftDeletePlatform hides the platform and deactivates its projection in one
// transaction. Media-session safety is enforced by the management service.
func (r *GormRepository) SoftDeletePlatform(ctx context.Context, platformID uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var platform model.GbCascadePlatform
		result := tx.Limit(1).Find(&platform, platformID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrPlatformNotFound
		}
		now := time.Now().UTC()
		if err := tx.Model(&model.GbCascadeChannelProjection{}).Where("platform_id = ? AND active = ?", platformID, true).
			Updates(map[string]any{"active": false, "revision": gorm.Expr("revision + ?", 1), "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.GbCascadeDeviceProjection{}).Where("platform_id = ? AND active = ?", platformID, true).
			Updates(map[string]any{"active": false, "revision": gorm.Expr("revision + ?", 1), "updated_at": now}).Error; err != nil {
			return err
		}
		return tx.Delete(&platform).Error
	})
}

func (r *GormRepository) RecordRegistrationSuccess(ctx context.Context, platformID uint64, registeredAt, expiresAt time.Time) error {
	return r.updateRuntimeFacts(ctx, platformID, map[string]any{
		"register_at": registeredAt, "register_expires_at": expiresAt,
		"last_error_code": "", "last_error_message": "", "last_error_at": nil,
	})
}

// RecordRegistrationExpired represents an Expires: 0 response. It does not touch heartbeat facts.
func (r *GormRepository) RecordRegistrationExpired(ctx context.Context, platformID uint64, _ time.Time) error {
	return r.updateRuntimeFacts(ctx, platformID, map[string]any{
		"register_at": nil, "register_expires_at": nil,
	})
}

func (r *GormRepository) RecordRegistrationFailure(ctx context.Context, platformID uint64, code, message string, at time.Time) error {
	return r.recordFailure(ctx, platformID, code, message, at)
}

func (r *GormRepository) RecordHeartbeatSuccess(ctx context.Context, platformID uint64, at time.Time) error {
	return r.updateRuntimeFacts(ctx, platformID, map[string]any{
		"heartbeat_at": at, "last_error_code": "", "last_error_message": "", "last_error_at": nil,
	})
}

func (r *GormRepository) RecordHeartbeatFailure(ctx context.Context, platformID uint64, code, message string, at time.Time) error {
	return r.recordFailure(ctx, platformID, code, message, at)
}

func (r *GormRepository) recordFailure(ctx context.Context, platformID uint64, code, message string, at time.Time) error {
	return r.updateRuntimeFacts(ctx, platformID, map[string]any{
		"last_error_code": code, "last_error_message": message, "last_error_at": at,
	})
}

func (r *GormRepository) updateRuntimeFacts(ctx context.Context, platformID uint64, updates map[string]any) error {
	updates["updated_at"] = time.Now().UTC()
	result := r.db.WithContext(ctx).Model(&model.GbCascadePlatform{}).Where("id = ?", platformID).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		var count int64
		if err := r.db.WithContext(ctx).Model(&model.GbCascadePlatform{}).Where("id = ?", platformID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return ErrPlatformNotFound
		}
	}
	return nil
}

type DeviceProjectionInput struct {
	SourceDeviceID    uint64
	PublishedDeviceID string
	Name              string
	Manufacturer      string
	Model             string
	Owner             string
	CivilCode         string
	Address           string
	Parental          int
	Secrecy           int
}

type ChannelProjectionInput struct {
	SourceDeviceID     uint64
	SourceChannelID    uint64
	PublishedChannelID string
	Name               string
	ParentOverride     string
	PTZAllowed         bool
}

type ProjectionSnapshot struct {
	Platform model.GbCascadePlatform
	Devices  []model.GbCascadeDeviceProjection
	Channels []model.GbCascadeChannelProjection
	Revision uint64
}

// ReplaceProjection atomically replaces one platform's active authorization projection.
func (r *GormRepository) ReplaceProjection(ctx context.Context, platformID uint64, expectedProjectionRevision uint64, devices []DeviceProjectionInput, channels []ChannelProjectionInput) error {
	channels = append([]ChannelProjectionInput(nil), channels...)
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var platform model.GbCascadePlatform
		if result := tx.Limit(1).Find(&platform, platformID); result.Error != nil {
			return result.Error
		} else if result.RowsAffected == 0 {
			return ErrPlatformNotFound
		}
		// 投影替换是破坏性全量操作:请求必须携带其基于的修订号,
		// 两个管理员基于同一旧快照保存时后提交者必须得到冲突而非静默覆盖
		if platform.ProjectionRevision != expectedProjectionRevision {
			return ErrRevisionConflict
		}
		if err := allocatePublishedChannelIDs(tx, platformID, devices, channels); err != nil {
			return err
		}
		if err := validateProjectionInputs(devices, channels); err != nil {
			return err
		}
		deviceIDs := make(map[uint64]uint64, len(devices))
		desiredDevices := make([]uint64, 0, len(devices))
		for _, input := range devices {
			row, err := upsertDeviceProjection(tx, platformID, input)
			if err != nil {
				return err
			}
			deviceIDs[input.SourceDeviceID] = row.ID
			desiredDevices = append(desiredDevices, input.SourceDeviceID)
		}
		if err := deactivateAbsentDevices(tx, platformID, desiredDevices); err != nil {
			return err
		}

		desiredChannels := make([]uint64, 0, len(channels))
		for _, input := range channels {
			if err := upsertChannelProjection(tx, platformID, deviceIDs[input.SourceDeviceID], input); err != nil {
				return err
			}
			desiredChannels = append(desiredChannels, input.SourceChannelID)
		}
		if err := deactivateAbsentChannels(tx, platformID, desiredChannels); err != nil {
			return err
		}
		// 提交时做真正的 CAS:revision 条件写回,并发陈旧写者得到 0 行而非覆盖
		result := tx.Model(&model.GbCascadePlatform{}).
			Where("id = ? AND projection_revision = ?", platformID, expectedProjectionRevision).
			Updates(map[string]any{
				"projection_revision": gorm.Expr("projection_revision + ?", 1),
				"updated_at":          time.Now().UTC(),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrRevisionConflict
		}
		return nil
	})
}

func validateProjectionInputs(devices []DeviceProjectionInput, channels []ChannelProjectionInput) error {
	deviceSet := make(map[uint64]struct{}, len(devices))
	publishedIDs := make(map[string]struct{}, len(devices)+len(channels))
	for _, input := range devices {
		if input.SourceDeviceID == 0 || input.PublishedDeviceID == "" {
			return ErrInvalidProjection
		}
		if _, exists := deviceSet[input.SourceDeviceID]; exists {
			return ErrInvalidProjection
		}
		deviceSet[input.SourceDeviceID] = struct{}{}
		if _, exists := publishedIDs[input.PublishedDeviceID]; exists {
			return ErrInvalidProjection
		}
		publishedIDs[input.PublishedDeviceID] = struct{}{}
	}
	channelSet := make(map[uint64]struct{}, len(channels))
	for _, input := range channels {
		if input.SourceChannelID == 0 || input.PublishedChannelID == "" {
			return ErrInvalidProjection
		}
		if _, exists := deviceSet[input.SourceDeviceID]; !exists {
			return ErrInvalidProjection
		}
		if _, exists := channelSet[input.SourceChannelID]; exists {
			return ErrInvalidProjection
		}
		channelSet[input.SourceChannelID] = struct{}{}
		if _, exists := publishedIDs[input.PublishedChannelID]; exists {
			return ErrInvalidProjection
		}
		publishedIDs[input.PublishedChannelID] = struct{}{}
	}
	return nil
}

func upsertDeviceProjection(tx *gorm.DB, platformID uint64, input DeviceProjectionInput) (*model.GbCascadeDeviceProjection, error) {
	var row model.GbCascadeDeviceProjection
	result := tx.Unscoped().Where("platform_id = ? AND source_device_id = ?", platformID, input.SourceDeviceID).First(&row)
	if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, result.Error
	}
	now := time.Now().UTC()
	if result.RowsAffected == 0 {
		// 按 source_device_id 查不到,但同一个 published_device_id 可能已经被一条
		// "源设备早已不存在"的历史投影占着 —— 设备删过又重新接入,gb_device 换了
		// 自增 id,published id 还是同一个国标号。这条路上 INSERT 必撞
		// uk_cascade_device_published 报 1062,共享接口只能回 409。
		// 认领那一行(改写 source_device_id)而不是新建:published id 不抖动,
		// 行 id 不变,挂在上面的通道投影的 device_projection_id 也不会悬空。
		claimed, claimErr := claimDeviceProjectionByPublishedID(tx, platformID, input.PublishedDeviceID)
		if claimErr != nil {
			return nil, claimErr
		}
		if claimed != nil {
			row = *claimed
			row.Revision++
		} else {
			row = model.GbCascadeDeviceProjection{PlatformID: platformID, SourceDeviceID: input.SourceDeviceID, CreatedAt: now, Revision: 1}
		}
	} else {
		row.Revision++
	}
	row.SourceDeviceID = input.SourceDeviceID
	row.PublishedDeviceID = input.PublishedDeviceID
	row.Name = input.Name
	row.Manufacturer = input.Manufacturer
	row.Model = input.Model
	row.Owner = input.Owner
	row.CivilCode = input.CivilCode
	row.Address = input.Address
	row.Parental = input.Parental
	row.Secrecy = input.Secrecy
	row.Active = true
	row.UpdatedAt = now
	row.DeletedAt = gorm.DeletedAt{}
	if err := tx.Unscoped().Save(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// claimDeviceProjectionByPublishedID 找出同一个上级平台上占着这个 published id 的投影行,
// 供 upsertDeviceProjection 在"按 source_device_id 查不到"时认领。查不到返回 nil。
//
// 认领不会抢走活设备的投影:设备的 published_device_id 就是它的国标号,而
// gb_device.device_id 唯一 —— 还活着的另一台设备不可能正占着这个号。能占着的只可能是
// 设备自己(那会在按 source_device_id 的查询里命中),或者一条源已消失的历史行。
//
// 通道投影不走这条路径:通道的 published id 由 allocatePublishedChannelIDs 统一分配,
// 撞号在分配阶段就被避让到别的号段,落不到 INSERT 撞键这一步。
//
// 用 Limit(1).Find 而不是 First:ErrRecordNotFound 可能被调用方关掉
// (见 TestReplaceProjectionCreatesRowsWhenRecordNotFoundErrorsAreMasked),
// 只有 RowsAffected 可靠。
func claimDeviceProjectionByPublishedID(tx *gorm.DB, platformID uint64, publishedDeviceID string) (*model.GbCascadeDeviceProjection, error) {
	if publishedDeviceID == "" {
		return nil, nil
	}
	var row model.GbCascadeDeviceProjection
	result := tx.Unscoped().Where("platform_id = ? AND published_device_id = ?", platformID, publishedDeviceID).Limit(1).Find(&row)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return &row, nil
}

func upsertChannelProjection(tx *gorm.DB, platformID, deviceProjectionID uint64, input ChannelProjectionInput) error {
	var row model.GbCascadeChannelProjection
	result := tx.Unscoped().Where("platform_id = ? AND source_channel_id = ?", platformID, input.SourceChannelID).First(&row)
	if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return result.Error
	}
	now := time.Now().UTC()
	if result.RowsAffected == 0 {
		row = model.GbCascadeChannelProjection{PlatformID: platformID, SourceChannelID: input.SourceChannelID, CreatedAt: now, Revision: 1}
	} else {
		row.Revision++
	}
	row.DeviceProjectionID = deviceProjectionID
	row.PublishedChannelID = input.PublishedChannelID
	row.Name = input.Name
	row.ParentOverride = input.ParentOverride
	row.PTZAllowed = input.PTZAllowed
	row.Active = true
	row.UpdatedAt = now
	row.DeletedAt = gorm.DeletedAt{}
	return tx.Unscoped().Save(&row).Error
}

func deactivateAbsentDevices(tx *gorm.DB, platformID uint64, desired []uint64) error {
	query := tx.Model(&model.GbCascadeDeviceProjection{}).Where("platform_id = ? AND active = ?", platformID, true)
	if len(desired) > 0 {
		query = query.Where("source_device_id NOT IN ?", desired)
	}
	return query.Updates(map[string]any{"active": false, "revision": gorm.Expr("revision + ?", 1), "updated_at": time.Now().UTC()}).Error
}

func deactivateAbsentChannels(tx *gorm.DB, platformID uint64, desired []uint64) error {
	query := tx.Model(&model.GbCascadeChannelProjection{}).Where("platform_id = ? AND active = ?", platformID, true)
	if len(desired) > 0 {
		query = query.Where("source_channel_id NOT IN ?", desired)
	}
	return query.Updates(map[string]any{"active": false, "revision": gorm.Expr("revision + ?", 1), "updated_at": time.Now().UTC()}).Error
}

func (r *GormRepository) ProjectionSnapshot(ctx context.Context, platformID uint64) (*ProjectionSnapshot, error) {
	snapshot := &ProjectionSnapshot{}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if result := tx.Limit(1).Find(&snapshot.Platform, platformID); result.Error != nil {
			return result.Error
		} else if result.RowsAffected == 0 {
			return ErrPlatformNotFound
		}
		if err := tx.Where("platform_id = ? AND active = ?", platformID, true).Order("id").Find(&snapshot.Devices).Error; err != nil {
			return err
		}
		if err := tx.Where("platform_id = ? AND active = ?", platformID, true).Order("id").Find(&snapshot.Channels).Error; err != nil {
			return err
		}
		snapshot.Revision = snapshot.Platform.ProjectionRevision
		return nil
	})
	if err != nil {
		return nil, err
	}
	return snapshot, nil
}

func (r *GormRepository) CreateMediaSession(ctx context.Context, session *model.GbCascadeMediaSession) error {
	if session == nil || session.PlatformID == 0 || session.DialogKey == "" {
		return fmt.Errorf("%w: media session identity", ErrInvalidProjection)
	}
	row := *session
	row.ID = 0
	if row.State == "" {
		row.State = model.CascadeMediaSessionStateReceived
	}
	now := time.Now().UTC()
	if row.ReceivedAt == nil {
		row.ReceivedAt = &now
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	row.UpdatedAt = now
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return err
	}
	*session = row
	return nil
}

func (r *GormRepository) FindMediaSessionByDialog(ctx context.Context, dialogKey string) (*model.GbCascadeMediaSession, error) {
	var session model.GbCascadeMediaSession
	result := r.db.WithContext(ctx).Where("dialog_key = ?", dialogKey).Limit(1).Find(&session)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return &session, nil
}

func (r *GormRepository) TransitionMediaSession(ctx context.Context, dialogKey string, from, to model.CascadeMediaSessionState, at time.Time) (bool, error) {
	if !canTransitionMedia(from, to) {
		return false, fmt.Errorf("%w: %s -> %s", ErrInvalidMediaTransition, from, to)
	}
	updates := map[string]any{"state": to, "updated_at": at}
	switch to {
	case model.CascadeMediaSessionStateAnswered:
		updates["answered_at"] = at
	case model.CascadeMediaSessionStateActive:
		updates["active_at"] = at
	case model.CascadeMediaSessionStateClosed, model.CascadeMediaSessionStateFailed, model.CascadeMediaSessionStateCancelled, model.CascadeMediaSessionStateAborted:
		updates["closed_at"] = at
	}
	result := r.db.WithContext(ctx).Model(&model.GbCascadeMediaSession{}).
		Where("dialog_key = ? AND state = ?", dialogKey, from).Updates(updates)
	return result.RowsAffected > 0, result.Error
}

func (r *GormRepository) ListNonterminalMediaSessions(ctx context.Context, platformID uint64) ([]model.GbCascadeMediaSession, error) {
	terminal := []model.CascadeMediaSessionState{
		model.CascadeMediaSessionStateClosed,
		model.CascadeMediaSessionStateFailed,
		model.CascadeMediaSessionStateCancelled,
		model.CascadeMediaSessionStateAborted,
	}
	var sessions []model.GbCascadeMediaSession
	query := r.db.WithContext(ctx).Where("state NOT IN ?", terminal).Order("id")
	if platformID != 0 {
		query = query.Where("platform_id = ?", platformID)
	}
	err := query.Find(&sessions).Error
	return sessions, err
}

func canTransitionMedia(from, to model.CascadeMediaSessionState) bool {
	switch from {
	case model.CascadeMediaSessionStateReceived:
		return to == model.CascadeMediaSessionStateProvisioning || to == model.CascadeMediaSessionStateCancelled || to == model.CascadeMediaSessionStateFailed || to == model.CascadeMediaSessionStateAborted
	case model.CascadeMediaSessionStateProvisioning:
		return to == model.CascadeMediaSessionStateAnswered || to == model.CascadeMediaSessionStateCancelled || to == model.CascadeMediaSessionStateFailed || to == model.CascadeMediaSessionStateAborted
	case model.CascadeMediaSessionStateAnswered:
		return to == model.CascadeMediaSessionStateActive || to == model.CascadeMediaSessionStateClosing || to == model.CascadeMediaSessionStateFailed || to == model.CascadeMediaSessionStateAborted
	case model.CascadeMediaSessionStateActive:
		return to == model.CascadeMediaSessionStateClosing || to == model.CascadeMediaSessionStateFailed || to == model.CascadeMediaSessionStateAborted
	case model.CascadeMediaSessionStateClosing:
		return to == model.CascadeMediaSessionStateClosed || to == model.CascadeMediaSessionStateFailed || to == model.CascadeMediaSessionStateAborted
	default:
		return false
	}
}
