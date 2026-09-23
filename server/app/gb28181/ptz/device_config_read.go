package ptz

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// 配置读取（A.2.4.7 ConfigDownload）的**多响应聚合**。
//
// ## 为什么必须聚合（2026-09-19 海康 DS-2DC2C040MY-DE 真机抓包）
//
// 平台原来把「一次 ConfigDownload 查询」当成「一次应答即终态」：第一条到达的应答
// 就把 operation 收成 accepted，后续同 SN 的应答全部落进 `logIgnoredPTZResponse`。
// 而标准 A.2.4.7 写得明确：
//
//	「可返回与查询 SN 值相同的多个响应，每个响应对应一个配置类型」
//
// 海康实测（SN=844，查 7 个类型）**回了 7 条独立的 SIP MESSAGE**，各自一个新 Call-ID、
// 2ms 内到齐，顺序与内容如下：
//
//	① BasicParam（有值）   ② 空应答            ③ 空应答
//	④ PictureMask（On=0 + 区域 144,295,704,576）
//	⑤ FrameMirror=2        ⑥ 空应答            ⑦ OSDConfig（有值）
//
// ⛔ 修之前平台只接住了①，**画面遮挡（④）、镜像（⑤）、OSD（⑦）全部丢失**：
// 库里的 `gb_device_config` 只有 BasicParam 一行，面板上「读不到画面遮挡」，
// 而 operation 是 `accepted`、没有任何错误码 —— 症状酷似"设备不支持"。
// **设备完全合规，是平台没实现"同 SN 多响应"。**
//
// ⛔ 另一个必须记住的实测事实：**空应答不携带任何类型标识**（见②③⑥，报文里只有
// CmdType/SN/DeviceID/Result，没有 `<ConfigType>` 也没有空块）。所以无法从报文推断
// "这个类型已经回过了" —— 这直接决定了收敛判据里的类型覆盖判据（判据 1）对海康
// **不成立**，真正兜住它的是判据 2（条数）与判据 3（静止）。
//
// ## 与 query.go 的 queryStage 是同一个模式，不是两套
//
// 预置位 / 巡航列表的分页聚合早就解决过"一个 operation 多条应答"：
// 每条应答走 [applyPTZResponseObservation] 保持 operation 活跃（不落终态），
// 累积到齐后再用 [applyPTZResponseTransition] 做一次终态 CAS。这里沿用同一套：
//
//   - 累积体在**进程内**（与 queryStage 一致）。重启丢失是可接受的：operation 仍是
//     sent/queued，scheduler 会重发，重新收一轮即可；而落库的值是按 (设备,目标,类型)
//     幂等覆盖的，不必为了"跨重启续攒"去加持久列（那是四个方言 + 三份全量快照的成本）。
//   - 每条应答的**块当场落库**，不等结算。否则中途超时 / 缺条时会连已经读到的值一起丢掉。
//
// ## 收敛判据（三条，任一到即结算）
//
//  1. **类型覆盖完整**：收到的块覆盖了查询里的全部类型。语义最正，但按上面的实测，
//     设备对"不支持的类型"回的是**不带标识的空应答**，这条对海康恒不成立。
//  2. **条数到齐**：收到的应答条数 ≥ 查询的类型数。**海康走的就是这条**（7 条含 3 条空）。
//  3. **静止收敛**：[configReadSettleWindow] 内没有新应答。这条是为"设备少回几条"兜底的 ——
//     典型是 2016 设备收到 7 个类型只回 1 条无块应答，此时判据 1、2 永远不成立；
//     没有它就会一直干等到 transport deadline，把"设备不支持这些类型"（正确的
//     `type_absent` 结论）误报成"超时"。
//
// ⛔ 单类型查询（`len(expected) == 1`，操作员手点「读取设备参数」和对账子 operation 都是）
// 在第一判据上立即结算，行为与聚合前**逐字节一致** —— 聚合只对多类型查询产生新语义。
const (
	// configReadSettleWindow 静止收敛窗口。
	//
	// 取值依据：海康 7 条应答在 2ms 内到齐，局域网设备正常在百毫秒级；1.5s 足够区分
	// "设备还在回"与"设备就回这么多"。而 scheduler tick 是 250ms，窗口内至少跨 6 个 tick，
	// 不会因为 tick 抖动提前收敛。
	// ⛔ 它必须显著小于 transport deadline（5s）：窗口是"提前结算"，不是"延长等待"。
	configReadSettleWindow = 1500 * time.Millisecond
)

// configReadStage 是一次配置读取在"收集期"内的累积体。
//
// ⛔ 它是**进程内**状态，字段刻意不含任何需要跨重启续攒的东西：重启后 operation 会被
// 重发，新一轮累积从零开始，这正是想要的（半截的旧状态比没有更危险）。
type configReadStage struct {
	// expected 本次查询请求的配置类型（来自 operation payload，与报文构建同源）。
	expected []string
	// received 已经回过块（`present`）的类型。空应答不计入 —— 它不携带类型标识，
	// 记不出"这个类型已回过"，这正是判据 1 对部分设备不适用的原因。
	received map[string]struct{}
	// responses 已收到的应答条数（**含空应答**）。
	responses int
	// hasData 是否至少有一条应答带回了块。结算时写进 operation.response_has_data。
	hasData bool
	// blocks 累积到的全部配置块，结算时用于回读对账（对账只比双方都在场的块）。
	blocks manscdp.DeviceConfigBlocks
	// lastObservedAt 最后一条应答的到达时间，静止收敛窗口的基准。
	lastObservedAt time.Time
}

// accumulateConfigReadStage 把一条应答并入累积体，并报告是否已经收齐。
//
// 调用方必须持有 [Service.configReadMu]。
func (s *Service) accumulateConfigReadStage(operation gbmodels.GbPTZOperation, result *manscdp.DeviceConfigReadResult, observedAt time.Time) configReadStage {
	stage := cloneConfigReadStage(s.configReadStages[operation.OperationID])
	if stage.received == nil {
		stage.received = make(map[string]struct{}, 4)
	}
	// payload 是唯一真源：重发 / 历史 operation 都能据此还原"这次问了哪些类型"。
	// 解析不出来时留空 → expected 为空 → 第一条应答即结算，退回聚合之前的行为。
	stage.expected = expectedConfigReadTypes(operation)

	present := result.Blocks.PresentConfigTypes()
	for _, configType := range present {
		stage.received[configType] = struct{}{}
	}
	if len(present) > 0 {
		stage.hasData = true
	}
	stage.blocks = mergeDeviceConfigBlocks(stage.blocks, result.Blocks)
	stage.responses++
	stage.lastObservedAt = observedAt
	return stage
}

// configReadStageComplete 报告累积体是否已经收齐（判据 1 / 2）。
func configReadStageComplete(stage configReadStage) bool {
	if len(stage.expected) == 0 {
		// 没有期望（payload 不可读）：不聚合，第一条即终态。
		return true
	}
	if stage.responses >= len(stage.expected) {
		return true
	}
	for _, configType := range stage.expected {
		if _, ok := stage.received[configType]; !ok {
			return false
		}
	}
	return true
}

// expectedConfigReadTypes 从 operation payload 取回本次查询的配置类型。
//
// ⛔ 取不到就返回 nil 而不是报错：读应答**已经收到了**，此时报错会让一条真实存在的
// 设备事实因为没有"期望清单"而落不了库。宁可退回"单条即终态"。
func expectedConfigReadTypes(operation gbmodels.GbPTZOperation) []string {
	types, err := decodeDeviceConfigReadTypes(operation.PayloadJSON)
	if err != nil {
		return nil
	}
	return types
}

// decodeDeviceConfigReadTypes 解析读 operation 的 payload。
//
// 与 [decodeDeviceConfigApplyTypes] 的区别：下发 payload 里配置类型要从 `blocks`
// 反推（块的在场性就是事实），而读 payload 只有 `configTypes` 一个列表。两者不共用
// 是因为"反推"对读 payload 恒为空 —— 共用会让每次读都退化成单条即终态。
func decodeDeviceConfigReadTypes(payloadJSON string) ([]string, error) {
	if strings.TrimSpace(payloadJSON) == "" {
		return nil, nil
	}
	var payload struct {
		ConfigTypes []string `json:"configTypes"`
	}
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return nil, err
	}
	if len(payload.ConfigTypes) == 0 {
		return nil, nil
	}
	types, err := normalizeDeviceConfigTypes(payload.ConfigTypes)
	if err != nil {
		// 请求侧写入 payload 时已经过同一把 normalize，走到这里说明是历史数据或结构演进：
		// 当作"没有期望清单"，退回单条即终态，而不是把应答判成非法。
		return nil, nil
	}
	return types, nil
}

// mergeDeviceConfigBlocks 把新到的块并进累积体。
//
// ⛔ 逐字段复制的"八行"是刻意的，别换成反射：`ConfigTypeOrder` / `Block()` 是
// 「类型名 → 块」的**单向**映射，没有反向 setter。反射版本要靠字段名拼 ConfigType 名，
// 那会变成第二份映射真源 —— 加一个新配置类型时它会静默漏掉一块。
// 这里漏掉一行的后果是编译期可见的（新增字段不会自动出现在这里，但 present 判定
// 与其字段一一对应，测试会红）。
func mergeDeviceConfigBlocks(dst, src manscdp.DeviceConfigBlocks) manscdp.DeviceConfigBlocks {
	if src.BasicParam != nil {
		dst.BasicParam = src.BasicParam
	}
	if src.VideoParamOpt != nil {
		dst.VideoParamOpt = src.VideoParamOpt
	}
	if src.VideoRecordPlan != nil {
		dst.VideoRecordPlan = src.VideoRecordPlan
	}
	if src.VideoAlarmRecord != nil {
		dst.VideoAlarmRecord = src.VideoAlarmRecord
	}
	if src.PictureMask != nil {
		dst.PictureMask = src.PictureMask
	}
	if src.FrameMirror != nil {
		dst.FrameMirror = src.FrameMirror
	}
	if src.AlarmReport != nil {
		dst.AlarmReport = src.AlarmReport
	}
	if src.OSDConfig != nil {
		dst.OSDConfig = src.OSDConfig
	}
	return dst
}

func cloneConfigReadStage(source configReadStage) configReadStage {
	clone := configReadStage{
		expected:       append([]string(nil), source.expected...),
		responses:      source.responses,
		hasData:        source.hasData,
		blocks:         source.blocks,
		lastObservedAt: source.lastObservedAt,
	}
	if source.received != nil {
		clone.received = make(map[string]struct{}, len(source.received)+1)
		for configType := range source.received {
			clone.received[configType] = struct{}{}
		}
	}
	return clone
}

// settleConfigReadStages 把静止超时的收集期结算掉（判据 3）。
//
// 挂在与 [Service.cleanupQueryStages] 同一处 tick 上 —— 两者都是"operation 还在等应答
// 时的进程内簿记维护"，分成两条 sql 之外的后台步骤最省心。
func (s *Service) settleConfigReadStages(ctx context.Context, now time.Time) error {
	if s == nil || s.db == nil {
		return nil
	}
	s.configReadMu.Lock()
	candidates := make(map[string]configReadStage)
	for operationID, stage := range s.configReadStages {
		if now.Sub(stage.lastObservedAt) < configReadSettleWindow {
			continue
		}
		candidates[operationID] = stage
	}
	s.configReadMu.Unlock()
	if len(candidates) == 0 {
		return nil
	}

	settled := make([]string, 0, len(candidates))
	for operationID, stage := range candidates {
		if err := s.settleConfigReadStage(ctx, operationID, stage); err != nil {
			return err
		}
		settled = append(settled, operationID)
	}
	s.configReadMu.Lock()
	for _, operationID := range settled {
		delete(s.configReadStages, operationID)
	}
	s.configReadMu.Unlock()
	return nil
}

// settleConfigReadStage 用已收到的部分结算一次配置读取。
//
// ⛔ CAS 失败**不算错误**：它正是"这个 operation 已经不由我们负责了"的表达 ——
// 已经被某条应答结算、已经被别的路径判终态、或者已经超时。此时直接把累积体丢掉，
// 不能报错（报错会让 tick 卡在这里，后面的 stage 再也不结算）。
func (s *Service) settleConfigReadStage(ctx context.Context, operationID string, stage configReadStage) error {
	var operation gbmodels.GbPTZOperation
	found := ptzWriter(s.db).WithContext(ctx).Where("operation_id = ?", operationID).Limit(1).Find(&operation)
	if found.Error != nil {
		return found.Error
	}
	if found.RowsAffected == 0 {
		return nil
	}

	hasData := stage.hasData
	settledAt := s.now()
	var applied bool
	err := schedulerTransaction(ctx, s.db, func(tx *gorm.DB) error {
		applied = false
		var err error
		// 终态转换本身就是一次 CAS：status 已不在 (queued, sent, unknown)、或者
		// deadline 已经过去时，它不会命中任何行。所以"这个 operation 还归我们管吗"
		// 不需要单独的预检，`applied == false` 就是答案。
		applied, err = applyPTZResponseTransition(tx, operation, "", "", ptzResponseTransition{
			Status: gbmodels.PTZOperationAccepted, DeviceResult: "OK", ResponseHasData: &hasData,
		}, settledAt)
		if err != nil {
			return err
		}
		if !applied {
			return nil
		}
		return markDeviceConfigReconcileMismatch(tx, operation, stage.blocks)
	})
	if err != nil {
		return err
	}
	if !applied {
		return nil
	}
	app.Log(ctx).Named("ptz").Info("配置读取在静止窗口后结算",
		zap.String("event", "ptz.device_config.read_settled"),
		zap.String("operation_id", operation.OperationID),
		zap.String("device_code", operation.DeviceCode),
		zap.Int("expected_types", len(stage.expected)),
		zap.Int("responses", stage.responses),
		zap.Int("received_types", len(stage.received)),
		zap.Bool("has_data", hasData),
	)
	return nil
}

func (s *Service) discardConfigReadStage(operationID string) {
	if s == nil || operationID == "" {
		return
	}
	s.configReadMu.Lock()
	delete(s.configReadStages, operationID)
	s.configReadMu.Unlock()
}

func (s *Service) clearConfigReadStages() {
	if s == nil {
		return
	}
	s.configReadMu.Lock()
	s.configReadStages = make(map[string]configReadStage)
	s.configReadMu.Unlock()
}
