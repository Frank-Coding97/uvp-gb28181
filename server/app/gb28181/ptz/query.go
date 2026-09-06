package ptz

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
	gbtrace "uvplatform.cn/uvp-gb28181/app/gb28181/trace"
)

type QueryKind string

type stagedCruiseTrack struct {
	item manscdp.CruiseTrack
	body []byte
}

type queryResponseStage struct {
	cmdType  string
	expected int
	presets  map[int]manscdp.Preset
	tracks   map[int]stagedCruiseTrack
}

const (
	QueryPreset          QueryKind = "preset"
	QueryHomePosition    QueryKind = "home_position"
	QueryCruiseTrackList QueryKind = "cruise_track_list"
	QueryCruiseTrack     QueryKind = "cruise_track"
	QueryPreciseStatus   QueryKind = "precise_status"

	// maxQueryStagePresets / maxQueryStageTracks 单次查询聚合的条目硬上限:
	// 异常或恶意设备可通过分页响应制造 O(条目数 × body 大小) 的内存放大,
	// 超出上限直接终止聚合
	maxQueryStagePresets = 10000
	maxQueryStageTracks  = 10000
	// maxQueryStageBodyBytes 单页响应体上限:在完整解析前拒绝超大 XML,
	// 防止解析器先构造完整结构再被条目上限拒绝
	maxQueryStageBodyBytes = 1 << 20
)

func (s *Service) Refresh(ctx context.Context, target Target, kind QueryKind, trackID int, idempotencyKey string) (gbmodels.GbPTZOperation, error) {
	command := Command{IdempotencyKey: idempotencyKey, Payload: map[string]interface{}{}, Profile: target.Profile}
	profile := target.Profile
	if profile.Version == "" {
		profile = protocol.ProfileFor(protocol.Version2016)
	}
	targetCode := target.ChannelCode
	switch kind {
	case QueryPreset:
		command.CmdType, command.Action = manscdp.CmdPresetQuery, "refresh_presets"
		command.ResponseRequired, command.MaxAttempts = true, 3
		command.Build = func(sn int) ([]byte, error) { return manscdp.BuildPresetQueryWithProfile(profile, targetCode, sn) }
	case QueryHomePosition:
		command.CmdType, command.Action = manscdp.CmdHomePositionQuery, "refresh_home_position"
		command.ResponseRequired, command.MaxAttempts = true, 3
		command.Build = func(sn int) ([]byte, error) {
			return manscdp.BuildHomePositionQueryWithProfile(profile, targetCode, sn)
		}
	case QueryCruiseTrackList:
		command.CmdType, command.Action = manscdp.CmdCruiseTrackListQuery, "refresh_cruise_tracks"
		command.ResponseRequired, command.MaxAttempts = true, 3
		command.Build = func(sn int) ([]byte, error) {
			return manscdp.BuildCruiseTrackListQueryWithProfile(profile, targetCode, sn)
		}
	case QueryCruiseTrack:
		command.CmdType, command.Action = manscdp.CmdCruiseTrackQuery, "refresh_cruise_track"
		command.ResponseRequired, command.MaxAttempts = true, 3
		command.Payload["trackId"] = trackID
		command.Build = func(sn int) ([]byte, error) {
			return manscdp.BuildCruiseTrackQueryWithProfile(profile, targetCode, sn, trackID)
		}
	case QueryPreciseStatus:
		command.CmdType, command.Action = manscdp.CmdPTZPreciseStatusQuery, "refresh_precise_status"
		if profile.SupportsPrecisePTZ() {
			command.CmdType = manscdp.CmdPTZPosition
		}
		command.ResponseRequired, command.MaxAttempts = true, 3
		command.Build = func(sn int) ([]byte, error) {
			return manscdp.BuildPTZPreciseStatusQueryWithProfile(profile, targetCode, sn)
		}
	default:
		return gbmodels.GbPTZOperation{}, fmt.Errorf("未知 PTZ 查询类型: %q", kind)
	}
	return s.Execute(ctx, target, command)
}

// RefreshHomePosition is the audited manual HomePosition query path. It is
// deliberately separate from Refresh so existing resource queries keep their
// legacy actor semantics while the home-position endpoint records the actual
// user who initiated the query.
func (s *Service) RefreshHomePosition(ctx context.Context, target Target, actorID, actorDeptID uint, idempotencyKey string) (gbmodels.GbPTZOperation, error) {
	profile := target.Profile
	if profile.Version == "" {
		profile = protocol.ProfileFor(protocol.Version2016)
	}
	return s.Execute(ctx, target, Command{
		CmdType:          manscdp.CmdHomePositionQuery,
		Action:           "refresh_home_position",
		IdempotencyKey:   idempotencyKey,
		Payload:          map[string]interface{}{},
		ResponseRequired: true,
		MaxAttempts:      3,
		ActorID:          actorID,
		ActorDeptID:      actorDeptID,
		Build: func(sn int) ([]byte, error) {
			return manscdp.BuildHomePositionQueryWithProfile(profile, target.ChannelCode, sn)
		},
		Profile: profile,
	})
}

func (s *Service) applyQueryResponse(ctx context.Context, operation gbmodels.GbPTZOperation, callID, cseq string, head manscdp.MessageHead, body []byte) error {
	if head.CmdType == manscdp.CmdHomePositionQuery {
		return s.applyHomePositionQueryResponse(ctx, operation, callID, cseq, body)
	}
	if err := validateQueryResponse(operation, head.CmdType, body); err != nil {
		return s.applyRejectedPTZResponse(ctx, operation, callID, cseq, "ERROR", ptzErrorProtocolInvalid, err.Error())
	}

	completedAt := s.now()
	s.queryStageMu.Lock()
	defer s.queryStageMu.Unlock()

	stage, stagedComplete, err := s.accumulateQueryStage(operation, head.CmdType, body)
	if err != nil {
		return err
	}
	applied := false
	terminalApplied := false
	err = schedulerTransaction(ctx, s.db, func(tx *gorm.DB) error {
		applied = false
		terminalApplied = false
		// First reserve this response with a status/deadline CAS. A terminal or
		// expired operation therefore cannot reach any cache write below.
		var err error
		applied, err = applyPTZResponseObservation(tx, operation, callID, cseq, completedAt)
		if err != nil || !applied {
			if !applied && err == nil {
				logIgnoredPTZResponse(ctx, operation, callID, cseq, head, body)
			}
			return err
		}

		newer, err := newerAcceptedQueryExists(tx, operation)
		if err != nil {
			return err
		}
		complete := newer
		if !newer {
			if stage != nil {
				complete = stagedComplete
				if complete {
					err = s.finalizeQueryStageTx(ctx, tx, operation, *stage, completedAt)
				}
			} else {
				complete, err = s.persistQueryCacheTx(ctx, tx, operation, head.CmdType, body)
			}
			if err != nil {
				return err
			}
		}
		if !complete {
			return nil
		}
		terminalApplied, err = applyPTZResponseTransition(tx, operation, callID, cseq, ptzResponseTransition{
			Status: gbmodels.PTZOperationAccepted, DeviceResult: "OK",
		}, completedAt)
		if err != nil {
			return err
		}
		if !terminalApplied {
			logIgnoredPTZResponse(ctx, operation, callID, cseq, head, body)
			return fmt.Errorf("PTZ 查询响应 operation 状态推进失败")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !applied || terminalApplied {
		delete(s.queryStages, operation.OperationID)
	} else if stage != nil {
		s.queryStages[operation.OperationID] = *stage
	}
	return nil
}

func validateQueryResponse(operation gbmodels.GbPTZOperation, cmdType string, body []byte) error {
	switch cmdType {
	case manscdp.CmdPresetQuery:
		_, err := manscdp.ParsePresetResponse(body)
		return err
	case manscdp.CmdCruiseTrackListQuery:
		_, err := manscdp.ParseCruiseTrackListResponse(body)
		return err
	case manscdp.CmdCruiseTrackQuery:
		_, err := manscdp.ParseCruiseTrackResponse(body)
		return err
	case manscdp.CmdPTZPreciseStatusQuery, manscdp.CmdPTZPosition:
		_, err := manscdp.ParsePTZPreciseStatusResponseWithProfile(operationProtocolProfile(operation), body)
		return err
	default:
		return fmt.Errorf("不支持的 PTZ 查询响应: %s", cmdType)
	}
}

func headSN(head manscdp.MessageHead) int {
	var sn int
	fmt.Sscanf(head.SN, "%d", &sn)
	return sn
}

func (s *Service) applyHomePositionQueryResponse(ctx context.Context, operation gbmodels.GbPTZOperation, callID, cseq string, body []byte) error {
	options, err := s.homePositionParseOptions(ctx, operation.ChannelID)
	if err != nil {
		return err
	}
	response, parseErr := manscdp.ParseHomePositionResponse(body, options)
	if parseErr != nil || response.SN != operation.SN || response.DeviceID != operationTargetCode(operation) {
		if parseErr == nil {
			parseErr = fmt.Errorf("HomePositionQuery 应答标识与 operation 不一致")
		}
		completedAt := s.now()
		return schedulerTransaction(ctx, s.db, func(tx *gorm.DB) error {
			_, err := applyPTZResponseTransition(tx, operation, callID, cseq, ptzResponseTransition{
				Status:      gbmodels.PTZOperationRejected,
				DeviceError: parseErr.Error(), ErrorCode: ptzErrorProtocolInvalid, ErrorMessage: parseErr.Error(),
			}, completedAt)
			return err
		})
	}

	entry := s.lockChannel(operation.ChannelID)
	entry.mu.Lock()
	defer func() {
		entry.mu.Unlock()
		s.unlockChannel(operation.ChannelID, entry)
	}()

	completedAt := s.now()
	hasData := response.HomePosition != nil
	rawSummary := summarizePTZBody(body)
	return schedulerTransaction(ctx, s.db, func(tx *gorm.DB) error {
		applied, err := applyPTZResponseTransition(tx, operation, callID, cseq, ptzResponseTransition{
			Status: gbmodels.PTZOperationAccepted, DeviceResult: "OK", ResponseHasData: &hasData,
		}, completedAt)
		if err != nil || !applied {
			return err
		}
		if !hasData {
			return nil
		}

		encoding := gbmodels.PTZHomePositionEnabledNumeric
		if response.HomePosition.EnabledEncoding == manscdp.HomePositionEnabledEncodingCompatBooleanText {
			encoding = gbmodels.PTZHomePositionEnabledCompatBooleanText
		}
		if _, _, err := s.applyHomePositionDB(tx, HomePositionUpdate{
			DeviceID: operation.DeviceID, ChannelID: operation.ChannelID, ChannelCode: operation.ChannelCode,
			Enabled: response.HomePosition.Enabled, ResetTime: response.HomePosition.ResetTime, PresetID: response.HomePosition.PresetIndex,
			EnabledEncoding: encoding, ConfirmedAt: completedAt,
			Source: gbmodels.PTZHomePositionSourceDeviceQuery, Verification: gbmodels.PTZHomePositionVerificationVerified,
			SourceSN: response.SN, SourceOperationID: operation.OperationID, SourceOperationSeq: operation.ID,
			RawSummary: rawSummary,
		}); err != nil {
			return err
		}

		mismatch, err := homePositionReconcileMismatch(tx, operation, response.HomePosition)
		if err != nil || !mismatch {
			return err
		}
		result := tx.Model(&gbmodels.GbPTZOperation{}).
			Where("id = ? AND status = ?", operation.ID, gbmodels.PTZOperationAccepted).
			Updates(map[string]interface{}{
				"error_code":    ptzErrorReconcileMismatch,
				"error_message": "设备查询值与看守位控制意图不一致",
			})
		return result.Error
	})
}

func homePositionReconcileMismatch(tx *gorm.DB, query gbmodels.GbPTZOperation, actual *manscdp.HomePositionConfig) (bool, error) {
	if query.CmdType != manscdp.CmdHomePositionQuery || query.Action != "refresh_home_position" ||
		query.TriggerOperationID == nil || strings.TrimSpace(*query.TriggerOperationID) == "" || actual == nil {
		return false, nil
	}
	var parent gbmodels.GbPTZOperation
	result := tx.Where("operation_id = ?", *query.TriggerOperationID).Limit(1).Find(&parent)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 0 {
		return false, nil
	}
	if parent.DeviceID != query.DeviceID || parent.DeviceCode != query.DeviceCode ||
		parent.ChannelID != query.ChannelID || parent.ChannelCode != query.ChannelCode ||
		parent.Status != gbmodels.PTZOperationAccepted ||
		parent.CmdType != manscdp.CmdDeviceControl || parent.Action != "home_position" ||
		parent.ReconcileOperationID == nil || *parent.ReconcileOperationID != query.OperationID {
		return false, nil
	}
	expected, err := decodeHomePositionControlPayload(parent.PayloadJSON)
	if err != nil {
		return false, err
	}
	if expected.Enabled != actual.Enabled {
		return true, nil
	}
	if !expected.Enabled {
		return false, nil
	}
	return !sameOptionalInt(expected.ResetTime, actual.ResetTime) || !sameOptionalInt(expected.PresetID, actual.PresetIndex), nil
}

func sameOptionalInt(left, right *int) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

// persistQueryCacheTx writes one query response using the caller's transaction
// and reports whether the response completed the query. Keeping this inside
// the operation CAS transaction prevents a timed-out response from creating
// or replacing any cache row.
func (s *Service) persistQueryCacheTx(ctx context.Context, tx *gorm.DB, operation gbmodels.GbPTZOperation, cmdType string, body []byte) (bool, error) {
	now := s.now()
	tx = tx.WithContext(ctx)
	switch cmdType {
	case manscdp.CmdCruiseTrackQuery:
		response, err := manscdp.ParseCruiseTrackResponse(body)
		if err != nil {
			return false, err
		}
		if response.CruiseTrack.Enabled == nil {
			enabled := true
			response.CruiseTrack.Enabled = &enabled
		}
		if err := upsertCruiseTrack(tx, operation, response.CruiseTrack, body, now, true); err != nil {
			return false, err
		}
		return true, nil

	case manscdp.CmdPTZPreciseStatusQuery, manscdp.CmdPTZPosition:
		response, err := manscdp.ParsePTZPreciseStatusResponseWithProfile(operationProtocolProfile(operation), body)
		if err != nil {
			return false, err
		}
		// ApplyPreciseNotify already owns the precise-state freshness/dedupe
		// rules. Binding its DB handle to tx keeps those writes in this CAS.
		cacheService := &Service{db: tx, now: s.now}
		_, err = cacheService.ApplyPreciseNotify(ctx, PreciseNotify{
			DeviceID: operation.DeviceID, DeviceCode: operation.DeviceCode, ChannelID: operation.ChannelID, ChannelCode: operation.ChannelCode,
			SN: response.SN, Pan: response.Pan, Tilt: response.Tilt, Zoom: response.Zoom, Focus: response.Focus, Iris: response.Iris,
			ReceivedAt: now, DedupeKey: operation.OperationID, RawSummary: summarizePTZBody(body),
		})
		return true, err
	default:
		return false, fmt.Errorf("不支持的 PTZ 查询响应: %s", cmdType)
	}
}

func (s *Service) accumulateQueryStage(operation gbmodels.GbPTZOperation, cmdType string, body []byte) (*queryResponseStage, bool, error) {
	if cmdType != manscdp.CmdPresetQuery && cmdType != manscdp.CmdCruiseTrackListQuery {
		return nil, false, nil
	}
	// 解析前先限输入体积:单页超大 XML 不允许进入解析器
	if len(body) > maxQueryStageBodyBytes {
		return nil, false, fmt.Errorf("PTZ 查询响应体超出上限 %d 字节", maxQueryStageBodyBytes)
	}
	stage := cloneQueryResponseStage(s.queryStages[operation.OperationID])
	if stage.cmdType != "" && stage.cmdType != cmdType {
		return nil, false, fmt.Errorf("PTZ 查询暂存类型不一致: %s", cmdType)
	}
	stage.cmdType = cmdType

	switch cmdType {
	case manscdp.CmdPresetQuery:
		response, err := manscdp.ParsePresetResponse(body)
		if err != nil {
			return nil, false, err
		}
		if stage.presets == nil {
			stage.presets = make(map[int]manscdp.Preset)
		}
		for _, item := range response.Presets {
			if len(stage.presets) >= maxQueryStagePresets {
				return nil, false, fmt.Errorf("PTZ 预置位条目数超出上限 %d", maxQueryStagePresets)
			}
			stage.presets[item.ID] = item
		}
		stage.expected = maxQueryStageExpected(stage.expected, response.SumNum, len(stage.presets))
		return &stage, len(stage.presets) >= stage.expected, nil

	case manscdp.CmdCruiseTrackListQuery:
		response, err := manscdp.ParseCruiseTrackListResponse(body)
		if err != nil {
			return nil, false, err
		}
		if stage.tracks == nil {
			stage.tracks = make(map[int]stagedCruiseTrack)
		}
		// body 只保留脱敏截断摘要:不再为每条轨迹重复保存整页 XML
		summary := []byte(summarizePTZBody(body))
		for _, item := range response.List.Tracks {
			if len(stage.tracks) >= maxQueryStageTracks {
				return nil, false, fmt.Errorf("PTZ 巡航轨迹条目数超出上限 %d", maxQueryStageTracks)
			}
			stage.tracks[item.ID] = stagedCruiseTrack{item: item, body: summary}
		}
		stage.expected = maxQueryStageExpected(stage.expected, response.SumNum, len(stage.tracks))
		return &stage, len(stage.tracks) >= stage.expected, nil
	}
	return nil, false, nil
}

func cloneQueryResponseStage(source queryResponseStage) queryResponseStage {
	clone := queryResponseStage{cmdType: source.cmdType, expected: source.expected}
	if source.presets != nil {
		clone.presets = make(map[int]manscdp.Preset, len(source.presets))
		for id, item := range source.presets {
			clone.presets[id] = item
		}
	}
	if source.tracks != nil {
		clone.tracks = make(map[int]stagedCruiseTrack, len(source.tracks))
		for id, item := range source.tracks {
			item.body = append([]byte(nil), item.body...)
			clone.tracks[id] = item
		}
	}
	return clone
}

func maxQueryStageExpected(current, reported, received int) int {
	if reported > current {
		current = reported
	}
	if received > current {
		current = received
	}
	return current
}

func (s *Service) finalizeQueryStageTx(ctx context.Context, tx *gorm.DB, operation gbmodels.GbPTZOperation, stage queryResponseStage, now time.Time) error {
	tx = tx.WithContext(ctx)
	switch stage.cmdType {
	case manscdp.CmdPresetQuery:
		ids := sortedPresetIDs(stage.presets)
		for _, id := range ids {
			item := stage.presets[id]
			preset := gbmodels.GbPTZPreset{
				DeviceID: operation.DeviceID, ChannelID: operation.ChannelID, PresetID: item.ID,
				Name: item.Name, Status: gbmodels.PTZPresetActive, LastOperationID: operation.OperationID, UpdatedAt: now,
			}
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "channel_id"}, {Name: "preset_id"}},
				DoUpdates: clause.AssignmentColumns([]string{"device_id", "name", "status", "last_operation_id", "updated_at"}),
			}).Create(&preset).Error; err != nil {
				return err
			}
		}
		missing := tx.Model(&gbmodels.GbPTZPreset{}).Where("channel_id = ?", operation.ChannelID)
		if len(ids) > 0 {
			missing = missing.Where("preset_id NOT IN ?", ids)
		}
		return missing.Update("status", gbmodels.PTZPresetDeleted).Error

	case manscdp.CmdCruiseTrackListQuery:
		ids := sortedCruiseTrackIDs(stage.tracks)
		for _, id := range ids {
			staged := stage.tracks[id]
			item := staged.item
			if item.Enabled == nil {
				enabled := true
				item.Enabled = &enabled
			}
			if err := upsertCruiseTrack(tx, operation, item, staged.body, now, false); err != nil {
				return err
			}
		}
		missing := tx.Where("channel_id = ?", operation.ChannelID)
		if len(ids) > 0 {
			missing = missing.Where("track_id NOT IN ?", ids)
		}
		return missing.Delete(&gbmodels.GbPTZCruiseTrack{}).Error
	}
	return fmt.Errorf("不支持的 PTZ 查询暂存类型: %s", stage.cmdType)
}

func sortedPresetIDs(items map[int]manscdp.Preset) []int {
	ids := make([]int, 0, len(items))
	for id := range items {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

func sortedCruiseTrackIDs(items map[int]stagedCruiseTrack) []int {
	ids := make([]int, 0, len(items))
	for id := range items {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

func (s *Service) discardQueryStage(operationID string) {
	if s == nil || strings.TrimSpace(operationID) == "" {
		return
	}
	s.queryStageMu.Lock()
	delete(s.queryStages, operationID)
	s.queryStageMu.Unlock()
}

func (s *Service) clearQueryStages() {
	if s == nil {
		return
	}
	s.queryStageMu.Lock()
	s.queryStages = make(map[string]queryResponseStage)
	s.queryStageMu.Unlock()
}

func (s *Service) cleanupQueryStages(ctx context.Context, now time.Time) error {
	if s == nil || s.db == nil {
		return nil
	}
	s.queryStageMu.Lock()
	operationIDs := make([]string, 0, len(s.queryStages))
	for operationID := range s.queryStages {
		operationIDs = append(operationIDs, operationID)
	}
	s.queryStageMu.Unlock()
	if len(operationIDs) == 0 {
		return nil
	}

	var operations []gbmodels.GbPTZOperation
	if err := ptzWriter(s.db).WithContext(ctx).
		Select("operation_id", "status", "attempt", "queue_deadline_at", "transport_deadline_at", "deadline_at").
		Where("operation_id IN ?", operationIDs).Find(&operations).Error; err != nil {
		return err
	}
	keep := make(map[string]struct{}, len(operations))
	for _, operation := range operations {
		if queryOperationCanStillRespond(operation, now) {
			keep[operation.OperationID] = struct{}{}
		}
	}
	s.queryStageMu.Lock()
	for _, operationID := range operationIDs {
		if _, ok := keep[operationID]; !ok {
			delete(s.queryStages, operationID)
		}
	}
	s.queryStageMu.Unlock()
	return nil
}

func queryOperationCanStillRespond(operation gbmodels.GbPTZOperation, now time.Time) bool {
	switch operation.Status {
	case gbmodels.PTZOperationUnknown:
		return operation.TransportDeadlineAt != nil && operation.TransportDeadlineAt.After(now)
	case gbmodels.PTZOperationSent:
		return operation.DeadlineAt != nil && operation.DeadlineAt.After(now)
	case gbmodels.PTZOperationQueued:
		if operation.Attempt == 0 {
			return operation.QueueDeadlineAt != nil && operation.QueueDeadlineAt.After(now)
		}
		return operation.TransportDeadlineAt != nil && operation.TransportDeadlineAt.After(now)
	default:
		return false
	}
}

func upsertCruiseTrack(db *gorm.DB, operation gbmodels.GbPTZOperation, item manscdp.CruiseTrack, body []byte, now time.Time, includePoints bool) error {
	detailPayload := map[string]interface{}{"trackId": item.ID, "name": item.Name}
	if includePoints {
		points := item.PointList.Points
		if points == nil {
			points = []manscdp.CruisePoint{}
		}
		detailPayload["sumNum"] = item.SumNum
		detailPayload["cruisePoints"] = points
	}
	detail, _ := json.Marshal(detailPayload)
	track := gbmodels.GbPTZCruiseTrack{
		DeviceID: operation.DeviceID, ChannelID: operation.ChannelID, TrackID: item.ID,
		Name: item.Name, Enabled: item.Enabled, DetailJSON: string(detail), RawSummary: summarizePTZBody(body), UpdatedAt: now,
	}
	updateColumns := []string{"device_id", "name", "enabled", "raw_summary", "updated_at"}
	if includePoints {
		updateColumns = append(updateColumns, "detail_json")
	} else {
		track.LastOperationID = operation.OperationID
		updateColumns = append(updateColumns, "last_operation_id")
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "channel_id"}, {Name: "track_id"}},
		DoUpdates: clause.AssignmentColumns(updateColumns),
	}).Create(&track).Error
}

func newerAcceptedQueryExists(tx *gorm.DB, operation gbmodels.GbPTZOperation) (bool, error) {
	var count int64
	result := tx.Model(&gbmodels.GbPTZOperation{}).
		Where("channel_id = ? AND cmd_type = ? AND action = ? AND payload_json = ? AND id > ? AND status = ?",
			operation.ChannelID, operation.CmdType, operation.Action, operation.PayloadJSON, operation.ID, gbmodels.PTZOperationAccepted).
		Count(&count)
	return count > 0, result.Error
}

func summarizePTZBody(body []byte) string {
	// GB2312/GB18030 设备的通知可能含非法 UTF-8 字节,直接进 DB 会导致
	// utf8mb4 写入失败 —— 先清洗为合法 UTF-8,再脱敏截断
	text := strings.TrimSpace(strings.ToValidUTF8(string(gbtrace.RedactSIP(body)), "�"))
	if len(text) > 4096 {
		// 按 rune 边界截断并再次清洗,避免切断多字节字符产生非法 UTF-8
		runes := []rune(text)
		if len(runes) > 4096 {
			text = string(runes[:4096])
		}
		text = strings.ToValidUTF8(text, "�")
	}
	return text
}
