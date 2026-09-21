package ptz

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

// actionTargetTrack 是目标跟踪操作的 action 名。
//
// 落库后它会作为 gb_ptz_operation.action 出现，所以这是一个**对外可见的契约名**：
// 改名等于让历史记录断档，别随手改。
//
// ⛔ 它**刻意没有**进 `controllers.maintenanceOperationActions`（设备维护记录白名单）：
// 那份名单的既定口径是「**破坏性/中断性**动作共用 gb28181:device:maintenance:view
// 这一个只读权限」（见 device_maintenance.go 的注释）。目标跟踪是**可逆**的普通控制，
// 塞进去等于让"能看设备维护记录"的人顺带看到谁在跟踪哪个目标，
// 而那个权限码的语义并不是这个。审计该看 gb_ptz_operation（按通道可查），
// 最近一次意图看 gb_device_target_track。
const actionTargetTrack = "target_track"

// TargetTrackRequest 是一次目标跟踪下发的业务参数（标准元素见 A.2.3.1.14）。
type TargetTrackRequest struct {
	Mode      manscdp.TargetTrackMode
	DeviceID2 string
	Area      *manscdp.TargetTrackArea
}

// TrackTarget 下发「目标跟踪控制命令」（GB/T 28181-2022 A.2.3.1.14）。
//
// ⛔⛔ 这条命令**无应答**，是本函数全部取舍的来源：
//
//   - **9.3.1 d)** 把"目标跟踪"与云台控制 / 远程启动 / 强制关键帧 / 拉框放大缩小 /
//     PTZ 精准控制 / 存储卡格式化列在同一句 ——「目标设备**不发送应答命令**」；
//     **表 1 序号 13** 的应答命令章节也写作「（无）」⇒ `ResponseRequired: false`。
//     写成 true 的后果**不是"多等一会儿"，而是把一个成功当失败报**：设备按标准不回执，
//     operation 会一直排到 transport deadline 才落 timeout，前端把一次正常下发
//     显示成「结果未知」。同族先例 FormatSDCard 已在海康真机上反证过这一点。
//   - **`MaxAttempts: 1`**：重发对查询无害，对"改变设备行为"的动作则可能把
//     "已下发、仅 SIP 200 丢包"再执行一次。宁可让 operation 停在 `unknown`。
//   - **`ResponseRequired:false` 由 Execute 同步发出**，一次调用后 operation 即终态 `sent`
//     ⇒ 前端断言与措辞都按"已下发"写，不允许出现"已完成"。
//
// ⭐ 与 FormatStorageCard 的关键差别：那边"成没成"可以事后再查一次 SDCardStatus 对账；
// 目标跟踪在 2022 全文里**没有任何查询/上报命令**（对比 A.2.4 那一族查询命令，
// 目标跟踪只在 A.2.3.1.14 出现过）⇒ 「设备当前在跟踪什么」这个问题在协议上**不可回答**。
// 因此本函数在下发成功后只做一件事：把**平台意图**写进 gb_device_target_track，
// 供界面显示"已下发、设备未回执"，绝不把它当成设备状态。
//
// targetCode 用**通道编码**（= 报文里 SN 之后的 DeviceID，标准尾注「指全景相机的球机通道」），
// 全景通道走 req.DeviceID2。
func (s *Service) TrackTarget(ctx context.Context, target Target, req TargetTrackRequest, actorID, actorDeptID uint, idempotencyKey string) (gbmodels.GbPTZOperation, error) {
	command := manscdp.TargetTrackCommand{Mode: req.Mode, DeviceID2: req.DeviceID2, Area: req.Area}
	if err := manscdp.ValidateTargetTrackCommand(command); err != nil {
		return gbmodels.GbPTZOperation{}, err
	}
	profile := target.Profile
	if profile.Version == "" {
		profile = protocol.ProfileFor(protocol.Version2016)
	}
	targetCode := strings.TrimSpace(target.ChannelCode)
	if targetCode == "" {
		return gbmodels.GbPTZOperation{}, fmt.Errorf("目标跟踪缺少目标通道编码")
	}
	area := req.Area
	deviceID2 := strings.TrimSpace(req.DeviceID2)
	// 抓住实际下发的那一帧原文（Execute 每次尝试只调用一次 Build），
	// 让它连同意图一起落库：排查"框选位置不对"这类问题时，能直接对着意图表里的报文看，
	// 不必再去 SIP 轨迹表里按 SN 捞。
	var wire []byte
	operation, err := s.Execute(ctx, target, Command{
		CmdType:          manscdp.CmdDeviceControl,
		Action:           actionTargetTrack,
		IdempotencyKey:   idempotencyKey,
		Payload:          targetTrackPayload(command, deviceID2),
		ResponseRequired: false, // 9.3.1 d) + 表 1 序号 13：无应答命令，见函数注释
		MaxAttempts:      1,
		ActorID:          actorID,
		ActorDeptID:      actorDeptID,
		TargetScope:      gbmodels.ControlTargetScopeChannel,
		TargetCode:       targetCode,
		Build: func(sn int) ([]byte, error) {
			body, buildErr := manscdp.BuildTargetTrackControlWithProfile(profile, targetCode, sn, command)
			if buildErr == nil {
				wire = body
			}
			return body, buildErr
		},
		Profile: profile,
	})
	if err != nil {
		// 没发出去就不改意图：设备什么都没收到，界面上的"当前指令"必须保持原样，
		// 否则会出现"界面上写着已下发自动跟踪、实际设备还在按上一条指令跑"。
		return operation, err
	}
	if err := s.persistTargetTrackIntent(ctx, operation, command, deviceID2, actorID, actorDeptID, area, wire); err != nil {
		return operation, err
	}
	return operation, nil
}

// targetTrackPayload 是写进 gb_ptz_operation.payload_json 的业务参数快照。
// 键名与报文元素对齐，排障时能直接和 SIP 原文对着看。
func targetTrackPayload(command manscdp.TargetTrackCommand, deviceID2 string) map[string]interface{} {
	payload := map[string]interface{}{
		"action":    actionTargetTrack,
		"mode":      string(command.Mode),
		"deviceId2": deviceID2,
	}
	if command.Area != nil {
		payload["targetArea"] = *command.Area
	}
	return payload
}

// persistTargetTrackIntent 把"平台已下发"这件事落成 (device_id, target_code) 一行。
//
// ⛔ CAS 在 operation 序号上，不是在时间上：同一个 idempotency key 的重放会拿到
// **那条旧的** operation，若不加保护，一次迟到的重放就会把更晚一次下发的意图覆盖回去
// （界面上会看到"刚点的 Stop 又变回 Auto"）。规则同 gb_device_storage_card：
// 库里已有更新的序号就整条放弃；更新语句本身也带 `<=` 作为二次保险。
func (s *Service) persistTargetTrackIntent(ctx context.Context, operation gbmodels.GbPTZOperation, command manscdp.TargetTrackCommand, deviceID2 string, actorID, actorDeptID uint, area *manscdp.TargetTrackArea, wire []byte) error {
	if operation.ID == 0 || operation.DeviceID == 0 {
		return fmt.Errorf("目标跟踪 operation 缺少可靠序列或设备标识")
	}
	targetCode := operationTargetCode(operation)
	if targetCode == "" {
		return fmt.Errorf("目标跟踪 operation 缺少目标编码")
	}
	if !gbmodels.TargetTrackModeValid(gbmodels.TargetTrackMode(command.Mode)) {
		return fmt.Errorf("目标跟踪模式 %q 不能落库", command.Mode)
	}
	now := s.now()

	values := map[string]interface{}{
		"mode":                 string(command.Mode),
		"device_id2":           deviceID2,
		"area_length":          areaInt(area, func(a manscdp.TargetTrackArea) int { return a.Length }),
		"area_width":           areaInt(area, func(a manscdp.TargetTrackArea) int { return a.Width }),
		"area_mid_point_x":     areaInt(area, func(a manscdp.TargetTrackArea) int { return a.MidPointX }),
		"area_mid_point_y":     areaInt(area, func(a manscdp.TargetTrackArea) int { return a.MidPointY }),
		"area_length_x":        areaInt(area, func(a manscdp.TargetTrackArea) int { return a.LengthX }),
		"area_length_y":        areaInt(area, func(a manscdp.TargetTrackArea) int { return a.LengthY }),
		"source_operation_seq": operation.ID,
		"source_sn":            operation.SN,
		"source_operation_id":  operation.OperationID,
		"commanded_by":         actorID,
		"commanded_by_dept_id": actorDeptID,
		"commanded_at":         now,
		"raw_summary":          summarizePTZBody(wire),
		"updated_at":           now,
	}
	values["channel_id"] = operation.ChannelID
	values["device_id"] = operation.DeviceID
	values["target_code"] = targetCode
	values["created_at"] = now

	return schedulerTransaction(ctx, s.db, func(tx *gorm.DB) error {
		var latest gbmodels.GbDeviceTargetTrack
		result := tx.Where("device_id = ? AND target_code = ?", operation.DeviceID, targetCode).Limit(1).Find(&latest)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 1 && latest.SourceOperationSeq > operation.ID {
			return nil
		}
		// 注意这里**不是** GORM 的 Save/Updates-on-model：目标跟踪的意图列里
		// 有大量可空列（Auto/Stop 不带框），必须显式把 nil 写进去——
		// 用 struct 更新会把 nil 当成"不更新"，于是"停止跟踪"之后界面还显示着上一次的框。
		var affected int64
		if row := tx.Model(&gbmodels.GbDeviceTargetTrack{}).
			Where("device_id = ? AND target_code = ? AND source_operation_seq <= ?", operation.DeviceID, targetCode, operation.ID).
			Updates(values); row.Error != nil {
			return row.Error
		} else {
			affected = row.RowsAffected
		}
		if affected > 0 {
			return nil
		}
		var existing gbmodels.GbDeviceTargetTrack
		found := tx.Where("device_id = ? AND target_code = ?", operation.DeviceID, targetCode).Limit(1).Find(&existing)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected == 1 {
			if existing.SourceOperationSeq > operation.ID {
				return nil
			}
			return fmt.Errorf("目标跟踪意图 CAS 未应用 operation %d", operation.ID)
		}
		createErr := tx.Model(&gbmodels.GbDeviceTargetTrack{}).Create(values).Error
		if createErr == nil {
			return nil
		}
		// 并发下可能刚被另一个请求插进去，重试一次受 seq 保护的更新，
		// 不去解析方言相关的重复键错误。
		retried := tx.Model(&gbmodels.GbDeviceTargetTrack{}).
			Where("device_id = ? AND target_code = ? AND source_operation_seq <= ?", operation.DeviceID, targetCode, operation.ID).
			Updates(values)
		if retried.Error != nil {
			return retried.Error
		}
		if retried.RowsAffected > 0 {
			return nil
		}
		return createErr
	})
}

// areaInt 把可选区域里的某一项取成 *int；area 为 nil 时返回 nil。
//
// ⛔ 绝不返回 0 兜底：窗口尺寸 0 是非法值，"没有框"与"框在 0 点"是两件事
// （见 GbDeviceTargetTrack.AreaLength 的注释）。
func areaInt(area *manscdp.TargetTrackArea, pick func(manscdp.TargetTrackArea) int) *int {
	if area == nil {
		return nil
	}
	value := pick(*area)
	return &value
}
