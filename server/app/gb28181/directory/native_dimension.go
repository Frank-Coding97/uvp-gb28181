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

// ScopeFunc GORM Scope 函数类型(用于 dept 数据隔离)
type ScopeFunc func(*gorm.DB) *gorm.DB

// NativeDimension 国标自动注册维度(原始目录树)
//
// 直接复用 gb_catalog_node 表,按 parent_id 层级关系展示。
// 这是最原始的设备/通道组织形式,保留国标注册时的层级结构。
//
// 使用场景:
//   - 查看设备原始上报的目录结构
//   - 排查国标注册问题时对照原始层级
//   - 作为其他维度(业务分组/行政区划)的数据源
//
// 数据隔离:通过 scope 函数注入(项目统一用 datascope.OwnerDeptScope)
type NativeDimension struct {
	db    *gorm.DB
	scope ScopeFunc // 数据隔离 scope(admin=全量,普通用户=按 dept 过滤)
}

// NewNativeDimension 构造函数
// scope 可以为 nil(测试时用 pass-through)
func NewNativeDimension(db *gorm.DB, scope ScopeFunc) *NativeDimension {
	if scope == nil {
		scope = func(db *gorm.DB) *gorm.DB { return db }
	}
	return &NativeDimension{
		db:    db,
		scope: scope,
	}
}

// GetRoots 获取顶层节点(parent_id IS NULL)
func (d *NativeDimension) GetRoots(withCounts bool) ([]Node, error) {
	var dbNodes []models.GbCatalogNode
	query := d.db.Scopes(d.scope).
		Where("parent_id IS NULL").
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
	query := d.db.Scopes(d.scope).
		Where("parent_id = ?", uint(parentIDUint)).
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

// beautifyCivilCodeName 对 civil_code 类型节点,用行政区划字典把编码解析成简称。
// 例:"行政区 34" → "安徽省","行政区 3402" → "芜湖市"。
// 入库 pipeline 建行政区节点时 name 只拼了 "行政区 " + 码(没查字典),这里补上。
//
// 字典服务未装配 / 编码查不到 / 编码非法 → 保留原始 name(不破坏溯源与兜底)。
func beautifyCivilCodeName(code, rawName string) string {
	if civilCodeSvcProvider == nil {
		return rawName
	}
	svc := civilCodeSvcProvider()
	if svc == nil || code == "" {
		return rawName
	}
	// 补零到 6 位:2 位省 +0000 / 4 位市 +00 / 6 位区县原样
	code6 := code
	for len(code6) < 6 {
		code6 += "0"
	}
	if len(code6) != 6 {
		return rawName
	}
	item := svc.Lookup(code6)
	if item == nil {
		return rawName
	}
	if item.ShortName != "" {
		return item.ShortName
	}
	if item.Name != "" {
		return item.Name
	}
	return rawName
}

// convertToNode 将 GbCatalogNode 转换为统一的 directory.Node
func (d *NativeDimension) convertToNode(dbNode models.GbCatalogNode, withCounts bool) Node {
	name := dbNode.Name
	if dbNode.NodeType == models.NodeTypeCivilCode {
		name = beautifyCivilCodeName(dbNode.CivilCode, dbNode.Name)
		// 与父行政区节点同名(如芜湖市 3402 与其市辖区 340200)→ 追加"(市辖区)"消歧
		if dbNode.ParentID != nil {
			var parent models.GbCatalogNode
			if d.db.Scopes(d.scope).
				Select("civil_code, node_type").
				Where("id = ?", *dbNode.ParentID).
				Limit(1).Find(&parent).RowsAffected > 0 &&
				parent.NodeType == models.NodeTypeCivilCode {
				parentName := beautifyCivilCodeName(parent.CivilCode, "")
				if parentName != "" && parentName == name {
					name = name + "(市辖区)"
				}
			}
		}
	}
	node := Node{
		ID:       strconv.FormatUint(uint64(dbNode.ID), 10),
		Name:     name,
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
		d.db.Scopes(d.scope).
			Model(&models.GbCatalogNode{}).
			Where("parent_id = ?", dbNode.ID).
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
