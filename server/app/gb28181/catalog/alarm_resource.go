package catalog

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func upsertAlarmResource(
	ctx context.Context,
	db *gorm.DB,
	ownerDeptID uint,
	sourceDeviceCode string,
	item CatalogItem,
	cls Classification,
	parentNode *gbmodels.GbCatalogNode,
) (*gbmodels.GbCatalogNode, *gbmodels.GbAlarmResource, error) {
	sourceDeviceCode = strings.TrimSpace(sourceDeviceCode)
	if sourceDeviceCode == "" {
		return nil, nil, fmt.Errorf("catalog: alarm resource %s has no source device", item.DeviceID)
	}

	resourceType := gbmodels.AlarmResourceInput
	if cls.NodeType == gbmodels.NodeTypeAlarmOutput {
		resourceType = gbmodels.AlarmResourceOutput
	}
	deviceID := uint(0)
	if id := lookupSourceDeviceID(db, Sender{SourceDeviceID: sourceDeviceCode}); id != nil {
		deviceID = *id
	}
	status := gbmodels.ChannelStatusOffline
	if item.StatusOn {
		status = gbmodels.ChannelStatusOnline
	}

	// 原子 upsert:全量 Catalog 与订阅增量可能并发处理同一资源,
	// "先查再建"会让后提交者命中唯一键错误并丢弃本次更新
	resource := gbmodels.GbAlarmResource{
		OwnerDeptID: ownerDeptID, DeviceID: deviceID, DeviceCode: sourceDeviceCode,
		AlarmCode: item.DeviceID, ResourceType: resourceType, TypeCode: catalogTypeCode(item.DeviceID),
		Name: fallbackName(item.Name, item.DeviceID), RawParentIDs: strings.TrimSpace(item.ParentID), Status: status,
	}
	upsert := db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "owner_dept_id"}, {Name: "device_code"}, {Name: "alarm_code"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"device_id", "resource_type", "type_code", "name", "raw_parent_ids",
			"status", "deleted_at", "updated_at",
		}),
	}).Create(&resource)
	if upsert.Error != nil {
		return nil, nil, upsert.Error
	}
	if err := db.WithContext(ctx).Unscoped().
		Where("owner_dept_id = ? AND device_code = ? AND alarm_code = ?", ownerDeptID, sourceDeviceCode, item.DeviceID).
		Limit(1).Find(&resource).Error; err != nil {
		return nil, nil, err
	}

	if err := replaceAlarmResourceParents(db.WithContext(ctx), resource.ID, item.ParentID); err != nil {
		return nil, nil, err
	}
	node, err := linkAlarmResourceNode(db.WithContext(ctx), ownerDeptID, cls.NodeType, item, parentNode, resource.ID)
	if err != nil {
		return nil, nil, err
	}
	return node, &resource, nil
}

// replaceAlarmResourceParents 全量替换告警资源的父节点关联
func replaceAlarmResourceParents(db *gorm.DB, resourceID uint, rawParentIDs string) error {
	if err := db.Where("alarm_resource_id = ?", resourceID).Delete(&gbmodels.GbAlarmResourceParent{}).Error; err != nil {
		return err
	}
	for _, parentCode := range SplitParentIDs(rawParentIDs) {
		if err := db.Create(&gbmodels.GbAlarmResourceParent{
			AlarmResourceID: resourceID,
			ParentCode:      parentCode,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

// linkAlarmResourceNode 找到或创建目录节点并关联到告警资源
func linkAlarmResourceNode(db *gorm.DB, ownerDeptID uint, nodeType gbmodels.NodeType, item CatalogItem, parentNode *gbmodels.GbCatalogNode, resourceID uint) (*gbmodels.GbCatalogNode, error) {
	var parentID *uint
	parentPath := "/"
	if parentNode != nil {
		parentID = &parentNode.ID
		parentPath = parentNode.Path
	}
	node, err := findOrCreateNode(db, ownerDeptID, nodeType, item.DeviceID, parentID, parentPath, fallbackName(item.Name, item.DeviceID))
	if err != nil {
		return nil, err
	}
	if node.AlarmResourceID == nil || *node.AlarmResourceID != resourceID {
		if err := db.Model(node).Update("alarm_resource_id", resourceID).Error; err != nil {
			return nil, err
		}
		node.AlarmResourceID = &resourceID
	}
	return node, nil
}

func catalogTypeCode(code string) string {
	code = strings.TrimSpace(code)
	if len(code) != 20 {
		return ""
	}
	return code[10:13]
}
