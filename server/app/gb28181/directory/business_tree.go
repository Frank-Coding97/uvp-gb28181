package directory

import (
	"context"
	"fmt"
	"sort"

	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
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
func BuildBusinessTree(ctx context.Context, db *gorm.DB, ownerDeptID uint) ([]DirectoryNodeVO, error) {
	var nodes []gbmodels.GbCatalogNode
	if err := db.WithContext(ctx).Where("owner_dept_id = ?", ownerDeptID).Order("sort_order, id").Find(&nodes).Error; err != nil {
		return nil, err
	}
	var devices []gbmodels.GbDevice
	if err := db.WithContext(ctx).Where("owner_dept_id = ?", ownerDeptID).Find(&devices).Error; err != nil {
		return nil, err
	}
	online := make(map[uint]bool, len(devices))
	for _, device := range devices {
		online[device.ID] = device.Status == gbmodels.DeviceStatusOnline
	}
	byID := make(map[uint]gbmodels.GbCatalogNode, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	aggs := make(map[uint]*businessAggregate)
	for _, node := range nodes {
		if node.NodeType != gbmodels.NodeTypeBizGroup && node.NodeType != gbmodels.NodeTypeVirtualOrg {
			continue
		}
		aggs[node.ID] = &businessAggregate{node: node, devices: map[uint]struct{}{}, online: map[uint]struct{}{}}
	}
	unknown := map[uint]struct{}{}
	unknownOnline := map[uint]struct{}{}
	for _, node := range nodes {
		if node.NodeType != gbmodels.NodeTypeDevice || node.DeviceID == nil {
			continue
		}
		deviceID := *node.DeviceID
		parentID := node.ParentID
		attached := false
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
			if agg := aggs[parent.ID]; agg != nil {
				agg.devices[deviceID] = struct{}{}
				if online[deviceID] {
					agg.online[deviceID] = struct{}{}
				}
				attached = true
			}
			parentID = parent.ParentID
		}
		if !attached {
			unknown[deviceID] = struct{}{}
			if online[deviceID] {
				unknownOnline[deviceID] = struct{}{}
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
	tree := build(0, 0)
	if len(unknown) > 0 {
		tree = append(tree, DirectoryNodeVO{
			Key: fmt.Sprintf("business:unknown:%d", ownerDeptID), Name: "未归属业务组织", Type: "unknown",
			ReadOnly: true, Count: len(unknown), OnlineCount: len(unknownOnline), Depth: 0,
		})
	}
	return tree, nil
}
