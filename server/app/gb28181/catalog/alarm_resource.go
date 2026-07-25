package catalog

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

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

	var resource gbmodels.GbAlarmResource
	result := db.WithContext(ctx).Unscoped().
		Where("owner_dept_id = ? AND device_code = ? AND alarm_code = ?", ownerDeptID, sourceDeviceCode, item.DeviceID).
		Limit(1).Find(&resource)
	if result.Error != nil {
		return nil, nil, result.Error
	}
	values := map[string]interface{}{
		"device_id": deviceID, "resource_type": resourceType, "type_code": catalogTypeCode(item.DeviceID),
		"name": fallbackName(item.Name, item.DeviceID), "raw_parent_ids": strings.TrimSpace(item.ParentID),
		"status": status, "deleted_at": nil,
	}
	if result.RowsAffected == 0 {
		resource = gbmodels.GbAlarmResource{
			OwnerDeptID: ownerDeptID, DeviceID: deviceID, DeviceCode: sourceDeviceCode,
			AlarmCode: item.DeviceID, ResourceType: resourceType, TypeCode: catalogTypeCode(item.DeviceID),
			Name: fallbackName(item.Name, item.DeviceID), RawParentIDs: strings.TrimSpace(item.ParentID), Status: status,
		}
		if err := db.WithContext(ctx).Create(&resource).Error; err != nil {
			return nil, nil, err
		}
	} else if err := db.WithContext(ctx).Unscoped().Model(&resource).Updates(values).Error; err != nil {
		return nil, nil, err
	}

	if err := db.WithContext(ctx).Where("alarm_resource_id = ?", resource.ID).Delete(&gbmodels.GbAlarmResourceParent{}).Error; err != nil {
		return nil, nil, err
	}
	for _, parentCode := range SplitParentIDs(item.ParentID) {
		if err := db.WithContext(ctx).Create(&gbmodels.GbAlarmResourceParent{
			AlarmResourceID: resource.ID,
			ParentCode:      parentCode,
		}).Error; err != nil {
			return nil, nil, err
		}
	}

	var parentID *uint
	parentPath := "/"
	if parentNode != nil {
		parentID = &parentNode.ID
		parentPath = parentNode.Path
	}
	node, err := findOrCreateNode(db.WithContext(ctx), ownerDeptID, cls.NodeType, item.DeviceID, parentID, parentPath, fallbackName(item.Name, item.DeviceID))
	if err != nil {
		return nil, nil, err
	}
	if node.AlarmResourceID == nil || *node.AlarmResourceID != resource.ID {
		if err := db.WithContext(ctx).Model(node).Update("alarm_resource_id", resource.ID).Error; err != nil {
			return nil, nil, err
		}
		node.AlarmResourceID = &resource.ID
	}
	return node, &resource, nil
}

func catalogTypeCode(code string) string {
	code = strings.TrimSpace(code)
	if len(code) != 20 {
		return ""
	}
	return code[10:13]
}
