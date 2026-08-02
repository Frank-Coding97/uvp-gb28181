package directory

import (
	"context"
	"sort"
	"strings"

	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

type PlacementSource string

const (
	PlacementCatalog  PlacementSource = "catalog"
	PlacementLegacy   PlacementSource = "legacy"
	PlacementDeviceID PlacementSource = "device_id"
	PlacementUnknown  PlacementSource = "unknown"
)

type NationalPlacementVO struct {
	DeviceID     uint            `json:"deviceId"`
	Key          string          `json:"key"`
	RawCode      string          `json:"rawCode"`
	ResolvedCode string          `json:"resolvedCode"`
	Name         string          `json:"name"`
	Source       PlacementSource `json:"source"`
	Reason       UnknownReason   `json:"reason,omitempty"`
}

func ResolveNationalPlacements(ctx context.Context, db *gorm.DB, ownerDeptID uint, lookup CivilCodeLookup) (map[uint]NationalPlacementVO, error) {
	var devices []gbmodels.GbDevice
	if err := db.WithContext(ctx).Where("owner_dept_id = ?", ownerDeptID).Order("id").Find(&devices).Error; err != nil {
		return nil, err
	}
	var nodes []gbmodels.GbCatalogNode
	if err := db.WithContext(ctx).Where("owner_dept_id = ?", ownerDeptID).Order("id").Find(&nodes).Error; err != nil {
		return nil, err
	}

	byID := make(map[uint]gbmodels.GbCatalogNode, len(nodes))
	deviceNodes := make(map[uint][]gbmodels.GbCatalogNode)
	for _, node := range nodes {
		byID[node.ID] = node
		if node.DeviceID != nil {
			deviceNodes[*node.DeviceID] = append(deviceNodes[*node.DeviceID], node)
		}
	}
	placements := make(map[uint]NationalPlacementVO, len(devices))
	for _, device := range devices {
		placement := NationalPlacementVO{DeviceID: device.ID, Key: "national:unknown", Source: PlacementUnknown}
		candidates := deviceNodes[device.ID]
		sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
		for _, node := range candidates {
			code, source := nearestCivilCode(node, byID)
			if code == "" {
				continue
			}
			placement.RawCode = code
			projected := ProjectCivilCode(code, "", lookup)
			if projected.Reason == "" {
				placement.Key = projected.Key
				placement.ResolvedCode = projected.Code
				placement.Name = projected.Name
				placement.Source = source
				placement.Reason = ""
				break
			}
			placement.Reason = projected.Reason
		}
		if placement.ResolvedCode == "" {
			code := ""
			if len(device.DeviceID) >= 6 {
				code = device.DeviceID[:6]
			}
			projected := ProjectCivilCode(code, "", lookup)
			if projected.Reason == "" {
				placement.Key = projected.Key
				placement.ResolvedCode = projected.Code
				placement.Name = projected.Name
				placement.Source = PlacementDeviceID
				placement.Reason = ""
			} else {
				placement.Reason = projected.Reason
				if placement.RawCode == "" {
					placement.RawCode = code
				}
			}
		}
		placements[device.ID] = placement
	}
	return placements, nil
}

func nearestCivilCode(start gbmodels.GbCatalogNode, byID map[uint]gbmodels.GbCatalogNode) (string, PlacementSource) {
	seen := map[uint]struct{}{}
	node := start
	for {
		if _, exists := seen[node.ID]; exists {
			return "", PlacementUnknown
		}
		seen[node.ID] = struct{}{}
		if strings.TrimSpace(node.CivilCode) != "" {
			source := PlacementCatalog
			if strings.TrimSpace(string(node.Source)) == "" {
				source = PlacementLegacy
			}
			return strings.TrimSpace(node.CivilCode), source
		}
		if node.ParentID == nil {
			return "", PlacementUnknown
		}
		parent, ok := byID[*node.ParentID]
		if !ok {
			return "", PlacementUnknown
		}
		node = parent
	}
}
