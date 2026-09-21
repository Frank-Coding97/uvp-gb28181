package ptz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

// 配置家族（A.2.4.7 查询 / A.2.3.2.5 下发 / A.2.6.9 应答）的业务编排。
//
// ## 与 video_param.go 的关系
//
// `VideoParamAttribute` 有它自己的读写编排（读按码流分行落库、写按码流逐行对账）。
// 本文件是**其余各配置类型**的通用通道：一条报文可带多组、整块落 JSON 快照、
// 按 `config_type` 对账。两条链路共用的底层（scheduler / 重试 / 超时 / ack→回读的
// 子 operation 机制 / operation 状态机）完全一致，所以这里只写"差在哪一段"。
//
// ## 门禁口径（与 A-5 完全一致，不是省事）
//
//   - **读不设门禁**：`ConfigDownload` 是 2016 就有的命令，对 2016 设备发出去不会超时 ——
//     它会正常回一个不带这些 2022 元素的 OK 应答，正好落成 `type_absent`，
//     而那恰恰是"设备不支持该配置类型"最可靠的判据。挡掉它等于把判定手段也挡掉。
//   - **写不设门禁，但必须回读对账**：写入应答（A.2.6.8）里没有任何回显，
//     `Result=OK` 只说明"收到并接受"。真相由自动追加的回读给出。
const (
	actionRefreshDeviceConfigs = "refresh_device_configs"
	actionApplyDeviceConfig    = "apply_device_config"

	// deviceConfigReconcileAttempts 对账子 operation 的重试预算。
	// 与视频参数对账同为 3：这是一次**读**，超时最坏烧一个 SN，不产生副作用。
	deviceConfigReconcileAttempts = 3

	// deviceConfigDiffLimit 单次对账最多列出几格差异（多出来的只报数量）。
	// ⛔ `error_message` 是要落库的短文本，不是日志；一条 OSD 配置逐格展开能到几十行，
	// 不设上限会让这一列变成"看不完的墙"，反而盖住其他 operation 的信息。
	deviceConfigDiffLimit = 8

	// osdPositionRowPixels 是 OSD 叠加**纵向**的落位网格（像素）—— 设备只把文字
	// 落在它的整数倍上，不在网格上的请求值会被**向下取整**（理由见
	// [foldOSDPositionsToRowGrid] 里的真机取证表）。横向是逐像素的，不吸附。
	osdPositionRowPixels = 16
)

// deviceConfigApplyPayload 是下发 operation 的 payload。
//
// ⛔ `Blocks` 直接放协议层结构：对账要比的是"我们要求的值"与"设备回读的值"，
// 两边必须是**同一个结构定义**。中间再倒一手 DTO 就是给自己造第二个真源。
type deviceConfigApplyPayload struct {
	ConfigTypes []string                   `json:"configTypes"`
	Blocks      manscdp.DeviceConfigBlocks `json:"blocks"`
}

// isReconcileMismatchCode 报告一个 error_code 是否表示"回读与下发不一致"。
//
// ⛔ 集中判定的理由：`DeriveVideoParamReconcileState` 是靠这个码把状态判成 mismatch 的，
// 而码现在有两个（视频参数 / 配置家族）。散落成 `==` 比较时，新增一个码就会
// 静默地把那条链路的状态判成 read_ok —— 面板显示"一切正常"，实际是不一致。
func isReconcileMismatchCode(code string) bool {
	switch strings.TrimSpace(code) {
	case ptzErrorVideoParamReconcileMismatch, ptzErrorDeviceConfigReconcileMismatch:
		return true
	default:
		return false
	}
}

// ReadDeviceConfigs 发起一次设备配置读取（A.2.4.7 ConfigDownload）。
//
// configTypes 必须落在 [manscdp.ConfigTypeOrder] 里；空列表是没有语义的请求
// （标准要求 `ConfigType` 至少给一个），这里**直接拒发**而不是发一条空报文——
// 设备收到空 ConfigType 的行为标准没定义，多半是回一个不带任何块的成功应答，
// 于是平台会把"我们没问"误记成"设备不支持"。
func (s *Service) ReadDeviceConfigs(ctx context.Context, target Target, configTypes []string, actorID, actorDeptID uint, idempotencyKey string) (gbmodels.GbPTZOperation, error) {
	normalized, err := normalizeDeviceConfigTypes(configTypes)
	if err != nil {
		return gbmodels.GbPTZOperation{}, err
	}
	profile := target.Profile
	if profile.Version == "" {
		profile = protocol.ProfileFor(protocol.Version2016)
	}
	return s.Execute(ctx, target, Command{
		CmdType:          manscdp.CmdConfigDownload,
		Action:           actionRefreshDeviceConfigs,
		IdempotencyKey:   idempotencyKey,
		Payload:          map[string]interface{}{"configTypes": normalized},
		ResponseRequired: true,
		MaxAttempts:      3,
		ActorID:          actorID,
		ActorDeptID:      actorDeptID,
		Build: func(sn int) ([]byte, error) {
			return manscdp.BuildConfigDownloadQueryWithProfile(profile, target.ChannelCode, sn, normalized)
		},
		Profile: profile,
	})
}

// ApplyDeviceConfig 下发一组配置（A.2.3.2.5 DeviceConfig）。
//
// `blocks.PresentConfigTypes()` 决定报文里出现哪些块，必须是 1..N 个：
// 空容器没有语义，构建侧也会拒发，但在这里先拒能给出更准确的错误文案。
func (s *Service) ApplyDeviceConfig(ctx context.Context, target Target, blocks manscdp.DeviceConfigBlocks, actorID, actorDeptID uint, idempotencyKey string) (gbmodels.GbPTZOperation, error) {
	setTypes := blocks.PresentConfigTypes()
	if len(setTypes) == 0 {
		return gbmodels.GbPTZOperation{}, fmt.Errorf("至少需要一组配置")
	}
	if err := validateDeviceConfigChannelTypes(setTypes); err != nil {
		return gbmodels.GbPTZOperation{}, err
	}
	if err := manscdp.ValidateDeviceConfigBlocks(blocks); err != nil {
		return gbmodels.GbPTZOperation{}, fmt.Errorf("设备配置不合法: %w", err)
	}
	profile := target.Profile
	if profile.Version == "" {
		profile = protocol.ProfileFor(protocol.Version2016)
	}
	return s.Execute(ctx, target, Command{
		CmdType:        manscdp.CmdDeviceConfig,
		Action:         actionApplyDeviceConfig,
		IdempotencyKey: idempotencyKey,
		Payload: map[string]interface{}{
			"configTypes": setTypes,
			"blocks":      blocks,
		},
		ResponseRequired: true,
		MaxAttempts:      3,
		ActorID:          actorID,
		ActorDeptID:      actorDeptID,
		Build: func(sn int) ([]byte, error) {
			return manscdp.BuildDeviceConfigBlocksWithProfile(profile, target.ChannelCode, sn, blocks)
		},
		Profile: profile,
	})
}

// normalizeDeviceConfigTypes 去空、去重、按 [manscdp.ConfigTypeOrder] 排序，并拒绝未知类型。
//
// ⛔ 排序是必需的，不是好看：`ConfigType` 在报文里的出现顺序影响的是**可复现性** ——
// 同一次操作重试时元素顺序抖动，抓包比对与日志 diff 都会误报"报文变了"。
func normalizeDeviceConfigTypes(configTypes []string) ([]string, error) {
	if len(configTypes) == 0 {
		return nil, fmt.Errorf("至少需要一个配置类型")
	}
	requested := make(map[string]struct{}, len(configTypes))
	for _, configType := range configTypes {
		trimmed := strings.TrimSpace(configType)
		if trimmed == "" {
			continue
		}
		if !isKnownDeviceConfigType(trimmed) {
			return nil, fmt.Errorf("未知的配置类型: %s", trimmed)
		}
		requested[trimmed] = struct{}{}
	}
	if len(requested) == 0 {
		return nil, fmt.Errorf("至少需要一个配置类型")
	}
	normalized := make([]string, 0, len(requested))
	for _, configType := range manscdp.ConfigTypeOrder {
		if _, ok := requested[configType]; ok {
			normalized = append(normalized, configType)
		}
	}
	return normalized, nil
}

func isKnownDeviceConfigType(configType string) bool {
	for _, known := range manscdp.ConfigTypeOrder {
		if known == configType {
			return true
		}
	}
	return false
}

// deviceConfigChannelWriteExclusions 是**本下发通道不发**的配置类型及原因。
//
// ⛔ 与 `manscdp.ReadOnlyDeviceConfigTypeReason` **分层不同，别合并成一张表**：
//   - 那个是**协议事实**（A.2.3.2 的 schema 里根本没有那个元素，发了就是非法报文）；
//   - 这里是**平台策略**（协议允许，但本通道拿不到下发它所必需的上下文）。
//
// 目前只有一项：`SnapShotConfig`（A.2.3.2.12）的 `SessionID` 与 `UploadURL` 必须由平台生成
// —— `UploadURL` 是**设备主动往哪里 POST 图像**的地址。通用接口若收客户端给的值，
// 等于允许任何持"设备配置下发"权限的账号把摄像头画面推到他自己的服务器上。
// 抓拍配置只走 `/channel/:id/snapshot-sessions`（由抓拍会话生成 SessionID 与带令牌的上传地址）。
var deviceConfigChannelWriteExclusions = map[string]string{
	manscdp.ConfigTypeSnapShotConfig: "抓拍配置须由抓拍会话下发（SessionID 与上传地址由平台生成），通用接口不收客户端指定的上传地址",
}

// validateDeviceConfigChannelTypes 拒绝本通道不负责下发的配置类型。
//
// ⛔ 必须在**发送前**拒，不能靠"构建侧只是不生成它的 XML"来表达：那样平台会发出一条
// "声称要配 X、报文里却一个块都没有"的 DeviceConfig，设备回 OK、平台记 accepted，
// 配置从头到尾没传出去，而两侧日志都正常（这正是本仓最贵的一类故障）。
func validateDeviceConfigChannelTypes(configTypes []string) error {
	for _, configType := range configTypes {
		if reason, excluded := deviceConfigChannelWriteExclusions[strings.TrimSpace(configType)]; excluded {
			return fmt.Errorf("配置类型 %s 不能通过本通道下发：%s", configType, reason)
		}
	}
	return nil
}

// ============================ 应答：读（A.2.6.9） ============================

// applyDeviceConfigReadResponse 处理配置家族的读取应答。
//
// 一条应答可带多组配置，**每个类型各自可能缺席** —— 缺席是 `type_absent` 的判据，
// 不是解析失败。所以这里不按"整条应答有没有数据"一刀切，而是逐类型落库：
// 在场就 upsert 一行，缺席就什么都不做（**不清理**已回读到的行，见下）。
//
// ⭐ 而且**一次查询会回来多条应答**：标准 A.2.4.7 允许"同 SN 多个响应，每个对应一个
// 配置类型"。海康真机实测：查 7 个类型回了 7 条独立 MESSAGE（各自一个新 Call-ID），
// 平台原来只接住第 1 条（BasicParam），**第 4 条的画面遮挡整个丢掉** ——
// 库里没有 PictureMask 行，operation 却是 accepted，看着像"设备没返回"。
// 收集期与三条收敛判据见 device_config_read.go。
func (s *Service) applyDeviceConfigReadResponse(ctx context.Context, operation gbmodels.GbPTZOperation, callID, cseq string, body []byte) error {
	result, parseErr := manscdp.ParseDeviceConfigReadResponseFor(body, manscdp.ConfigDownloadExpectation{
		SN:       operation.SN,
		DeviceID: operationTargetCode(operation),
	})
	if parseErr != nil {
		return s.applyRejectedPTZResponse(ctx, operation, callID, cseq, "ERROR", ptzErrorProtocolInvalid, parseErr.Error())
	}

	completedAt := s.now()
	// ⛔ 判据是"有没有问到东西"，不是"设备回的是 OK 还是 ERROR"：
	// 部分设备回 ERROR 时仍会把已知的块带上，那部分数据是真实的，不该丢。
	rawSummary := summarizePTZBody(body)

	// 收集期是进程内状态，串行化累积与结算（结算在 scheduler tick 里跑）。
	s.configReadMu.Lock()
	defer s.configReadMu.Unlock()

	stage := s.accumulateConfigReadStage(operation, result, completedAt)
	hasData := stage.hasData
	complete := configReadStageComplete(stage)

	terminalApplied := false
	observedApplied := false
	err := schedulerTransaction(ctx, s.db, func(tx *gorm.DB) error {
		terminalApplied = false
		observedApplied = false
		// 每条应答都先落库：中途超时 / 缺条时，已经读到的值仍然要留下，
		// 不能因为"还没收齐"就把设备已经给出的事实丢掉。
		applied, err := applyPTZResponseObservation(tx, operation, callID, cseq, completedAt)
		if err != nil {
			return err
		}
		if !applied {
			return nil
		}
		observedApplied = true
		if err := s.persistDeviceConfigsWithDB(ctx, tx, operation, result, rawSummary); err != nil {
			return err
		}
		if !complete {
			// 收齐之前 operation 保持活跃：后面的应答还要能命中它。
			return nil
		}
		terminalApplied, err = applyPTZResponseTransition(tx, operation, callID, cseq, ptzResponseTransition{
			Status: gbmodels.PTZOperationAccepted, DeviceResult: result.Result, ResponseHasData: &hasData,
		}, completedAt)
		if err != nil {
			return err
		}
		if !terminalApplied {
			return nil
		}
		// 对账比的是**累积到的全部块**：逐条比会把"这一条没带某个类型"当成
		// 不一致（下发过、设备在另一条里回了），凭空造出假差异。
		return markDeviceConfigReconcileMismatch(tx, operation, stage.blocks)
	})
	if err != nil {
		return err
	}

	// 事务外维护收集期生命周期，与 applyQueryResponse 同构：
	// 只要这次应答没让 operation 走到终态、也还没收齐，就把累积体留在表里等后面的应答。
	if !terminalApplied && !complete && observedApplied {
		s.configReadStages[operation.OperationID] = stage
	} else {
		delete(s.configReadStages, operation.OperationID)
	}
	return nil
}

// ============================ 应答：写（A.2.6.8） ============================

// applyDeviceConfigAckResponse 处理 DeviceConfig 的写入应答。
//
// ⛔ ack 不是终态：应答里既没有回显、也说明不了值有没有生效。所以这里在同一事务里
// 追加一条 **ConfigDownload 对账子 operation**，由它把回读值落库并与本次下发的值逐格比对。
// 与 A-5 的 applyDeviceConfigResponse 是同一个机制，差别只有"用哪个 action / 带哪些类型"。
func (s *Service) applyDeviceConfigAckResponse(ctx context.Context, operation gbmodels.GbPTZOperation, callID, cseq string, body []byte) error {
	ack, parseErr := manscdp.ParseDeviceConfigResponseFor(body, manscdp.ConfigDownloadExpectation{
		SN:       operation.SN,
		DeviceID: operationTargetCode(operation),
	})
	if parseErr != nil {
		return s.applyRejectedPTZResponse(ctx, operation, callID, cseq, "ERROR", ptzErrorProtocolInvalid, parseErr.Error())
	}
	if !ack.Accepted() {
		return s.applyRejectedPTZResponse(ctx, operation, callID, cseq, ack.Result, ptzErrorDeviceRejected, "设备拒绝该设备配置")
	}

	completedAt := s.now()
	return schedulerTransaction(ctx, s.db, func(tx *gorm.DB) error {
		applied, err := applyPTZResponseTransition(tx, operation, callID, cseq, ptzResponseTransition{
			Status: gbmodels.PTZOperationAccepted, DeviceResult: "OK",
		}, completedAt)
		if err != nil || !applied {
			return err
		}
		return s.createDeviceConfigReconcile(tx, operation, completedAt)
	})
}

// createDeviceConfigReconcile 在同一个事务里追加一条回读对账子 operation。
//
// ⛔ 为什么在同一事务里而不是"ack 之后再发一次"：ack 落库与对账排队要么一起成功、
// 要么一起没有，否则会出现"父 operation 显示已接受、但回读永远不会发生"的哑状态。
// 重试与超时预算由 scheduler 统一承担（照 createVideoParamReconcile 的既有做法）。
//
// ⛔ 对账的 configTypes 取自**父 operation 的 payload**，不是重新推导一遍：
// 推导等于写第二份"哪些类型属于这次下发"的判定，而它与实际发出的报文一旦不一致，
// 对账就会去回读一组没发过的类型（结果恒为 type_absent，看起来像设备不支持）。
func (s *Service) createDeviceConfigReconcile(tx *gorm.DB, parent gbmodels.GbPTZOperation, createdAt time.Time) error {
	if parent.CmdType != manscdp.CmdDeviceConfig {
		return nil
	}
	configTypes, err := decodeDeviceConfigApplyTypes(parent.PayloadJSON)
	if err != nil {
		return err
	}
	if len(configTypes) == 0 {
		// 没有可对账的类型：不排对账，但也不报错 —— 父 operation 已经落了 accepted，
		// 这里报错会让整条 ack 事务回滚，把"设备已接受"这个事实一起丢掉。
		return nil
	}
	payloadJSON, err := canonicalPayload(map[string]interface{}{
		"configTypes":        configTypes,
		"triggerOperationId": parent.OperationID,
	})
	if err != nil {
		return err
	}
	operationID := uuid.NewString()
	values := map[string]interface{}{
		"operation_id": operationID, "idempotency_key": "device-config-reconcile:" + parent.OperationID,
		"device_id": parent.DeviceID, "device_code": parent.DeviceCode,
		"channel_id": parent.ChannelID, "channel_code": parent.ChannelCode,
		"cmd_type": manscdp.CmdConfigDownload, "action": actionRefreshDeviceConfigs,
		"profile_version": parent.ProfileVersion, "profile_charset": parent.ProfileCharset,
		"target_scope": parent.TargetScope, "target_code": parent.TargetCode,
		"payload_json": payloadJSON, "sn": s.nextSN(), "status": gbmodels.PTZOperationQueued,
		"attempt": 0, "response_required": true, "max_attempts": deviceConfigReconcileAttempts,
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
		return fmt.Errorf("关联设备配置自动对账 operation 失败")
	}
	return nil
}

// ============================ 落库 ============================

// persistDeviceConfigsWithDB 把一次回读结果落成"每配置类型一行"。
//
// 与 persistVideoParamsWithDB 的差别只有一处：**没有清理步骤**。
// 视频参数那边要删掉"本轮没再出现的码流"，因为码流集合是设备报出来的、会变；
// 这里每行的键是 (device, target, config_type)，而 config_type 的取值域是标准固定的
// 一小撮 —— "这次没带"意味着 type_absent，语义是"设备这次没给"，不是"这个类型不存在了"。
// 所以保留上一次的值，由 observed_at + 最近一次回读的结论一起决定前端怎么展示。
func (s *Service) persistDeviceConfigsWithDB(ctx context.Context, db *gorm.DB, operation gbmodels.GbPTZOperation, result *manscdp.DeviceConfigReadResult, summary string) error {
	if operation.ID == 0 || operation.DeviceID == 0 {
		return fmt.Errorf("ConfigDownload operation 缺少可靠序列或设备标识")
	}
	targetCode := operationTargetCode(operation)
	if targetCode == "" {
		return fmt.Errorf("ConfigDownload operation 缺少目标编码")
	}
	now := s.now()
	db = db.WithContext(ctx)
	for _, configType := range result.Blocks.PresentConfigTypes() {
		block, present := result.Blocks.Block(configType)
		if !present {
			continue
		}
		payload, err := json.Marshal(block)
		if err != nil {
			return fmt.Errorf("配置 %s 序列化失败: %w", configType, err)
		}
		if err := upsertDeviceConfig(db, operation, targetCode, configType, string(payload), summary, now); err != nil {
			return err
		}
	}
	return nil
}

// upsertDeviceConfig 写一行配置快照，带**迟到应答保护**。
//
// 保护方式与 upsertVideoParam 一致：只在 `source_operation_seq <= 本次 operation.ID` 时覆盖。
// 迟到的旧应答（乱序到达的 ConfigDownload）不会把更新的值写回去。
func upsertDeviceConfig(db *gorm.DB, operation gbmodels.GbPTZOperation, targetCode, configType, payloadJSON, summary string, now time.Time) error {
	values := map[string]interface{}{
		"payload_json":         payloadJSON,
		"source_operation_seq": operation.ID,
		"source_sn":            operation.SN,
		"source_operation_id":  operation.OperationID,
		"observed_at":          now,
		"raw_summary":          summary,
		"updated_at":           now,
	}

	updateNewer := func() (*gorm.DB, error) {
		result := db.Model(&gbmodels.GbDeviceConfig{}).
			Where("device_id = ? AND target_code = ? AND config_type = ? AND source_operation_seq <= ?",
				operation.DeviceID, targetCode, configType, operation.ID).
			Updates(values)
		return result, result.Error
	}
	if result, err := updateNewer(); err != nil {
		return err
	} else if result.RowsAffected == 1 {
		return nil
	}

	var current gbmodels.GbDeviceConfig
	found := db.Where("device_id = ? AND target_code = ? AND config_type = ?",
		operation.DeviceID, targetCode, configType).Limit(1).Find(&current)
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
		return fmt.Errorf("设备配置缓存 CAS 未应用 operation %d", operation.ID)
	}

	values["device_id"] = operation.DeviceID
	values["target_code"] = targetCode
	values["config_type"] = configType
	values["created_at"] = now
	createErr := db.Model(&gbmodels.GbDeviceConfig{}).Create(values).Error
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
	if again := db.Where("device_id = ? AND target_code = ? AND config_type = ?",
		operation.DeviceID, targetCode, configType).Limit(1).Find(&current); again.Error != nil {
		return again.Error
	} else if again.RowsAffected == 1 && current.SourceOperationSeq >= operation.ID {
		return nil
	}
	return createErr
}

// ============================ 对账 ============================

// deviceConfigDiff 描述一组配置里一格字段的下发值/回读值差异。
type deviceConfigDiff struct {
	ConfigType string
	// Path 指向差异所在的字段路径（如 `AlarmReport.MotionDetection` /
	// `OSDConfig.Items[0].X`）。段名用的是**协议层结构的字段名**，不是 XML 元素名 ——
	// 后端不承担"结构字段 → 元素名"的翻译责任，那是前端的展示层的事。
	Path   string
	Wanted string
	Actual string
}

// markDeviceConfigReconcileMismatch 在**对账子 operation** 上标出值不一致。
//
// 只在"这条 ConfigDownload 是由某条 DeviceConfig 派生出来的"时判定 ——
// 手动点「读取设备配置」触发的回读没有可比的意图，不该被判成 mismatch。
// 结论标在子 operation 上，父 operation 保持 accepted（设备确实接受了命令，这是事实）。
func markDeviceConfigReconcileMismatch(tx *gorm.DB, query gbmodels.GbPTZOperation, actual manscdp.DeviceConfigBlocks) error {
	if query.CmdType != manscdp.CmdConfigDownload || query.Action != actionRefreshDeviceConfigs ||
		query.TriggerOperationID == nil || strings.TrimSpace(*query.TriggerOperationID) == "" {
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
	wanted, err := decodeDeviceConfigApplyBlocks(parent.PayloadJSON)
	if err != nil || wanted.IsEmpty() {
		return err
	}
	diffs := diffDeviceConfigBlocks(wanted, actual)
	if len(diffs) == 0 {
		return nil
	}
	return tx.Model(&gbmodels.GbPTZOperation{}).
		Where("id = ? AND status = ?", query.ID, gbmodels.PTZOperationAccepted).
		Updates(map[string]interface{}{
			"error_code":    ptzErrorDeviceConfigReconcileMismatch,
			"error_message": formatDeviceConfigDiff(diffs),
		}).Error
}

// diffDeviceConfigBlocks 逐类型、逐字段比较"下发值"与"回读值"。
//
// ⛔ 只比**双方都在场**的块：下发时没带这个类型（平台没配）而设备回了一个值，
// 或者设备根本没回这个类型，都不是"不一致" —— 前者是平台的意图范围，后者是 type_absent，
// 各自由别的判据表达。混进来会让每一次部分下发都报一堆假差异。
func diffDeviceConfigBlocks(wanted, actual manscdp.DeviceConfigBlocks) []deviceConfigDiff {
	diffs := make([]deviceConfigDiff, 0, deviceConfigDiffLimit)
	for _, configType := range manscdp.ConfigTypeOrder {
		wantBlock, wantOK := wanted.Block(configType)
		haveBlock, haveOK := actual.Block(configType)
		if !wantOK || !haveOK {
			continue
		}
		wantTree := comparableConfigTree(wantBlock)
		haveTree := comparableConfigTree(haveBlock)
		if configType == manscdp.ConfigTypePictureMask {
			// ① 零面积条目一律不参与对账，理由见 [dropBlankPictureMaskRegions]。
			wantTree = dropBlankPictureMaskRegions(wantTree)
			haveTree = dropBlankPictureMaskRegions(haveTree)
			// ② 任一边关闭时 `regions` 整个不参与对账，理由见 [dropPictureMaskRegions]。
			if pictureMaskDisabled(wantTree) || pictureMaskDisabled(haveTree) {
				wantTree = dropPictureMaskRegions(wantTree)
				haveTree = dropPictureMaskRegions(haveTree)
			}
		}
		if configType == manscdp.ConfigTypeOSDConfig {
			// ⛔ 只折**下发侧**，回读侧原样保留：设备能落到的值才是可比对象，
			// 而"回读值不在网格上"本身就说明设备没按预期落位，必须报出来。
			// 理由与真机证据见 [foldOSDPositionsToRowGrid]。
			wantTree = foldOSDPositionsToRowGrid(wantTree)
		}
		walkDeviceConfigDiff(configType, configType, wantTree, haveTree, &diffs)
	}
	return diffs
}

// dropBlankPictureMaskRegions 摘掉 `PictureMask.regions` 里**零面积**的条目。
//
// ⭐ 判据与前端（`DeviceConfigDrawer.vue` 的 `rectHasArea`）刻意同源：**零面积
// （`右<=左` 或 `下<=上`）不是"一条遮挡"，而是"删掉这一槽"的表达**
// （真机证据见 `manscdp.PictureMaskClearPoint`）。平台下发时会为"被删掉的槽位"
// 显式补一条零面积（见 `manscdp.pictureMaskFullRegions`），设备执行后**通常不回显**它，
// 但这台海康在"全零"形态下会把删除痕迹回读回来（2026-09-19 实测回 `704,576,704,576`，
// `Num` 仍是 1）。**回显与不回显都是合法形态，不该由对账来判**：不摘掉就会出现
// 「下发 3 条、设备回 3 条真 + 1 条零 ⇒ 长度不等 ⇒ 报"值未生效"」这种明明做对了
// 却说没生效的假差异 —— 与 2026-09-19 那次 `regions=[](实际 […])` 是同一类。
//
// ⛔ 别把它当"宽容"读：**有面积的差异照旧逐条比**。平台画了 2 个区、设备只报 1 个，
// 摘完两侧长度仍不等，必须能报出来（反向锚点见 `device_config_test.go`）。
func dropBlankPictureMaskRegions(tree any) any {
	object, ok := tree.(map[string]any)
	if !ok {
		return tree
	}
	items, ok := object["regions"].([]any)
	if !ok {
		return object
	}
	kept := make([]any, 0, len(items))
	for _, item := range items {
		if pictureMaskRegionHasArea(item) {
			kept = append(kept, item)
		}
	}
	trimmed := make(map[string]any, len(object))
	for key, child := range object {
		trimmed[key] = child
	}
	trimmed["regions"] = kept
	return trimmed
}

// pictureMaskRegionHasArea 报告一条已折叠的区域树在画面上是否真的占了面积。
//
// ⛔ **形态读不出来时返回 true（保留）**，不是 false：宁可多比一条、让差异被报出来，
// 也别把一条真区域当成删除标记吞掉 —— 后者是**静默漏报**，比假报难查得多。
func pictureMaskRegionHasArea(item any) bool {
	region, ok := item.(map[string]any)
	if !ok {
		return true
	}
	left, leftOK := configScalarInt(region["left"])
	right, rightOK := configScalarInt(region["right"])
	top, topOK := configScalarInt(region["top"])
	bottom, bottomOK := configScalarInt(region["bottom"])
	if !leftOK || !rightOK || !topOK || !bottomOK {
		return true
	}
	return right > left && bottom > top
}

// pictureMaskDisabled 报告一棵已折叠的 `PictureMask` 树是否处于关闭状态（`on == 0`）。
func pictureMaskDisabled(tree any) bool {
	object, ok := tree.(map[string]any)
	if !ok {
		return false
	}
	text, missing := configScalarText(object["on"])
	return !missing && text == "0"
}

// dropPictureMaskRegions 从 `PictureMask` 树里摘掉 `regions`，让关闭状态下的区域残留
// 不参与对账。
//
// ⛔⛔ 为什么"关闭时的区域残留"**不是**「值未生效」（2026-09-19 真机现场）：
//
// 标准 A.2.1.17 里 `On` 是 0/1 开关，`RegionList` 是**独立**的可选元素
// （`minOccurs="0"`）—— 标准**没有**「关闭时必须一并清空区域」这条规定。
// 所以设备收到 `On=0` 后保留原有区域，是**合法形态**，不是"没照做"。
//
// 真机就是这么表现的（海康 DS-2DC2C040MY-DE）：操作员只想停用遮挡、没画任何区域，
// 前端 [buildPicture] 会把全 0 槽位跳过 ⇒ 下发 `{on:0,regions:[]}`；设备执行成
// `On=0` 但区域原样保留 ⇒ 对账报 `PictureMask.regions=[](实际 […])` ⇒ 界面提示
// 「设备已接受命令，但值未生效」。
//
// **这条报错的杀伤力在于把"已经成功"说成了"没生效"**：遮挡在设备侧**确实已移除**
// （`On=0`），平台却说没生效，操作员于是反复下发、以为功能坏了。
//
// ⭐ 判据取"**任一边**关闭即忽略"，而不是"两边都关闭"：`On` 本身的差异由 `on`
// 这个叶子单独报出（设备没关会被抓住），而区域内容在任一边关闭时都不构成有效承诺。
//
// ⛔ 反向别搞错：**两边都启用时 `regions` 照常逐格比** —— 那时区域才是双方都要
// 负责的约定值，"平台画了 2 个区、设备只报 1 个"必须能报出来。
func dropPictureMaskRegions(tree any) any {
	object, ok := tree.(map[string]any)
	if !ok {
		return tree
	}
	trimmed := make(map[string]any, len(object))
	for key, child := range object {
		if key == "regions" {
			continue
		}
		trimmed[key] = child
	}
	return trimmed
}

// foldOSDPositionsToRowGrid 把 `OSDConfig` 的**纵向**落位折到设备能落到的行网格上。
//
// ⛔⛔ 为什么"坐标被改了 3 个像素"**不是**「值未生效」（2026-09-20 真机现场）：
//
// 操作员在播放控制台把时间戳拖到 `(18, 51)`，界面立刻提示
// 「设备已接受命令，但值未生效：回读值与下发值不一致: OSDConfig.timeY=51(实际 48)」——
// 而时间戳在画面上的**位置明明变了**（这就是操作员说的"其实已经生效了"）。
//
// 真机控制变量扫描（海康 DS-2DC2C040MY-DE / 声明画布 704x576 / 2026-09-20，
// 逐值下发 + 主动回读）：
//
//	下发 TimeY    8   12   17   20   25   33   100   158   255
//	回读 TimeY    0    0   16   16   16   32    96   144   240   ← 全部 = 下发值 → 向下取整到 16
//	下发 TimeX   18  289  317 / 文本行 X 25  33                ← 原样回读（含两个奇数）
//	文本行 Y     37 → 32， 100 → 96                           ← 与 TimeY 同一条网格
//
// 即：**纵向只能落在行高 16 的整数倍上（向下取整），横向是逐像素的**。
// 这不是"没照做"，是设备把平台的请求值折算到它实际能落的那一行 —— 折算幅度
// 最多 15 像素（不足一行），肉眼上就是"位置变了"。
//
// 判据取「**下发值**向下取整到网格」而不是"两边都取整"或"给个容差"：
//
//   - 折的是**下发侧**：比较对象变成"设备能做到的那个值"，语义正确；
//   - 回读侧**不折**：设备若回一个不在网格上的值（另一族设备 / 真没照做），
//     照旧会被报出来 —— 若两边都折，`51 → 48` 对上设备回的 `49`（48）会被吞成假绿；
//   - 不用"差值 < 16 就当吸附"这种容差：那等于把"设备把 Y 落在任意近邻"都算成功，
//     `51` 对上 `50` 也放过，而设备真落 50 说明它没按行网格走，是另一种异常。
//
// ⛔ 反向别搞错：**X 照旧逐格比**（`25` 对上 `26` 必须报），纵向差异超出一次折算
// （如 `51` 对上 `200`）也必须报 —— 反向锚点见 `device_config_test.go`。
func foldOSDPositionsToRowGrid(tree any) any {
	object, ok := tree.(map[string]any)
	if !ok {
		return tree
	}
	folded := make(map[string]any, len(object))
	for key, child := range object {
		folded[key] = child
	}
	changed := false
	if y, ok := configScalarInt(object["timeY"]); ok {
		if snapped := y - y%osdPositionRowPixels; snapped != y {
			folded["timeY"] = strconv.Itoa(snapped)
			changed = true
		}
	}
	if items, ok := object["items"].([]any); ok {
		// 文本行**整组**替换：逐条折过之后才知道这组有没有变化，
		// 没变化时保持原 slice，免得给调用方一个"处处不同但值相等"的树。
		foldedItems := make([]any, len(items))
		itemsChanged := false
		for index, item := range items {
			foldedItem, itemChanged := foldOSDTextItemRow(item)
			foldedItems[index] = foldedItem
			itemsChanged = itemsChanged || itemChanged
		}
		if itemsChanged {
			folded["items"] = foldedItems
			changed = true
		}
	}
	if !changed {
		return tree
	}
	return folded
}

// foldOSDTextItemRow 折一条文本行的 `y`；`x` 与 `text` 原样保留（真机实证横向不吸附）。
func foldOSDTextItemRow(item any) (any, bool) {
	object, ok := item.(map[string]any)
	if !ok {
		return item, false
	}
	y, ok := configScalarInt(object["y"])
	if !ok {
		return item, false
	}
	if snapped := y - y%osdPositionRowPixels; snapped != y {
		folded := make(map[string]any, len(object))
		for key, child := range object {
			folded[key] = child
		}
		folded["y"] = strconv.Itoa(snapped)
		return folded, true
	}
	return item, false
}

// walkDeviceConfigDiff 递归展开两个 JSON 树并在叶子处记差异。
//
// ⛔ 这里**不设上限**：截断只做一次，在 [formatDeviceConfigDiff] 里。
// 两处各写一份上限就是同一个语义的两个真源，改一处忘一处会出现
// "收集时截了、文案里又说列出了全部"这种自相矛盾的消息。
func walkDeviceConfigDiff(configType, path string, want, actual any, out *[]deviceConfigDiff) {
	switch wantValue := want.(type) {
	case map[string]any:
		actualValue, ok := actual.(map[string]any)
		if !ok {
			*out = append(*out, deviceConfigDiff{configType, path, summarizeConfigValue(want), summarizeConfigValue(actual)})
			return
		}
		for key, child := range wantValue {
			walkDeviceConfigDiff(configType, path+"."+key, child, actualValue[key], out)
		}
	case []any:
		actualValue, ok := actual.([]any)
		if !ok || len(actualValue) != len(wantValue) {
			*out = append(*out, deviceConfigDiff{configType, path, summarizeConfigValue(want), summarizeConfigValue(actual)})
			return
		}
		for index := range wantValue {
			walkDeviceConfigDiff(configType, fmt.Sprintf("%s[%d]", path, index), wantValue[index], actualValue[index], out)
		}
	default:
		if !sameConfigScalar(want, actual) {
			*out = append(*out, deviceConfigDiff{configType, path, summarizeConfigValue(want), summarizeConfigValue(actual)})
		}
	}
}

// comparableConfigTree 把一块配置折成可逐格比较的 JSON 树，并剔除**非协议字段**。
//
// ⛔ 必须剔除 `AdditionalStreams`：它是解析器的计数标注（"设备还回了几个别的码流"），
// 平台构建时恒为 0 而回读时可能 >0。不剔除的话，每次多码流设备的对账都会凭空多出一格
// "0 ≠ 1" 的差异 —— 那是个**假不一致**，会让人去查根本不存在的配置问题。
func comparableConfigTree(block any) any {
	encoded, err := json.Marshal(block)
	if err != nil {
		return nil
	}
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.UseNumber()
	var tree any
	if err := decoder.Decode(&tree); err != nil {
		return nil
	}
	return stripNonProtocolFields(tree)
}

func stripNonProtocolFields(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			if key == "AdditionalStreams" {
				continue
			}
			result[key] = stripNonProtocolFields(child)
		}
		return result
	case []any:
		result := make([]any, 0, len(typed))
		for _, child := range typed {
			result = append(result, stripNonProtocolFields(child))
		}
		return result
	default:
		return value
	}
}

// sameConfigScalar 比较两个标量叶子。
//
// ⛔ 数字统一按十进制文本比：JSON 解码在 `UseNumber` 下给出 json.Number，
// 而结构体直接解码可能给出 float64 —— 同一份数据两条来路，用 `==` 比会因为类型不同
// 判成"不一致"（`1` 与 `1` 不相等），那是最难查的一类假差异。
func sameConfigScalar(want, actual any) bool {
	wantText, wantNil := configScalarText(want)
	actualText, actualNil := configScalarText(actual)
	if wantNil || actualNil {
		return wantNil && actualNil
	}
	return wantText == actualText
}

func configScalarText(value any) (string, bool) {
	switch typed := value.(type) {
	case nil:
		return "", true
	case string:
		return typed, false
	case bool:
		if typed {
			return "true", false
		}
		return "false", false
	case json.Number:
		return typed.String(), false
	case float64:
		return strings.TrimSuffix(strings.TrimSuffix(fmt.Sprintf("%f", typed), "0"), "."), false
	default:
		return fmt.Sprintf("%v", typed), false
	}
}

// configScalarInt 把一个已折成标量的叶子读成整数；读不出来时第二个返回值为 false。
//
// ⛔ 走 [configScalarText] 而不是类型断言：树的来源是 `json.Decoder.UseNumber()`，
// 数字是 `json.Number` 而不是 `int` / `float64`，直接断言必然拿不到（会静默退化成
// "读不出来"，于是判据悄悄变成恒真）。
func configScalarInt(value any) (int, bool) {
	text, isNil := configScalarText(value)
	if isNil {
		return 0, false
	}
	number, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil {
		return 0, false
	}
	return number, true
}

func summarizeConfigValue(value any) string {
	if text, isNil := configScalarText(value); !isNil {
		return truncateConfigText(text)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "?"
	}
	return truncateConfigText(string(encoded))
}

func truncateConfigText(text string) string {
	const limit = 64
	if len(text) <= limit {
		return text
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}

// formatDeviceConfigDiff 把差异列表折成一句可读文本。
//
// ⛔ 差异是**信息**不是**错误**：典型来源是设备能力边界（下发 1080P、实际 720P），
// 所以文案用"不一致 / 实际"而不是"失败"。
// ⛔ 截断只在这里做（收集侧不截）：`error_message` 是要落库并显示在面板上的短文本，
// 一整份 OSD 配置逐格展开能到几十行，不设上限这一列会变成"看不完的墙"，
// 反而盖住其他 operation 的信息。
func formatDeviceConfigDiff(diffs []deviceConfigDiff) string {
	if len(diffs) == 0 {
		return ""
	}
	shown := diffs
	if len(shown) > deviceConfigDiffLimit {
		shown = shown[:deviceConfigDiffLimit]
	}
	parts := make([]string, 0, len(shown))
	for _, diff := range shown {
		parts = append(parts, fmt.Sprintf("%s=%s(实际 %s)", diff.Path, diff.Wanted, diff.Actual))
	}
	text := "回读值与下发值不一致: " + strings.Join(parts, ", ")
	if len(diffs) > len(shown) {
		text += fmt.Sprintf(" …（差异共 %d 处，仅列出前 %d 处）", len(diffs), len(shown))
	}
	return text
}

// decodeDeviceConfigApplyTypes 取回一条 DeviceConfig operation 里请求的配置类型。
func decodeDeviceConfigApplyTypes(payloadJSON string) ([]string, error) {
	blocks, err := decodeDeviceConfigApplyBlocks(payloadJSON)
	if err != nil {
		return nil, err
	}
	types := blocks.PresentConfigTypes()
	if len(types) > 0 {
		return types, nil
	}
	// payload 里带了 configTypes 而 blocks 反序列化不出来（历史数据 / 结构演进）时，
	// 退回到 payload 自己记录的类型列表 —— 对账的目标是"回读一次"，宁可用记录里的列表，
	// 也不要因为解析不出结构就静默不排对账。
	var fallback struct {
		ConfigTypes []string `json:"configTypes"`
	}
	if err := json.Unmarshal([]byte(payloadJSON), &fallback); err != nil {
		return nil, err
	}
	return normalizeDeviceConfigTypes(fallback.ConfigTypes)
}

// decodeDeviceConfigApplyBlocks 取回一条 DeviceConfig operation 里"我们要求的值"。
func decodeDeviceConfigApplyBlocks(payloadJSON string) (manscdp.DeviceConfigBlocks, error) {
	if strings.TrimSpace(payloadJSON) == "" {
		return manscdp.DeviceConfigBlocks{}, nil
	}
	var payload deviceConfigApplyPayload
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return manscdp.DeviceConfigBlocks{}, err
	}
	return payload.Blocks, nil
}

// ============================ 面板状态 ============================

// 配置家族面板的六态。**与视频参数面板同一套取值**（前端契约一致），
// 只是名字里去掉 video 前缀 —— 两个面板的判据是同一套，不该有两套字符串。
const (
	DeviceConfigStateNeverRead  = VideoParamStateNeverRead
	DeviceConfigStatePending    = VideoParamStatePending
	DeviceConfigStateReadOK     = VideoParamStateReadOK
	DeviceConfigStateTypeAbsent = VideoParamStateTypeAbsent
	DeviceConfigStateMismatch   = VideoParamStateMismatch
	DeviceConfigStateFailed     = VideoParamStateFailed
)

// DeviceConfigReconcileState 是透出给前端的"最近一次回读结论"。
type DeviceConfigReconcileState = VideoParamReconcileState

// DeriveDeviceConfigReconcileState 与 DeriveVideoParamReconcileState 是同一个纯函数：
// 判据只看 operation 的 status / error_code / response_has_data / trigger_operation_id，
// 与配置类型无关。这里做一层命名转发，让调用点读到的是本族的词。
func DeriveDeviceConfigReconcileState(latest *gbmodels.GbPTZOperation) DeviceConfigReconcileState {
	return DeriveVideoParamReconcileState(latest)
}

// ActionSnapshotConfig 是**抓拍会话**下发 `SnapShotConfig`（A.2.3.2.12）用的 action。
//
// ⛔ 它与 [ActionApplyDeviceConfig] 走的是**同一条配置族下发机制**（`CmdType=DeviceConfig`
// + payload 里装 `blocks`），只是不走通用 UI 通道（`SnapShotConfig` 在该通道被
// [deviceConfigChannelWriteExclusions] 拒收：`SessionID` 与 `UploadURL` 必须由平台生成）。
//
// ⛔⛔ 这个常量存在本身就是为了堵一处**静默发错报文**：在此之前该 action 只是一个写在
// `controllers` 里的字面量 `"snapshot_config"`，`ptz` 包完全不认识它 —— 于是所有
// "按 action 分流"的地方（报文重建 / 应答分派）都把它当成 A-5 视频参数，
// 实际发给设备的是 `<VideoParamAttribute Num="0">`（一块配置都没有），设备照回
// `Result=OK`、operation 记 accepted、回读还判 read_ok。**抓拍指令从未发出过**，
// 症状只有"设备没上传图片"。2026-09-20 海康真机复现（SN 10179/10181/10183）。
const ActionSnapshotConfig = "snapshot_config"

// ActionRefreshDeviceConfigs / ActionApplyDeviceConfig 供控制器按 action 查"最近一次回读"。
const (
	ActionRefreshDeviceConfigs = actionRefreshDeviceConfigs
	ActionApplyDeviceConfig    = actionApplyDeviceConfig
)

// ==================== 配置族 operation 的形态判定（唯一真源） ====================

// deviceConfigBlockActions 是**用 payload 的 `blocks` 装要下发的配置块**的 action 名单。
//
// ⛔ 这张名单的用途**只有一处**：payload 里没有 `blocks` 时，判断这属于"数据坏了"还是
// "本来就不该有块"。**不能用它来决定走哪条重建/分派路径**（见 [deviceConfigOperationForm]）
// —— 名单是会漏的，`snapshot_config` 就漏了整整一轮。
var deviceConfigBlockActions = map[string]struct{}{
	actionApplyDeviceConfig: {},
	ActionSnapshotConfig:    {},
}

// deviceConfigOperationForm 判定一条 `CmdType=DeviceConfig` 的 operation 属于哪一族形态。
//
// 返回值 `isBlockFamily` 为真表示"它按配置族处理"（重建发 `blocks`、应答走
// [Service.applyDeviceConfigAckResponse]）；为假表示它是 A-5 视频参数那条路。
//
// ⛔⛔ **判据刻意取"payload 里到底装了什么"这个数据事实，而不是 action 白名单**：
// 白名单每新增一个 action 都会漏，而漏的后果极不对称 —— 配置族报文被重建成
// `<VideoParamAttribute Num="0">`，设备照回 `Result=OK`，两侧日志全绿，
// 只有"设备没照做"这一个模糊症状（这正是抓拍整条链路静默失效的原因）。
// 按 payload 判之后，**新增 action 只要用 `blocks` 装 payload 就自动走对**。
//
// 反过来，名单仍然管一件事：action 声明自己属于配置族、payload 里却没有 `blocks`，
// 说明 payload 被写坏了 —— 这时**报错而不是回落成 A-5**，否则故障又会伪装成"设备没照做"。
func deviceConfigOperationForm(action, payloadJSON string) (manscdp.DeviceConfigBlocks, bool, error) {
	var payload deviceConfigApplyPayload
	if strings.TrimSpace(payloadJSON) != "" {
		if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
			return manscdp.DeviceConfigBlocks{}, false, err
		}
	}
	if !payload.Blocks.IsEmpty() {
		return payload.Blocks, true, nil
	}
	if _, declared := deviceConfigBlockActions[strings.TrimSpace(action)]; declared {
		return manscdp.DeviceConfigBlocks{}, true,
			errors.New("设备配置下发无法重建:operation payload 缺少 blocks")
	}
	return manscdp.DeviceConfigBlocks{}, false, nil
}
