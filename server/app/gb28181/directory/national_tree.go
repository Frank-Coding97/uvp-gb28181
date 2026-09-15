package directory

import (
	"context"
	"fmt"
	"sort"

	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/civilcode"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
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
	for _, device := range devices {
		online[device.ID] = device.Status == gbmodels.DeviceStatusOnline
	}

	type aggregate struct {
		projection CivilCodeProjection
		devices    map[uint]struct{}
		online     map[uint]struct{}
	}
	aggregates := map[string]*aggregate{}
	unknownDevices := map[uint]struct{}{}
	unknownOnline := map[uint]struct{}{}
	for deviceID, placement := range placements {
		if placement.ResolvedCode == "" {
			unknownDevices[deviceID] = struct{}{}
			if online[deviceID] {
				unknownOnline[deviceID] = struct{}{}
			}
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
	organizations, err := buildCatalogOrganizations(ctx, db, ownerDeptID, placements, online)
	if err != nil {
		return nil, err
	}
	for areaCode, nodes := range organizations {
		attachOrganizations(tree, areaCode, nodes)
	}
	if len(unknownDevices) > 0 {
		tree = append(tree, DirectoryNodeVO{
			Key: "national:unknown", Name: "未知行政区", Type: "unknown", ReadOnly: true,
			Count: len(unknownDevices), OnlineCount: len(unknownOnline), Depth: 0,
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

func buildCatalogOrganizations(ctx context.Context, db *gorm.DB, ownerDeptID uint, placements map[uint]NationalPlacementVO, online map[uint]bool) (map[string][]DirectoryNodeVO, error) {
	var nodes []gbmodels.GbCatalogNode
	if err := db.WithContext(ctx).Where("owner_dept_id = ?", ownerDeptID).Order("sort_order, id").Find(&nodes).Error; err != nil {
		return nil, err
	}
	byID := make(map[uint]gbmodels.GbCatalogNode, len(nodes))
	deviceNodes := map[uint][]gbmodels.GbCatalogNode{}
	for _, node := range nodes {
		byID[node.ID] = node
		if node.DeviceID != nil {
			deviceNodes[*node.DeviceID] = append(deviceNodes[*node.DeviceID], node)
		}
	}
	aggregates := map[uint]*organizationAggregate{}
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
					childOrgID = parent.ID
				}
				parentID = parent.ParentID
			}
		}
	}
	children := map[uint][]uint{}
	for id, agg := range aggregates {
		children[agg.parentID] = append(children[agg.parentID], id)
	}
	result := map[string][]DirectoryNodeVO{}
	for _, id := range children[0] {
		agg := aggregates[id]
		result[agg.areaCode] = append(result[agg.areaCode], buildOneOrganization(id, aggregates, children, 0))
	}
	return result, nil
}

func buildOneOrganization(id uint, aggregates map[uint]*organizationAggregate, children map[uint][]uint, depth int) DirectoryNodeVO {
	agg := aggregates[id]
	childNodes := make([]DirectoryNodeVO, 0, len(children[id]))
	for _, childID := range children[id] {
		childNodes = append(childNodes, buildOneOrganization(childID, aggregates, children, depth+1))
	}
	return DirectoryNodeVO{Key: "national:catalog:" + fmt.Sprint(id), Name: agg.node.Name, Type: "organization", ReadOnly: true, Count: len(agg.devices), OnlineCount: len(agg.online), Depth: depth, Children: childNodes}
}

func attachOrganizations(nodes []DirectoryNodeVO, areaCode string, organizations []DirectoryNodeVO) bool {
	for i := range nodes {
		if nodes[i].Code == areaCode {
			for j := range organizations {
				organizations[j].Depth = nodes[i].Depth + 1
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
