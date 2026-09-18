package ptz

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

// RefreshStorageCards 发起一次「存储卡状态查询」（A.2.4.14）。
//
// 与 RefreshHomePosition 同族，刻意不走 Refresh 的 QueryKind 分支：
// 存储卡是**设备级**查询（标准的 DeviceID 是"查询目标设备编码"），
// 而 QueryKind 那套是给"通道级云台资源"用的，混在一起会让调用方以为
// 传 channelCode 才是对的。
//
// ⛔ 这里**不为 2022 版本设门禁**。理由同 HomePositionQuery：
// profile 只是"登记的说法"，不是事实；一台被登记成 2016、实际按 2022 应答的
// 设备，发出这一帧是平台唯一能发现它的手段。真要不发，是调用方（比如
// 自动对账）的策略问题，不是报文层的职责。
func (s *Service) RefreshStorageCards(ctx context.Context, target Target, actorID, actorDeptID uint, idempotencyKey string) (gbmodels.GbPTZOperation, error) {
	profile := target.Profile
	if profile.Version == "" {
		profile = protocol.ProfileFor(protocol.Version2016)
	}
	return s.Execute(ctx, target, Command{
		CmdType:          manscdp.CmdSDCardStatus,
		Action:           "refresh_storage_cards",
		IdempotencyKey:   idempotencyKey,
		Payload:          map[string]interface{}{},
		ResponseRequired: true,
		MaxAttempts:      3,
		ActorID:          actorID,
		ActorDeptID:      actorDeptID,
		Build: func(sn int) ([]byte, error) {
			return manscdp.BuildSDCardStatusQueryWithProfile(profile, target.ChannelCode, sn)
		},
		Profile: profile,
	})
}

// applyStorageCardResponse 处理设备回来的 SDCardStatus 应答。
//
// 与 QueryPreset 那族不同，这里**不走 queryStage 聚合**：A.2.6.16 的应答
// 本身就是一个完整列表（Item maxOccurs=8），标准没有给它定义"分批多响应"
// 的语义（附录 M 点名的三类是目录查询响应、文件查询响应、订阅通知）。
// 所以一次应答即终态，不需要等凑齐 SumNum。
func (s *Service) applyStorageCardResponse(ctx context.Context, operation gbmodels.GbPTZOperation, callID, cseq string, body []byte) error {
	status, parseErr := manscdp.ParseSDCardStatusResponseFor(body, manscdp.SDCardStatusExpectation{
		SN:       operation.SN,
		DeviceID: operationTargetCode(operation),
	})
	if parseErr != nil {
		return s.applyRejectedPTZResponse(ctx, operation, callID, cseq, "ERROR", ptzErrorProtocolInvalid, parseErr.Error())
	}
	completedAt := s.now()
	return schedulerTransaction(ctx, s.db, func(tx *gorm.DB) error {
		applied, err := applyPTZResponseTransition(tx, operation, callID, cseq, ptzResponseTransition{
			Status: gbmodels.PTZOperationAccepted, DeviceResult: "OK",
		}, completedAt)
		if err != nil {
			return err
		}
		if !applied {
			return fmt.Errorf("SDCardStatus operation 已完成或已超时")
		}
		return s.persistStorageCardsWithDB(ctx, tx, operation, status, body)
	})
}

// persistStorageCardsWithDB 把一次查询结果落成"每张卡一行"。
//
// 三步，顺序不能换：
//  1. **迟到应答保护**：库里已有更新的查询（source_operation_seq 更大）就整批放弃。
//     没有这一步，一个超时后迟到的旧应答会把新数据覆盖回去。
//  2. **逐卡 upsert**：按 (device_id, target_code, card_id) 定位；已存在的行
//     同样受 seq 保护，比它旧的写入不动它。
//  3. **清理本轮没再出现的卡**：设备从上一次查到 2 张变成这次 1 张时，第 2 张
//     必须消失。⛔ 删的条件是 `source_operation_seq < 本次`，不是"不在本次列表里"
//     —— 后者会把另一个 target_code 行、或更晚一次查询写进来的行一起误删。
func (s *Service) persistStorageCardsWithDB(ctx context.Context, db *gorm.DB, operation gbmodels.GbPTZOperation, status manscdp.SDCardStatus, body []byte) error {
	if operation.ID == 0 || operation.DeviceID == 0 {
		return fmt.Errorf("SDCardStatus operation 缺少可靠序列或设备标识")
	}
	targetCode := operationTargetCode(operation)
	if targetCode == "" {
		return fmt.Errorf("SDCardStatus operation 缺少目标编码")
	}
	now := s.now()
	summary := summarizePTZBody(body)
	db = db.WithContext(ctx)

	var latest gbmodels.GbDeviceStorageCard
	result := db.Where("device_id = ? AND target_code = ?", operation.DeviceID, targetCode).
		Order("source_operation_seq DESC").Limit(1).Find(&latest)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 && latest.SourceOperationSeq > operation.ID {
		return nil
	}

	seen := make([]int, 0, len(status.Items))
	for _, item := range status.Items {
		seen = append(seen, item.ID)
		if err := upsertStorageCard(db, operation, targetCode, item, summary, now); err != nil {
			return err
		}
	}

	cleanup := db.Where("device_id = ? AND target_code = ? AND source_operation_seq < ?",
		operation.DeviceID, targetCode, operation.ID)
	if len(seen) > 0 {
		cleanup = cleanup.Where("card_id NOT IN ?", seen)
	}
	return cleanup.Delete(&gbmodels.GbDeviceStorageCard{}).Error
}

func upsertStorageCard(db *gorm.DB, operation gbmodels.GbPTZOperation, targetCode string, item manscdp.SDCardItem, summary string, now time.Time) error {
	values := map[string]interface{}{
		"hdd_name":             item.HddName,
		"status":               mapStorageCardState(item.Status),
		"format_progress":      item.FormatProgress,
		"capacity_mb":          item.Capacity,
		"free_space_mb":        item.FreeSpace,
		"source_operation_seq": operation.ID,
		"source_sn":            operation.SN,
		"source_operation_id":  operation.OperationID,
		"observed_at":          now,
		"raw_summary":          summary,
		"updated_at":           now,
	}

	updateNewer := func() (*gorm.DB, error) {
		result := db.Model(&gbmodels.GbDeviceStorageCard{}).
			Where("device_id = ? AND target_code = ? AND card_id = ? AND source_operation_seq <= ?",
				operation.DeviceID, targetCode, item.ID, operation.ID).
			Updates(values)
		return result, result.Error
	}
	if result, err := updateNewer(); err != nil {
		return err
	} else if result.RowsAffected == 1 {
		return nil
	}

	var current gbmodels.GbDeviceStorageCard
	found := db.Where("device_id = ? AND target_code = ? AND card_id = ?",
		operation.DeviceID, targetCode, item.ID).Limit(1).Find(&current)
	if found.Error != nil {
		return found.Error
	}
	if found.RowsAffected == 1 {
		if current.SourceOperationSeq > operation.ID {
			return nil
		}
		if retried, err := updateNewer(); err != nil {
			return err
		} else if retried.RowsAffected == 1 {
			return nil
		}
		return fmt.Errorf("SDCardStatus 缓存 CAS 未应用 operation %d", operation.ID)
	}

	values["device_id"] = operation.DeviceID
	values["target_code"] = targetCode
	values["card_id"] = item.ID
	values["created_at"] = now
	createErr := db.Model(&gbmodels.GbDeviceStorageCard{}).Create(values).Error
	if createErr == nil {
		return nil
	}
	// 并发下可能刚被另一个应答插进去，重试一次受 seq 保护的更新，
	// 不去解析方言相关的重复键错误。
	if retried, err := updateNewer(); err != nil {
		return err
	} else if retried.RowsAffected == 1 {
		return nil
	}
	if again := db.Where("device_id = ? AND target_code = ? AND card_id = ?",
		operation.DeviceID, targetCode, item.ID).Limit(1).Find(&current); again.Error != nil {
		return again.Error
	} else if again.RowsAffected == 1 && current.SourceOperationSeq >= operation.ID {
		return nil
	}
	return createErr
}

// mapStorageCardState 把报文层的状态映射到落库枚举。未知取值落到 unknown，
// 而不是 error —— 见 models.StorageCardStateUnknown 的注释。
func mapStorageCardState(state manscdp.SDCardState) gbmodels.StorageCardState {
	switch state {
	case manscdp.SDCardStateOK:
		return gbmodels.StorageCardStateOK
	case manscdp.SDCardStateFormatting:
		return gbmodels.StorageCardStateFormatting
	case manscdp.SDCardStateUnformatted:
		return gbmodels.StorageCardStateUnformatted
	case manscdp.SDCardStateIdle:
		return gbmodels.StorageCardStateIdle
	case manscdp.SDCardStateError:
		return gbmodels.StorageCardStateError
	default:
		return gbmodels.StorageCardStateUnknown
	}
}
