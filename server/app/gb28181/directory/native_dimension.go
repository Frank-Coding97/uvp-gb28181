package directory

import (
	"fmt"
	"strconv"

	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

const (
	// defaultOrderBy 默认排序规则
	defaultOrderBy = "sort_order ASC, name ASC"
)

// NativeDimension 国标自动注册维度(原始目录树)
//
// 直接复用 gb_catalog_node 表,按 parent_id 层级关系展示。
// 这是最原始的设备/通道组织形式,保留国标注册时的层级结构。
//
// 使用场景:
//   - 查看设备原始上报的目录结构
//   - 排查国标注册问题时对照原始层级
//   - 作为其他维度(业务分组/行政区划)的数据源
type NativeDimension struct {
	db          *gorm.DB
	ownerDeptID uint
}

// NewNativeDimension 构造函数
func NewNativeDimension(db *gorm.DB, ownerDeptID uint) *NativeDimension {
	return &NativeDimension{
		db:          db,
		ownerDeptID: ownerDeptID,
	}
}

// GetRoots 获取顶层节点(parent_id IS NULL)
func (d *NativeDimension) GetRoots(withCounts bool) ([]Node, error) {
	var dbNodes []models.GbCatalogNode
	query := d.db.Where("owner_dept_id = ? AND parent_id IS NULL", d.ownerDeptID).
		Order(defaultOrderBy)

	if err := query.Find(&dbNodes).Error; err != nil {
		return nil, fmt.Errorf("query roots failed: %w", err)
	}

	nodes := make([]Node, 0, len(dbNodes))
	for _, dbNode := range dbNodes {
		node := d.convertToNode(dbNode, withCounts)
		nodes = append(nodes, node)
	}

	return nodes, nil
}

// GetChildren 获取指定父节点的子节点
func (d *NativeDimension) GetChildren(parentID string, withCounts bool) ([]Node, error) {
	parentIDUint, err := strconv.ParseUint(parentID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid parentID: %w", err)
	}

	var dbNodes []models.GbCatalogNode
	query := d.db.Where("owner_dept_id = ? AND parent_id = ?", d.ownerDeptID, uint(parentIDUint)).
		Order(defaultOrderBy)

	if err := query.Find(&dbNodes).Error; err != nil {
		return nil, fmt.Errorf("query children failed: %w", err)
	}

	nodes := make([]Node, 0, len(dbNodes))
	for _, dbNode := range dbNodes {
		node := d.convertToNode(dbNode, withCounts)
		nodes = append(nodes, node)
	}

	return nodes, nil
}

// convertToNode 将 GbCatalogNode 转换为统一的 directory.Node
func (d *NativeDimension) convertToNode(dbNode models.GbCatalogNode, withCounts bool) Node {
	node := Node{
		ID:       strconv.FormatUint(uint64(dbNode.ID), 10),
		Name:     dbNode.Name,
		NodeType: string(dbNode.NodeType),
	}

	// ParentID
	if dbNode.ParentID != nil {
		parentIDStr := strconv.FormatUint(uint64(*dbNode.ParentID), 10)
		node.ParentID = &parentIDStr
	}

	// ChannelID
	if dbNode.ChannelID != nil {
		channelIDStr := strconv.FormatUint(uint64(*dbNode.ChannelID), 10)
		node.ChannelID = &channelIDStr
	}

	// DeviceID
	if dbNode.DeviceID != nil {
		deviceIDStr := strconv.FormatUint(uint64(*dbNode.DeviceID), 10)
		node.DeviceID = &deviceIDStr
	}

	// CivilCode
	node.CivilCode = dbNode.CivilCode

	// HasChildren/IsLeaf
	// 通道节点一定是叶子,其他节点查询是否有子节点
	if dbNode.NodeType == models.NodeTypeChannel {
		node.HasChildren = false
		node.IsLeaf = true
	} else {
		var childCount int64
		d.db.Model(&models.GbCatalogNode{}).
			Where("owner_dept_id = ? AND parent_id = ?", d.ownerDeptID, dbNode.ID).
			Count(&childCount)
		node.HasChildren = childCount > 0
		node.IsLeaf = childCount == 0
	}

	// withCounts: 暂时返回 0,后续 Phase 2 优化
	if withCounts {
		node.MountCount = 0
		node.ChannelCount = 0
	}

	return node
}
