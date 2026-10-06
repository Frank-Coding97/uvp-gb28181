package recordingplan

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/recordingplan/schedule"
)

const (
	SelectionByDevice  = "device"
	SelectionByChannel = "channel"

	AssignmentAssigned  = "assigned"
	AssignmentConflict  = "conflict"
	AssignmentForbidden = "forbidden"
	AssignmentNotFound  = "not_found"
)

var (
	ErrPlanDisabled          = &DomainError{Code: "RECORDING_PLAN_DISABLED", Message: "停用的录像计划不能新增分配"}
	ErrSelectionInvalid      = &DomainError{Code: "RECORDING_PLAN_SELECTION_INVALID", Message: "分配类型或选择项不合法"}
	ErrChannelModeInvalid    = &DomainError{Code: "RECORDING_MODE_INVALID", Message: "录像模式不合法"}
	ErrScheduledPlanRequired = &DomainError{Code: "RECORDING_SCHEDULE_REQUIRED", Message: "切换为计划录像前必须先分配启用的录像计划"}
	// ErrChannelNotFound 通道在落库前消失（并发删除）：显式报错而不是静默跳过，
	// 否则 binding 已建、通道录像模式没改，状态自相矛盾。
	ErrChannelNotFound = &DomainError{Code: "RECORDING_CHANNEL_NOT_FOUND", Message: "通道不存在或已被删除"}
)

type AssignmentSelection struct {
	Type string `json:"type"`
	IDs  []uint `json:"ids"`
}

type AssignmentItem struct {
	ID     uint   `json:"id"`
	Status string `json:"status"`
}

type AssignmentResult struct {
	Items         []AssignmentItem `json:"items"`
	AssignedCount int              `json:"assignedCount"`
}

type AssignmentOption struct {
	ID         uint   `json:"id"`
	Name       string `json:"name"`
	Code       string `json:"code"`
	DeviceCode string `json:"deviceCode,omitempty"`
	Online     bool   `json:"online"`
	// Bound 「是否已分配录像计划」曾用于前端表格的「分配状态」列，该列已按产品
	// 要求移除 ⇒ 字段保留（避免破坏 API 契约/存量调用方）但当前**恒为 false**，
	// 且 SearchChannels 不再为此多打一次 gb_recording_plan_binding 查询。
	Bound      bool   `json:"bound"`
}

type AssignmentOptionPage struct {
	List     []AssignmentOption `json:"list"`
	Total    int64              `json:"total"`
	Page     int                `json:"page"`
	PageSize int                `json:"pageSize"`
}

type AssignmentService struct {
	db  *gorm.DB
	now func() time.Time
	// visibleScope 设备可见性过滤（归属 OR 共享 + notcheckuser 白名单），
	// 与设备列表页 datascope.VisibilityScope 同一套口径。
	//
	// ⛔⛔ 列表与落库**必须**共用它：历史上候选列表查 `owner_dept_id`、
	// 而这里也曾各自写死 `owner_dept_id = ?`，一旦只改列表就会造出
	// 「列表能勾、点确认却 forbidden / 静默丢更新」的漂移。
	// 为 nil 时（单元测试/内部调用）退化为「只看归属部门」。
	visibleScope func(*gorm.DB) *gorm.DB
	// fallbackDeptID 仅在 visibleScope == nil 时用作归属部门兜底。
	fallbackDeptID uint
}

func NewAssignmentService(db *gorm.DB) *AssignmentService {
	return &AssignmentService{db: db, now: time.Now}
}

// WithVisibleScope 注入设备可见性过滤，由 controller 层传入
// datascope.VisibilityScope(c, "owner_dept_id", "<设备主键列>")。
func (s *AssignmentService) WithVisibleScope(scope func(*gorm.DB) *gorm.DB) *AssignmentService {
	s.visibleScope = scope
	return s
}

// scope 套用可见性过滤。⛔ 每次查询都必须走它，禁止再单独写 owner_dept_id 条件——
// 那正是本次「设备列表 7 台、分配弹窗只有 1 台」的根因。
func (s *AssignmentService) scope(query *gorm.DB) *gorm.DB {
	if s.visibleScope != nil {
		return query.Scopes(s.visibleScope)
	}
	return query
}

// visibleDevices 按可见性取设备（分配展开时用）。
func (s *AssignmentService) visibleDevices(ctx context.Context, ids []uint) ([]models.GbDevice, error) {
	var devices []models.GbDevice
	query := s.db.WithContext(ctx).Model(&models.GbDevice{}).Where("id IN ?", ids)
	if err := s.scope(query).Find(&devices).Error; err != nil {
		return nil, err
	}
	return devices, nil
}

// visibleChannelsByDeviceCodes 按设备编码取可见通道（分配展开时用）。
func (s *AssignmentService) visibleChannelsByDeviceCodes(ctx context.Context, codes []string) ([]models.GbChannel, error) {
	var channels []models.GbChannel
	if len(codes) == 0 {
		return channels, nil
	}
	query := s.db.WithContext(ctx).Model(&models.GbChannel{}).Where("device_id IN ?", codes)
	if err := s.scope(query).Order("id").Find(&channels).Error; err != nil {
		return nil, err
	}
	return channels, nil
}

func (s *AssignmentService) SearchDevices(ctx context.Context, ownerDeptID uint, keyword string, page, pageSize int) (*AssignmentOptionPage, error) {
	return s.SearchDevicesFiltered(ctx, ownerDeptID, keyword, nil, page, pageSize)
}

func (s *AssignmentService) SearchDevicesFiltered(ctx context.Context, ownerDeptID uint, keyword string, online *bool, page, pageSize int) (*AssignmentOptionPage, error) {
	page, pageSize = normalizePage(page, pageSize)
	// ⚠️ ownerDeptID 仅在未注入 visibleScope 时作为兜底；注入后由datascope 统一裁决
	// （归属 OR 共享 + notcheckuser 白名单），与设备列表页口径一致。
	query := s.db.WithContext(ctx).Model(&models.GbDevice{})
	if s.visibleScope == nil {
		query = query.Where("owner_dept_id = ?", ownerDeptID)
	}
	query = s.scope(query)
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("name LIKE ? OR alias LIKE ? OR device_id LIKE ?", like, like, like)
	}
	if online != nil {
		query = query.Where("status = ?", *online)
	}
	result := &AssignmentOptionPage{List: []AssignmentOption{}, Page: page, PageSize: pageSize}
	if err := query.Count(&result.Total).Error; err != nil {
		return nil, err
	}
	var rows []models.GbDevice
	// ⚠️ 必须**分页前**排序：在线设备优先（status: 1在线 / 0离线，desc），
	// 同状态再按 id 保持稳定顺序 —— 否则后端分页会把在线设备切到第二页去，
	// 用户永远看不到。id 兜底是为了让翻页结果不抖动。
	if err := query.Order("status DESC, id").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		return nil, err
	}
	bound, err := s.deviceOccupancy(ctx, rows)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result.List = append(result.List, AssignmentOption{ID: row.ID, Name: row.Name, Code: row.DeviceID, Online: row.Status == models.DeviceStatusOnline, Bound: bound[row.ID]})
	}
	return result, nil
}

// deviceOccupancy 返回「本页这些设备」的通道占用情况（key = 设备主键）。
//
// Bound=true 表示**该设备下所有通道都已被录像计划占用**（全占用）⇒ 前端置灰不可选。
// 「部分占用」刻意**不**标记为 Bound：那台设备仍有空闲通道可分配，置灰会让用户
// 无法分配这部分；这种情况交由分配时的逐项 conflict 提示处理。
// 无通道的设备不算占用（Bound=false），否则会误伤"还没建通道"的设备。
func (s *AssignmentService) deviceOccupancy(ctx context.Context, rows []models.GbDevice) (map[uint]bool, error) {
	codes := make([]string, 0, len(rows))
	for _, row := range rows {
		codes = append(codes, row.DeviceID)
	}
	occupied := make(map[uint]bool, len(rows))
	if len(codes) == 0 {
		return occupied, nil
	}
	// 一次聚合查询拿到「每个设备的通道总数」与「已被占用数」，避免 N+1。
	// ⛔ 必须显式给表起别名再Select：GORM 的 Model(&GbChannel{}) 不会自动生成 `c`，
	// 写 `c.device_id` 会报 "no such column: c.device_id"（联表列名必须带别名前缀）。
	type agg struct {
		DeviceID string
		Total    int64
		Bound    int64
	}
	var aggs []agg
	if err := s.db.WithContext(ctx).Table("gb_channel AS c").
		Select("c.device_id AS device_id, COUNT(*) AS total, COUNT(b.channel_id) AS bound").
		Joins("LEFT JOIN gb_recording_plan_binding b ON b.channel_id = c.id").
		Where("c.device_id IN ?", codes).
		Group("c.device_id").Scan(&aggs).Error; err != nil {
		return nil, err
	}
	byCode := make(map[string]agg, len(aggs))
	for _, a := range aggs {
		byCode[a.DeviceID] = a
	}
	for _, row := range rows {
		a, ok := byCode[row.DeviceID]
		if !ok || a.Total == 0 || a.Bound == 0 {
			continue
		}
		occupied[row.ID] = a.Bound >= a.Total
	}
	return occupied, nil
}

func (s *AssignmentService) SearchChannels(ctx context.Context, ownerDeptID uint, keyword string, online *bool, page, pageSize int) (*AssignmentOptionPage, error) {
	page, pageSize = normalizePage(page, pageSize)
	query := s.db.WithContext(ctx).Model(&models.GbChannel{})
	if s.visibleScope == nil {
		query = query.Where("owner_dept_id = ?", ownerDeptID)
	}
	query = s.scope(query)
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("name LIKE ? OR alias LIKE ? OR channel_id LIKE ? OR device_id LIKE ?", like, like, like, like)
	}
	if online != nil {
		query = query.Where("status = ?", *online)
	}
	result := &AssignmentOptionPage{List: []AssignmentOption{}, Page: page, PageSize: pageSize}
	if err := query.Count(&result.Total).Error; err != nil {
		return nil, err
	}
	var rows []models.GbChannel
	// 同设备候选：在线通道优先，status: 1在线 / 0离线；id 兜底保证翻页稳定。
	if err := query.Order("status DESC, id").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		return nil, err
	}
	ids := make([]uint, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	// Bound「是否已被某个录像计划占用」：用于候选列表的前置标记（已占用项置灰、
	// 不给勾选），让用户在**提交前**就知道结果，而不是提交后才被拒。
	// ⚠️ 只查**本页** id（而不是全表 join），否则每页都要扫全量绑定表。
	var bindings []models.GbRecordingPlanBinding
	if len(ids) > 0 {
		if err := s.db.WithContext(ctx).Where("channel_id IN ?", ids).Find(&bindings).Error; err != nil {
			return nil, err
		}
	}
	bound := make(map[uint]bool, len(bindings))
	for _, binding := range bindings {
		bound[binding.ChannelID] = true
	}
	for _, row := range rows {
		result.List = append(result.List, AssignmentOption{ID: row.ID, Name: row.Name, Code: row.ChannelID, DeviceCode: row.DeviceID, Online: row.Status == models.ChannelStatusOnline, Bound: bound[row.ID]})
	}
	return result, nil
}

func (s *AssignmentService) Assign(ctx context.Context, ownerDeptID, actorID uint, planID uint64, selection AssignmentSelection) (*AssignmentResult, error) {
	plan, err := findPlan(s.db.WithContext(ctx), ownerDeptID, planID)
	if err != nil {
		return nil, err
	}
	if plan.Status != 1 {
		return nil, ErrPlanDisabled
	}
	if len(selection.IDs) == 0 || (selection.Type != SelectionByChannel && selection.Type != SelectionByDevice) {
		return nil, ErrSelectionInvalid
	}
	// 兜底部门：未注入 visibleScope 时（单测/内部调用）沿用调用方传入的部门，
	// 保持既有行为；注入后该值不参与可见性裁决。
	s.fallbackDeptID = ownerDeptID
	channelIDs, result, err := s.resolveSelection(ctx, ownerDeptID, selection)
	if err != nil {
		return nil, err
	}
	if len(channelIDs) == 0 {
		return result, nil
	}
	periods, err := loadPeriods(s.db.WithContext(ctx), planID)
	if err != nil {
		return nil, err
	}
	evaluation := schedule.Evaluate(periods, s.now())
	desired := models.RecordingDesiredIdle
	if evaluation.Matched {
		desired = models.RecordingDesiredRecording
	}
	now := s.now()
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, channelID := range channelIDs {
			binding := models.GbRecordingPlanBinding{PlanID: planID, ChannelID: channelID, OwnerDeptID: ownerDeptID, AssignedBy: actorID, AssignedAt: now}
			if err := tx.Create(&binding).Error; err != nil {
				if isDuplicateError(err) {
					return ErrChannelAlreadyBound
				}
				return err
			}
			// ⛔⛔ 这里原来带 `AND owner_dept_id = ?`，是个**静默失效**点：
			// 绑定行已按可见性放行了跨部门/共享通道，更新却仍按归属部门卡，
			// ⇒ RowsAffected=0 而**不报错**，结果是「接口说分配成功、
			// 通道的录像模式却没切成按计划」，极难察觉。
			// 现在可见性已在 resolveSelection 阶段校验过，这里按主键更新即可。
			// 且必须断言确实更新到行，否则同样静默。
			result := tx.Model(&models.GbChannel{}).Where("id = ?", channelID).
				Updates(map[string]any{"recording_mode": models.RecordingModeScheduled, "cloud_recording_enabled": evaluation.Matched})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return ErrChannelNotFound
			}
			state := models.GbRecordingPlanChannelState{
				ChannelID: channelID, PlanID: &planID, PlanVersion: plan.Version, DesiredState: desired,
				ActualState: models.RecordingStateIdle, NextTransitionAt: evaluation.NextTransition, ReconcileAt: now,
			}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "channel_id"}}, DoUpdates: clause.Assignments(map[string]any{
				"plan_id": planID, "plan_version": plan.Version, "desired_state": desired,
				"next_transition_at": evaluation.NextTransition, "reconcile_at": now, "lease_owner": "", "lease_until": nil,
			})}).Create(&state).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	result.AssignedCount = len(channelIDs)
	return result, nil
}

func (s *AssignmentService) SetMode(ctx context.Context, ownerDeptID, channelID uint, mode string) error {
	if mode != models.RecordingModeOff && mode != models.RecordingModeContinuous && mode != models.RecordingModeScheduled {
		return ErrChannelModeInvalid
	}
	now := s.now()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var channel models.GbChannel
		result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND owner_dept_id = ?", channelID, ownerDeptID).Limit(1).Find(&channel)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrPlanNotFound
		}
		desired := mode == models.RecordingModeContinuous
		var planID *uint64
		var planVersion uint64
		var nextTransition *time.Time
		if mode == models.RecordingModeScheduled {
			var binding models.GbRecordingPlanBinding
			found := tx.Where("channel_id = ?", channelID).Limit(1).Find(&binding)
			if found.Error != nil {
				return found.Error
			}
			if found.RowsAffected == 0 {
				return ErrScheduledPlanRequired
			}
			plan, err := findPlan(tx, ownerDeptID, binding.PlanID)
			if err != nil || plan.Status != 1 {
				return ErrScheduledPlanRequired
			}
			periods, err := loadPeriods(tx, binding.PlanID)
			if err != nil {
				return err
			}
			evaluation := schedule.Evaluate(periods, now)
			desired = evaluation.Matched
			planID, planVersion, nextTransition = &binding.PlanID, plan.Version, evaluation.NextTransition
		} else if err := tx.Where("channel_id = ?", channelID).Delete(&models.GbRecordingPlanBinding{}).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.GbChannel{}).Where("id = ?", channelID).Updates(map[string]any{
			"recording_mode": mode, "cloud_recording_enabled": desired,
		}).Error; err != nil {
			return err
		}
		desiredState := models.RecordingDesiredIdle
		if desired {
			desiredState = models.RecordingDesiredRecording
		}
		state := models.GbRecordingPlanChannelState{ChannelID: channelID, PlanID: planID, PlanVersion: planVersion, DesiredState: desiredState, ActualState: models.RecordingStateIdle, NextTransitionAt: nextTransition, ReconcileAt: now}
		return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "channel_id"}}, DoUpdates: clause.Assignments(map[string]any{
			"plan_id": planID, "plan_version": planVersion, "desired_state": desiredState,
			"next_transition_at": nextTransition, "reconcile_at": now, "lease_owner": "", "lease_until": nil,
		})}).Create(&state).Error
	})
}

func (s *AssignmentService) resolveSelection(ctx context.Context, ownerDeptID uint, selection AssignmentSelection) ([]uint, *AssignmentResult, error) {
	if selection.Type == SelectionByDevice {
		// ⛔ 必须与候选列表同一套可见性，否则会出现「列表能勾、点确认全 forbidden」
		devices, err := s.visibleDevices(ctx, selection.IDs)
		if err != nil {
			return nil, nil, err
		}

		codes := make([]string, 0, len(devices))
		for _, device := range devices {
			codes = append(codes, device.DeviceID)
		}
		channels, err := s.visibleChannelsByDeviceCodes(ctx, codes)
		if err != nil {
			return nil, nil, err
		}

		ids := make([]uint, 0, len(channels))
		for _, channel := range channels {
			ids = append(ids, channel.ID)
		}
		return s.classifyChannels(ctx, ownerDeptID, ids)
	}
	return s.classifyChannels(ctx, ownerDeptID, selection.IDs)
}

func (s *AssignmentService) classifyChannels(ctx context.Context, ownerDeptID uint, requested []uint) ([]uint, *AssignmentResult, error) {
	requested = uniqueIDs(requested)
	// ⛔⛔ 可见性判定必须与候选列表同源：这里**只**看「在不在可见集合里」，
	// 绝不能再写 `channel.OwnerDeptID != ownerDeptID` —— 那只认归属部门，
	// 会把「共享可见 / 超管白名单」可见的通道误判成 forbidden，
	// 造成「列表里能勾、点确认却拒绝」的漂移。
	visibleIDs, err := s.visibleChannelIDSet(ctx, requested)
	if err != nil {
		return nil, nil, err
	}
	var channels []models.GbChannel
	if err := s.db.WithContext(ctx).Where("id IN ?", requested).Find(&channels).Error; err != nil {
		return nil, nil, err
	}
	channelByID := make(map[uint]models.GbChannel, len(channels))
	for _, channel := range channels {
		channelByID[channel.ID] = channel
	}
	var bindings []models.GbRecordingPlanBinding
	if err := s.db.WithContext(ctx).Where("channel_id IN ?", requested).Find(&bindings).Error; err != nil {
		return nil, nil, err
	}
	bound := make(map[uint]struct{}, len(bindings))
	for _, binding := range bindings {
		bound[binding.ChannelID] = struct{}{}
	}
	result := &AssignmentResult{Items: make([]AssignmentItem, 0, len(requested))}
	valid := make([]uint, 0, len(requested))
	for _, id := range requested {
		status := AssignmentAssigned
		_, exists := channelByID[id]
		switch {
		case !exists:
			status = AssignmentNotFound
		case !visibleIDs[id]:
			status = AssignmentForbidden
		default:
			if _, taken := bound[id]; taken {
				status = AssignmentConflict
			} else {
				valid = append(valid, id)
			}
		}
		result.Items = append(result.Items, AssignmentItem{ID: id, Status: status})
	}
	return valid, result, nil
}

// visibleChannelIDSet 返回给定通道中「当前操作者可见」的集合。
// 未注入 visibleScope 时退化为「归属部门 == ownerDeptID」，保持旧行为。
func (s *AssignmentService) visibleChannelIDSet(ctx context.Context, channelIDs []uint) (map[uint]bool, error) {
	visible := make(map[uint]bool, len(channelIDs))
	if len(channelIDs) == 0 {
		return visible, nil
	}
	if s.visibleScope == nil {
		var rows []models.GbChannel
		query := s.db.WithContext(ctx).Model(&models.GbChannel{}).
			Where("id IN ? AND owner_dept_id = ?", channelIDs, s.fallbackDeptID)
		if err := query.Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			visible[row.ID] = true
		}
		return visible, nil
	}
	// 注入 visibility 后不需要 ownerDeptID：范围完全由 datascope 裁决。
	// 这里用「查得到即可见」，因此必须走 Model(&GbChannel{}) 让 scope 拼得上。
	var rows []models.GbChannel
	query := s.db.WithContext(ctx).Model(&models.GbChannel{}).Where("id IN ?", channelIDs)
	if err := s.scope(query).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		visible[row.ID] = true
	}
	return visible, nil
}

func uniqueIDs(values []uint) []uint {
	seen := make(map[uint]struct{}, len(values))
	result := make([]uint, 0, len(values))
	for _, value := range values {
		if value == 0 {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func normalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}
