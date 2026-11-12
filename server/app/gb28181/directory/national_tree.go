package directory

import (
	"context"
	"fmt"
	"sort"

	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/gb28181/civilcode"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

func BuildNationalTree(ctx context.Context, db *gorm.DB, ownerDeptID uint, lookup CivilCodeLookup) ([]DirectoryNodeVO, error) {
	placements, err := ResolveNationalPlacements(ctx, db, ownerDeptID, lookup)
	if err != nil {
		return nil, err
	}
	if len(placements) == 0 {
		return []DirectoryNodeVO{}, nil
	}
	var devices []gbmodels.GbDevice
	if err := db.WithContext(ctx).Where("owner_dept_id = ?", ownerDeptID).Find(&devices).Error; err != nil {
		return nil, err
	}
	online := make(map[uint]bool, len(devices))
	deviceByID := make(map[uint]gbmodels.GbDevice, len(devices))
	for _, device := range devices {
		online[device.ID] = device.Status == gbmodels.DeviceStatusOnline
		deviceByID[device.ID] = device
	}

	type aggregate struct {
		projection CivilCodeProjection
		devices    map[uint]struct{}
		online     map[uint]struct{}
	}
	aggregates := map[string]*aggregate{}
	for deviceID, placement := range placements {
		if placement.ResolvedCode == "" {
			continue
		}
		for _, projection := range projectionChain(placement.ResolvedCode, lookup) {
			agg := aggregates[projection.Code]
			if agg == nil {
				agg = &aggregate{projection: projection, devices: map[uint]struct{}{}, online: map[uint]struct{}{}}
				aggregates[projection.Code] = agg
			}
			agg.devices[deviceID] = struct{}{}
			if online[deviceID] {
				agg.online[deviceID] = struct{}{}
			}
		}
	}

	children := map[string][]string{}
	for code, agg := range aggregates {
		children[agg.projection.ParentCode] = append(children[agg.projection.ParentCode], code)
	}
	var build func(string, int) []DirectoryNodeVO
	build = func(parent string, depth int) []DirectoryNodeVO {
		codes := children[parent]
		sort.Strings(codes)
		result := make([]DirectoryNodeVO, 0, len(codes))
		for _, code := range codes {
			agg := aggregates[code]
			result = append(result, DirectoryNodeVO{
				Key: "national:area:" + code, Name: agg.projection.Name, Type: "area", Code: code,
				ReadOnly: true, Count: len(agg.devices), OnlineCount: len(agg.online), Depth: depth,
				Children: build(code, depth+1),
			})
		}
		return result
	}
	tree := build("", 0)
	organizations, organizationDevices, err := buildCatalogOrganizations(ctx, db, ownerDeptID, placements, online, deviceByID)
	if err != nil {
		return nil, err
	}
	for areaCode, nodes := range organizations {
		attachOrganizations(tree, areaCode, nodes)
	}
	var unknownNodes []DirectoryNodeVO
	deviceIDs := make([]uint, 0, len(placements))
	for deviceID := range placements {
		deviceIDs = append(deviceIDs, deviceID)
	}
	sort.Slice(deviceIDs, func(i, j int) bool { return deviceIDs[i] < deviceIDs[j] })
	for _, deviceID := range deviceIDs {
		placement := placements[deviceID]
		if _, inOrganization := organizationDevices[deviceID]; inOrganization {
			continue
		}
		device, ok := deviceByID[deviceID]
		if !ok {
			continue
		}
		depth := placementDepth(tree, placement.ResolvedCode)
		leaf := deviceDirectoryNode(device, fmt.Sprintf("national:device:%d", deviceID), depth+1)
		if placement.ResolvedCode != "" && attachDeviceToArea(tree, placement.ResolvedCode, leaf) {
			continue
		}
		leaf.Depth = 1
		unknownNodes = append(unknownNodes, leaf)
	}
	if len(unknownNodes) > 0 {
		unknownOnlineCount := 0
		for _, node := range unknownNodes {
			if node.Device.Online {
				unknownOnlineCount++
			}
		}
		tree = append(tree, DirectoryNodeVO{
			Key: "national:unknown", Name: "未知行政区", Type: "unknown", ReadOnly: true,
			Count: len(unknownNodes), OnlineCount: unknownOnlineCount, Depth: 0, Children: unknownNodes,
		})
	}
	return tree, nil
}

type organizationAggregate struct {
	node     gbmodels.GbCatalogNode
	parentID uint
	areaCode string
	devices  map[uint]struct{}
	online   map[uint]struct{}
}

func buildCatalogOrganizations(ctx context.Context, db *gorm.DB, ownerDeptID uint, placements map[uint]NationalPlacementVO, online map[uint]bool, devices map[uint]gbmodels.GbDevice) (map[string][]DirectoryNodeVO, map[uint]struct{}, error) {
	var nodes []gbmodels.GbCatalogNode
	if err := db.WithContext(ctx).Where("owner_dept_id = ?", ownerDeptID).Order("sort_order, id").Find(&nodes).Error; err != nil {
		return nil, nil, err
	}
	byID := make(map[uint]gbmodels.GbCatalogNode, len(nodes))
	deviceNodes := map[uint][]gbmodels.GbCatalogNode{}
	deviceByCode := make(map[string]uint, len(devices))
	for deviceID, device := range devices {
		deviceByCode[device.DeviceID] = deviceID
	}
	for _, node := range nodes {
		byID[node.ID] = node
		if node.DeviceID != nil {
			deviceNodes[*node.DeviceID] = append(deviceNodes[*node.DeviceID], node)
		} else if node.NodeType == gbmodels.NodeTypeDevice {
			if deviceID, ok := deviceByCode[node.Code]; ok {
				deviceNodes[deviceID] = append(deviceNodes[deviceID], node)
			}
		}
	}
	aggregates := map[uint]*organizationAggregate{}
	directDevices := map[uint]map[uint]DirectoryNodeVO{}
	organizationDevices := map[uint]struct{}{}
	for deviceID, placement := range placements {
		for _, deviceNode := range deviceNodes[deviceID] {
			parentID := deviceNode.ParentID
			childOrgID := uint(0)
			seen := map[uint]struct{}{}
			for parentID != nil {
				if _, exists := seen[*parentID]; exists {
					break
				}
				seen[*parentID] = struct{}{}
				parent, ok := byID[*parentID]
				if !ok {
					break
				}
				if parent.NodeType == gbmodels.NodeTypeCivilCode {
					break
				}
				if parent.NodeType == gbmodels.NodeTypeBizGroup || parent.NodeType == gbmodels.NodeTypeVirtualOrg {
					agg := aggregates[parent.ID]
					if agg == nil {
						agg = &organizationAggregate{node: parent, devices: map[uint]struct{}{}, online: map[uint]struct{}{}, areaCode: placement.ResolvedCode}
						aggregates[parent.ID] = agg
					}
					if childOrgID != 0 {
						aggregates[childOrgID].parentID = parent.ID
					}
					agg.devices[deviceID] = struct{}{}
					if online[deviceID] {
						agg.online[deviceID] = struct{}{}
					}
					if childOrgID == 0 {
						if device, ok := devices[deviceID]; ok {
							if directDevices[parent.ID] == nil {
								directDevices[parent.ID] = map[uint]DirectoryNodeVO{}
							}
							directDevices[parent.ID][deviceID] = deviceDirectoryNode(device, directoryDeviceKey("national", parent.ID, deviceID), 0)
							organizationDevices[deviceID] = struct{}{}
						}
					}
					childOrgID = parent.ID
				}
				parentID = parent.ParentID
			}
			if childOrgID != 0 {
				break
			}
		}
	}
	children := map[uint][]uint{}
	for id, agg := range aggregates {
		children[agg.parentID] = append(children[agg.parentID], id)
	}
	for parentID := range children {
		sort.Slice(children[parentID], func(i, j int) bool {
			left, right := aggregates[children[parentID][i]].node, aggregates[children[parentID][j]].node
			if left.SortOrder != right.SortOrder {
				return left.SortOrder < right.SortOrder
			}
			if left.Name != right.Name {
				return left.Name < right.Name
			}
			return left.ID < right.ID
		})
	}
	result := map[string][]DirectoryNodeVO{}
	for _, id := range children[0] {
		agg := aggregates[id]
		result[agg.areaCode] = append(result[agg.areaCode], buildOneOrganization(id, aggregates, children, directDevices, 0))
	}
	return result, organizationDevices, nil
}

func buildOneOrganization(id uint, aggregates map[uint]*organizationAggregate, children map[uint][]uint, directDevices map[uint]map[uint]DirectoryNodeVO, depth int) DirectoryNodeVO {
	agg := aggregates[id]
	childNodes := make([]DirectoryNodeVO, 0, len(children[id])+len(directDevices[id]))
	for _, childID := range children[id] {
		childNodes = append(childNodes, buildOneOrganization(childID, aggregates, children, directDevices, depth+1))
	}
	deviceIDs := make([]uint, 0, len(directDevices[id]))
	for deviceID := range directDevices[id] {
		deviceIDs = append(deviceIDs, deviceID)
	}
	sort.Slice(deviceIDs, func(i, j int) bool { return deviceIDs[i] < deviceIDs[j] })
	for _, deviceID := range deviceIDs {
		deviceNode := directDevices[id][deviceID]
		deviceNode.Depth = depth + 1
		childNodes = append(childNodes, deviceNode)
	}
	return DirectoryNodeVO{Key: "national:catalog:" + fmt.Sprint(id), Name: agg.node.Name, Type: "organization", ReadOnly: true, Count: len(agg.devices), OnlineCount: len(agg.online), Depth: depth, Children: childNodes}
}

func placementDepth(nodes []DirectoryNodeVO, code string) int {
	for _, node := range nodes {
		if node.Type == "area" && node.Code == code {
			return node.Depth
		}
		if depth := placementDepth(node.Children, code); depth >= 0 {
			return depth
		}
	}
	return -1
}

func attachDeviceToArea(nodes []DirectoryNodeVO, code string, device DirectoryNodeVO) bool {
	for i := range nodes {
		if nodes[i].Type == "area" && nodes[i].Code == code {
			device.Depth = nodes[i].Depth + 1
			nodes[i].Children = append(nodes[i].Children, device)
			return true
		}
		if attachDeviceToArea(nodes[i].Children, code, device) {
			return true
		}
	}
	return false
}

func attachOrganizations(nodes []DirectoryNodeVO, areaCode string, organizations []DirectoryNodeVO) bool {
	for i := range nodes {
		if nodes[i].Code == areaCode {
			for j := range organizations {
				adjustDirectoryDepth(&organizations[j], nodes[i].Depth+1-organizations[j].Depth)
			}
			nodes[i].Children = append(nodes[i].Children, organizations...)
			return true
		}
		if attachOrganizations(nodes[i].Children, areaCode, organizations) {
			return true
		}
	}
	return false
}

func adjustDirectoryDepth(node *DirectoryNodeVO, offset int) {
	node.Depth += offset
	for i := range node.Children {
		adjustDirectoryDepth(&node.Children[i], offset)
	}
}

func projectionChain(code string, lookup CivilCodeLookup) []CivilCodeProjection {
	standard := code
	if len(code) == 8 {
		standard = code[:6]
	}
	entry := lookup.Lookup(standard)
	if entry == nil {
		return nil
	}
	var reversed []CivilCodeProjection
	for current := entry; current != nil; current = parentEntry(current, lookup) {
		name := current.ShortName
		if name == "" {
			name = current.Name
		}
		reversed = append(reversed, CivilCodeProjection{Code: current.Code, StandardCode: current.Code, Name: name, Level: current.Level, ParentCode: current.ParentCode})
	}
	chain := make([]CivilCodeProjection, len(reversed))
	for i := range reversed {
		chain[len(reversed)-1-i] = reversed[i]
	}
	if len(code) == 8 {
		chain = append(chain, CivilCodeProjection{Code: code, StandardCode: standard, Name: code, Level: civilcode.LevelCounty + 1, ParentCode: standard})
	}
	return chain
}

func parentEntry(entry *civilcode.SysCivilCode, lookup CivilCodeLookup) *civilcode.SysCivilCode {
	if entry.ParentCode == "" {
		return nil
	}
	return lookup.Lookup(entry.ParentCode)
}
