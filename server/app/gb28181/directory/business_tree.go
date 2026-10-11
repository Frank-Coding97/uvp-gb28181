package directory

import (
	"context"
	"fmt"
	"sort"

	"gorm.io/gorm"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// BuildAdministrativeTree 只展示行政区划层级。设备仍以 count/onlineCount 汇总，
// 选择节点时由 directory filter 反查设备，不把同一物理设备复制成多条记录。
func BuildAdministrativeTree(ctx context.Context, db *gorm.DB, ownerDeptID uint, lookup CivilCodeLookup) ([]DirectoryNodeVO, error) {
	tree, err := BuildNationalTree(ctx, db, ownerDeptID, lookup)
	if err != nil {
		return nil, err
	}
	var strip func([]DirectoryNodeVO) []DirectoryNodeVO
	strip = func(nodes []DirectoryNodeVO) []DirectoryNodeVO {
		out := make([]DirectoryNodeVO, 0, len(nodes))
		for _, node := range nodes {
			if node.Type != "area" && node.Type != "unknown" {
				continue
			}
			node.Key = administrativeKey(node.Key)
			node.Children = strip(node.Children)
			out = append(out, node)
		}
		return out
	}
	return strip(tree), nil
}

func administrativeKey(key string) string {
	if len(key) >= len("national:") && key[:len("national:")] == "national:" {
		return "administrative:" + key[len("national:"):]
	}
	return key
}

type businessAggregate struct {
	node    gbmodels.GbCatalogNode
	parent  uint
	devices map[uint]struct{}
	online  map[uint]struct{}
}

// BuildBusinessTree 按 Catalog 的 215 业务分组和 216 虚拟组织关系构造业务视图。
// 一个物理设备可以被多个业务组织引用，但列表查询始终按 device_id 去重。
//
// ⛔⛔ 统计的「叶」是**国标目录项里的资源类节点**(设备/摄像机/报警设备)，
// 不是只有 node_type='device' —— 国标附录 J 的图 J.2 示例里业务树最下面挂的是
// 132 段(网络摄像机)，本仓把它存成 channel。详见 BusinessLeafTypes。
//
// ⛔⛔ **视图只展示设备实际上报的业务分组树**，平台不替设备补「未归属业务组织」兜底
// (该节点不是国标概念，附录 J 里没有这一说)：设备没上报业务分组就不属于本视图，
// 要查这类设备请去「综合目录 / 设备管理」。2026-10-11 产品决策。
// 原实现「每个部门各生成一个同名兜底桶」在超管可见多部门时会平铺出 N 个同名节点
// (开发库 4 个部门 ⇒ 4 个「未归属业务组织」并列)，且把平台的组织结构混进了国标树。
func BuildBusinessTree(ctx context.Context, db *gorm.DB, ownerDeptID uint) ([]DirectoryNodeVO, error) {
	scope := func(q *gorm.DB) *gorm.DB { return q.Where("owner_dept_id = ?", ownerDeptID) }
	var nodes []gbmodels.GbCatalogNode
	if err := scope(db.WithContext(ctx)).Order("sort_order, id").Find(&nodes).Error; err != nil {
		return nil, err
	}
	resolver, err := LoadBusinessLeafResolver(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	online := resolver.online()
	byID := make(map[uint]gbmodels.GbCatalogNode, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	aggs := make(map[uint]*businessAggregate)
	for _, node := range nodes {
		if !IsBusinessContainerType(node.NodeType) {
			continue
		}
		aggs[node.ID] = &businessAggregate{node: node, devices: map[uint]struct{}{}, online: map[uint]struct{}{}}
	}
	// ⛔ 只统计**真正挂在**业务分组/虚拟组织下的叶；没归属的叶直接跳过，不生成任何兜底节点
	// (2026-10-11 产品决策，理由见函数头注释)。BusinessAncestorIDs 只会返回 215/216 祖先，
	// 挂在行政区划等其它父节点下的叶天然拿不到祖先 ⇒ 不会污染统计。
	for _, node := range nodes {
		if !IsBusinessLeafType(node.NodeType) {
			continue
		}
		// 叶节点自己不带物理设备主键是常态(通道/报警资源靠关联表反查)，统一交给 resolver。
		deviceID, ok := resolver.Resolve(node)
		if !ok {
			continue
		}
		for _, ancestorID := range BusinessAncestorIDs(byID, node.ParentID) {
			agg := aggs[ancestorID]
			if agg == nil {
				continue
			}
			agg.devices[deviceID] = struct{}{}
			if online[deviceID] {
				agg.online[deviceID] = struct{}{}
			}
		}
	}
	children := map[uint][]uint{}
	for id, agg := range aggs {
		parentID := uint(0)
		if agg.node.ParentID != nil {
			if parent, ok := aggs[*agg.node.ParentID]; ok {
				parentID = parent.node.ID
			}
		}
		agg.parent = parentID
		children[parentID] = append(children[parentID], id)
	}
	var build func(uint, int) []DirectoryNodeVO
	build = func(parentID uint, depth int) []DirectoryNodeVO {
		ids := append([]uint(nil), children[parentID]...)
		sort.Slice(ids, func(i, j int) bool {
			return aggs[ids[i]].node.Name < aggs[ids[j]].node.Name
		})
		out := make([]DirectoryNodeVO, 0, len(ids))
		for _, id := range ids {
			agg := aggs[id]
			out = append(out, DirectoryNodeVO{
				Key: "business:catalog:" + fmt.Sprint(id), Name: agg.node.Name,
				Type: string(agg.node.NodeType), Code: agg.node.Code, ReadOnly: true,
				Count: len(agg.devices), OnlineCount: len(agg.online), Depth: depth,
				Children: build(id, depth+1),
			})
		}
		return out
	}
	return build(0, 0), nil
}
