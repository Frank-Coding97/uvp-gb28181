package catalog

import (
	"context"
	"strings"

	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// CivilCodeLookup 行政区划字典查询接口(解耦 civilcode.Service)
type CivilCodeLookup interface {
	Lookup(code string) interface{} // 返回 *civilcode.SysCivilCode 或 nil
}

var (
	civilCodeLookup CivilCodeLookup // 注入点,bootstrap 阶段注入
)

// SetCivilCodeLookup 注入行政区划字典服务(给 bootstrap / 测试用)
func SetCivilCodeLookup(lookup CivilCodeLookup) {
	civilCodeLookup = lookup
}

// upsertDevice 设备节点:gb_device upsert + 在 catalog tree 建/找节点
//
// 设备节点直接挂在 civil_code 链下;若 item.ParentID 是另一个 device/biz_group,
// 用 ParentID 做 hint,优先归属;否则挂在 civil_code 末端
func upsertDevice(
	ctx context.Context,
	db *gorm.DB,
	ownerDeptID uint,
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
			Model:               item.Model,
			OwnerDeptID:         ownerDeptID,
			SubscribeCapability: gbmodels.SubscribeUnknown,
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
		if ownerDeptID != 0 && dev.OwnerDeptID == 0 {
			updates["owner_dept_id"] = ownerDeptID
			dev.OwnerDeptID = ownerDeptID
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
	node, err := findOrCreateNode(db.WithContext(ctx), dev.OwnerDeptID, gbmodels.NodeTypeDevice, item.DeviceID, pid, parentPath, fallbackName(item.Name, item.DeviceID))
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
	ownerDeptID uint,
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

	// 4 层兜底解析 CivilCode(L1:XML上报 → L2:DeviceID前6位 → L3:父节点 → L4:000000)
	parentCivilCode := ""
	if parentNode != nil {
		parentCivilCode = parentNode.CivilCode
	}
	resolvedCivilCode := resolveCivilCode(item.CivilCode, cls.CivilCode, parentCivilCode)

	var ch gbmodels.GbChannel
	res := db.WithContext(ctx).Where("device_id = ? AND channel_id = ?", sourceDeviceID, item.DeviceID).Limit(1).Find(&ch)
	if res.Error != nil {
		return nil, nil, res.Error
	}
	if res.RowsAffected == 0 {
		ch = gbmodels.GbChannel{
			ChannelID:       item.DeviceID,
			DeviceID:        sourceDeviceID,
			Name:            fallbackName(item.Name, item.DeviceID),
			Manufacturer:    item.Manufacturer,
			Model:           item.Model,
			Owner:           item.Owner,
			CivilCode:       resolvedCivilCode,
			ParentID:        item.ParentID,
			PTZType:         int8(item.PTZType),
			Longitude:       item.Longitude,
			Latitude:        item.Latitude,
			Status:          status,
			OnDemandLive:    true,
			AudioEnabled:    true,
			OwnerDeptID:     ownerDeptID,
			StreamTransport: "TCP-Passive",
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
			"civil_code":   resolvedCivilCode,
			"parent_id":    item.ParentID,
			"ptz_type":     int8(item.PTZType),
			"longitude":    item.Longitude,
			"latitude":     item.Latitude,
			"status":       status,
		}
		if ownerDeptID != 0 && ch.OwnerDeptID == 0 {
			updates["owner_dept_id"] = ownerDeptID
			ch.OwnerDeptID = ownerDeptID
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
	node, err := findOrCreateNode(db.WithContext(ctx), ch.OwnerDeptID, gbmodels.NodeTypeChannel, item.DeviceID, pid, parentPath, fallbackName(item.Name, item.DeviceID))
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
		if err := ensurePrimaryMount(ctx, db, ch.OwnerDeptID, ch.ID, parentNode.ID, item.Name); err != nil {
			return nil, nil, err
		}
	}

	_ = cls
	return node, &ch, nil
}

// ensurePrimaryMount 保证主挂载存在
func ensurePrimaryMount(ctx context.Context, db *gorm.DB, ownerDeptID, channelID, parentNodeID uint, displayName string) error {
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
			updates := map[string]any{"is_primary": true}
			if ownerDeptID != 0 && existed.OwnerDeptID == 0 {
				updates["owner_dept_id"] = ownerDeptID
			}
			return db.WithContext(ctx).Model(&existed).Updates(updates).Error
		}
		if ownerDeptID != 0 && existed.OwnerDeptID == 0 {
			return db.WithContext(ctx).Model(&existed).Update("owner_dept_id", ownerDeptID).Error
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
		OwnerDeptID:  ownerDeptID,
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

// resolveCivilCode 4 层兜底策略解析行政区划码
//
// L1: XML 显式上报的 CivilCode(item.CivilCode)
// L2: 从 20 位国标 DeviceID 前 6 位提取(cls.CivilCode,classifier 已算出)
// L3: 从父节点继承(parentCivilCode,调用方传入父设备/父节点的 civil_code)
// L4: 归"未分配"兜底桶(000000)
//
// L1/L2 结果需校验 sys_civil_code 字典存在性,查不到降级下一层
func resolveCivilCode(
	itemCivilCode   string, // L1: XML 上报
	clsCivilCode    string, // L2: classifier 从 DeviceID 提取
	parentCivilCode string, // L3: 父节点
) string {
	const unassigned = "000000" // L4 兜底

	// L1: XML 显式上报优先
	if itemCivilCode != "" && len(itemCivilCode) == 6 && isAllDigit(itemCivilCode) {
		if civilCodeLookup != nil && civilCodeLookup.Lookup(itemCivilCode) != nil {
			return itemCivilCode
		}
		// 上报了但字典查不到(厂商私有编码/错误编码),降级 L2
	}

	// L2: DeviceID 前 6 位提取(classifier 已算出)
	if clsCivilCode != "" && len(clsCivilCode) == 6 {
		if civilCodeLookup != nil && civilCodeLookup.Lookup(clsCivilCode) != nil {
			return clsCivilCode
		}
		// classifier 提取了但字典查不到,降级 L3
	}

	// L3: 父节点继承(父子同行政区大概率成立)
	if parentCivilCode != "" && len(parentCivilCode) == 6 {
		// 父节点已经过前 3 层兜底,直接信任(不再校验字典)
		return parentCivilCode
	}

	// L4: 归"未分配"兜底桶
	return unassigned
}
