package directory

import (
	"context"
	"fmt"
	"sort"

	"gorm.io/gorm"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// BuildAdministrativeTree 展示行政区划层级和设备叶节点。
func BuildAdministrativeTree(ctx context.Context, db *gorm.DB, ownerDeptID uint, lookup CivilCodeLookup) ([]DirectoryNodeVO, error) {
	tree, err := BuildNationalTree(ctx, db, ownerDeptID, lookup)
	if err != nil {
		return nil, err
	}
	var strip func([]DirectoryNodeVO) []DirectoryNodeVO
	strip = func(nodes []DirectoryNodeVO) []DirectoryNodeVO {
		out := make([]DirectoryNodeVO, 0, len(nodes))
		for _, node := range nodes {
			if node.Type == "unknown" {
				node.Children = uniqueDeviceNodes(collectDeviceNodes(node.Children))
				out = append(out, node)
				continue
			}
			if node.Type != "area" {
				continue
			}
			var areaChildren []DirectoryNodeVO
			var deviceNodes []DirectoryNodeVO
			for _, child := range node.Children {
				if child.Type == "area" {
					areaChildren = append(areaChildren, strip([]DirectoryNodeVO{child})...)
				} else {
					deviceNodes = append(deviceNodes, collectDeviceNodes([]DirectoryNodeVO{child})...)
				}
			}
			node.Key = administrativeKey(node.Key)
			node.Children = append(areaChildren, uniqueDeviceNodes(deviceNodes)...)
			for i := range node.Children {
				if node.Children[i].Type == "device" {
					node.Children[i].Depth = node.Depth + 1
				}
			}
			out = append(out, node)
		}
		return out
	}
	return strip(tree), nil
}

func collectDeviceNodes(nodes []DirectoryNodeVO) []DirectoryNodeVO {
	var out []DirectoryNodeVO
	for _, node := range nodes {
		if node.Type == "device" {
			out = append(out, node)
			continue
		}
		out = append(out, collectDeviceNodes(node.Children)...)
	}
	return out
}

func uniqueDeviceNodes(nodes []DirectoryNodeVO) []DirectoryNodeVO {
	seen := map[uint]struct{}{}
	out := make([]DirectoryNodeVO, 0, len(nodes))
	for _, node := range nodes {
		if node.Device == nil {
			continue
		}
		if _, exists := seen[node.Device.ID]; exists {
			continue
		}
		seen[node.Device.ID] = struct{}{}
		out = append(out, node)
	}
	return out
}

func administrativeKey(key string) string {
	if len(key) >= len("national:") && key[:len("national:")] == "national:" {
		return "administrative:" + key[len("national:"):]
	}
	return key
}

type businessAggregate struct {
	node     gbmodels.GbCatalogNode
	channels map[uint]struct{}
	online   map[uint]struct{}
}

// BuildBusinessTree 按 Catalog 的 215 业务分组、216 虚拟组织和其下视频通道构造业务视图。
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
	byID := make(map[uint]gbmodels.GbCatalogNode, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	var channels []gbmodels.GbChannel
	if err := scope(db.WithContext(ctx).Model(&gbmodels.GbChannel{})).Find(&channels).Error; err != nil {
		return nil, err
	}
	channelByID := make(map[uint]gbmodels.GbChannel, len(channels))
	for _, channel := range channels {
		channelByID[channel.ID] = channel
	}
	var devices []gbmodels.GbDevice
	if err := scope(db.WithContext(ctx).Model(&gbmodels.GbDevice{})).Find(&devices).Error; err != nil {
		return nil, err
	}
	deviceNameByCode := make(map[string]string, len(devices))
	for _, device := range devices {
		name := device.Alias
		if name == "" {
			name = device.Name
		}
		if name != "" {
			deviceNameByCode[device.DeviceID] = name
		}
	}
	aggs := make(map[uint]*businessAggregate)
	directChannels := make(map[uint]map[uint]DirectoryNodeVO)
	for _, node := range nodes {
		if !IsBusinessContainerType(node.NodeType) {
			continue
		}
		// 216 是业务分组内的虚拟目录，孤立的 216（包括异常兜底节点）不应
		// 被提升为业务树根节点。
		if node.NodeType == gbmodels.NodeTypeVirtualOrg && !hasBusinessGroupAncestor(byID, node.ParentID) {
			continue
		}
		aggs[node.ID] = &businessAggregate{node: node, channels: map[uint]struct{}{}, online: map[uint]struct{}{}}
	}
	// 业务视图以收到的 215/216 容器和其下的通道目录项为准。通道反查出的物理设备
	// 只用于其它设备视图；不能在这里变成叶节点，否则选中 NVR 会把它的所有通道带进来。
	for _, node := range nodes {
		if node.NodeType != gbmodels.NodeTypeChannel {
			continue
		}
		// Upsert 会回填 channel_id。没有关联 ID 的旧目录行不能通过相同编码
		// 反向捞出一条通道记录，避免陈旧目录扩大业务视图。
		if node.ChannelID == nil {
			continue
		}
		channel, ok := channelByID[*node.ChannelID]
		if !ok {
			continue
		}
		ancestorIDs := BusinessAncestorIDs(byID, node.ParentID)
		if !hasBusinessGroupID(byID, ancestorIDs) {
			continue
		}
		for index, ancestorID := range ancestorIDs {
			agg := aggs[ancestorID]
			if agg == nil {
				continue
			}
			agg.channels[channel.ID] = struct{}{}
			if channel.Status == gbmodels.ChannelStatusOnline {
				agg.online[channel.ID] = struct{}{}
			}
			if index == 0 {
				if directChannels[ancestorID] == nil {
					directChannels[ancestorID] = map[uint]DirectoryNodeVO{}
				}
				directChannels[ancestorID][channel.ID] = DirectoryNodeVO{
					Key: "business:catalog:" + fmt.Sprint(node.ID), Name: node.Name,
					Type: string(gbmodels.NodeTypeChannel), Code: node.Code, ReadOnly: true,
					Count: 1, OnlineCount: boolCount(channel.Status == gbmodels.ChannelStatusOnline),
				}
			}
		}
	}
	children := map[uint][]uint{}
	for id, agg := range aggs {
		parentID := businessParentID(byID, agg.node.ParentID)
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
			childNodes := build(id, depth+1)
			channelIDs := make([]uint, 0, len(directChannels[id]))
			for channelID := range directChannels[id] {
				channelIDs = append(channelIDs, channelID)
			}
			sort.Slice(channelIDs, func(i, j int) bool { return channelIDs[i] < channelIDs[j] })
			for _, channelID := range channelIDs {
				channelNode := directChannels[id][channelID]
				channelNode.Depth = depth + 1
				childNodes = append(childNodes, channelNode)
			}
			meta := map[string]string{}
			if sourceName := deviceNameByCode[agg.node.SourceDeviceID]; sourceName != "" {
				meta["sourceName"] = sourceName
			}
			out = append(out, DirectoryNodeVO{
				Key: "business:catalog:" + fmt.Sprint(id), Name: agg.node.Name,
				Type: string(agg.node.NodeType), Code: agg.node.Code, ReadOnly: true,
				Count: len(agg.channels), OnlineCount: len(agg.online), Depth: depth,
				Meta:     meta,
				Children: childNodes,
			})
		}
		return out
	}
	return build(0, 0), nil
}

func businessParentID(nodes map[uint]gbmodels.GbCatalogNode, parentID *uint) uint {
	seen := map[uint]struct{}{}
	for parentID != nil {
		if _, ok := seen[*parentID]; ok {
			return 0
		}
		seen[*parentID] = struct{}{}
		parent, ok := nodes[*parentID]
		if !ok {
			return 0
		}
		if IsBusinessContainerType(parent.NodeType) {
			return parent.ID
		}
		parentID = parent.ParentID
	}
	return 0
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}

func hasBusinessGroupAncestor(byID map[uint]gbmodels.GbCatalogNode, parentID *uint) bool {
	return hasBusinessGroupID(byID, BusinessAncestorIDs(byID, parentID))
}

func hasBusinessGroupID(byID map[uint]gbmodels.GbCatalogNode, ids []uint) bool {
	for _, id := range ids {
		if node, ok := byID[id]; ok && node.NodeType == gbmodels.NodeTypeBizGroup {
			return true
		}
	}
	return false
}
