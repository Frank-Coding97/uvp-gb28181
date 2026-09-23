package ptz

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// ⛔ 回归锚点（2026-09-19 海康 DS-2DC2C040MY-DE 真机验证发现）：
// 标准 A.2.4.7 明文允许「同 SN 多个响应，每个对应一个配置类型」，海康就是**每个类型
// 一条独立 SIP MESSAGE**（查 7 个类型回 7 条，2ms 内到齐，第一条恰好是空应答）。
//
// 修之前：第一条就把 operation 收成 accepted，后面全部落进 logIgnoredPTZResponse ——
// 画面遮挡 / 镜像 / OSD 一个字段都读不出来，operation 还显示
// `accepted + response_has_data=false`，症状酷似"设备不支持"。**设备合规，平台违规。**
//
// 修之后：首条不再终结，收齐（或静止）才结算，期间每条应答的块都当场落库。

func configReadPayload(t *testing.T, configTypes ...string) string {
	t.Helper()
	payload, err := canonicalPayload(map[string]interface{}{"configTypes": configTypes})
	require.NoError(t, err)
	return payload
}

func newConfigReadOperation(t *testing.T, db *gorm.DB, operationID string, configTypes ...string) gbmodels.GbPTZOperation {
	t.Helper()
	return createDeviceConfigOperation(t, db, struct {
		OperationID string
		Action      string
		CmdType     string
		DeviceID    uint
		TargetCode  string
		Trigger     *string
		PayloadJSON string
	}{
		OperationID: operationID, Action: actionRefreshDeviceConfigs,
		CmdType: manscdp.CmdConfigDownload, DeviceID: 1, TargetCode: "C1",
		PayloadJSON: configReadPayload(t, configTypes...),
	})
}

func reloadOperation(t *testing.T, db *gorm.DB, id uint) gbmodels.GbPTZOperation {
	t.Helper()
	var operation gbmodels.GbPTZOperation
	require.NoError(t, db.Where("id = ?", id).Limit(1).Find(&operation).Error)
	return operation
}

// TestDeviceConfigReadAggregatesSameSNAcrossResponses **真机 7 条序列**（2026-09-19
// 海康 DS-2DC2C040MY-DE 抓包复原）：平台一条 SN=844 的报文查 7 个类型，设备回 7 条
// 独立 MESSAGE（各自新 Call-ID，2ms 内到齐），其中 3 条是**不带任何类型标识的空应答**。
//
// 断言的是修之前丢得最狠的那条：画面遮挡在第 4 条里，原来被整条丢弃。
func TestDeviceConfigReadAggregatesSameSNAcrossResponses(t *testing.T) {
	service, db := newDeviceConfigTestService(t)
	operation := newConfigReadOperation(t, db, "dc-agg-1",
		manscdp.ConfigTypeBasicParam, manscdp.ConfigTypeVideoRecordPlan,
		manscdp.ConfigTypeVideoAlarmRecord, manscdp.ConfigTypePictureMask,
		manscdp.ConfigTypeFrameMirror, manscdp.ConfigTypeAlarmReport,
		manscdp.ConfigTypeOSDConfig)

	// 按真机顺序逐条喂入。⛔ 第一条是**有值的 BasicParam**（不是空应答）——
	// 修之前正是它赢了 CAS 把 operation 收成终态，于是后面 6 条全被丢弃。
	responses := []string{
		"<BasicParam><Name>IP CAMERA</Name><Expiration>3600</Expiration>" +
			"<HeartBeatInterval>60</HeartBeatInterval><HeartBeatCount>3</HeartBeatCount>" +
			"<PositionCapability>0</PositionCapability></BasicParam>",
		"", // ② 空应答：对应 VideoRecordPlan，报文里没有任何类型标识
		"", // ③ 空应答：对应 VideoAlarmRecord
		"<PictureMask><On>0</On><SumNum>1</SumNum><RegionList Num=\"1\">" +
			"<Item><Seq>1</Seq><Point>144,295,704,576</Point></Item></RegionList></PictureMask>",
		"<FrameMirror>2</FrameMirror>",
		"", // ⑥ 空应答：对应 AlarmReport
		"<OSDConfig><Length>704</Length><Width>576</Width><TimeX>0</TimeX><TimeY>32</TimeY>" +
			"<TimeEnable>1</TimeEnable><TimeType>1</TimeType><TextEnable>0</TextEnable>" +
			"<SumNum>0</SumNum></OSDConfig>",
	}
	for index, inner := range responses {
		callID := "call-" + strconv.Itoa(index+1)
		require.NoError(t, service.applyDeviceConfigReadResponse(context.Background(),
			operation, callID, "20", configResponse(5, "C1", inner)))
		if index < len(responses)-1 {
			pending := reloadOperation(t, db, operation.ID)
			require.Equal(t, gbmodels.PTZOperationSent, pending.Status,
				"第 %d 条应答不许把 operation 收成终态（原因: %s）",
				index+1, pending.ErrorMessage)
		}
	}

	settled := reloadOperation(t, db, operation.ID)
	require.Equal(t, gbmodels.PTZOperationAccepted, settled.Status)
	require.NotNil(t, settled.ResponseHasData)
	require.True(t, *settled.ResponseHasData, "读到过块，has_data 就必须是 true")

	list := loadDeviceConfigs(t, db, 1, "C1")
	types := make([]string, 0, len(list))
	for _, row := range list {
		types = append(types, row.ConfigType)
	}
	require.ElementsMatch(t, []string{
		manscdp.ConfigTypeBasicParam, manscdp.ConfigTypePictureMask,
		manscdp.ConfigTypeFrameMirror, manscdp.ConfigTypeOSDConfig,
	}, types, "7 条里带块的 4 条都要落库；3 条空应答不产生行")

	for _, row := range list {
		if row.ConfigType != manscdp.ConfigTypePictureMask {
			continue
		}
		var stored manscdp.PictureMaskBlock
		require.NoError(t, decodeStoredPayload(t, row.PayloadJSON, &stored))
		require.Len(t, stored.Regions, 1)
		require.Equal(t, "144,295,704,576", stored.Regions[0].PointLiteral(),
			"遮挡区域的值必须原样落库 —— 这正是本次真机排查读不出来的那个字段")
		require.Equal(t, 0, stored.On, "设备明确回了 On=0（停用但区域仍在），不许被平台改写成别的值")
	}
}

// TestDeviceConfigReadKeepsFirstBlockWhenLaterResponsesArrive 首条带块的应答
// 不许被后到的应答顶掉（累积是**并集**，不是覆盖）。
func TestDeviceConfigReadKeepsFirstBlockWhenLaterResponsesArrive(t *testing.T) {
	service, db := newDeviceConfigTestService(t)
	operation := newConfigReadOperation(t, db, "dc-union-1",
		manscdp.ConfigTypeFrameMirror, manscdp.ConfigTypePictureMask)

	require.NoError(t, service.applyDeviceConfigReadResponse(context.Background(), operation, "call-a", "20",
		configResponse(5, "C1", "<FrameMirror>2</FrameMirror>")))
	require.NoError(t, service.applyDeviceConfigReadResponse(context.Background(), operation, "call-b", "20",
		configResponse(5, "C1", "<PictureMask><On>1</On><SumNum>1</SumNum><RegionList Num=\"1\">"+
			"<Item><Seq>1</Seq><Point>10,10,20,20</Point></Item></RegionList></PictureMask>")))

	require.Equal(t, gbmodels.PTZOperationAccepted, reloadPTZStatus(t, db, operation.ID))
	require.Len(t, loadDeviceConfigs(t, db, 1, "C1"), 2, "两条应答各自的块都要在库里")
}

// TestDeviceConfigReadSettlesOnQuietWindow 设备少回几条时靠**静止窗口**结算，
// 不能干等到 transport deadline（那会把"设备不支持这些类型"误报成"超时"）。
//
// 这是 2016 设备查 2022 配置类型时的真实形态：只回一条无块应答。
func TestDeviceConfigReadSettlesOnQuietWindow(t *testing.T) {
	service, db := newDeviceConfigTestService(t)
	operation := newConfigReadOperation(t, db, "dc-quiet-1",
		manscdp.ConfigTypePictureMask, manscdp.ConfigTypeFrameMirror, manscdp.ConfigTypeOSDConfig)

	require.NoError(t, service.applyDeviceConfigReadResponse(context.Background(), operation, "call-a", "1",
		configResponse(5, "C1", "")))
	require.Equal(t, gbmodels.PTZOperationSent, reloadPTZStatus(t, db, operation.ID))

	// 窗口未到：不许提前结算。
	require.NoError(t, service.settleConfigReadStages(context.Background(), videoParamTestNow().Add(time.Second)))
	require.Equal(t, gbmodels.PTZOperationSent, reloadPTZStatus(t, db, operation.ID),
		"窗口内还有可能来下一条应答，不能提前结算")

	require.NoError(t, service.settleConfigReadStages(context.Background(),
		videoParamTestNow().Add(configReadSettleWindow+time.Millisecond)))

	settled := reloadOperation(t, db, operation.ID)
	require.Equal(t, gbmodels.PTZOperationAccepted, settled.Status,
		"静止后必须结算成 accepted：设备回了但没数据，结论是 type_absent，不是 timeout")
	require.NotNil(t, settled.ResponseHasData)
	require.False(t, *settled.ResponseHasData, "一条块都没回来，has_data=false")

	// 结算后收集期表要清干净，不能随历史 operation 无限累积。
	service.configReadMu.Lock()
	_, leak := service.configReadStages[operation.OperationID]
	service.configReadMu.Unlock()
	require.False(t, leak)
}

// TestDeviceConfigReadQuietWindowKeepsPartialData 结算保留已读到的部分：
// 缺条时已经落库的块不能因为"没收到其余类型"被回滚或清理。
func TestDeviceConfigReadQuietWindowKeepsPartialData(t *testing.T) {
	service, db := newDeviceConfigTestService(t)
	operation := newConfigReadOperation(t, db, "dc-quiet-2",
		manscdp.ConfigTypePictureMask, manscdp.ConfigTypeFrameMirror, manscdp.ConfigTypeOSDConfig)

	require.NoError(t, service.applyDeviceConfigReadResponse(context.Background(), operation, "call-a", "1",
		configResponse(5, "C1", "<PictureMask><On>0</On><SumNum>1</SumNum><RegionList Num=\"1\">"+
			"<Item><Seq>1</Seq><Point>1,2,3,4</Point></Item></RegionList></PictureMask>")))

	// 窗口内落库已经发生（不等结算）。
	require.Len(t, loadDeviceConfigs(t, db, 1, "C1"), 1)

	require.NoError(t, service.settleConfigReadStages(context.Background(),
		videoParamTestNow().Add(configReadSettleWindow+time.Millisecond)))

	require.Equal(t, gbmodels.PTZOperationAccepted, reloadPTZStatus(t, db, operation.ID))
	require.Len(t, loadDeviceConfigs(t, db, 1, "C1"), 1, "部分数据必须留下")
}

// TestDeviceConfigReadSingleTypeKeepsLegacyBehaviour 单类型查询（操作员手点
// 「读取设备参数」、以及下发后的对账子 operation 都是这一形态）在首条应答即结算 ——
// 聚合只对多类型查询产生新语义，向后兼容是硬要求。
func TestDeviceConfigReadSingleTypeKeepsLegacyBehaviour(t *testing.T) {
	service, db := newDeviceConfigTestService(t)
	operation := newConfigReadOperation(t, db, "dc-single-1", manscdp.ConfigTypePictureMask)

	require.NoError(t, service.applyDeviceConfigReadResponse(context.Background(), operation, "call-a", "1",
		configResponse(5, "C1", "<PictureMask><On>0</On><SumNum>1</SumNum></PictureMask>")))

	require.Equal(t, gbmodels.PTZOperationAccepted, reloadPTZStatus(t, db, operation.ID),
		"单类型查询的首条应答就是终态（与聚合前一致）")

	service.configReadMu.Lock()
	_, leak := service.configReadStages[operation.OperationID]
	service.configReadMu.Unlock()
	require.False(t, leak, "已结算的 operation 不许在收集期表里留残留")
}

// TestDeviceConfigReadWithoutExpectedTypesDoesNotStall payload 解不出类型清单时
// 退回"单条即终态"，绝不能让一条**已经收到**的设备事实因为"没有期望清单"落不了库。
func TestDeviceConfigReadWithoutExpectedTypesDoesNotStall(t *testing.T) {
	service, db := newDeviceConfigTestService(t)
	operation := newConfigReadOperation(t, db, "dc-noexpect-1")

	require.NoError(t, service.applyDeviceConfigReadResponse(context.Background(), operation, "call-a", "1",
		configResponse(5, "C1", "<FrameMirror>1</FrameMirror>")))
	require.Equal(t, gbmodels.PTZOperationAccepted, reloadPTZStatus(t, db, operation.ID))
	require.Len(t, loadDeviceConfigs(t, db, 1, "C1"), 1)
}

// TestDeviceConfigReadQuietSettlementSkipsTerminalOperation operation 已被别处判终态时，
// 静止结算必须安静退出（删掉累积体、不动库）—— 报错会让 tick 卡死，后面的收集期再也不结算。
func TestDeviceConfigReadQuietSettlementSkipsTerminalOperation(t *testing.T) {
	service, db := newDeviceConfigTestService(t)
	operation := newConfigReadOperation(t, db, "dc-terminal-1",
		manscdp.ConfigTypePictureMask, manscdp.ConfigTypeFrameMirror)

	require.NoError(t, service.applyDeviceConfigReadResponse(context.Background(), operation, "call-a", "1",
		configResponse(5, "C1", "")))

	// 模拟另一条路径把 operation 判成 rejected（例如设备明确回错）。
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Where("id = ?", operation.ID).
		Updates(map[string]interface{}{"status": gbmodels.PTZOperationRejected}).Error)

	require.NoError(t, service.settleConfigReadStages(context.Background(),
		videoParamTestNow().Add(configReadSettleWindow+time.Millisecond)))
	require.Equal(t, gbmodels.PTZOperationRejected, reloadPTZStatus(t, db, operation.ID),
		"终态不许被静止结算改写")

	service.configReadMu.Lock()
	_, leak := service.configReadStages[operation.OperationID]
	service.configReadMu.Unlock()
	require.False(t, leak, "结算不了的累积体要丢掉，不能永远占着表")
}

// TestMergeDeviceConfigBlocksKeepsEveryType 累积体必须**按类型合并**：
// 后一条报文只补自己带的块，不能把前一条已经收到的块顶掉。
func TestMergeDeviceConfigBlocksKeepsEveryType(t *testing.T) {
	first := manscdp.DeviceConfigBlocks{
		PictureMask: &manscdp.PictureMaskBlock{
			On: 1, Regions: []manscdp.PictureMaskRegion{{Seq: 1, Left: 10, Top: 10, Right: 20, Bottom: 20}},
		},
	}
	second := manscdp.DeviceConfigBlocks{FrameMirror: &manscdp.FrameMirrorBlock{Value: 2}}

	merged := mergeDeviceConfigBlocks(first, second)
	require.ElementsMatch(t, []string{manscdp.ConfigTypePictureMask, manscdp.ConfigTypeFrameMirror},
		merged.PresentConfigTypes())

	// 同类型再来一条时以新值覆盖（设备对同一类型回了两条时，后一条是更新的）。
	newer := manscdp.DeviceConfigBlocks{FrameMirror: &manscdp.FrameMirrorBlock{Value: 1}}
	merged = mergeDeviceConfigBlocks(merged, newer)
	block, ok := merged.Block(manscdp.ConfigTypeFrameMirror)
	require.True(t, ok)
	require.Equal(t, manscdp.FrameMirrorLeftRight, block.(*manscdp.FrameMirrorBlock).Value)
}

// TestConfigReadStageCompleteJudgements 收敛判据本身的边界。
func TestConfigReadStageCompleteJudgements(t *testing.T) {
	// 无期望清单 → 立即可结算（向后兼容）。
	require.True(t, configReadStageComplete(configReadStage{}))
	// 条数到齐（含空应答计数）—— 海康走的就是这条。
	require.True(t, configReadStageComplete(configReadStage{
		expected:  []string{"PictureMask", "FrameMirror"},
		responses: 2,
		received:  map[string]struct{}{"FrameMirror": {}},
	}))
	// 条数不够但类型覆盖完整 → 也收（设备把两个类型合并成一条回时）。
	require.True(t, configReadStageComplete(configReadStage{
		expected:  []string{"PictureMask", "FrameMirror"},
		responses: 1,
		received:  map[string]struct{}{"PictureMask": {}, "FrameMirror": {}},
	}))
	// 又没到条数、又没覆盖全 → 继续收集。
	require.False(t, configReadStageComplete(configReadStage{
		expected:  []string{"PictureMask", "FrameMirror", "OSDConfig"},
		responses: 1,
		received:  map[string]struct{}{"PictureMask": {}},
	}))
}

func decodeStoredPayload(t *testing.T, payloadJSON string, target interface{}) error {
	t.Helper()
	return json.Unmarshal([]byte(payloadJSON), target)
}
