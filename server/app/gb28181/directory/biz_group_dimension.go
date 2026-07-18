package directory

import (
	"fmt"
	"strconv"

	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// BizGroupDimension 业务分组维度
//
// 数据源:
//   - gb_catalog_node WHERE node_type = 'biz_group'(业务分组节点)
//   - gb_channel_mount(挂载在业务组下的通道)
//   - gb_channel(通道信息)
//
// GetChildren 语义:业务组的子节点包括
//   1. 子业务组(gb_catalog_node WHERE parent_id = X AND node_type = 'biz_group')
//   2. 挂载在该组下的通道(gb_channel_mount JOIN gb_channel WHERE parent_node_id = X)
//
// 同通道多挂载(D-6 决策):允许重复显示
//   - 每个 gb_channel_mount 记录都产出一个 Node,不去重
//   - 前端展示"多挂载"角标提示用户
type BizGroupDimension struct {
	db    *gorm.DB
	scope ScopeFunc
}

// NewBizGroupDimension 构造函数
func NewBizGroupDimension(db *gorm.DB, scope ScopeFunc) *BizGroupDimension {
	if scope == nil {
		scope = func(db *gorm.DB) *gorm.DB { return db }
	}
	return &BizGroupDimension{db: db, scope: scope}
}

// GetRoots 获取顶级业务组
func (d *BizGroupDimension) GetRoots(withCounts bool) ([]Node, error) {
	var dbNodes []models.GbCatalogNode
	err := d.db.Scopes(d.scope).
		Where("node_type = ? AND parent_id IS NULL", models.NodeTypeBizGroup).
		Order(defaultOrderBy).
		Find(&dbNodes).Error
	if err != nil {
		return nil, fmt.Errorf("query biz_group roots failed: %w", err)
	}

	nodes := make([]Node, 0, len(dbNodes))
	for _, dbNode := range dbNodes {
		nodes = append(nodes, d.convertGroupToNode(dbNode, withCounts))
	}
	return nodes, nil
}

// GetChildren 获取业务组的子节点(子业务组 + 挂载通道)
func (d *BizGroupDimension) GetChildren(parentID string, withCounts bool) ([]Node, error) {
	parentIDUint, err := strconv.ParseUint(parentID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid parentID: %w", err)
	}

	// 1. 子业务组
	var subGroups []models.GbCatalogNode
	err = d.db.Scopes(d.scope).
		Where("node_type = ? AND parent_id = ?", models.NodeTypeBizGroup, uint(parentIDUint)).
		Order(defaultOrderBy).
		Find(&subGroups).Error
	if err != nil {
		return nil, fmt.Errorf("query sub biz_groups failed: %w", err)
	}

	// 2. 挂载在当前业务组下的通道(D-6: 允许重复)
	var mounts []mountWithChannel
	err = d.db.
		Table("gb_channel_mount AS m").
		Select("m.id AS mount_id, m.channel_id AS channel_row_id, m.parent_node_id, m.is_primary, m.display_name, m.mount_source, "+
			"c.channel_id AS channel_code, c.device_id AS device_code, c.name AS channel_name").
		Joins("JOIN gb_channel c ON m.channel_id = c.id").
		Where("m.parent_node_id = ?", uint(parentIDUint)).
		Order("m.sort_order ASC, m.id ASC").
		Scan(&mounts).Error
	if err != nil {
		return nil, fmt.Errorf("query mounts failed: %w", err)
	}

	// 3. 组装 (子业务组在前,挂载通道在后)
	nodes := make([]Node, 0, len(subGroups)+len(mounts))
	for _, g := range subGroups {
		nodes = append(nodes, d.convertGroupToNode(g, withCounts))
	}
	for _, m := range mounts {
		nodes = append(nodes, d.convertMountToNode(m, uint(parentIDUint)))
	}
	return nodes, nil
}

// mountWithChannel gb_channel_mount JOIN gb_channel 的行结构
type mountWithChannel struct {
	MountID       uint   `gorm:"column:mount_id"`
	ChannelRowID  uint   `gorm:"column:channel_row_id"` // gb_channel.id
	ParentNodeID  uint   `gorm:"column:parent_node_id"`
	IsPrimary     bool   `gorm:"column:is_primary"`
	DisplayName   string `gorm:"column:display_name"`
	MountSource   string `gorm:"column:mount_source"`
	ChannelCode   string `gorm:"column:channel_code"` // gb_channel.channel_id
	DeviceCode    string `gorm:"column:device_code"`
	ChannelName   string `gorm:"column:channel_name"`
}

// convertGroupToNode 把 gb_catalog_node(biz_group)转成 directory.Node
func (d *BizGroupDimension) convertGroupToNode(dbNode models.GbCatalogNode, withCounts bool) Node {
	node := Node{
		ID:       strconv.FormatUint(uint64(dbNode.ID), 10),
		Name:     dbNode.Name,
		NodeType: NodeTypeBizGroup,
	}
	if dbNode.ParentID != nil {
		pid := strconv.FormatUint(uint64(*dbNode.ParentID), 10)
		node.ParentID = &pid
	}

	// 是否有子(子业务组 或 挂载通道)
	var subCount int64
	d.db.Scopes(d.scope).
		Model(&models.GbCatalogNode{}).
		Where("node_type = ? AND parent_id = ?", models.NodeTypeBizGroup, dbNode.ID).
		Count(&subCount)

	var mountCount int64
	d.db.Model(&models.GbChannelMount{}).Where("parent_node_id = ?", dbNode.ID).Count(&mountCount)

	node.HasChildren = subCount > 0 || mountCount > 0
	node.IsLeaf = !node.HasChildren

	if withCounts {
		node.MountCount = int(mountCount)
	}
	return node
}

// convertMountToNode 把 mount+channel join 结果转成 channel 类型 Node
func (d *BizGroupDimension) convertMountToNode(m mountWithChannel, parentNodeID uint) Node {
	// display name 优先取 mount.display_name,否则取 channel.name
	name := m.DisplayName
	if name == "" {
		name = m.ChannelName
	}

	pid := strconv.FormatUint(uint64(parentNodeID), 10)
	chIDStr := strconv.FormatUint(uint64(m.ChannelRowID), 10)

	node := Node{
		// ID 用 mount ID(允许同通道多挂载在不同组时区分)
		ID:          fmt.Sprintf("mount-%d", m.MountID),
		Name:        name,
		NodeType:    NodeTypeChannel,
		ParentID:    &pid,
		ChannelID:   &chIDStr,
		HasChildren: false,
		IsLeaf:      true,
	}

	// 复用国标通道编码
	if m.ChannelCode != "" {
		code := m.ChannelCode
		node.ChannelID = &code // 前端点播用国标编码
	}

	return node
}
