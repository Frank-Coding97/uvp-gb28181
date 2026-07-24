package ptz

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	gbtrace "uvplatform.cn/uvp-gb28181/app/gb28181/trace"
)

type QueryKind string

const (
	QueryPreset          QueryKind = "preset"
	QueryHomePosition    QueryKind = "home_position"
	QueryCruiseTrackList QueryKind = "cruise_track_list"
	QueryCruiseTrack     QueryKind = "cruise_track"
	QueryPreciseStatus   QueryKind = "precise_status"
)

func (s *Service) Refresh(ctx context.Context, target Target, kind QueryKind, trackID int, idempotencyKey string) (gbmodels.GbPTZOperation, error) {
	command := Command{IdempotencyKey: idempotencyKey, Payload: map[string]interface{}{}}
	switch kind {
	case QueryPreset:
		command.CmdType, command.Action = manscdp.CmdPresetQuery, "refresh_presets"
		command.Build = func(sn int) ([]byte, error) { return manscdp.BuildPresetQuery(target.ChannelCode, sn) }
	case QueryHomePosition:
		command.CmdType, command.Action = manscdp.CmdHomePositionQuery, "refresh_home_position"
		command.ResponseRequired, command.MaxAttempts = true, 3
		command.Build = func(sn int) ([]byte, error) { return manscdp.BuildHomePositionQuery(target.ChannelCode, sn) }
	case QueryCruiseTrackList:
		command.CmdType, command.Action = manscdp.CmdCruiseTrackListQuery, "refresh_cruise_tracks"
		command.Build = func(sn int) ([]byte, error) { return manscdp.BuildCruiseTrackListQuery(target.ChannelCode, sn) }
	case QueryCruiseTrack:
		command.CmdType, command.Action = manscdp.CmdCruiseTrackQuery, "refresh_cruise_track"
		command.Payload["trackId"] = trackID
		command.Build = func(sn int) ([]byte, error) { return manscdp.BuildCruiseTrackQuery(target.ChannelCode, sn, trackID) }
	case QueryPreciseStatus:
		command.CmdType, command.Action = manscdp.CmdPTZPreciseStatusQuery, "refresh_precise_status"
		command.Build = func(sn int) ([]byte, error) { return manscdp.BuildPTZPreciseStatusQuery(target.ChannelCode, sn) }
	default:
		return gbmodels.GbPTZOperation{}, fmt.Errorf("未知 PTZ 查询类型: %q", kind)
	}
	return s.Execute(ctx, target, command)
}

func (s *Service) applyQueryResponse(ctx context.Context, operation gbmodels.GbPTZOperation, callID, cseq string, head manscdp.MessageHead, body []byte) error {
	if head.CmdType == manscdp.CmdHomePositionQuery {
		return s.applyHomePositionQueryResponse(ctx, operation, callID, cseq, body)
	}
	if err := s.persistQueryCache(ctx, operation, head.CmdType, body); err != nil {
		_, _, _ = s.ApplyResponse(ctx, Response{OperationID: operation.OperationID, CallID: callID, CSeq: cseq, SIPStatus: 200, DeviceResult: "ERROR", DeviceError: err.Error()})
		return err
	}
	_, _, err := s.ApplyResponse(ctx, Response{OperationID: operation.OperationID, CallID: callID, CSeq: cseq, SIPStatus: 200, DeviceResult: "OK"})
	return err
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
	if parseErr != nil || response.SN != operation.SN || response.DeviceID != operation.ChannelCode {
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

	mutex := s.lockFor(operation.ChannelID)
	mutex.Lock()
	defer mutex.Unlock()

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

func (s *Service) persistQueryCache(ctx context.Context, operation gbmodels.GbPTZOperation, cmdType string, body []byte) error {
	now := s.now()
	switch cmdType {
	case manscdp.CmdPresetQuery:
		response, err := manscdp.ParsePresetResponse(body)
		if err != nil {
			return err
		}
		return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			for _, item := range response.Presets {
				preset := gbmodels.GbPTZPreset{DeviceID: operation.DeviceID, ChannelID: operation.ChannelID, PresetID: item.ID, Name: item.Name, Status: gbmodels.PTZPresetActive, LastOperationID: operation.OperationID, UpdatedAt: now}
				if err := tx.Clauses(clause.OnConflict{
					Columns:   []clause.Column{{Name: "channel_id"}, {Name: "preset_id"}},
					DoUpdates: clause.AssignmentColumns([]string{"device_id", "name", "status", "last_operation_id", "updated_at"}),
				}).Create(&preset).Error; err != nil {
					return err
				}
			}
			expected := response.SumNum
			if expected <= 0 {
				expected = len(response.Presets)
			}
			var received int64
			if err := tx.Model(&gbmodels.GbPTZPreset{}).
				Where("channel_id = ? AND last_operation_id = ?", operation.ChannelID, operation.OperationID).
				Count(&received).Error; err != nil {
				return err
			}
			if received >= int64(expected) {
				return tx.Model(&gbmodels.GbPTZPreset{}).
					Where("channel_id = ? AND last_operation_id <> ?", operation.ChannelID, operation.OperationID).
					Update("status", gbmodels.PTZPresetDeleted).Error
			}
			return nil
		})
	case manscdp.CmdCruiseTrackListQuery:
		response, err := manscdp.ParseCruiseTrackListResponse(body)
		if err != nil {
			return err
		}
		return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// 只有 SumNum=0 或者已收到完整列表时才能删除缓存中的缺失项。
			// 设备可能分页/截断返回列表;此时保留未出现在本批响应中的旧记录。
			if response.SumNum == 0 {
				if err := tx.Where("channel_id = ?", operation.ChannelID).Delete(&gbmodels.GbPTZCruiseTrack{}).Error; err != nil {
					return err
				}
			} else if response.SumNum == response.List.Num {
				presentIDs := make([]int, 0, len(response.List.Tracks))
				for _, item := range response.List.Tracks {
					presentIDs = append(presentIDs, item.ID)
				}
				deleteQuery := tx.Where("channel_id = ?", operation.ChannelID)
				if len(presentIDs) == 0 {
					deleteQuery = deleteQuery.Where("1 = 1")
				} else {
					deleteQuery = deleteQuery.Where("track_id NOT IN ?", presentIDs)
				}
				if err := deleteQuery.Delete(&gbmodels.GbPTZCruiseTrack{}).Error; err != nil {
					return err
				}
			}
			for _, item := range response.List.Tracks {
				if item.Enabled == nil {
					enabled := true
					item.Enabled = &enabled
				}
				if err := upsertCruiseTrack(tx, operation, item, body, now, false); err != nil {
					return err
				}
			}
			return nil
		})
	case manscdp.CmdCruiseTrackQuery:
		response, err := manscdp.ParseCruiseTrackResponse(body)
		if err != nil {
			return err
		}
		if response.CruiseTrack.Enabled == nil {
			enabled := true
			response.CruiseTrack.Enabled = &enabled
		}
		return upsertCruiseTrack(s.db.WithContext(ctx), operation, response.CruiseTrack, body, now, true)
	case manscdp.CmdPTZPreciseStatusQuery:
		response, err := manscdp.ParsePTZPreciseStatusResponse(body)
		if err != nil {
			return err
		}
		_, err = s.ApplyPreciseNotify(ctx, PreciseNotify{
			DeviceID: operation.DeviceID, DeviceCode: operation.DeviceCode, ChannelID: operation.ChannelID, ChannelCode: operation.ChannelCode,
			SN: response.SN, Pan: response.Pan, Tilt: response.Tilt, Zoom: response.Zoom, Focus: response.Focus, Iris: response.Iris,
			ReceivedAt: now, DedupeKey: operation.OperationID, RawSummary: summarizePTZBody(body),
		})
		return err
	default:
		return fmt.Errorf("不支持的 PTZ 查询响应: %s", cmdType)
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
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "channel_id"}, {Name: "track_id"}},
		DoUpdates: clause.AssignmentColumns(updateColumns),
	}).Create(&track).Error
}

func summarizePTZBody(body []byte) string {
	text := strings.TrimSpace(string(gbtrace.RedactSIP(body)))
	if len(text) > 4096 {
		return text[:4096]
	}
	return text
}
