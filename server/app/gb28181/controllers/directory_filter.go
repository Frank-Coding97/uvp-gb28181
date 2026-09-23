package controllers

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/civilcode"
	gbdirectory "uvplatform.cn/uvp-gb28181/app/gb28181/directory"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

type directoryDeviceScope struct {
	IDs   []uint
	Codes []string
}

func resolveDirectoryDeviceScope(c *gin.Context, db *gorm.DB) (*directoryDeviceScope, error) {
	filter, err := gbdirectory.ParseFilter(c.Query("directoryView"), c.Query("directoryKey"), "")
	if err != nil || filter == nil {
		return nil, err
	}
	var ids []uint
	switch filter.Kind {
	case gbdirectory.FilterCustomGroup:
		var group gbmodels.GbCustomGroup
		result := db.WithContext(c.Request.Context()).Scopes(ownerDeptScope(c)).Where("id = ?", filter.GroupID).Limit(1).Find(&group)
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 0 {
			return nil, gbdirectory.ErrGroupNotFound
		}
		var groupIDs []uint
		if err := db.WithContext(c.Request.Context()).Model(&gbmodels.GbCustomGroup{}).Scopes(ownerDeptScope(c)).Where("path LIKE ?", group.Path+"%").Pluck("id", &groupIDs).Error; err != nil {
			return nil, err
		}
		if err := db.WithContext(c.Request.Context()).Model(&gbmodels.GbCustomGroupDevice{}).Distinct("device_id").Where("group_id IN ?", groupIDs).Pluck("device_id", &ids).Error; err != nil {
			return nil, err
		}
	case gbdirectory.FilterCustomUngrouped:
		query := db.WithContext(c.Request.Context()).Model(&gbmodels.GbDevice{}).Scopes(ownerDeptScope(c))
		if filter.OwnerDeptScoped {
			query = query.Where("owner_dept_id = ?", filter.OwnerDeptID)
		}
		if err := query.Where("NOT EXISTS (?)", db.Model(&gbmodels.GbCustomGroupDevice{}).Select("1").Where("gb_custom_group_device.device_id = gb_device.id")).Pluck("id", &ids).Error; err != nil {
			return nil, err
		}
	case gbdirectory.FilterNationalCatalog:
		var root gbmodels.GbCatalogNode
		result := db.WithContext(c.Request.Context()).Scopes(ownerDeptScope(c)).Where("id = ?", filter.NodeID).Limit(1).Find(&root)
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 0 {
			return nil, fmt.Errorf("catalog node not found")
		}
		if err := db.WithContext(c.Request.Context()).Model(&gbmodels.GbCatalogNode{}).Scopes(ownerDeptScope(c)).Distinct("device_id").Where("path LIKE ? AND device_id IS NOT NULL", root.Path+"%").Pluck("device_id", &ids).Error; err != nil {
			return nil, err
		}
	case gbdirectory.FilterBusinessCatalog:
		var root gbmodels.GbCatalogNode
		result := db.WithContext(c.Request.Context()).Scopes(ownerDeptScope(c)).Where("id = ? AND node_type IN ?", filter.NodeID, []gbmodels.NodeType{gbmodels.NodeTypeBizGroup, gbmodels.NodeTypeVirtualOrg}).Limit(1).Find(&root)
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 0 {
			return nil, fmt.Errorf("business catalog node not found")
		}
		if err := db.WithContext(c.Request.Context()).Model(&gbmodels.GbCatalogNode{}).Scopes(ownerDeptScope(c)).Distinct("device_id").Where("path LIKE ? AND node_type = ? AND device_id IS NOT NULL", root.Path+"%", gbmodels.NodeTypeDevice).Pluck("device_id", &ids).Error; err != nil {
			return nil, err
		}
	case gbdirectory.FilterBusinessDevice:
		var device gbmodels.GbDevice
		result := db.WithContext(c.Request.Context()).Scopes(ownerDeptScope(c)).Where("id = ?", filter.NodeID).Limit(1).Find(&device)
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 1 {
			ids = []uint{device.ID}
		}
	case gbdirectory.FilterBusinessUnknown:
		query := db.WithContext(c.Request.Context()).Model(&gbmodels.GbDevice{}).Scopes(ownerDeptScope(c))
		if filter.OwnerDeptScoped {
			query = query.Where("owner_dept_id = ?", filter.OwnerDeptID)
		}
		var devices []gbmodels.GbDevice
		if err := query.Find(&devices).Error; err != nil {
			return nil, err
		}
		var catalogNodes []gbmodels.GbCatalogNode
		if err := db.WithContext(c.Request.Context()).Model(&gbmodels.GbCatalogNode{}).Scopes(ownerDeptScope(c)).Where("node_type = ? AND device_id IS NOT NULL", gbmodels.NodeTypeDevice).Find(&catalogNodes).Error; err != nil {
			return nil, err
		}
		attached := make(map[uint]struct{}, len(catalogNodes))
		byID := make(map[uint]gbmodels.GbCatalogNode, len(catalogNodes))
		for _, node := range catalogNodes {
			byID[node.ID] = node
		}
		// 业务节点可能不在同一批查询中，补齐父链。
		var allNodes []gbmodels.GbCatalogNode
		if err := db.WithContext(c.Request.Context()).Model(&gbmodels.GbCatalogNode{}).Scopes(ownerDeptScope(c)).Find(&allNodes).Error; err != nil {
			return nil, err
		}
		for _, node := range allNodes {
			byID[node.ID] = node
		}
		for _, node := range catalogNodes {
			parentID := node.ParentID
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
				if parent.NodeType == gbmodels.NodeTypeBizGroup || parent.NodeType == gbmodels.NodeTypeVirtualOrg {
					attached[*node.DeviceID] = struct{}{}
					break
				}
				parentID = parent.ParentID
			}
		}
		for _, device := range devices {
			if _, ok := attached[device.ID]; !ok {
				ids = append(ids, device.ID)
			}
		}
	default:
		deptIDs, err := visibleDirectoryDeptIDs(c, db)
		if err != nil {
			return nil, err
		}
		lookup := civilcode.NewService(db)
		if err := lookup.WarmCache(c.Request.Context()); err != nil {
			return nil, err
		}
		for _, deptID := range deptIDs {
			placements, err := gbdirectory.ResolveNationalPlacements(c.Request.Context(), db, deptID, lookup)
			if err != nil {
				return nil, err
			}
			for id, placement := range placements {
				if filter.Kind == gbdirectory.FilterNationalUnknown && placement.ResolvedCode == "" || filter.Kind == gbdirectory.FilterNationalArea && areaContains(filter.Code, placement.ResolvedCode) {
					ids = append(ids, id)
				}
			}
		}
	}
	var codes []string
	if len(ids) > 0 {
		if err := db.WithContext(c.Request.Context()).Model(&gbmodels.GbDevice{}).Scopes(ownerDeptScope(c)).Where("id IN ?", ids).Pluck("device_id", &codes).Error; err != nil {
			return nil, err
		}
	}
	return &directoryDeviceScope{IDs: ids, Codes: codes}, nil
}

func areaContains(area, resolved string) bool {
	if len(area) == 8 {
		return area == resolved
	}
	if len(area) != 6 || len(resolved) < 6 {
		return false
	}
	switch {
	case strings.HasSuffix(area, "0000"):
		return area[:2] == resolved[:2]
	case strings.HasSuffix(area, "00"):
		return area[:4] == resolved[:4]
	default:
		return area == resolved[:6]
	}
}
