package directory

import (
	"context"

	"gorm.io/gorm"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// BusinessContainerTypes 业务树的容器类型(业务分组 215 / 虚拟组织 216)。
// 供 SQL `node_type IN ?` 使用，与 IsBusinessContainerType 同源。
var BusinessContainerTypes = []gbmodels.NodeType{
	gbmodels.NodeTypeBizGroup,
	gbmodels.NodeTypeVirtualOrg,
}

// BusinessLeafTypes 业务树的「叶」——国标附录 J 里挂在业务分组/虚拟组织下面的
// 「设备目录项」。供 SQL `node_type IN ?` 使用，与 IsBusinessLeafType 同源。
//
// ⛔⛔ 不能只认 device(2026-10-11 实测)：
// GB/T 28181-2022 附录 J 的图 J.2 示例里，业务树最下面挂的目录项是
// `11010100001320000009`(类型码 132 = 网络摄像机 IPC)，而本仓把 131/132 存成 channel。
// 只数 device 就等于永远数不到叶子 ⇒ 「业务组织」视图恒为 0/0，点选后右侧空列表。
var BusinessLeafTypes = []gbmodels.NodeType{
	gbmodels.NodeTypeDevice,
	gbmodels.NodeTypeChannel,
	gbmodels.NodeTypeAlarmInput,
	gbmodels.NodeTypeAlarmOutput,
}

// IsBusinessLeafType 判一个目录节点类型是否属于业务树的叶(资源类节点)。
func IsBusinessLeafType(t gbmodels.NodeType) bool {
	for _, leaf := range BusinessLeafTypes {
		if t == leaf {
			return true
		}
	}
	return false
}

// IsBusinessContainerType 判一个目录节点类型是否是业务树的容器(业务分组/虚拟组织)。
func IsBusinessContainerType(t gbmodels.NodeType) bool {
	for _, container := range BusinessContainerTypes {
		if t == container {
			return true
		}
	}
	return false
}

// BusinessLeafResolver 把叶子节点解析成「它所属的物理设备」(gb_device.id)。
//
// 四路来源，按可靠性排序 —— 缺哪一路都真的会漏，逐条都是踩出来的：
//  1. node.DeviceID        —— upsertDevice 回填的物理设备主键(最直接)；
//  2. node.ChannelID       —— gb_channel.device_id(所属设备国标编码) → gb_device.id；
//  3. node.AlarmResourceID —— gb_alarm_resource.device_id(物理设备主键)；
//  4. node.Code            —— 直接用编码反查 gb_device.device_id。
//
// ⛔ 第 4 条不是冗余：catalog/pipeline.go 的 `resolveBusinessParent` 在
// `case NodeTypeDevice` 分支建节点时**不回填 device_id**(只有 upsertDevice 回填)，
// 而实测开发库 gb_catalog_node.device_id 54 行全为 NULL ⇒ 只靠第 1 条等于不计数。
type BusinessLeafResolver struct {
	devices       []gbmodels.GbDevice
	deviceByCode  map[string]uint
	channelDevice map[uint]uint
	alarmDevice   map[uint]uint
}

// LoadBusinessLeafResolver 按给定数据范围(scope)预载四张表，避免逐节点查库。
// scope 为 nil 表示不限范围。
func LoadBusinessLeafResolver(ctx context.Context, db *gorm.DB, scope func(*gorm.DB) *gorm.DB) (*BusinessLeafResolver, error) {
	r := &BusinessLeafResolver{
		deviceByCode:  map[string]uint{},
		channelDevice: map[uint]uint{},
		alarmDevice:   map[uint]uint{},
	}
	q := db.WithContext(ctx).Model(&gbmodels.GbDevice{})
	if scope != nil {
		q = scope(q)
	}
	if err := q.Find(&r.devices).Error; err != nil {
		return nil, err
	}
	for _, d := range r.devices {
		if d.DeviceID != "" {
			r.deviceByCode[d.DeviceID] = d.ID
		}
	}

	cq := db.WithContext(ctx).Model(&gbmodels.GbChannel{})
	if scope != nil {
		cq = scope(cq)
	}
	var channels []gbmodels.GbChannel
	if err := cq.Find(&channels).Error; err != nil {
		return nil, err
	}
	for _, ch := range channels {
		if id, ok := r.deviceByCode[ch.DeviceID]; ok {
			r.channelDevice[ch.ID] = id
		}
	}

	aq := db.WithContext(ctx).Model(&gbmodels.GbAlarmResource{})
	if scope != nil {
		aq = scope(aq)
	}
	var alarms []gbmodels.GbAlarmResource
	if err := aq.Find(&alarms).Error; err != nil {
		return nil, err
	}
	for _, a := range alarms {
		if a.DeviceID != 0 {
			r.alarmDevice[a.ID] = a.DeviceID
		}
	}
	return r, nil
}

// Resolve 返回该叶节点归属的物理设备 id。
func (r *BusinessLeafResolver) Resolve(node gbmodels.GbCatalogNode) (uint, bool) {
	if node.DeviceID != nil && *node.DeviceID != 0 {
		return *node.DeviceID, true
	}
	if node.ChannelID != nil {
		if id, ok := r.channelDevice[*node.ChannelID]; ok {
			return id, true
		}
	}
	if node.AlarmResourceID != nil {
		if id, ok := r.alarmDevice[*node.AlarmResourceID]; ok {
			return id, true
		}
	}
	if id, ok := r.deviceByCode[node.Code]; ok {
		return id, true
	}
	return 0, false
}

// online 返回 device id → 是否在线(业务视图的在线数取这里)。
func (r *BusinessLeafResolver) online() map[uint]bool {
	out := make(map[uint]bool, len(r.devices))
	for _, d := range r.devices {
		out[d.ID] = d.Status == gbmodels.DeviceStatusOnline
	}
	return out
}

// BusinessAncestorIDs 沿 parent 链向上收集**全部**「业务分组/虚拟组织」祖先 id。
//
// ⛔ 不 break 是既有契约：一个物理设备可以被多个业务组织引用(业务分组套虚拟组织时两级都算)，
// 由 business_tree_test.go 的反向断言锁着，不能改成"只取最近一个"。
// seen 防脏数据里的 parent 自环。
func BusinessAncestorIDs(byID map[uint]gbmodels.GbCatalogNode, parentID *uint) []uint {
	var out []uint
	seen := map[uint]struct{}{}
	for parentID != nil {
		if _, ok := seen[*parentID]; ok {
			break
		}
		seen[*parentID] = struct{}{}
		parent, ok := byID[*parentID]
		if !ok {
			break
		}
		if IsBusinessContainerType(parent.NodeType) {
			out = append(out, parent.ID)
		}
		parentID = parent.ParentID
	}
	return out
}

// CollectBusinessLeafDeviceIDs 返回 path 前缀子树下所有叶节点归属的物理设备 id(去重)。
// 用于「点选业务分组后右侧设备列表」的筛选 —— 与 BuildBusinessTree 的计数口径保持一致。
func CollectBusinessLeafDeviceIDs(ctx context.Context, db *gorm.DB, scope func(*gorm.DB) *gorm.DB, pathPrefix string) ([]uint, error) {
	q := db.WithContext(ctx).Model(&gbmodels.GbCatalogNode{}).
		Where("path LIKE ? AND node_type IN ?", pathPrefix+"%", BusinessLeafTypes)
	if scope != nil {
		q = scope(q)
	}
	var nodes []gbmodels.GbCatalogNode
	if err := q.Find(&nodes).Error; err != nil {
		return nil, err
	}
	resolver, err := LoadBusinessLeafResolver(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	seen := map[uint]struct{}{}
	ids := make([]uint, 0, len(nodes))
	for _, node := range nodes {
		id, ok := resolver.Resolve(node)
		if !ok {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, nil
}
