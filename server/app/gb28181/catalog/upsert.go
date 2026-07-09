package catalog

import (
	"context"
	"strings"

	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// upsertDevice 设备节点:gb_device upsert + 在 catalog tree 建/找节点
//
// 设备节点直接挂在 civil_code 链下;若 item.ParentID 是另一个 device/biz_group,
// 用 ParentID 做 hint,优先归属;否则挂在 civil_code 末端
func upsertDevice(
	ctx context.Context,
	db *gorm.DB,
	tenantID uint,
	item CatalogItem,
	cls Classification,
	parentNode *gbmodels.GbCatalogNode,
) (*gbmodels.GbCatalogNode, *gbmodels.GbDevice, error) {
	// 1. 物理设备 upsert(无 device 记录则建)
	var dev gbmodels.GbDevice
	res := db.WithContext(ctx).Where("device_id = ?", item.DeviceID).Limit(1).Find(&dev)
	if res.Error != nil {
		return nil, nil, res.Error
	}
	if res.RowsAffected == 0 {
		dev = gbmodels.GbDevice{
			DeviceID:            item.DeviceID,
			Name:                fallbackName(item.Name, item.DeviceID),
			Manufacturer:        item.Manufacturer,
			Model:                item.Model,
			TenantID:             tenantID,
			SubscribeCapability:  gbmodels.SubscribeUnknown,
		}
		if err := db.WithContext(ctx).Create(&dev).Error; err != nil {
			return nil, nil, err
		}
	} else {
		// 已存在仅更新可变字段(不动 keepalive_time / status,那是注册心跳的事)
		updates := map[string]any{}
		if item.Name != "" && item.Name != dev.Name {
			updates["name"] = item.Name
		}
		if item.Manufacturer != "" && item.Manufacturer != dev.Manufacturer {
			updates["manufacturer"] = item.Manufacturer
		}
		if item.Model != "" && item.Model != dev.Model {
			updates["model"] = item.Model
		}
		if len(updates) > 0 {
			if err := db.WithContext(ctx).Model(&dev).Updates(updates).Error; err != nil {
				return nil, nil, err
			}
		}
	}

	// 2. catalog_node 建/找
	deviceIDCopy := dev.ID
	var pid *uint
	parentPath := "/"
	if parentNode != nil {
		pid = &parentNode.ID
		parentPath = parentNode.Path
	}
	node, err := findOrCreateNode(db.WithContext(ctx), tenantID, gbmodels.NodeTypeDevice, item.DeviceID, pid, parentPath, fallbackName(item.Name, item.DeviceID))
	if err != nil {
		return nil, nil, err
	}
	// 回填 device_id 关联(首次创建时)
	if node.DeviceID == nil || *node.DeviceID != deviceIDCopy {
		if err := db.WithContext(ctx).Model(node).Update("device_id", deviceIDCopy).Error; err != nil {
			return nil, nil, err
		}
		node.DeviceID = &deviceIDCopy
	}
	_ = cls // 设备节点 anomaly 由 caller 处理
	return node, &dev, nil
}

// upsertChannel 通道节点:gb_channel upsert + catalog_node 建 + gb_channel_mount 主挂载
//
// 通道节点的关键在于建立"通道在哪个目录下"的多挂载关系(plan §3.5),
// 主挂载 is_primary=1,挂在 parentNode 下。
func upsertChannel(
	ctx context.Context,
	db *gorm.DB,
	tenantID uint,
	sourceDeviceID string,
	item CatalogItem,
	cls Classification,
	parentNode *gbmodels.GbCatalogNode,
) (*gbmodels.GbCatalogNode, *gbmodels.GbChannel, error) {
	// 1. 物理通道 upsert
	status := gbmodels.ChannelStatusOffline
	if item.StatusOn {
		status = gbmodels.ChannelStatusOnline
	}
	var ch gbmodels.GbChannel
	res := db.WithContext(ctx).Where("device_id = ? AND channel_id = ?", sourceDeviceID, item.DeviceID).Limit(1).Find(&ch)
	if res.Error != nil {
		return nil, nil, res.Error
	}
	if res.RowsAffected == 0 {
		ch = gbmodels.GbChannel{
			ChannelID:    item.DeviceID,
			DeviceID:     sourceDeviceID,
			Name:         fallbackName(item.Name, item.DeviceID),
			Manufacturer: item.Manufacturer,
			Model:        item.Model,
			Owner:        item.Owner,
			CivilCode:    item.CivilCode,
			ParentID:     item.ParentID,
			PTZType:      int8(item.PTZType),
			Longitude:    item.Longitude,
			Latitude:     item.Latitude,
			Status:       status,
			TenantID:     tenantID,
		}
		if err := db.WithContext(ctx).Create(&ch).Error; err != nil {
			return nil, nil, err
		}
	} else {
		updates := map[string]any{
			"name":         fallbackName(item.Name, ch.Name),
			"manufacturer": item.Manufacturer,
			"model":        item.Model,
			"owner":        item.Owner,
			"civil_code":   item.CivilCode,
			"parent_id":    item.ParentID,
			"ptz_type":     int8(item.PTZType),
			"longitude":    item.Longitude,
			"latitude":     item.Latitude,
			"status":       status,
		}
		if err := db.WithContext(ctx).Model(&ch).Updates(updates).Error; err != nil {
			return nil, nil, err
		}
	}

	// 2. catalog_node(channel 类型)建/找
	chIDCopy := ch.ID
	var pid *uint
	parentPath := "/"
	if parentNode != nil {
		pid = &parentNode.ID
		parentPath = parentNode.Path
	}
	node, err := findOrCreateNode(db.WithContext(ctx), tenantID, gbmodels.NodeTypeChannel, item.DeviceID, pid, parentPath, fallbackName(item.Name, item.DeviceID))
	if err != nil {
		return nil, nil, err
	}
	if node.ChannelID == nil || *node.ChannelID != chIDCopy {
		if err := db.WithContext(ctx).Model(node).Update("channel_id", chIDCopy).Error; err != nil {
			return nil, nil, err
		}
		node.ChannelID = &chIDCopy
	}

	// 3. 主挂载 gb_channel_mount 建/找(parentNode = 该通道默认挂载点)
	if parentNode != nil {
		if err := ensurePrimaryMount(ctx, db, tenantID, ch.ID, parentNode.ID, item.Name); err != nil {
			return nil, nil, err
		}
	}

	_ = cls
	return node, &ch, nil
}

// ensurePrimaryMount 保证主挂载存在
func ensurePrimaryMount(ctx context.Context, db *gorm.DB, tenantID, channelID, parentNodeID uint, displayName string) error {
	var existed gbmodels.GbChannelMount
	res := db.WithContext(ctx).Where("channel_id = ? AND parent_node_id = ?", channelID, parentNodeID).Limit(1).Find(&existed)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		// 已存在不动(避免 is_primary 抖动);若该 channel 没有任何主挂载,把这个标主
		var primaryCount int64
		if err := db.WithContext(ctx).Model(&gbmodels.GbChannelMount{}).
			Where("channel_id = ? AND is_primary = ?", channelID, true).
			Count(&primaryCount).Error; err != nil {
			return err
		}
		if primaryCount == 0 {
			return db.WithContext(ctx).Model(&existed).Update("is_primary", true).Error
		}
		return nil
	}
	// 新建挂载:若当前 channel 还没有主挂载,这一个就是主的
	var primaryCount int64
	if err := db.WithContext(ctx).Model(&gbmodels.GbChannelMount{}).
		Where("channel_id = ? AND is_primary = ?", channelID, true).
		Count(&primaryCount).Error; err != nil {
		return err
	}
	m := &gbmodels.GbChannelMount{
		TenantID:     tenantID,
		ChannelID:    channelID,
		ParentNodeID: parentNodeID,
		DisplayName:  strings.TrimSpace(displayName),
		IsPrimary:    primaryCount == 0,
		MountSource:  gbmodels.MountSourceCatalog,
	}
	return db.WithContext(ctx).Create(m).Error
}

// fallbackName 名称为空时给个兜底
func fallbackName(name, fallback string) string {
	if strings.TrimSpace(name) != "" {
		return name
	}
	return fallback
}
