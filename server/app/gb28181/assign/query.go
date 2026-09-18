package assign

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
)

type DepartmentCount struct {
	DeptID       uint  `json:"deptId"`
	DirectCount  int64 `json:"directCount"`
	SubtreeCount int64 `json:"subtreeCount"`
}

type WorkbenchSummary struct {
	AllCount        int64             `json:"allCount"`
	AssignedCount   int64             `json:"assignedCount"`
	UnassignedCount int64             `json:"unassignedCount"`
	Departments     []DepartmentCount `json:"departments"`
}

type DeviceBrief struct {
	ID            uint   `json:"id"`
	DeviceID      string `json:"deviceId"`
	Name          string `json:"name"`
	Status        int8   `json:"status"`
	Online        bool   `json:"online"`
	OwnerDeptID   uint   `json:"ownerDeptId"`
	OwnerDeptName string `json:"ownerDeptName"`
}

type ResolveResult struct {
	Devices        []DeviceBrief `json:"devices"`
	UnavailableIDs []uint        `json:"unavailableIds"`
}

type QueryService struct {
	db    *gorm.DB
	scope func(*gorm.DB) *gorm.DB
}

func NewQueryService(db *gorm.DB, scope func(*gorm.DB) *gorm.DB) *QueryService {
	return &QueryService{db: db, scope: scope}
}

func (s *QueryService) Summary(ctx context.Context, visibleDeptIDs []uint, needFilter bool) (WorkbenchSummary, error) {
	var rows []struct {
		OwnerDeptID uint  `gorm:"column:owner_dept_id"`
		Count       int64 `gorm:"column:device_count"`
	}
	query := s.db.WithContext(ctx).Model(&gbmodels.GbDevice{})
	if s.scope != nil {
		query = query.Scopes(s.scope)
	}
	if err := query.Select("owner_dept_id, COUNT(*) AS device_count").Group("owner_dept_id").Scan(&rows).Error; err != nil {
		return WorkbenchSummary{}, err
	}

	direct := make(map[uint]int64, len(rows))
	result := WorkbenchSummary{Departments: make([]DepartmentCount, 0)}
	visibleOwners := make([]uint, 0, len(rows))
	for _, row := range rows {
		direct[row.OwnerDeptID] = row.Count
		result.AllCount += row.Count
		if row.OwnerDeptID == 0 {
			result.UnassignedCount += row.Count
		} else {
			result.AssignedCount += row.Count
			visibleOwners = append(visibleOwners, row.OwnerDeptID)
		}
	}

	var departments []basemodels.SysDepartment
	deptQuery := s.db.WithContext(ctx).Model(&basemodels.SysDepartment{}).
		Where("status = ? OR status IS NULL", 1)
	if needFilter {
		allowed := uniqueIDs(append(append([]uint(nil), visibleDeptIDs...), visibleOwners...))
		if len(allowed) == 0 {
			return result, nil
		}
		deptQuery = deptQuery.Where("id IN ?", allowed)
	}
	if err := deptQuery.Order("id ASC").Find(&departments).Error; err != nil {
		return WorkbenchSummary{}, err
	}

	children := make(map[uint][]uint)
	for _, department := range departments {
		if department.ParentID != nil {
			children[*department.ParentID] = append(children[*department.ParentID], department.ID)
		}
	}
	var subtreeCount func(uint, map[uint]bool) int64
	subtreeCount = func(deptID uint, visiting map[uint]bool) int64 {
		if visiting[deptID] {
			return direct[deptID]
		}
		visiting[deptID] = true
		total := direct[deptID]
		for _, childID := range children[deptID] {
			total += subtreeCount(childID, visiting)
		}
		delete(visiting, deptID)
		return total
	}
	for _, department := range departments {
		result.Departments = append(result.Departments, DepartmentCount{
			DeptID:       department.ID,
			DirectCount:  direct[department.ID],
			SubtreeCount: subtreeCount(department.ID, make(map[uint]bool)),
		})
	}
	return result, nil
}

func (s *QueryService) Resolve(ctx context.Context, deviceIDs []uint) (ResolveResult, error) {
	orderedIDs := uniqueIDs(deviceIDs)
	result := ResolveResult{Devices: make([]DeviceBrief, 0, len(orderedIDs)), UnavailableIDs: make([]uint, 0)}
	if len(orderedIDs) == 0 {
		return result, nil
	}

	var devices []gbmodels.GbDevice
	query := s.db.WithContext(ctx).Model(&gbmodels.GbDevice{})
	if s.scope != nil {
		query = query.Scopes(s.scope)
	}
	if err := query.Where("id IN ?", orderedIDs).Find(&devices).Error; err != nil {
		return ResolveResult{}, err
	}
	deviceByID := make(map[uint]gbmodels.GbDevice, len(devices))
	deptIDs := make([]uint, 0, len(devices))
	for _, device := range devices {
		deviceByID[device.ID] = device
		if device.OwnerDeptID > 0 {
			deptIDs = append(deptIDs, device.OwnerDeptID)
		}
	}

	deptNames := make(map[uint]string)
	if ids := uniqueIDs(deptIDs); len(ids) > 0 {
		var departments []basemodels.SysDepartment
		if err := s.db.WithContext(ctx).Where("id IN ?", ids).Find(&departments).Error; err != nil {
			return ResolveResult{}, err
		}
		for _, department := range departments {
			deptNames[department.ID] = department.Name
		}
	}

	for _, id := range orderedIDs {
		device, ok := deviceByID[id]
		if !ok {
			result.UnavailableIDs = append(result.UnavailableIDs, id)
			continue
		}
		ownerName := "未分配"
		if device.OwnerDeptID > 0 {
			ownerName = deptNames[device.OwnerDeptID]
			if ownerName == "" {
				ownerName = fmt.Sprintf("部门 #%d", device.OwnerDeptID)
			}
		}
		result.Devices = append(result.Devices, DeviceBrief{
			ID:            device.ID,
			DeviceID:      device.DeviceID,
			Name:          device.Name,
			Status:        device.Status,
			Online:        device.Status == gbmodels.DeviceStatusOnline,
			OwnerDeptID:   device.OwnerDeptID,
			OwnerDeptName: ownerName,
		})
	}
	return result, nil
}

func uniqueIDs(ids []uint) []uint {
	seen := make(map[uint]struct{}, len(ids))
	result := make([]uint, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}
