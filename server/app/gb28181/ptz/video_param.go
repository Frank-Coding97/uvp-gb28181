package ptz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

const (
	actionRefreshVideoParams = "refresh_video_params"
	actionApplyVideoParams   = "apply_video_params"

	// ptzErrorVideoParamReconcileMismatch 标在**对账子 operation** 上，
	// 表示"设备已接受命令，但回读值与下发值不一致"。⛔ 它不是失败：
	// 典型来源是手机的摄像头能力边界（下发 1080P、实际 720P），设备没做错。
	ptzErrorVideoParamReconcileMismatch = "VIDEO_PARAM_RECONCILE_MISMATCH"

	videoParamReconcileAttempts = 3
)

// ReadVideoParams 发起一次「视频参数属性」读取（A.2.4.7 ConfigDownload）。
//
// ⛔ 这里**不为 2022 设门禁**。`ConfigDownload` 是 2016 就有的命令（只是 2016 的
// ConfigType 只有 4 个取值），所以对 2016 设备发这一帧**不会超时**：设备会正常回
// 一个不带 `VideoParamAttribute` 元素的 OK 应答，正好落到 manscdp 的
// ConfigErrorTypeAbsent —— 那就是「设备不支持该配置类型」最可靠的判据。
// 详见 docs/gb28181-2022-video-param-attribute-panel.md §十。
func (s *Service) ReadVideoParams(ctx context.Context, target Target, actorID, actorDeptID uint, idempotencyKey string) (gbmodels.GbPTZOperation, error) {
	profile := target.Profile
	if profile.Version == "" {
		profile = protocol.ProfileFor(protocol.Version2016)
	}
	configTypes := []string{manscdp.ConfigTypeVideoParamAttribute}
	return s.Execute(ctx, target, Command{
		CmdType:          manscdp.CmdConfigDownload,
		Action:           actionRefreshVideoParams,
		IdempotencyKey:   idempotencyKey,
		Payload:          map[string]interface{}{"configTypes": configTypes},
		ResponseRequired: true,
		MaxAttempts:      3,
		ActorID:          actorID,
		ActorDeptID:      actorDeptID,
		Build: func(sn int) ([]byte, error) {
			return manscdp.BuildConfigDownloadQueryWithProfile(profile, target.ChannelCode, sn, configTypes)
		},
		Profile: profile,
	})
}

// ApplyVideoParams 下发「视频参数属性」（A.2.3.2.5 DeviceConfig）。
//
// ⛔ 与读相反，写入**有副作用**，所以：
//  1. 参数在发出前按附录 G 严格校验（manscdp.ValidateVideoParamItems）。
//     平台自己发出的取值乱来，对端会静默当 0 处理，而这种错在回读对账里
//     只表现为"设备没照做"，归因成本极高。
//  2. **不做 profile 门禁**，但**必须回读对账**：`VideoParamAttribute` 是 2022 新增，
//     2016 设备不认识它，而标准没有定义它收到之后该怎么回应 —— 三种可能回应里
//     "宽松解析器回 Result=OK 却什么都没改"是最危险的那一种（假成功）。
//     真相由自动回读暴露，不由 profile 断言：被误登记成 2016 的真 2022 设备，
//     不试一次就永远用不了这功能（同 RefreshStorageCards 的既有口径）。
func (s *Service) ApplyVideoParams(ctx context.Context, target Target, items []manscdp.VideoParamItem, actorID, actorDeptID uint, idempotencyKey string) (gbmodels.GbPTZOperation, error) {
	if err := manscdp.ValidateVideoParamItems(items); err != nil {
		return gbmodels.GbPTZOperation{}, fmt.Errorf("视频参数不合法: %w", err)
	}
	profile := target.Profile
	if profile.Version == "" {
		profile = protocol.ProfileFor(protocol.Version2016)
	}
	return s.Execute(ctx, target, Command{
		CmdType:          manscdp.CmdDeviceConfig,
		Action:           actionApplyVideoParams,
		IdempotencyKey:   idempotencyKey,
		Payload:          map[string]interface{}{"items": items},
		ResponseRequired: true,
		MaxAttempts:      3,
		ActorID:          actorID,
		ActorDeptID:      actorDeptID,
		Build: func(sn int) ([]byte, error) {
			return manscdp.BuildVideoParamAttributeConfigWithProfile(profile, target.ChannelCode, sn, items)
		},
		Profile: profile,
	})
}

// applyConfigDownloadResponse 处理设备回来的 ConfigDownload 应答（读）。
func (s *Service) applyConfigDownloadResponse(ctx context.Context, operation gbmodels.GbPTZOperation, callID, cseq string, body []byte) error {
	result, parseErr := manscdp.ParseConfigDownloadResponseFor(body, manscdp.ConfigDownloadExpectation{
		SN:       operation.SN,
		DeviceID: operationTargetCode(operation),
	}, manscdp.ConfigTypeVideoParamAttribute)
	if parseErr != nil {
		// ⭐ 「应答合法但没带这个配置类型」是一种**结论**，不是解析失败：
		// 它等价于"设备不支持 VideoParamAttribute"（规格 §十④ 最可靠的那条判据）。
		// 落成 accepted + response_has_data=false，与 home position 对
		// "设备回了但没数据"的表达完全一致。
		var configErr *manscdp.ConfigDownloadError
		if errors.As(parseErr, &configErr) && configErr.Code == manscdp.ConfigErrorTypeAbsent {
			return s.applyVideoParamTypeAbsent(ctx, operation, callID, cseq, configErr.Error())
		}
		return s.applyRejectedPTZResponse(ctx, operation, callID, cseq, "ERROR", ptzErrorProtocolInvalid, parseErr.Error())
	}

	completedAt := s.now()
	hasData := result.HasVideoParamAttribute()
	rawSummary := summarizePTZBody(body)
	items := result.VideoParamItems()
	return schedulerTransaction(ctx, s.db, func(tx *gorm.DB) error {
		applied, err := applyPTZResponseTransition(tx, operation, callID, cseq, ptzResponseTransition{
			Status: gbmodels.PTZOperationAccepted, DeviceResult: "OK", ResponseHasData: &hasData,
		}, completedAt)
		if err != nil || !applied {
			return err
		}
		if err := s.persistVideoParamsWithDB(ctx, tx, operation, items, rawSummary); err != nil {
			return err
		}
		return markVideoParamReconcileMismatch(tx, operation, items)
	})
}

// applyVideoParamTypeAbsent 处理"设备回了应答但没带该配置类型"。
//
// ⛔ 刻意**不清理**已回读到的行：`type_absent` 说的是"设备这次没给这个类型"，
// 而 `Num="0"` 说的是"设备明确回了个空配置"。前者不清、后者清 —— 混为一谈会把
// 一次偶发的漏带元素变成"删掉好数据"。
// 前端据 operation 的 response_has_data=false 展示「设备未返回此配置类型」。
func (s *Service) applyVideoParamTypeAbsent(ctx context.Context, operation gbmodels.GbPTZOperation, callID, cseq, message string) error {
	completedAt := s.now()
	hasData := false
	return schedulerTransaction(ctx, s.db, func(tx *gorm.DB) error {
		_, err := applyPTZResponseTransition(tx, operation, callID, cseq, ptzResponseTransition{
			Status:          gbmodels.PTZOperationAccepted,
			DeviceResult:    "OK",
			ResponseHasData: &hasData,
			DeviceError:     message,
		}, completedAt)
		return err
	})
}

// applyDeviceConfigResponse 处理 DeviceConfig 的写入应答（A.2.6.8）。
//
// ⛔ ack 不是终态：`Result=OK` 的语义只是"收到并接受"，应答里既没有回显、
// 也说明不了值有没有生效。所以这里在同一个事务里追加一条 **ConfigDownload
// 对账子 operation**，由它把回读值落库并与本次下发的值逐格比对。
// 这就是左栏那句「本面板不提供未接入的伪控制滑杆」的可执行版本。
func (s *Service) applyDeviceConfigResponse(ctx context.Context, operation gbmodels.GbPTZOperation, callID, cseq string, body []byte) error {
	ack, parseErr := manscdp.ParseDeviceConfigResponseFor(body, manscdp.ConfigDownloadExpectation{
		SN:       operation.SN,
		DeviceID: operationTargetCode(operation),
	})
	if parseErr != nil {
		return s.applyRejectedPTZResponse(ctx, operation, callID, cseq, "ERROR", ptzErrorProtocolInvalid, parseErr.Error())
	}
	if !ack.Accepted() {
		return s.applyRejectedPTZResponse(ctx, operation, callID, cseq, ack.Result, ptzErrorDeviceRejected, "设备拒绝该视频参数配置")
	}

	completedAt := s.now()
	return schedulerTransaction(ctx, s.db, func(tx *gorm.DB) error {
		applied, err := applyPTZResponseTransition(tx, operation, callID, cseq, ptzResponseTransition{
			Status: gbmodels.PTZOperationAccepted, DeviceResult: "OK",
		}, completedAt)
		if err != nil || !applied {
			return err
		}
		return s.createVideoParamReconcile(tx, operation, completedAt)
	})
}

// createVideoParamReconcile 在同一个事务里追加一条回读对账子 operation。
//
// ⛔ 为什么在同一事务里而不是"ack 之后再发一次"：ack 落库与对账排队要么一起成功、
// 要么一起没有，否则会出现"父 operation 显示已接受、但回读永远不会发生"的哑状态。
// 重试与超时预算由 scheduler 统一承担（照 createHomePositionReconcile 的既有做法）。
//
// ⛔ 这里**不做版本/能力门禁**。规格 §十③ 的结论是门禁只用于"平台自己决定要发"的
// 写入动作；对账是一次**读**，最坏结果是超时烧一个 SN，而它恰好是"设备到底认不认
// 这个配置类型"的唯一可靠判据 —— 挡掉它等于把判定手段也一起挡掉。
func (s *Service) createVideoParamReconcile(tx *gorm.DB, parent gbmodels.GbPTZOperation, createdAt time.Time) error {
	if parent.CmdType != manscdp.CmdDeviceConfig {
		return nil
	}
	configTypes := []string{manscdp.ConfigTypeVideoParamAttribute}
	payloadJSON, err := canonicalPayload(map[string]interface{}{
		"configTypes":        configTypes,
		"triggerOperationId": parent.OperationID,
	})
	if err != nil {
		return err
	}
	operationID := uuid.NewString()
	values := map[string]interface{}{
		"operation_id": operationID, "idempotency_key": "video-param-reconcile:" + parent.OperationID,
		"device_id": parent.DeviceID, "device_code": parent.DeviceCode,
		"channel_id": parent.ChannelID, "channel_code": parent.ChannelCode,
		"cmd_type": manscdp.CmdConfigDownload, "action": actionRefreshVideoParams,
		"profile_version": parent.ProfileVersion, "profile_charset": parent.ProfileCharset,
		"target_scope": parent.TargetScope, "target_code": parent.TargetCode,
		"payload_json": payloadJSON, "sn": s.nextSN(), "status": gbmodels.PTZOperationQueued,
		"attempt": 0, "response_required": true, "max_attempts": videoParamReconcileAttempts,
		"actor_id": parent.ActorID, "actor_dept_id": parent.ActorDeptID,
		"trigger_operation_id": parent.OperationID,
		"queue_deadline_at":    createdAt.Add(5 * time.Second), "created_at": createdAt,
	}
	if err := tx.Model(&gbmodels.GbPTZOperation{}).Create(values).Error; err != nil {
		return err
	}
	result := tx.Model(&gbmodels.GbPTZOperation{}).
		Where("id = ? AND status = ? AND reconcile_operation_id IS NULL", parent.ID, gbmodels.PTZOperationAccepted).
		Update("reconcile_operation_id", operationID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("关联视频参数自动对账 operation 失败")
	}
	return nil
}

// persistVideoParamsWithDB 把一次回读结果落成"每码流一行"。
//
// 三步，顺序不能换（同 persistStorageCardsWithDB）：
//  1. **迟到应答保护**：库里已有更新的回读（source_operation_seq 更大）就整批放弃。
//  2. **逐条 upsert**：按 (device_id, target_code, stream_number) 定位；已存在的行
//     同样受 seq 保护，比它旧的写入不动它。
//  3. **清理本轮没再出现的码流**：设备从两条码流变成一条时，第二条必须消失。
//     ⛔ 删的条件是 `source_operation_seq < 本次`，不是"不在本次列表里" ——
//     后者会把另一个 target_code 行、或更晚一次回读写进来的行一起误删。
func (s *Service) persistVideoParamsWithDB(ctx context.Context, db *gorm.DB, operation gbmodels.GbPTZOperation, items []manscdp.VideoParamItem, summary string) error {
	if operation.ID == 0 || operation.DeviceID == 0 {
		return fmt.Errorf("ConfigDownload operation 缺少可靠序列或设备标识")
	}
	targetCode := operationTargetCode(operation)
	if targetCode == "" {
		return fmt.Errorf("ConfigDownload operation 缺少目标编码")
	}
	now := s.now()
	db = db.WithContext(ctx)

	var latest gbmodels.GbDeviceVideoParam
	result := db.Where("device_id = ? AND target_code = ?", operation.DeviceID, targetCode).
		Order("source_operation_seq DESC").Limit(1).Find(&latest)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 && latest.SourceOperationSeq > operation.ID {
		return nil
	}

	seen := make([]int, 0, len(items))
	for _, item := range items {
		seen = append(seen, item.StreamNumber)
		if err := upsertVideoParam(db, operation, targetCode, item, summary, now); err != nil {
			return err
		}
	}

	cleanup := db.Where("device_id = ? AND target_code = ? AND source_operation_seq < ?",
		operation.DeviceID, targetCode, operation.ID)
	if len(seen) > 0 {
		cleanup = cleanup.Where("stream_number NOT IN ?", seen)
	}
	return cleanup.Delete(&gbmodels.GbDeviceVideoParam{}).Error
}

func upsertVideoParam(db *gorm.DB, operation gbmodels.GbPTZOperation, targetCode string, item manscdp.VideoParamItem, summary string, now time.Time) error {
	values := map[string]interface{}{
		"video_format":         item.VideoFormat,
		"resolution":           item.Resolution,
		"frame_rate":           item.FrameRate,
		"bit_rate_type":        item.BitRateType,
		"video_bit_rate":       item.VideoBitRate,
		"source_operation_seq": operation.ID,
		"source_sn":            operation.SN,
		"source_operation_id":  operation.OperationID,
		"observed_at":          now,
		"raw_summary":          summary,
		"updated_at":           now,
	}

	updateNewer := func() (*gorm.DB, error) {
		result := db.Model(&gbmodels.GbDeviceVideoParam{}).
			Where("device_id = ? AND target_code = ? AND stream_number = ? AND source_operation_seq <= ?",
				operation.DeviceID, targetCode, item.StreamNumber, operation.ID).
			Updates(values)
		return result, result.Error
	}
	if result, err := updateNewer(); err != nil {
		return err
	} else if result.RowsAffected == 1 {
		return nil
	}

	var current gbmodels.GbDeviceVideoParam
	found := db.Where("device_id = ? AND target_code = ? AND stream_number = ?",
		operation.DeviceID, targetCode, item.StreamNumber).Limit(1).Find(&current)
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
		return fmt.Errorf("ConfigDownload 缓存 CAS 未应用 operation %d", operation.ID)
	}

	values["device_id"] = operation.DeviceID
	values["target_code"] = targetCode
	values["stream_number"] = item.StreamNumber
	values["created_at"] = now
	createErr := db.Model(&gbmodels.GbDeviceVideoParam{}).Create(values).Error
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
	if again := db.Where("device_id = ? AND target_code = ? AND stream_number = ?",
		operation.DeviceID, targetCode, item.StreamNumber).Limit(1).Find(&current); again.Error != nil {
		return again.Error
	} else if again.RowsAffected == 1 && current.SourceOperationSeq >= operation.ID {
		return nil
	}
	return createErr
}

// videoParamDiff 描述一个码流上一个字段的下发值/回读值差异。
type videoParamDiff struct {
	StreamNumber int
	// Field 用标准元素名（VideoFormat / Resolution / …），不掺展示文案：
	// 人读串只在前端做，这样后端不承担翻译责任，也不会产生两套字段名。
	Field  string
	Wanted string
	Actual string
}

// decodeVideoParamPayload 取回一条 DeviceConfig operation 里"我们要求的值"。
func decodeVideoParamPayload(payloadJSON string) ([]manscdp.VideoParamItem, error) {
	var payload struct {
		Items []manscdp.VideoParamItem `json:"items"`
	}
	if strings.TrimSpace(payloadJSON) == "" {
		return nil, nil
	}
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return nil, fmt.Errorf("解析视频参数下发 operation 参数失败: %w", err)
	}
	return payload.Items, nil
}

// diffVideoParamItems 逐码流、逐字段比对下发值与回读值。
//
// 返回空切片 = 完全一致（对账通过）。
// ⛔ 这是纯函数，行为可单测：对账是本卡的核心承诺，不能只靠"看日志对不对"。
func diffVideoParamItems(want, actual []manscdp.VideoParamItem) []videoParamDiff {
	actualByStream := make(map[int]manscdp.VideoParamItem, len(actual))
	for _, item := range actual {
		actualByStream[item.StreamNumber] = item
	}
	diffs := make([]videoParamDiff, 0)
	for _, wanted := range want {
		got, present := actualByStream[wanted.StreamNumber]
		if !present {
			// 设备没回这个码流 —— 这是最值得暴露的一种差异：命令被接受了，
			// 但那个码流的配置根本没出现在回读里。
			diffs = append(diffs, videoParamDiff{
				StreamNumber: wanted.StreamNumber, Field: "StreamNumber",
				Wanted: "已下发", Actual: "回读中不存在",
			})
			continue
		}
		for _, field := range []struct {
			name   string
			wanted string
			actual string
		}{
			{"VideoFormat", wanted.VideoFormat, got.VideoFormat},
			{"Resolution", wanted.Resolution, got.Resolution},
			{"FrameRate", wanted.FrameRate, got.FrameRate},
			{"BitRateType", wanted.BitRateType, got.BitRateType},
			{"VideoBitRate", bitRateText(wanted.VideoBitRate), bitRateText(got.VideoBitRate)},
		} {
			if field.wanted == field.actual {
				continue
			}
			diffs = append(diffs, videoParamDiff{
				StreamNumber: wanted.StreamNumber, Field: field.name,
				Wanted: field.wanted, Actual: field.actual,
			})
		}
	}
	return diffs
}

// bitRateText 把条件必选字段渲染成可比较的文本。
// ⛔ "缺席"必须与"值为 0"区分开：前者是 VBR 下的正常形态，
// 后者是设备真的报了个 0。两者文本不同，否则对账会把它们判成一致。
func bitRateText(value *string) string {
	if value == nil {
		return "(未提供)"
	}
	return strings.TrimSpace(*value)
}

// formatVideoParamDiff 生成给人看的差异摘要（写进 error_message）。
func formatVideoParamDiff(diffs []videoParamDiff) string {
	parts := make([]string, 0, len(diffs))
	for _, diff := range diffs {
		parts = append(parts, fmt.Sprintf("码流 %d 的 %s: 下发 %q 回读 %q",
			diff.StreamNumber, diff.Field, diff.Wanted, diff.Actual))
	}
	if len(parts) == 0 {
		return ""
	}
	return "设备已接受命令，但回读值不一致 —— " + strings.Join(parts, "；")
}

// ActionRefreshVideoParams 是"读取设备参数"对应的 operation.action。
// 控制器用它筛出"最近一次回读"，从而把下面那套状态判据透出给前端。
// ⛔ 用常量而不是让控制器写字符串：这个值同时出现在服务层的写入点与查询条件里，
// 写错一处就会得到"永远查不到最近一次回读"的静默空判。
const ActionRefreshVideoParams = actionRefreshVideoParams

// 视频参数面板的四态（+ 两个过渡态）。判据单点定义在这里，由
// DeriveVideoParamReconcileState 从「最近一次回读 operation」推导出来。
//
// ⛔ 为什么必须有这套判据而不是让前端看列表空不空：`Result=OK` 只表示"收到并接受"，
// 列表为空既可能是"设备不认识这个配置类型"（type_absent，能力问题），
// 也可能是"还没问过"（never_read，操作问题）—— 两者的下一步动作完全不同。
const (
	// VideoParamStateNeverRead 从未回读过，列表里的东西无从谈起。
	VideoParamStateNeverRead = "never_read"
	// VideoParamStatePending 回读还在飞（queued/sent），等它落地。
	VideoParamStatePending = "pending"
	// VideoParamStateReadOK 回读成功且设备带了该配置类型。
	VideoParamStateReadOK = "read_ok"
	// VideoParamStateTypeAbsent 设备回了 OK 但**没带** VideoParamAttribute 元素 ——
	// 等价于"设备不支持该配置类型"（2016 设备与部分厂商实现的典型形态）。
	VideoParamStateTypeAbsent = "type_absent"
	// VideoParamStateMismatch 下发已被接受，但回读值与下发值不一致。
	// ⛔ 这不是失败：典型来源是设备能力边界（下发 1080P、实际 720P）。
	VideoParamStateMismatch = "mismatch"
	// VideoParamStateFailed 回读被拒/超时/报文不合法。与上面几种区分开：
	// 这种是"没拿到答案"，不是"拿到了答案说不行"。
	VideoParamStateFailed = "failed"
)

// VideoParamReconcileState 是透出给前端的"最近一次回读结论"。
type VideoParamReconcileState struct {
	State           string `json:"state"`
	OperationID     string `json:"operationId,omitempty"`
	Status          string `json:"status,omitempty"`
	ResponseHasData *bool  `json:"responseHasData,omitempty"`
	ErrorCode       string `json:"errorCode,omitempty"`
	ErrorMessage    string `json:"errorMessage,omitempty"`
	// DeviceError 是"设备侧原话"：type_absent 的判定理由（设备没带那个元素）走这里，
	// error_message 则用于对账不一致的逐格差异。两者都要透出 ——
	// 前端要能说清"为什么这一格是空的"，而不只是"空着"。
	DeviceError string     `json:"deviceError,omitempty"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
	// DerivedFromApply 说明这条回读是不是由一次下发派生出来的对账。
	// 手动点「读取设备参数」触发的回读没有可比的意图，不参与 mismatch 判定。
	DerivedFromApply bool `json:"derivedFromApply"`
}

// DeriveVideoParamReconcileState 是纯函数：给定"最近一次回读 operation"（可为 nil）
// 推导面板状态。latest 为 nil = 从未回读。
//
// ⛔ 判定顺序不能换：mismatch 的前提是 status=accepted（设备确实接受了命令），
// 所以先按 status 分流，再在 accepted 里看 error_code 与 response_has_data。
func DeriveVideoParamReconcileState(latest *gbmodels.GbPTZOperation) VideoParamReconcileState {
	if latest == nil {
		return VideoParamReconcileState{State: VideoParamStateNeverRead}
	}
	state := VideoParamReconcileState{
		OperationID:      latest.OperationID,
		Status:           string(latest.Status),
		ResponseHasData:  latest.ResponseHasData,
		ErrorCode:        latest.ErrorCode,
		ErrorMessage:     latest.ErrorMessage,
		DeviceError:      latest.DeviceError,
		CompletedAt:      latest.CompletedAt,
		DerivedFromApply: latest.TriggerOperationID != nil && strings.TrimSpace(*latest.TriggerOperationID) != "",
	}
	switch latest.Status {
	case gbmodels.PTZOperationQueued, gbmodels.PTZOperationSent, gbmodels.PTZOperationUnknown, "":
		state.State = VideoParamStatePending
	case gbmodels.PTZOperationRejected, gbmodels.PTZOperationTimeout, gbmodels.PTZOperationCancelled:
		state.State = VideoParamStateFailed
	case gbmodels.PTZOperationAccepted:
		switch {
		case latest.ErrorCode == ptzErrorVideoParamReconcileMismatch:
			state.State = VideoParamStateMismatch
		case latest.ResponseHasData != nil && !*latest.ResponseHasData:
			state.State = VideoParamStateTypeAbsent
		default:
			state.State = VideoParamStateReadOK
		}
	default:
		state.State = VideoParamStatePending
	}
	return state
}

// markVideoParamReconcileMismatch 在**对账子 operation** 上标出值不一致。
//
// 只在"这条 ConfigDownload 是由某条 DeviceConfig 派生出来的"时判定 ——
// 手动点「读取设备参数」触发的回读没有可比的意图，不该被判成 mismatch。
// 照 homePositionReconcileMismatch 的既有做法：结论标在子 operation 上，
// 父 operation 保持 accepted（设备确实接受了命令，这是事实）。
func markVideoParamReconcileMismatch(tx *gorm.DB, query gbmodels.GbPTZOperation, actual []manscdp.VideoParamItem) error {
	if query.CmdType != manscdp.CmdConfigDownload || query.TriggerOperationID == nil ||
		strings.TrimSpace(*query.TriggerOperationID) == "" {
		return nil
	}
	var parent gbmodels.GbPTZOperation
	result := tx.Where("operation_id = ?", strings.TrimSpace(*query.TriggerOperationID)).Limit(1).Find(&parent)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 || parent.CmdType != manscdp.CmdDeviceConfig {
		return nil
	}
	want, err := decodeVideoParamPayload(parent.PayloadJSON)
	if err != nil || len(want) == 0 {
		return err
	}
	diffs := diffVideoParamItems(want, actual)
	if len(diffs) == 0 {
		return nil
	}
	return tx.Model(&gbmodels.GbPTZOperation{}).
		Where("id = ? AND status = ?", query.ID, gbmodels.PTZOperationAccepted).
		Updates(map[string]interface{}{
			"error_code":    ptzErrorVideoParamReconcileMismatch,
			"error_message": formatVideoParamDiff(diffs),
		}).Error
}
