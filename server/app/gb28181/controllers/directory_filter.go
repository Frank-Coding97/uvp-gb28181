package controllers

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/gb28181/civilcode"
	gbdirectory "uvplatform.com/uvp-gb28181/app/gb28181/directory"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	basemodels "uvplatform.com/uvp-gb28181/app/models"
)

type directoryDeviceScope struct {
	IDs        []uint
	Codes      []string
	ChannelIDs []uint
}

func resolveDirectoryDeviceScope(c *gin.Context, db *gorm.DB) (*directoryDeviceScope, error) {
	filter, err := gbdirectory.ParseFilter(c.Query("directoryView"), c.Query("directoryKey"), "")
	if err != nil || filter == nil {
		return nil, err
	}
	var ids []uint
	switch filter.Kind {
	case gbdirectory.FilterDepartment:
		departments, _, err := visibleDirectoryDepartments(c, db)
		if err != nil {
			return nil, err
		}
		deptIDs, exists := gbdirectory.CollectDepartmentSubtreeIDs(departments, filter.DepartmentID)
		if !exists {
			return nil, gbdirectory.ErrDepartmentNotFound
		}
		if err := db.WithContext(c.Request.Context()).Model(&gbmodels.GbDevice{}).
			Scopes(ownerDeptScope(c)).Where("owner_dept_id IN ?", deptIDs).Pluck("id", &ids).Error; err != nil {
			return nil, err
		}
	case gbdirectory.FilterDepartmentEmpty:
		departmentExists := db.WithContext(c.Request.Context()).Model(&basemodels.SysDepartment{}).
			Select("1").Where("sys_department.id = gb_device.owner_dept_id")
		query := db.WithContext(c.Request.Context()).Model(&gbmodels.GbDevice{}).
			Scopes(ownerDeptScope(c)).Where("owner_dept_id = 0 OR NOT EXISTS (?)", departmentExists)
		result := &directoryDeviceScope{IDs: []uint{}, Codes: []string{}}
		if err := query.Pluck("id", &result.IDs).Error; err != nil {
			return nil, err
		}
		if len(result.IDs) > 0 {
			if err := query.Session(&gorm.Session{}).Where("id IN ?", result.IDs).Distinct().Pluck("device_id", &result.Codes).Error; err != nil {
				return nil, err
			}
		}
		return result, nil
	case gbdirectory.FilterBusinessEmpty:
		return &directoryDeviceScope{IDs: []uint{}, Codes: []string{}, ChannelIDs: []uint{}}, nil
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
		result := db.WithContext(c.Request.Context()).Scopes(ownerDeptScope(c)).Where("id = ?", filter.NodeID).Limit(1).Find(&root)
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 0 || (!gbdirectory.IsBusinessContainerType(root.NodeType) && root.NodeType != gbmodels.NodeTypeChannel) {
			return nil, fmt.Errorf("business catalog node not found")
		}
		channelIDs, err := gbdirectory.CollectBusinessLeafChannelIDs(c.Request.Context(), db, ownerDeptScope(c), root)
		if err != nil {
			return nil, err
		}
		resultScope := &directoryDeviceScope{ChannelIDs: channelIDs}
		if len(channelIDs) > 0 {
			var channelCodes []string
			if err := db.WithContext(c.Request.Context()).Model(&gbmodels.GbChannel{}).
				Scopes(visibleScope(c)).Where("id IN ?", channelIDs).Distinct().Pluck("device_id", &channelCodes).Error; err != nil {
				return nil, err
			}
			resultScope.Codes = channelCodes
			if len(channelCodes) > 0 {
				if err := db.WithContext(c.Request.Context()).Model(&gbmodels.GbDevice{}).
					Scopes(visibleScope(c)).Where("device_id IN ?", channelCodes).Pluck("id", &resultScope.IDs).Error; err != nil {
					return nil, err
				}
			}
		}
		return resultScope, nil
	case gbdirectory.FilterBusinessDevice:
		var device gbmodels.GbDevice
		result := db.WithContext(c.Request.Context()).Scopes(ownerDeptScope(c)).Where("id = ?", filter.NodeID).Limit(1).Find(&device)
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 1 {
			ids = []uint{device.ID}
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
