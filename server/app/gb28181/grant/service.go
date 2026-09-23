package grant

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/datascope"
)

var ErrTargetTypeInvalid = errors.New("共享目标类型不合法")

type DeviceScope func(*gorm.DB) *gorm.DB

type GrantItem struct {
	ID         uint   `json:"id"`
	TargetType string `json:"targetType"`
	TargetID   uint   `json:"targetId"`
	TargetName string `json:"targetName"`
	Invalid    bool   `json:"invalid"`
}

type DeviceGrantState struct {
	DeviceID   uint        `json:"deviceId"`
	DeviceCode string      `json:"deviceCode"`
	Name       string      `json:"name"`
	Revision   string      `json:"revision"`
	Grants     []GrantItem `json:"grants"`
}

type QueryResult struct {
	Devices        []DeviceGrantState `json:"devices"`
	UnavailableIDs []uint             `json:"unavailableIds"`
}

type TargetOption struct {
	ID       uint   `json:"id"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	DeptID   uint   `json:"deptId,omitempty"`
	DeptName string `json:"deptName,omitempty"`
}

type TargetPage struct {
	List     []TargetOption `json:"list"`
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"pageSize"`
}

type Service struct {
	db    *gorm.DB
	scope DeviceScope
}

func NewService(db *gorm.DB, scope DeviceScope) *Service {
	return &Service{db: db, scope: scope}
}

func (s *Service) Query(ctx context.Context, deviceIDs []uint) (QueryResult, error) {
	uniqueIDs := uniqueNonZeroIDs(deviceIDs)
	result := QueryResult{
		Devices:        make([]DeviceGrantState, 0, len(uniqueIDs)),
		UnavailableIDs: make([]uint, 0),
	}
	if len(uniqueIDs) == 0 {
		return result, nil
	}

	query := s.db.WithContext(ctx).Model(&gbmodels.GbDevice{}).Where("id IN ?", uniqueIDs)
	if s.scope != nil {
		query = query.Scopes(s.scope)
	}
	var devices []gbmodels.GbDevice
	if err := query.Select("id, device_id, name").Find(&devices).Error; err != nil {
		return QueryResult{}, err
	}
	deviceByID := make(map[uint]gbmodels.GbDevice, len(devices))
	visibleIDs := make([]uint, 0, len(devices))
	for _, device := range devices {
		deviceByID[device.ID] = device
		visibleIDs = append(visibleIDs, device.ID)
	}

	grantsByDevice, err := s.loadGrants(ctx, visibleIDs)
	if err != nil {
		return QueryResult{}, err
	}
	departmentNames, userNames, invalidDepartments, invalidUsers, err := s.resolveTargetNames(ctx, grantsByDevice)
	if err != nil {
		return QueryResult{}, err
	}
	for _, deviceID := range uniqueIDs {
		device, ok := deviceByID[deviceID]
		if !ok {
			result.UnavailableIDs = append(result.UnavailableIDs, deviceID)
			continue
		}
		grants := grantsByDevice[deviceID]
		state := DeviceGrantState{
			DeviceID:   device.ID,
			DeviceCode: device.DeviceID,
			Name:       device.Name,
			Revision:   revisionFor(grants),
			Grants:     make([]GrantItem, 0, len(grants)),
		}
		for _, current := range grants {
			item := GrantItem{ID: current.ID, TargetType: current.TargetType, TargetID: current.TargetID}
			switch current.TargetType {
			case gbmodels.GrantTargetTypeDept:
				item.TargetName = departmentNames[current.TargetID]
				item.Invalid = invalidDepartments[current.TargetID]
			case gbmodels.GrantTargetTypeUser:
				item.TargetName = userNames[current.TargetID]
				item.Invalid = invalidUsers[current.TargetID]
			default:
				item.Invalid = true
			}
			if item.TargetName == "" {
				item.TargetName = fmt.Sprintf("#%d", item.TargetID)
				item.Invalid = true
			}
			state.Grants = append(state.Grants, item)
		}
		result.Devices = append(result.Devices, state)
	}
	return result, nil
}

func (s *Service) SearchTargets(ctx context.Context, targetType, q string, page, pageSize int, access datascope.OwnerDeptAccess) (TargetPage, error) {
	if targetType != gbmodels.GrantTargetTypeDept && targetType != gbmodels.GrantTargetTypeUser {
		return TargetPage{}, ErrTargetTypeInvalid
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}
	result := TargetPage{List: make([]TargetOption, 0), Page: page, PageSize: pageSize}
	if !access.FullAccess && len(access.DeptIDs) == 0 {
		return result, nil
	}
	keyword := "%" + strings.TrimSpace(q) + "%"
	if targetType == gbmodels.GrantTargetTypeDept {
		query := s.db.WithContext(ctx).Model(&basemodels.SysDepartment{}).
			Where("status = ? OR status IS NULL", 1)
		if !access.FullAccess {
			query = query.Where("id IN ?", access.DeptIDs)
		}
		if strings.TrimSpace(q) != "" {
			query = query.Where("name LIKE ?", keyword)
		}
		if err := query.Count(&result.Total).Error; err != nil {
			return TargetPage{}, err
		}
		var departments []basemodels.SysDepartment
		if err := query.Select("id, name").Order("name ASC, id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&departments).Error; err != nil {
			return TargetPage{}, err
		}
		for _, department := range departments {
			result.List = append(result.List, TargetOption{ID: department.ID, Type: targetType, Name: department.Name})
		}
		return result, nil
	}

	type userTargetRow struct {
		ID       uint
		Username string
		DeptID   uint
		DeptName string
	}
	query := s.db.WithContext(ctx).Model(&basemodels.User{}).
		Joins("LEFT JOIN sys_department d ON d.id = sys_users.dept_id AND d.deleted_at IS NULL").
		Where("sys_users.status = ?", 1)
	if !access.FullAccess {
		query = query.Where("sys_users.dept_id IN ?", access.DeptIDs)
	}
	if strings.TrimSpace(q) != "" {
		query = query.Where("sys_users.username LIKE ? OR sys_users.nick_name LIKE ?", keyword, keyword)
	}
	if err := query.Count(&result.Total).Error; err != nil {
		return TargetPage{}, err
	}
	var rows []userTargetRow
	if err := query.Select("sys_users.id, sys_users.username, sys_users.dept_id, d.name AS dept_name").
		Order("sys_users.username ASC, sys_users.id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&rows).Error; err != nil {
		return TargetPage{}, err
	}
	for _, row := range rows {
		result.List = append(result.List, TargetOption{ID: row.ID, Type: targetType, Name: row.Username, DeptID: row.DeptID, DeptName: row.DeptName})
	}
	return result, nil
}

func (s *Service) loadGrants(ctx context.Context, deviceIDs []uint) (map[uint][]gbmodels.GbDeviceGrant, error) {
	result := make(map[uint][]gbmodels.GbDeviceGrant, len(deviceIDs))
	if len(deviceIDs) == 0 {
		return result, nil
	}
	var grants []gbmodels.GbDeviceGrant
	if err := s.db.WithContext(ctx).Where("device_id IN ?", deviceIDs).
		Order("device_id ASC, target_type ASC, target_id ASC").Find(&grants).Error; err != nil {
		return nil, err
	}
	for _, current := range grants {
		result[current.DeviceID] = append(result[current.DeviceID], current)
	}
	return result, nil
}

func (s *Service) resolveTargetNames(ctx context.Context, grantsByDevice map[uint][]gbmodels.GbDeviceGrant) (map[uint]string, map[uint]string, map[uint]bool, map[uint]bool, error) {
	departmentIDs := make(map[uint]struct{})
	userIDs := make(map[uint]struct{})
	for _, grants := range grantsByDevice {
		for _, current := range grants {
			switch current.TargetType {
			case gbmodels.GrantTargetTypeDept:
				departmentIDs[current.TargetID] = struct{}{}
			case gbmodels.GrantTargetTypeUser:
				userIDs[current.TargetID] = struct{}{}
			}
		}
	}
	departmentNames := make(map[uint]string, len(departmentIDs))
	userNames := make(map[uint]string, len(userIDs))
	invalidDepartments := make(map[uint]bool, len(departmentIDs))
	invalidUsers := make(map[uint]bool, len(userIDs))
	if len(departmentIDs) > 0 {
		var departments []basemodels.SysDepartment
		if err := s.db.WithContext(ctx).Unscoped().Where("id IN ?", mapKeys(departmentIDs)).Find(&departments).Error; err != nil {
			return nil, nil, nil, nil, err
		}
		for _, department := range departments {
			departmentNames[department.ID] = department.Name
			invalidDepartments[department.ID] = department.DeletedAt.Valid || (department.Status != nil && *department.Status != 1)
		}
		for id := range departmentIDs {
			if _, ok := departmentNames[id]; !ok {
				invalidDepartments[id] = true
			}
		}
	}
	if len(userIDs) > 0 {
		var users []basemodels.User
		if err := s.db.WithContext(ctx).Unscoped().Where("id IN ?", mapKeys(userIDs)).Find(&users).Error; err != nil {
			return nil, nil, nil, nil, err
		}
		for _, user := range users {
			name := user.NickName
			if name == "" {
				name = user.Username
			}
			userNames[user.ID] = name
			invalidUsers[user.ID] = user.DeletedAt.Valid || user.Status != 1
		}
		for id := range userIDs {
			if _, ok := userNames[id]; !ok {
				invalidUsers[id] = true
			}
		}
	}
	return departmentNames, userNames, invalidDepartments, invalidUsers, nil
}

func revisionFor(grants []gbmodels.GbDeviceGrant) string {
	tuples := make([]string, 0, len(grants))
	for _, current := range grants {
		tuples = append(tuples, fmt.Sprintf("%s:%d", current.TargetType, current.TargetID))
	}
	sort.Strings(tuples)
	sum := sha256.Sum256([]byte(strings.Join(tuples, "\n")))
	return fmt.Sprintf("%x", sum)
}

func uniqueNonZeroIDs(values []uint) []uint {
	result := make([]uint, 0, len(values))
	seen := make(map[uint]struct{}, len(values))
	for _, value := range values {
		if value == 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func mapKeys(values map[uint]struct{}) []uint {
	result := make([]uint, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	return result
}
