package repository

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

type BusinessGroupSourceAuthorizer func(tx *gorm.DB, sourceDeviceIDs, sourceChannelIDs []uint64) error

type businessGroupShareSource struct {
	channel gbmodels.GbChannel
	device  gbmodels.GbDevice
}

type BusinessNodeInput struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	ParentCode string `json:"parentCode"`
	Sort       int    `json:"sort"`
}

type BusinessBindingInput struct {
	SourceChannelID uint64 `json:"sourceChannelId"`
	NodeCode        string `json:"nodeCode"`
}

type BusinessGroupResourceChanges struct {
	ShareChannelIDs        []uint64                         `json:"shareChannelIds"`
	UnshareChannelIDs      []uint64                         `json:"unshareChannelIds"`
	ChannelPermissions     []BusinessChannelPermissionInput `json:"channelPermissions"`
	PublishGroup           *bool                            `json:"publishGroup,omitempty"`
	ExpectedConfigRevision uint64                           `json:"expectedConfigRevision,omitempty"`
}

type BusinessChannelPermissionInput struct {
	SourceChannelID uint64 `json:"sourceChannelId"`
	PTZAllowed      bool   `json:"ptzAllowed"`
}

// ReplaceBusinessGroup atomically replaces the business tree and its channel
// assignments. Its revision is shared with channel projections by design.
func (r *GormRepository) ReplaceBusinessGroup(ctx context.Context, platformID uint64, expectedProjectionRevision uint64, nodes []BusinessNodeInput, bindings []BusinessBindingInput) error {
	return r.replaceBusinessGroup(ctx, platformID, expectedProjectionRevision, nodes, bindings, nil, BusinessGroupResourceChanges{}, BusinessGroupPublicationUpdate{}, nil, nil)
}

// ReplaceBusinessGroupWithShares preserves the legacy requirement that every
// newly shared channel is included in the submitted business bindings.
func (r *GormRepository) ReplaceBusinessGroupWithShares(ctx context.Context, platformID uint64, expectedProjectionRevision uint64, nodes []BusinessNodeInput, bindings []BusinessBindingInput, shareChannelIDs []uint64, authorize BusinessGroupSourceAuthorizer) error {
	return r.ReplaceBusinessGroupWithResourceChanges(ctx, platformID, expectedProjectionRevision, nodes, bindings, shareChannelIDs, BusinessGroupResourceChanges{}, authorize)
}

// ReplaceBusinessGroupWithResourceChanges atomically changes shared resources,
// channel PTZ permissions, and the business tree under one projection revision.
func (r *GormRepository) ReplaceBusinessGroupWithResourceChanges(ctx context.Context, platformID uint64, expectedProjectionRevision uint64, nodes []BusinessNodeInput, bindings []BusinessBindingInput, legacyShareChannelIDs []uint64, changes BusinessGroupResourceChanges, authorize BusinessGroupSourceAuthorizer) error {
	publication := BusinessGroupPublicationUpdate{PublishGroup: changes.PublishGroup, ExpectedConfigRevision: changes.ExpectedConfigRevision}
	changes.PublishGroup = nil
	changes.ExpectedConfigRevision = 0
	return r.ReplaceBusinessGroupWithOptions(ctx, platformID, expectedProjectionRevision, nodes, bindings, legacyShareChannelIDs, changes, publication, authorize, nil)
}

func (r *GormRepository) ReplaceBusinessGroupWithOptions(ctx context.Context, platformID, expectedProjectionRevision uint64, nodes []BusinessNodeInput, bindings []BusinessBindingInput, legacyShareChannelIDs []uint64, changes BusinessGroupResourceChanges, publication BusinessGroupPublicationUpdate, authorize BusinessGroupSourceAuthorizer, authorizePublication BusinessGroupPublicationAuthorizer) error {
	nodes = append([]BusinessNodeInput(nil), nodes...)
	bindings = append([]BusinessBindingInput(nil), bindings...)
	legacyShareChannelIDs = append([]uint64(nil), legacyShareChannelIDs...)
	changes.ShareChannelIDs = append([]uint64(nil), changes.ShareChannelIDs...)
	changes.UnshareChannelIDs = append([]uint64(nil), changes.UnshareChannelIDs...)
	changes.ChannelPermissions = append([]BusinessChannelPermissionInput(nil), changes.ChannelPermissions...)
	if publication.PublishGroup != nil {
		publishGroup := *publication.PublishGroup
		publication.PublishGroup = &publishGroup
	}
	return r.replaceBusinessGroup(ctx, platformID, expectedProjectionRevision, nodes, bindings, legacyShareChannelIDs, changes, publication, authorize, authorizePublication)
}

func (r *GormRepository) replaceBusinessGroup(ctx context.Context, platformID uint64, expectedProjectionRevision uint64, nodes []BusinessNodeInput, bindings []BusinessBindingInput, legacyShareChannelIDs []uint64, changes BusinessGroupResourceChanges, publication BusinessGroupPublicationUpdate, authorize BusinessGroupSourceAuthorizer, authorizePublication BusinessGroupPublicationAuthorizer) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var platform model.GbCascadePlatform
		result := tx.Limit(1).Find(&platform, platformID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrPlatformNotFound
		}
		if platform.ProjectionRevision != expectedProjectionRevision {
			return ErrRevisionConflict
		}
		modeChanged := publication.PublishGroup != nil && platform.PublishGroup != *publication.PublishGroup
		if modeChanged {
			if publication.ExpectedConfigRevision == 0 || platform.ConfigRevision != publication.ExpectedConfigRevision {
				return ErrRevisionConflict
			}
			if authorizePublication != nil {
				if err := authorizePublication(); err != nil {
					return err
				}
			}
		}
		if platform.PublishGroup && (len(nodes) == 0 || (publication.PublishGroup != nil && !*publication.PublishGroup)) {
			// Turning publication off may retain or edit the tree without preserving
			// directory ownership. The off switch and edits still commit atomically.
			if publication.PublishGroup != nil && !*publication.PublishGroup {
				platform.PublishGroup = false
			}
		}
		if platform.PublishGroup && len(nodes) == 0 {
			return invalidBusinessGroup("发布业务分组期间不能清空分组树，请先关闭发布")
		}
		if err := applyBusinessGroupResourceChanges(tx, platform, bindings, legacyShareChannelIDs, changes, authorize); err != nil {
			return err
		}
		bindings = excludeBusinessGroupBindings(bindings, changes.UnshareChannelIDs)

		var storedNodes []model.GbCascadeBusinessNode
		var err error
		if platform.PublishGroup || (publication.PublishGroup != nil && *publication.PublishGroup) {
			storedNodes, err = validatePublishedBusinessGroup(tx, platform, nodes, bindings)
		} else {
			storedNodes, err = validateBusinessGroup(tx, platform, nodes, bindings)
		}
		if err != nil {
			return err
		}
		if publication.PublishGroup != nil && *publication.PublishGroup {
			platform.PublishGroup = true
		}
		now := time.Now().UTC()
		if err := tx.Where("platform_id = ?", platformID).Delete(&model.GbCascadeBusinessBinding{}).Error; err != nil {
			return err
		}
		if err := tx.Where("platform_id = ?", platformID).Delete(&model.GbCascadeBusinessNode{}).Error; err != nil {
			return err
		}
		for i := range storedNodes {
			if err := tx.Create(&storedNodes[i]).Error; err != nil {
				return err
			}
		}
		for _, binding := range bindings {
			row := model.GbCascadeBusinessBinding{
				PlatformID: platformID, SourceChannelID: binding.SourceChannelID, NodeCode: binding.NodeCode,
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		updates := map[string]any{
			"projection_revision": gorm.Expr("projection_revision + ?", 1),
			"updated_at":          now,
		}
		query := tx.Model(&model.GbCascadePlatform{}).Where("id = ? AND projection_revision = ?", platformID, expectedProjectionRevision)
		if modeChanged {
			query = query.Where("config_revision = ?", publication.ExpectedConfigRevision)
			updates["publish_group"] = *publication.PublishGroup
			updates["config_revision"] = gorm.Expr("config_revision + ?", 1)
		}
		write := query.Updates(updates)
		if write.Error != nil {
			return write.Error
		}
		if write.RowsAffected == 0 {
			return ErrRevisionConflict
		}
		return nil
	})
}

func excludeBusinessGroupBindings(bindings []BusinessBindingInput, unsharedChannelIDs []uint64) []BusinessBindingInput {
	if len(unsharedChannelIDs) == 0 || len(bindings) == 0 {
		return bindings
	}
	unshared := make(map[uint64]struct{}, len(unsharedChannelIDs))
	for _, id := range unsharedChannelIDs {
		unshared[id] = struct{}{}
	}
	filtered := bindings[:0]
	for _, binding := range bindings {
		if _, removed := unshared[binding.SourceChannelID]; removed {
			continue
		}
		filtered = append(filtered, binding)
	}
	return filtered
}

func applyBusinessGroupResourceChanges(tx *gorm.DB, platform model.GbCascadePlatform, bindings []BusinessBindingInput, legacyShareChannelIDs []uint64, changes BusinessGroupResourceChanges, authorize BusinessGroupSourceAuthorizer) error {
	legacyShares, err := uniqueBusinessGroupIDs(legacyShareChannelIDs, "所选通道不能重复")
	if err != nil {
		return err
	}
	if len(legacyShares) > 0 {
		bound := make(map[uint64]struct{}, len(legacyShares))
		for _, id := range legacyShares {
			bound[id] = struct{}{}
		}
		for _, binding := range bindings {
			delete(bound, binding.SourceChannelID)
		}
		if len(bound) > 0 {
			return invalidBusinessGroup("本次新增共享的通道必须同时归入一个虚拟组织")
		}
	}
	shares := make(map[uint64]struct{}, len(changes.ShareChannelIDs))
	for _, id := range changes.ShareChannelIDs {
		if id == 0 {
			return invalidBusinessGroup("所选通道编号无效")
		}
		if _, exists := shares[id]; exists {
			return invalidBusinessGroup("所选通道不能重复")
		}
		shares[id] = struct{}{}
	}
	for _, id := range legacyShares {
		if _, exists := shares[id]; exists {
			return invalidBusinessGroup("同一通道不能重复新增共享")
		}
		shares[id] = struct{}{}
	}
	unshares := make(map[uint64]struct{}, len(changes.UnshareChannelIDs))
	for _, id := range changes.UnshareChannelIDs {
		if id == 0 {
			return invalidBusinessGroup("取消共享的通道编号无效")
		}
		if _, exists := unshares[id]; exists {
			return invalidBusinessGroup("取消共享的通道不能重复")
		}
		if _, exists := shares[id]; exists {
			return invalidBusinessGroup("同一通道不能同时新增和取消共享")
		}
		unshares[id] = struct{}{}
	}
	permissionIDs := make(map[uint64]struct{}, len(changes.ChannelPermissions))
	for _, permission := range changes.ChannelPermissions {
		if permission.SourceChannelID == 0 {
			return invalidBusinessGroup("通道云台权限编号无效")
		}
		if _, exists := permissionIDs[permission.SourceChannelID]; exists {
			return invalidBusinessGroup("同一通道云台权限不能重复提交")
		}
		if _, conflicts := unshares[permission.SourceChannelID]; conflicts {
			return invalidBusinessGroup("取消共享的通道不能同时修改云台权限")
		}
		permissionIDs[permission.SourceChannelID] = struct{}{}
	}
	if len(legacyShares) > 0 {
		if err := addBusinessGroupShares(tx, platform, legacyShares, authorize); err != nil {
			return err
		}
	}
	if len(changes.ShareChannelIDs) > 0 {
		if err := addBusinessGroupShares(tx, platform, changes.ShareChannelIDs, authorize); err != nil {
			return err
		}
	}
	modified := append([]uint64(nil), changes.UnshareChannelIDs...)
	for _, permission := range changes.ChannelPermissions {
		modified = append(modified, permission.SourceChannelID)
	}
	if len(modified) > 0 {
		if err := authorizeBusinessGroupChannelChanges(tx, platform.ID, modified, authorize); err != nil {
			return err
		}
	}
	if len(changes.UnshareChannelIDs) > 0 {
		if err := deactivateBusinessGroupShares(tx, platform.ID, changes.UnshareChannelIDs); err != nil {
			return err
		}
	}
	for _, permission := range changes.ChannelPermissions {
		result := tx.Model(&model.GbCascadeChannelProjection{}).
			Where("platform_id = ? AND source_channel_id = ? AND active = ?", platform.ID, permission.SourceChannelID, true).
			Updates(map[string]any{"ptz_allowed": permission.PTZAllowed, "revision": gorm.Expr("revision + ?", 1), "updated_at": time.Now().UTC()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return invalidBusinessGroup("只能修改当前上级已共享的有效通道云台权限")
		}
	}
	return nil
}

func authorizeBusinessGroupChannelChanges(tx *gorm.DB, platformID uint64, channelIDs []uint64, authorize BusinessGroupSourceAuthorizer) error {
	ids, err := uniqueBusinessGroupIDs(channelIDs, "通道变更编号不能重复")
	if err != nil {
		return err
	}
	var rows []model.GbCascadeChannelProjection
	if err := tx.Where("platform_id = ? AND source_channel_id IN ? AND active = ?", platformID, ids, true).Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) != len(ids) {
		return invalidBusinessGroup("只能变更当前上级已共享的有效通道")
	}
	deviceProjectionIDs := make([]uint64, 0, len(rows))
	for _, row := range rows {
		deviceProjectionIDs = append(deviceProjectionIDs, row.DeviceProjectionID)
	}
	var deviceRows []model.GbCascadeDeviceProjection
	if err := tx.Where("platform_id = ? AND id IN ? AND active = ?", platformID, deviceProjectionIDs, true).Find(&deviceRows).Error; err != nil {
		return err
	}
	if len(deviceRows) != len(uniqueUint64(deviceProjectionIDs)) {
		return invalidBusinessGroup("共享通道所属设备投影无效")
	}
	deviceIDs := make([]uint64, 0, len(deviceRows))
	for _, device := range deviceRows {
		deviceIDs = append(deviceIDs, device.SourceDeviceID)
	}
	if authorize != nil {
		return authorize(tx, deviceIDs, ids)
	}
	return nil
}

func deactivateBusinessGroupShares(tx *gorm.DB, platformID uint64, channelIDs []uint64) error {
	var channels []model.GbCascadeChannelProjection
	if err := tx.Where("platform_id = ? AND source_channel_id IN ? AND active = ?", platformID, channelIDs, true).Find(&channels).Error; err != nil {
		return err
	}
	if len(channels) != len(channelIDs) {
		return invalidBusinessGroup("只能取消当前上级已共享的有效通道")
	}
	deviceProjectionIDs := make([]uint64, 0, len(channels))
	for _, channel := range channels {
		deviceProjectionIDs = append(deviceProjectionIDs, channel.DeviceProjectionID)
	}
	result := tx.Model(&model.GbCascadeChannelProjection{}).
		Where("platform_id = ? AND source_channel_id IN ? AND active = ?", platformID, channelIDs, true).
		Updates(map[string]any{"active": false, "revision": gorm.Expr("revision + ?", 1), "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != int64(len(channelIDs)) {
		return invalidBusinessGroup("只能取消当前上级已共享的有效通道")
	}
	if err := tx.Where("platform_id = ? AND source_channel_id IN ?", platformID, channelIDs).
		Delete(&model.GbCascadeBusinessBinding{}).Error; err != nil {
		return err
	}
	for _, deviceProjectionID := range uniqueUint64(deviceProjectionIDs) {
		var activeCount int64
		if err := tx.Model(&model.GbCascadeChannelProjection{}).
			Where("device_projection_id = ? AND active = ?", deviceProjectionID, true).Count(&activeCount).Error; err != nil {
			return err
		}
		if activeCount == 0 {
			if err := tx.Model(&model.GbCascadeDeviceProjection{}).
				Where("platform_id = ? AND id = ? AND active = ?", platformID, deviceProjectionID, true).
				Updates(map[string]any{"active": false, "revision": gorm.Expr("revision + ?", 1), "updated_at": time.Now().UTC()}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func uniqueBusinessGroupIDs(ids []uint64, duplicateMessage string) ([]uint64, error) {
	unique := make([]uint64, 0, len(ids))
	seen := make(map[uint64]struct{}, len(ids))
	for _, id := range ids {
		if id == 0 {
			return nil, invalidBusinessGroup("通道编号无效")
		}
		if _, exists := seen[id]; exists {
			return nil, invalidBusinessGroup(duplicateMessage)
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	return unique, nil
}

func uniqueUint64(ids []uint64) []uint64 {
	unique := make([]uint64, 0, len(ids))
	seen := make(map[uint64]struct{}, len(ids))
	for _, id := range ids {
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	return unique
}

func addBusinessGroupShares(tx *gorm.DB, platform model.GbCascadePlatform, shareChannelIDs []uint64, authorize BusinessGroupSourceAuthorizer) error {
	selected := make(map[uint64]struct{}, len(shareChannelIDs))
	for _, id := range shareChannelIDs {
		if id == 0 {
			return invalidBusinessGroup("所选通道编号无效")
		}
		if _, exists := selected[id]; exists {
			return invalidBusinessGroup("所选通道不能重复")
		}
		selected[id] = struct{}{}
	}
	sources, err := loadBusinessGroupShareSources(tx, shareChannelIDs)
	if err != nil {
		return err
	}
	deviceIDs := uniqueBusinessGroupSourceDeviceIDs(sources)
	if authorize != nil {
		if err := authorize(tx, deviceIDs, append([]uint64(nil), shareChannelIDs...)); err != nil {
			return err
		}
	}

	deviceRows, err := loadBusinessGroupDeviceProjections(tx, platform.ID, sources)
	if err != nil {
		return err
	}
	channelRows, err := loadBusinessGroupChannelProjections(tx, platform.ID, shareChannelIDs)
	if err != nil {
		return err
	}
	deviceInputs := businessGroupDeviceInputs(sources, deviceRows)
	channelInputs := businessGroupNewChannelInputs(sources, channelRows, platform.PTZEnabled)
	allocationDevices, err := businessGroupAllocationDevices(tx, platform.ID, sources, deviceRows)
	if err != nil {
		return err
	}
	if err := allocatePublishedChannelIDs(tx, platform.ID, allocationDevices, channelInputs); err != nil {
		return err
	}
	if err := validateProjectionInputs(deviceInputs, channelInputs); err != nil {
		return err
	}
	for _, input := range deviceInputs {
		if !publishedIDPattern.MatchString(input.PublishedDeviceID) {
			return invalidBusinessGroup("源设备编号必须是 20 位数字")
		}
	}
	if err := validateProjectionBusinessCodeCollisions(tx, platform.ID, deviceInputs, channelInputs); err != nil {
		return err
	}
	if err := validateBusinessGroupShareIdentityCollisions(tx, platform, deviceInputs, channelInputs); err != nil {
		return err
	}

	deviceProjectionIDs := make(map[uint64]uint64, len(sources))
	for _, source := range sources {
		deviceSourceID := uint64(source.device.ID)
		if _, alreadyHandled := deviceProjectionIDs[deviceSourceID]; alreadyHandled {
			continue
		}
		row, exists := deviceRows[deviceSourceID]
		if exists {
			if !row.Active || row.DeletedAt.Valid {
				updates := map[string]any{"active": true, "revision": gorm.Expr("revision + ?", 1), "updated_at": time.Now().UTC()}
				updates["deleted_at"] = nil
				if err := tx.Unscoped().Model(&model.GbCascadeDeviceProjection{}).Where("id = ?", row.ID).Updates(updates).Error; err != nil {
					return err
				}
				row.Active = true
				row.DeletedAt = gorm.DeletedAt{}
				row.Revision++
			}
			deviceProjectionIDs[deviceSourceID] = row.ID
			continue
		}
		input := businessGroupDeviceInput(source.device)
		created, err := upsertDeviceProjection(tx, platform.ID, input)
		if err != nil {
			return err
		}
		deviceProjectionIDs[deviceSourceID] = created.ID
	}

	channelInputsByID := make(map[uint64]ChannelProjectionInput, len(channelInputs))
	for _, input := range channelInputs {
		channelInputsByID[input.SourceChannelID] = input
	}
	for _, source := range sources {
		channelID := uint64(source.channel.ID)
		deviceProjectionID := deviceProjectionIDs[uint64(source.device.ID)]
		if row, exists := channelRows[channelID]; exists {
			if row.DeviceProjectionID != deviceProjectionID {
				return invalidBusinessGroup("已共享通道的设备投影与源设备不匹配")
			}
			if !row.Active || row.DeletedAt.Valid {
				if err := tx.Unscoped().Model(&model.GbCascadeChannelProjection{}).Where("id = ?", row.ID).Updates(map[string]any{
					"active": true, "revision": gorm.Expr("revision + ?", 1), "updated_at": time.Now().UTC(), "deleted_at": nil,
				}).Error; err != nil {
					return err
				}
			}
			continue
		}
		input := channelInputsByID[channelID]
		if err := upsertChannelProjection(tx, platform.ID, deviceProjectionID, input); err != nil {
			return err
		}
	}
	return nil
}

func loadBusinessGroupShareSources(tx *gorm.DB, channelIDs []uint64) ([]businessGroupShareSource, error) {
	var channels []gbmodels.GbChannel
	if err := tx.Where("id IN ? AND deleted_at IS NULL", channelIDs).Find(&channels).Error; err != nil {
		return nil, err
	}
	if len(channels) != len(channelIDs) {
		return nil, invalidBusinessGroup("所选通道不存在或已删除")
	}
	deviceCodes := make([]string, 0, len(channels))
	seenCodes := make(map[string]struct{}, len(channels))
	for _, channel := range channels {
		if _, exists := seenCodes[channel.DeviceID]; !exists {
			seenCodes[channel.DeviceID] = struct{}{}
			deviceCodes = append(deviceCodes, channel.DeviceID)
		}
	}
	var devices []gbmodels.GbDevice
	if err := tx.Where("device_id IN ?", deviceCodes).Find(&devices).Error; err != nil {
		return nil, err
	}
	devicesByCode := make(map[string]gbmodels.GbDevice, len(devices))
	for _, device := range devices {
		devicesByCode[device.DeviceID] = device
	}
	sources := make([]businessGroupShareSource, 0, len(channels))
	for _, channel := range channels {
		device, exists := devicesByCode[channel.DeviceID]
		if !exists {
			return nil, invalidBusinessGroup("所选通道的源设备不存在")
		}
		sources = append(sources, businessGroupShareSource{channel: channel, device: device})
	}
	return sources, nil
}

func uniqueBusinessGroupSourceDeviceIDs(sources []businessGroupShareSource) []uint64 {
	ids := make([]uint64, 0, len(sources))
	seen := make(map[uint64]struct{}, len(sources))
	for _, source := range sources {
		id := uint64(source.device.ID)
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

func loadBusinessGroupDeviceProjections(tx *gorm.DB, platformID uint64, sources []businessGroupShareSource) (map[uint64]model.GbCascadeDeviceProjection, error) {
	deviceIDs := uniqueBusinessGroupSourceDeviceIDs(sources)
	var rows []model.GbCascadeDeviceProjection
	if err := tx.Unscoped().Where("platform_id = ? AND source_device_id IN ?", platformID, deviceIDs).Find(&rows).Error; err != nil {
		return nil, err
	}
	bySource := make(map[uint64]model.GbCascadeDeviceProjection, len(rows))
	for _, row := range rows {
		bySource[row.SourceDeviceID] = row
	}
	return bySource, nil
}

func loadBusinessGroupChannelProjections(tx *gorm.DB, platformID uint64, channelIDs []uint64) (map[uint64]model.GbCascadeChannelProjection, error) {
	var rows []model.GbCascadeChannelProjection
	if err := tx.Unscoped().Where("platform_id = ? AND source_channel_id IN ?", platformID, channelIDs).Find(&rows).Error; err != nil {
		return nil, err
	}
	bySource := make(map[uint64]model.GbCascadeChannelProjection, len(rows))
	for _, row := range rows {
		bySource[row.SourceChannelID] = row
	}
	return bySource, nil
}

func businessGroupDeviceInputs(sources []businessGroupShareSource, existing map[uint64]model.GbCascadeDeviceProjection) []DeviceProjectionInput {
	inputs := make([]DeviceProjectionInput, 0, len(sources))
	seen := make(map[uint64]struct{}, len(sources))
	for _, source := range sources {
		id := uint64(source.device.ID)
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		if row, exists := existing[id]; exists {
			inputs = append(inputs, DeviceProjectionInput{SourceDeviceID: id, PublishedDeviceID: row.PublishedDeviceID})
		} else {
			inputs = append(inputs, businessGroupDeviceInput(source.device))
		}
	}
	return inputs
}

func businessGroupDeviceInput(device gbmodels.GbDevice) DeviceProjectionInput {
	name := strings.TrimSpace(device.Alias)
	if name == "" {
		name = device.Name
	}
	return DeviceProjectionInput{
		SourceDeviceID: uint64(device.ID), PublishedDeviceID: device.DeviceID,
		Name: name, Manufacturer: device.Manufacturer, Model: device.Model, Parental: 1,
	}
}

func businessGroupNewChannelInputs(sources []businessGroupShareSource, existing map[uint64]model.GbCascadeChannelProjection, ptzAllowed bool) []ChannelProjectionInput {
	inputs := make([]ChannelProjectionInput, 0, len(sources))
	for _, source := range sources {
		id := uint64(source.channel.ID)
		if row, exists := existing[id]; exists && row.Active && !row.DeletedAt.Valid {
			continue
		}
		name := strings.TrimSpace(source.channel.Alias)
		if name == "" {
			name = source.channel.Name
		}
		inputs = append(inputs, ChannelProjectionInput{
			SourceDeviceID: uint64(source.device.ID), SourceChannelID: id,
			PublishedChannelID: source.channel.ChannelID, Name: name,
			PTZAllowed: ptzAllowed,
		})
	}
	return inputs
}

func businessGroupAllocationDevices(tx *gorm.DB, platformID uint64, sources []businessGroupShareSource, required map[uint64]model.GbCascadeDeviceProjection) ([]DeviceProjectionInput, error) {
	var rows []model.GbCascadeDeviceProjection
	if err := tx.Unscoped().Where("platform_id = ?", platformID).Find(&rows).Error; err != nil {
		return nil, err
	}
	inputs := make([]DeviceProjectionInput, 0, len(rows)+len(sources))
	seen := make(map[uint64]struct{}, len(rows)+len(sources))
	for _, row := range rows {
		if _, exists := seen[row.SourceDeviceID]; exists {
			continue
		}
		seen[row.SourceDeviceID] = struct{}{}
		inputs = append(inputs, DeviceProjectionInput{SourceDeviceID: row.SourceDeviceID, PublishedDeviceID: row.PublishedDeviceID})
	}
	for _, source := range sources {
		id := uint64(source.device.ID)
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		if row, exists := required[id]; exists {
			inputs = append(inputs, DeviceProjectionInput{SourceDeviceID: id, PublishedDeviceID: row.PublishedDeviceID})
		} else {
			inputs = append(inputs, businessGroupDeviceInput(source.device))
		}
	}
	return inputs, nil
}

func validateBusinessGroupShareIdentityCollisions(tx *gorm.DB, platform model.GbCascadePlatform, devices []DeviceProjectionInput, channels []ChannelProjectionInput) error {
	used := map[string]struct{}{}
	for _, id := range []string{platform.LocalDeviceID, platform.UpstreamServerID} {
		if id != "" {
			used[id] = struct{}{}
		}
	}
	for _, input := range devices {
		if _, exists := used[input.PublishedDeviceID]; exists {
			return invalidBusinessGroup("共享设备编号与平台身份编号冲突")
		}
		used[input.PublishedDeviceID] = struct{}{}
	}
	for _, input := range channels {
		if _, exists := used[input.PublishedChannelID]; exists {
			return invalidBusinessGroup("共享通道编号与平台或设备编号冲突")
		}
		used[input.PublishedChannelID] = struct{}{}
	}

	deviceCodes := make([]string, 0, len(devices))
	for _, input := range devices {
		deviceCodes = append(deviceCodes, input.PublishedDeviceID)
	}
	if len(deviceCodes) == 0 {
		return nil
	}
	var count int64
	if err := tx.Unscoped().Model(&model.GbCascadeChannelProjection{}).
		Where("platform_id = ? AND published_channel_id IN ?", platform.ID, deviceCodes).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return invalidBusinessGroup("共享设备编号与已保留的通道编号冲突")
	}
	channelCodes := make([]string, 0, len(channels))
	for _, input := range channels {
		channelCodes = append(channelCodes, input.PublishedChannelID)
	}
	if len(channelCodes) == 0 {
		return nil
	}
	if err := tx.Unscoped().Model(&model.GbCascadeDeviceProjection{}).
		Where("platform_id = ? AND published_device_id IN ?", platform.ID, channelCodes).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return invalidBusinessGroup("共享通道编号与已保留的设备编号冲突")
	}
	return nil
}

func validateBusinessGroup(tx *gorm.DB, platform model.GbCascadePlatform, nodes []BusinessNodeInput, bindings []BusinessBindingInput) ([]model.GbCascadeBusinessNode, error) {
	stored := make([]model.GbCascadeBusinessNode, 0, len(nodes))
	byCode := make(map[string]BusinessNodeInput, len(nodes))
	rootCount := 0
	for _, input := range nodes {
		if len(input.Code) != 20 || !allASCIIDigits(input.Code) {
			return nil, invalidBusinessGroup("节点编号必须是 20 位数字")
		}
		if _, exists := byCode[input.Code]; exists {
			return nil, invalidBusinessGroup("业务分组节点编号重复")
		}
		input.Name = strings.TrimSpace(input.Name)
		if input.Name == "" {
			return nil, invalidBusinessGroup("节点名称不能为空")
		}
		if utf8.RuneCountInString(input.Name) > 128 {
			return nil, invalidBusinessGroup("节点名称不能超过 128 个字符")
		}
		switch input.Kind {
		case "group":
			rootCount++
			if input.Code[10:13] != "215" || input.ParentCode != "" {
				return nil, invalidBusinessGroup("业务分组根必须使用 215 编码且没有父节点")
			}
		case "organization":
			if input.Code[10:13] != "216" || input.ParentCode == "" {
				return nil, invalidBusinessGroup("虚拟组织必须使用 216 编码并指定父节点")
			}
		default:
			return nil, invalidBusinessGroup("节点类型只能是 group 或 organization")
		}
		byCode[input.Code] = input
		stored = append(stored, model.GbCascadeBusinessNode{
			PlatformID: platform.ID, Code: input.Code, Name: input.Name, Kind: input.Kind,
			ParentCode: input.ParentCode, Sort: input.Sort,
		})
	}
	if len(nodes) > 0 && rootCount != 1 {
		return nil, invalidBusinessGroup("每个平台必须且只能配置一个业务分组根")
	}
	newCodes := make(map[string]struct{}, len(nodes))
	newNodes := make(map[string]BusinessNodeInput, len(nodes))
	for _, input := range nodes {
		newCodes[input.Code] = struct{}{}
		newNodes[input.Code] = input
	}
	var existingNodes []model.GbCascadeBusinessNode
	if err := tx.Where("platform_id = ?", platform.ID).Find(&existingNodes).Error; err != nil {
		return nil, err
	}
	for _, existing := range existingNodes {
		if _, remains := newCodes[existing.Code]; remains {
			continue
		}
		for _, child := range existingNodes {
			if child.ParentCode != existing.Code {
				continue
			}
			newChild, remains := newNodes[child.Code]
			if !remains || newChild.ParentCode == existing.Code {
				return nil, invalidBusinessGroup("不能删除仍有子节点的目录节点，请先在本次保存中移动或删除其内容")
			}
		}
	}
	for _, input := range nodes {
		if input.Kind == "organization" {
			parent, exists := byCode[input.ParentCode]
			if !exists || (parent.Kind != "group" && parent.Kind != "organization") {
				return nil, invalidBusinessGroup("虚拟组织的父节点不存在或类型无效")
			}
		}
	}
	if rootCount == 1 {
		rootCode := ""
		for _, input := range nodes {
			if input.Kind == "group" {
				rootCode = input.Code
				break
			}
		}
		reachable := map[string]bool{rootCode: true}
		remaining := len(nodes) - 1
		for progress := true; progress && remaining > 0; {
			progress = false
			for _, input := range nodes {
				if input.Kind == "organization" && !reachable[input.Code] && reachable[input.ParentCode] {
					reachable[input.Code] = true
					remaining--
					progress = true
				}
			}
		}
		if remaining != 0 {
			return nil, invalidBusinessGroup("业务分组存在孤立节点或循环父子关系")
		}
	}

	if err := validateBusinessCodesAgainstPlatform(tx, platform, nodes); err != nil {
		return nil, err
	}

	bindingChannels := make(map[uint64]struct{}, len(bindings))
	for _, binding := range bindings {
		if binding.SourceChannelID == 0 {
			return nil, invalidBusinessGroup("挂载通道不能为空")
		}
		if _, exists := bindingChannels[binding.SourceChannelID]; exists {
			return nil, invalidBusinessGroup("同一通道在一个上级下只能挂载到一个组织")
		}
		bindingChannels[binding.SourceChannelID] = struct{}{}
		node, exists := byCode[binding.NodeCode]
		if !exists || node.Kind != "organization" {
			return nil, invalidBusinessGroup("通道只能挂载到当前业务树中的虚拟组织")
		}
		var count int64
		if err := tx.Model(&model.GbCascadeChannelProjection{}).
			Where("platform_id = ? AND source_channel_id = ? AND active = ?", platform.ID, binding.SourceChannelID, true).Count(&count).Error; err != nil {
			return nil, err
		}
		if count != 1 {
			return nil, invalidBusinessGroup("只能挂载当前上级已共享的有效通道")
		}
	}
	return stored, nil
}

func validatePublishedBusinessGroup(tx *gorm.DB, platform model.GbCascadePlatform, nodes []BusinessNodeInput, bindings []BusinessBindingInput) ([]model.GbCascadeBusinessNode, error) {
	if len(platform.LocalDeviceID) != 20 || !allASCIIDigits(platform.LocalDeviceID) || platform.LocalDeviceID[10:13] != "200" {
		return nil, invalidBusinessGroup("发布业务分组要求本平台设备 ID 为有效的 20 位 200 类型系统编码")
	}
	storedNodes, err := validateBusinessGroup(tx, platform, nodes, bindings)
	if err != nil {
		return nil, err
	}
	rootCount := 0
	for _, node := range nodes {
		if node.Kind == "group" && node.ParentCode == "" {
			rootCount++
		}
	}
	if rootCount != 1 {
		return nil, invalidBusinessGroup("发布业务分组前必须配置唯一的 215 根节点")
	}
	return storedNodes, nil
}

func loadBusinessGroupInputs(tx *gorm.DB, platformID uint64) ([]BusinessNodeInput, []BusinessBindingInput, error) {
	var storedNodes []model.GbCascadeBusinessNode
	if err := tx.Where("platform_id = ?", platformID).Order("sort, id").Find(&storedNodes).Error; err != nil {
		return nil, nil, err
	}
	var storedBindings []model.GbCascadeBusinessBinding
	if err := tx.Where("platform_id = ?", platformID).Order("id").Find(&storedBindings).Error; err != nil {
		return nil, nil, err
	}
	nodes := make([]BusinessNodeInput, 0, len(storedNodes))
	for _, node := range storedNodes {
		nodes = append(nodes, BusinessNodeInput{Code: node.Code, Name: node.Name, Kind: node.Kind, ParentCode: node.ParentCode, Sort: node.Sort})
	}
	bindings := make([]BusinessBindingInput, 0, len(storedBindings))
	for _, binding := range storedBindings {
		bindings = append(bindings, BusinessBindingInput{SourceChannelID: binding.SourceChannelID, NodeCode: binding.NodeCode})
	}
	return nodes, bindings, nil
}

func validateBusinessCodesAgainstPlatform(tx *gorm.DB, platform model.GbCascadePlatform, nodes []BusinessNodeInput) error {
	codes := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if node.Code == platform.LocalDeviceID || node.Code == platform.UpstreamServerID {
			return invalidBusinessGroup("节点编号与平台 SIP 身份编号冲突")
		}
		codes = append(codes, node.Code)
	}
	if len(codes) == 0 {
		return nil
	}
	var count int64
	if err := tx.Model(&model.GbCascadeDeviceProjection{}).
		Where("platform_id = ? AND active = ? AND published_device_id IN ?", platform.ID, true, codes).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return invalidBusinessGroup("节点编号与已共享设备编号冲突")
	}
	if err := tx.Model(&model.GbCascadeChannelProjection{}).
		Where("platform_id = ? AND active = ? AND published_channel_id IN ?", platform.ID, true, codes).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return invalidBusinessGroup("节点编号与已共享通道编号冲突")
	}
	return nil
}

func allASCIIDigits(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return value != ""
}

func invalidBusinessGroup(reason string) error {
	return fmt.Errorf("%w: %s", ErrInvalidProjection, reason)
}
