package ptz

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// ---- 脚手架 ----

func newDeviceConfigTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbPTZOperation{}, &gbmodels.GbPTZOperationAttempt{}, &gbmodels.GbDeviceConfig{}))
	service, err := NewService(db, &fakeTrackedSender{}, videoParamTestNow)
	require.NoError(t, err)
	return service, db
}

// createDeviceConfigOperation 造一条"已发出、等应答"的 operation。
// ⛔ transport_deadline_at / deadline_at 必须设：应答落地的 CAS 要求 operation 仍在
// 有效窗口内，不设的话 applyPTZResponseTransition 会**静默不生效**（用例会假绿）。
func createDeviceConfigOperation(t *testing.T, db *gorm.DB, p struct {
	OperationID string
	Action      string
	CmdType     string
	DeviceID    uint
	TargetCode  string
	Trigger     *string
	PayloadJSON string
}) gbmodels.GbPTZOperation {
	t.Helper()
	deadline := videoParamTestNow().Add(time.Minute)
	operation := gbmodels.GbPTZOperation{
		OperationID: p.OperationID, IdempotencyKey: p.OperationID,
		DeviceID: p.DeviceID, DeviceCode: "D", ChannelID: 7, ChannelCode: p.TargetCode,
		TargetCode: p.TargetCode, CmdType: p.CmdType, Action: p.Action,
		SN: 5, Status: gbmodels.PTZOperationSent, ResponseRequired: true,
		TransportDeadlineAt: &deadline, DeadlineAt: &deadline,
		TriggerOperationID: p.Trigger, PayloadJSON: p.PayloadJSON,
		CreatedAt: videoParamTestNow(),
	}
	require.NoError(t, db.Create(&operation).Error)
	return operation
}

func configResponse(sn int, deviceID, blocksXML string) []byte {
	return []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\r\n" +
		"<Response><CmdType>ConfigDownload</CmdType>\r\n" +
		"<SN>" + strconv.Itoa(sn) + "</SN><DeviceID>" + deviceID + "</DeviceID>" +
		"<Result>OK</Result>" + blocksXML + "</Response>")
}

func loadDeviceConfigs(t *testing.T, db *gorm.DB, deviceID uint, targetCode string) []gbmodels.GbDeviceConfig {
	t.Helper()
	var list []gbmodels.GbDeviceConfig
	require.NoError(t, db.Where("device_id = ? AND target_code = ?", deviceID, targetCode).
		Order("config_type").Find(&list).Error)
	return list
}

func deviceConfigPayloadFor(t *testing.T, blocks manscdp.DeviceConfigBlocks) string {
	t.Helper()
	payload, err := canonicalPayload(map[string]interface{}{
		"configTypes": blocks.PresentConfigTypes(),
		"blocks":      blocks,
	})
	require.NoError(t, err)
	return payload
}

// ---- 读取与落库 ----

// TestApplyDeviceConfigReadResponsePersistsOneRowPerType 一条应答带多组配置时，
// **每组一行**，且缺席的类型不产生行 —— 缺席是 type_absent 的判据，不是"空配置"。
func TestApplyDeviceConfigReadResponsePersistsOneRowPerType(t *testing.T) {
	service, db := newDeviceConfigTestService(t)
	operation := createDeviceConfigOperation(t, db, struct {
		OperationID string
		Action      string
		CmdType     string
		DeviceID    uint
		TargetCode  string
		Trigger     *string
		PayloadJSON string
	}{OperationID: "dc-read-1", Action: actionRefreshDeviceConfigs,
		CmdType: manscdp.CmdConfigDownload, DeviceID: 1, TargetCode: "C1"})

	body := configResponse(5, "C1",
		"<FrameMirror>1</FrameMirror>"+
			"<AlarmReport><MotionDetection>1</MotionDetection><FieldDetection>0</FieldDetection></AlarmReport>"+
			"<PictureMask><On>1</On><SumNum>1</SumNum><RegionList Num=\"1\">"+
			"<Item><Seq>1</Seq><Point>20,30,50,60</Point></Item></RegionList></PictureMask>")

	require.NoError(t, service.applyDeviceConfigReadResponse(context.Background(), operation, "call-1", "1", body))

	list := loadDeviceConfigs(t, db, 1, "C1")
	require.Len(t, list, 3, "应答带了三组配置，就该有三行")

	byType := make(map[string]gbmodels.GbDeviceConfig, len(list))
	for _, row := range list {
		byType[row.ConfigType] = row
		require.Equal(t, operation.ID, row.SourceOperationSeq, "迟到应答保护依据必须是这次 operation 的 id")
	}
	require.Contains(t, byType, manscdp.ConfigTypeFrameMirror)
	require.Contains(t, byType, manscdp.ConfigTypeAlarmReport)
	require.Contains(t, byType, manscdp.ConfigTypePictureMask)
	require.NotContains(t, byType, manscdp.ConfigTypeOSDConfig, "没回的类型不该凭空多出一行")

	// payload 必须能解回原值：它是对账比较的左边，存歪了对账就没意义。
	var mirror manscdp.FrameMirrorBlock
	require.NoError(t, json.Unmarshal([]byte(byType[manscdp.ConfigTypeFrameMirror].PayloadJSON), &mirror))
	require.Equal(t, manscdp.FrameMirrorLeftRight, mirror.Value)

	var mask manscdp.PictureMaskBlock
	require.NoError(t, json.Unmarshal([]byte(byType[manscdp.ConfigTypePictureMask].PayloadJSON), &mask))
	require.Equal(t, "20,30,50,60", mask.Regions[0].PointLiteral())

	// 应答必须落到 accepted + response_has_data=true。
	require.Equal(t, gbmodels.PTZOperationAccepted, reloadPTZStatus(t, db, operation.ID))
	var reloaded gbmodels.GbPTZOperation
	require.NoError(t, db.Where("id = ?", operation.ID).Limit(1).Find(&reloaded).Error)
	require.NotNil(t, reloaded.ResponseHasData)
	require.True(t, *reloaded.ResponseHasData)
}

// TestApplyDeviceConfigReadResponseLateAnswerDoesNotOverwrite 迟到的旧应答不许覆盖新值。
//
// ⛔ 这条例用两遍同样的报文、不同的 operation 序号，断言的是**落库的 source_operation_seq
// 不倒退** —— 只断言 payload 相等是测不出来的（两份 payload 本来就一样）。
func TestApplyDeviceConfigReadResponseLateAnswerDoesNotOverwrite(t *testing.T) {
	service, db := newDeviceConfigTestService(t)
	older := createDeviceConfigOperation(t, db, struct {
		OperationID string
		Action      string
		CmdType     string
		DeviceID    uint
		TargetCode  string
		Trigger     *string
		PayloadJSON string
	}{OperationID: "dc-read-old", Action: actionRefreshDeviceConfigs,
		CmdType: manscdp.CmdConfigDownload, DeviceID: 1, TargetCode: "C1"})
	newer := createDeviceConfigOperation(t, db, struct {
		OperationID string
		Action      string
		CmdType     string
		DeviceID    uint
		TargetCode  string
		Trigger     *string
		PayloadJSON string
	}{OperationID: "dc-read-new", Action: actionRefreshDeviceConfigs,
		CmdType: manscdp.CmdConfigDownload, DeviceID: 1, TargetCode: "C1"})

	require.NoError(t, service.applyDeviceConfigReadResponse(context.Background(), newer, "c", "1",
		configResponse(5, "C1", "<FrameMirror>2</FrameMirror>")))
	require.NoError(t, service.applyDeviceConfigReadResponse(context.Background(), older, "c", "2",
		configResponse(5, "C1", "<FrameMirror>1</FrameMirror>")))

	list := loadDeviceConfigs(t, db, 1, "C1")
	require.Len(t, list, 1)
	require.Equal(t, newer.ID, list[0].SourceOperationSeq, "旧应答不该把序号写回去")

	var mirror manscdp.FrameMirrorBlock
	require.NoError(t, json.Unmarshal([]byte(list[0].PayloadJSON), &mirror))
	require.Equal(t, manscdp.FrameMirrorUpDown, mirror.Value, "值也必须还是新的那份")
}

func reloadPTZStatus(t *testing.T, db *gorm.DB, id uint) gbmodels.PTZOperationStatus {
	t.Helper()
	var operation gbmodels.GbPTZOperation
	require.NoError(t, db.Where("id = ?", id).Limit(1).Find(&operation).Error)
	return operation.Status
}

// ---- 写入 ack → 自动对账 ----

// TestApplyDeviceConfigAckQueuesReconcileWithSameTypes 写入被接受后必须排一条
// **同类型集合**的回读对账。
//
// ⛔ 对账类型取自父 operation 的 payload 而不是重新推导：推导等于写第二份判定，
// 与实际发出的报文一旦不一致，对账就会去回读一组没发过的类型（结果恒为 type_absent，
// 看起来像"设备不支持"）。
func TestApplyDeviceConfigAckQueuesReconcileWithSameTypes(t *testing.T) {
	service, db := newDeviceConfigTestService(t)
	blocks := manscdp.DeviceConfigBlocks{
		FrameMirror: &manscdp.FrameMirrorBlock{Value: manscdp.FrameMirrorLeftRight},
		AlarmReport: &manscdp.AlarmReportBlock{MotionDetection: manscdp.AlarmReportOn, FieldDetection: manscdp.AlarmReportOff},
	}
	operation := createDeviceConfigOperation(t, db, struct {
		OperationID string
		Action      string
		CmdType     string
		DeviceID    uint
		TargetCode  string
		Trigger     *string
		PayloadJSON string
	}{OperationID: "dc-apply-1", Action: actionApplyDeviceConfig,
		CmdType: manscdp.CmdDeviceConfig, DeviceID: 1, TargetCode: "C1",
		PayloadJSON: deviceConfigPayloadFor(t, blocks)})

	ack := []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\r\n" +
		"<Response><CmdType>DeviceConfig</CmdType><SN>5</SN><DeviceID>C1</DeviceID>" +
		"<Result>OK</Result></Response>")
	require.NoError(t, service.applyDeviceConfigAckResponse(context.Background(), operation, "c", "1", ack))

	var reloaded gbmodels.GbPTZOperation
	require.NoError(t, db.Where("id = ?", operation.ID).Limit(1).Find(&reloaded).Error)
	require.Equal(t, gbmodels.PTZOperationAccepted, reloaded.Status)
	require.NotNil(t, reloaded.ReconcileOperationID, "ack 之后必须排一条对账回读")

	var child gbmodels.GbPTZOperation
	require.NoError(t, db.Where("operation_id = ?", *reloaded.ReconcileOperationID).Limit(1).Find(&child).Error)
	require.Equal(t, manscdp.CmdConfigDownload, child.CmdType)
	require.Equal(t, actionRefreshDeviceConfigs, child.Action)
	require.Equal(t, operation.OperationID, *child.TriggerOperationID)

	var payload struct {
		ConfigTypes []string `json:"configTypes"`
	}
	require.NoError(t, json.Unmarshal([]byte(child.PayloadJSON), &payload))
	require.Equal(t, blocks.PresentConfigTypes(), payload.ConfigTypes)
}

// ---- 对账比较 ----

// TestDiffDeviceConfigBlocksSkipsAdditionalStreams 多码流设备回读时 AdditionalStreams>0，
// 它不是协议字段，**不许**被判成不一致。
//
// ⛔ 这是一条真会发生的假差异：平台构建时该字段恒为 0，设备回读时可能 >0。
// 不排除它，每次多码流设备的对账都会多报一格 "0 ≠ 1"，把人引去查不存在的配置问题。
func TestDiffDeviceConfigBlocksSkipsAdditionalStreams(t *testing.T) {
	wanted := manscdp.DeviceConfigBlocks{
		VideoRecordPlan: &manscdp.VideoRecordPlanBlock{
			RecordEnable: manscdp.AlarmReportOn, StreamNumber: 0,
			Schedules: []manscdp.RecordSchedule{{WeekDayNum: 1, Segments: []manscdp.RecordTimeSegment{
				{StartHour: 8, StartMin: 0, StartSec: 0, StopHour: 12, StopMin: 30, StopSec: 0},
			}}},
		},
	}
	observed := manscdp.DeviceConfigBlocks{
		VideoRecordPlan: &manscdp.VideoRecordPlanBlock{
			RecordEnable: manscdp.AlarmReportOn, StreamNumber: 0, AdditionalStreams: 2,
			Schedules: []manscdp.RecordSchedule{{WeekDayNum: 1, Segments: []manscdp.RecordTimeSegment{
				{StartHour: 8, StartMin: 0, StartSec: 0, StopHour: 12, StopMin: 30, StopSec: 0},
			}}},
		},
	}
	require.Empty(t, diffDeviceConfigBlocks(wanted, observed),
		"AdditionalStreams 是解析器标注，不是协议字段，不该产生差异")
}

// TestDiffDeviceConfigBlocksOnlyComparesTypesPresentOnBothSides 只比双方都在场的块。
func TestDiffDeviceConfigBlocksOnlyComparesTypesPresentOnBothSides(t *testing.T) {
	wanted := manscdp.DeviceConfigBlocks{
		FrameMirror: &manscdp.FrameMirrorBlock{Value: manscdp.FrameMirrorLeftRight},
		OSDConfig:   &manscdp.OSDConfigBlock{Length: 1920, Width: 1080},
	}
	observed := manscdp.DeviceConfigBlocks{
		FrameMirror: &manscdp.FrameMirrorBlock{Value: manscdp.FrameMirrorLeftRight},
		// OSDConfig 缺席：那是 type_absent，不是"不一致"。
		// AlarmReport 额外出现：平台这次没配它，也不该算差异。
		AlarmReport: &manscdp.AlarmReportBlock{MotionDetection: manscdp.AlarmReportOn},
	}
	require.Empty(t, diffDeviceConfigBlocks(wanted, observed),
		"缺席与未下发都不是不一致，只有双方都在场的块才参与比较")
}

// TestDiffDeviceConfigBlocksIgnoresMaskRegionsWhenDisabled 关闭状态下的区域残留
// **不是**「值未生效」—— 这是 2026-09-19 的真机现场（海康 DS-2DC2C040MY-DE）：
//
//	平台下发 {on:0,regions:[]}（操作员只点了"停用"、没画区域；前端会跳过全 0 槽位）
//	设备回读 {on:0,regions:[{1,144,295,704,576}]}（保留原生区域，**合法**）
//	⇒ 修复前报 "PictureMask.regions=[](实际 […])" ⇒ 界面说「设备已接受命令，但值未生效」
//
// ⛔ 这条假差异的杀伤力在于把**已经成功**说成没生效：遮挡在设备侧**确实已移除**
// （`On=0`），操作员却以为功能坏了，于是反复下发。
//
// 标准依据：A.2.1.17 里 `On` 是 0/1 开关、`RegionList` 独立且 `minOccurs="0"`，
// **没有**「关闭时必须一并清空区域」这条规定。
func TestDiffDeviceConfigBlocksIgnoresMaskRegionsWhenDisabled(t *testing.T) {
	wanted := manscdp.DeviceConfigBlocks{
		PictureMask: &manscdp.PictureMaskBlock{
			On: manscdp.PictureMaskOff, Regions: []manscdp.PictureMaskRegion{},
		},
	}
	observed := manscdp.DeviceConfigBlocks{
		PictureMask: &manscdp.PictureMaskBlock{On: manscdp.PictureMaskOff, Regions: []manscdp.PictureMaskRegion{
			{Seq: 1, Left: 144, Top: 295, Right: 704, Bottom: 576},
		}},
	}
	require.Empty(t, diffDeviceConfigBlocks(wanted, observed),
		"关闭状态下设备保留区域是标准允许的形态，不该报「值未生效」")
}

// TestDiffDeviceConfigBlocksStillReportsMaskSwitchWhenDeviceIgnoredIt
// 忽略区域**不能**连开关本身一起忽略：设备没执行停用必须报出来。
//
// ⛔ 这是上一条的安全边界：把 `on` 也放过，就等于"停用永远报成功"，
// 那是把假差异换成了假绿 —— 更糟。
func TestDiffDeviceConfigBlocksStillReportsMaskSwitchWhenDeviceIgnoredIt(t *testing.T) {
	wanted := manscdp.DeviceConfigBlocks{
		PictureMask: &manscdp.PictureMaskBlock{On: manscdp.PictureMaskOff},
	}
	observed := manscdp.DeviceConfigBlocks{
		PictureMask: &manscdp.PictureMaskBlock{On: manscdp.PictureMaskOn, Regions: []manscdp.PictureMaskRegion{
			{Seq: 1, Left: 144, Top: 295, Right: 704, Bottom: 576},
		}},
	}
	diffs := diffDeviceConfigBlocks(wanted, observed)
	require.Len(t, diffs, 1, "设备没停用遮挡时必须报出 on 这一格")
	require.Equal(t, "PictureMask.on", diffs[0].Path)
}

// TestDiffDeviceConfigBlocksIgnoresMaskRegionsWhenEitherSideDisabled
// 口径是「**任一边**关闭即忽略区域」，不是"两边都关闭"。
//
// 理由：区域内容在任一边关闭时都不构成有效承诺，而 `On` 的差异由 `on` 那一格
// 单独报出 —— 不需要靠 regions 来间接表达"状态不同"。
func TestDiffDeviceConfigBlocksIgnoresMaskRegionsWhenEitherSideDisabled(t *testing.T) {
	// 平台说要启用（并画了区），设备回的是关闭：只报 on，不报 regions。
	wanted := manscdp.DeviceConfigBlocks{
		PictureMask: &manscdp.PictureMaskBlock{On: manscdp.PictureMaskOn, Regions: []manscdp.PictureMaskRegion{
			{Seq: 1, Left: 10, Top: 10, Right: 20, Bottom: 20},
		}},
	}
	observed := manscdp.DeviceConfigBlocks{
		PictureMask: &manscdp.PictureMaskBlock{On: manscdp.PictureMaskOff, Regions: []manscdp.PictureMaskRegion{}},
	}
	diffs := diffDeviceConfigBlocks(wanted, observed)
	require.Len(t, diffs, 1, "只应报 on 一格；关闭态的区域内容不参与比较")
	require.Equal(t, "PictureMask.on", diffs[0].Path)
}

// TestDiffDeviceConfigBlocksComparesMaskRegionsWhenEnabled 两边都**启用**时，
// 区域照常逐格比。
//
// ⛔ 这是前三条的反向守卫：别为了消掉关闭态的假差异，把启用态的真差异一起放过。
// 启用状态下区域是双方都要负责的约定值。
func TestDiffDeviceConfigBlocksComparesMaskRegionsWhenEnabled(t *testing.T) {
	wanted := manscdp.DeviceConfigBlocks{
		PictureMask: &manscdp.PictureMaskBlock{On: manscdp.PictureMaskOn, Regions: []manscdp.PictureMaskRegion{
			{Seq: 1, Left: 10, Top: 10, Right: 20, Bottom: 20},
		}},
	}

	// ① 同一条区域、坐标不同 ⇒ 必须指到具体那一格。
	coordinateDiff := diffDeviceConfigBlocks(wanted, manscdp.DeviceConfigBlocks{
		PictureMask: &manscdp.PictureMaskBlock{On: manscdp.PictureMaskOn, Regions: []manscdp.PictureMaskRegion{
			{Seq: 1, Left: 10, Top: 10, Right: 30, Bottom: 20},
		}},
	})
	require.Len(t, coordinateDiff, 1)
	require.Equal(t, "PictureMask.regions[0].right", coordinateDiff[0].Path)
	require.Equal(t, "20", coordinateDiff[0].Wanted)
	require.Equal(t, "30", coordinateDiff[0].Actual)

	// ② 平台画了 2 个区、设备只报 1 个 ⇒ 条数不同也要报。
	countDiff := diffDeviceConfigBlocks(manscdp.DeviceConfigBlocks{
		PictureMask: &manscdp.PictureMaskBlock{On: manscdp.PictureMaskOn, Regions: []manscdp.PictureMaskRegion{
			{Seq: 1, Left: 10, Top: 10, Right: 20, Bottom: 20},
			{Seq: 2, Left: 30, Top: 30, Right: 40, Bottom: 40},
		}},
	}, wanted)
	require.Len(t, countDiff, 1)
	require.Equal(t, "PictureMask.regions", countDiff[0].Path)
}

// TestDiffDeviceConfigBlocksIgnoresBlankMaskRegions 零面积条目**不是一条遮挡**，
// 不该由对账来判它 —— 它是"删掉这一槽"的表达（真机证据见 `manscdp.PictureMaskClearPoint`）。
//
// 现场背景（2026-09-20，海康）：平台为"被删掉的槽位"显式补零面积之后，设备**有时**会把
// 删除痕迹回读回来（2026-09-19 实测全零形态回 `704,576,704,576`、`Num` 仍是 1）。
// **回显与不回显都是合法形态**：不摘掉就会出现「下发 3 条、设备回 3 条真 + 1 条零 ⇒
// 长度不等 ⇒ 报"值未生效"」这种明明做对了却说没生效的假差异。
//
// ⛔ 反向守卫见下一条用例：**有面积**的差异照旧要报，别把它做成"regions 一律不比"。
func TestDiffDeviceConfigBlocksIgnoresBlankMaskRegions(t *testing.T) {
	wanted := manscdp.DeviceConfigBlocks{
		PictureMask: &manscdp.PictureMaskBlock{On: manscdp.PictureMaskOn, Regions: []manscdp.PictureMaskRegion{
			{Seq: 1, Left: 286, Top: 396, Right: 419, Bottom: 547},
			{Seq: 2, Left: 560, Top: 50, Right: 694, Bottom: 351},
			{Seq: 3, Left: 89, Top: 132, Right: 307, Bottom: 335},
		}},
	}
	// ① 设备把删除痕迹回显回来（第 4 条零面积，而且坐标被设备换成了它自己画布上的写法）。
	require.Empty(t, diffDeviceConfigBlocks(wanted, manscdp.DeviceConfigBlocks{
		PictureMask: &manscdp.PictureMaskBlock{On: manscdp.PictureMaskOn, Regions: []manscdp.PictureMaskRegion{
			{Seq: 1, Left: 286, Top: 396, Right: 419, Bottom: 547},
			{Seq: 2, Left: 560, Top: 50, Right: 694, Bottom: 351},
			{Seq: 3, Left: 89, Top: 132, Right: 307, Bottom: 335},
			{Seq: 4, Left: 704, Top: 576, Right: 704, Bottom: 576},
		}},
	}), "零面积删除痕迹回显不该算不一致")

	// ② 设备把第 4 条**真的删掉了**（压根不在回显里）⇒ 同样一致。
	require.Empty(t, diffDeviceConfigBlocks(wanted, manscdp.DeviceConfigBlocks{
		PictureMask: &manscdp.PictureMaskBlock{On: manscdp.PictureMaskOn, Regions: []manscdp.PictureMaskRegion{
			{Seq: 1, Left: 286, Top: 396, Right: 419, Bottom: 547},
			{Seq: 2, Left: 560, Top: 50, Right: 694, Bottom: 351},
			{Seq: 3, Left: 89, Top: 132, Right: 307, Bottom: 335},
		}},
	}), "设备照做删掉了被删的槽位，不该报差异")

	// ③ ⛔ 反向守卫：设备**没删**（第 4 条真区域还在）⇒ 必须报出来 —— 这正是 2026-09-20
	// 现场那条「设备已接受命令，但值未生效」的真身，摘零面积不能把它一起摘掉。
	retained := diffDeviceConfigBlocks(wanted, manscdp.DeviceConfigBlocks{
		PictureMask: &manscdp.PictureMaskBlock{On: manscdp.PictureMaskOn, Regions: []manscdp.PictureMaskRegion{
			{Seq: 1, Left: 286, Top: 396, Right: 419, Bottom: 547},
			{Seq: 2, Left: 560, Top: 50, Right: 694, Bottom: 351},
			{Seq: 3, Left: 89, Top: 132, Right: 307, Bottom: 335},
			{Seq: 4, Left: 365, Top: 135, Right: 508, Bottom: 330},
		}},
	})
	require.Len(t, retained, 1, "设备留着一条平台已删掉的**真**区域，必须报出来")
	require.Equal(t, "PictureMask.regions", retained[0].Path)
}

// TestDiffDeviceConfigBlocksFoldsOSDPositionsToRowGrid 纵向落位被设备折算到行网格
// **不是**「值未生效」—— 这是 2026-09-20 的真机现场（海康 DS-2DC2C040MY-DE）：
//
//	操作员把时间戳拖到 (18, 51)，界面立刻说
//	「设备已接受命令，但值未生效：回读值与下发值不一致: OSDConfig.timeY=51(实际 48)」，
//	而时间戳在画面上的**位置明明变了** —— 这就是操作员说的"其实已经生效了"。
//
// 真机控制变量扫描的结论（tmp/osd_grid_probe.py + tmp/osd_item_probe.py）：
// **纵向只能落在行高 16 的整数倍上（向下取整），横向是逐像素的**。
// 折算幅度不足一行（<16 像素），所以肉眼看到的就是"移过去了"。
//
// ⛔ 本用例的后半段是反向守卫：别为了消掉这条假差异，把"真没照做"一起放过。
func TestDiffDeviceConfigBlocksFoldsOSDPositionsToRowGrid(t *testing.T) {
	osd := func(timeX, timeY int, items ...manscdp.OSDTextItem) manscdp.DeviceConfigBlocks {
		return manscdp.DeviceConfigBlocks{OSDConfig: &manscdp.OSDConfigBlock{
			Length: 704, Width: 576, TimeX: timeX, TimeY: timeY,
			TimeEnable: manscdp.AlarmReportOn, TextEnable: manscdp.AlarmReportOff,
			Items: items,
		}}
	}

	// ① 现场那条：拖到 (18, 51)、设备落 (18, 48) ⇒ 已经生效，不该报。
	require.Empty(t, diffDeviceConfigBlocks(osd(18, 51), osd(18, 48)),
		"纵向折算到行网格是设备的落位精度，不是「值未生效」")

	// ② 网格上的 16 的倍数原样落位（真机上 33→32、100→96、255→240 全都只差取整余数）。
	require.Empty(t, diffDeviceConfigBlocks(osd(18, 100), osd(18, 96)),
		"100 折成 96 == 设备实际落位，一致")

	// ③ 文本行走同一条网格（真机实测 Text Y 37→32、100→96，X 25/33 原样回读）。
	require.Empty(t, diffDeviceConfigBlocks(
		osd(18, 32, manscdp.OSDTextItem{Text: "UVP", X: 25, Y: 37}),
		osd(18, 32, manscdp.OSDTextItem{Text: "UVP", X: 25, Y: 32}),
	), "文本行的 y 与 TimeY 是同一套行网格")

	// ④ ⛔ 反向守卫 A：落在网格上的纵向差异是真差异 —— 32 对上 33 必须报。
	require.Len(t, diffDeviceConfigBlocks(osd(18, 32), osd(18, 33)), 1,
		"折算只吸收「不足一行」的取整，设备真把位置放错一整行必须报出来")

	// ⑤ ⛔ 反向守卫 B：横向**不吸附** —— 25 对上 26 必须报
	// （真机上 TimeX 18/289/317 与文本行 X 25/33 全部原样回读，含两个奇数）。
	horizontal := diffDeviceConfigBlocks(osd(25, 48), osd(26, 48))
	require.Len(t, horizontal, 1, "横向是逐像素的，任何差异都是真差异")
	require.Equal(t, "OSDConfig.timeX", horizontal[0].Path)

	// ⑥ ⛔ 反向守卫 C：纵向**真**没照做 —— 51 对上 200 必须报，
	// 且报出来的是折算后的值（读者才知道差在哪一行）。
	far := diffDeviceConfigBlocks(osd(18, 51), osd(18, 200))
	require.Len(t, far, 1)
	require.Equal(t, "48", far[0].Wanted, "报出的应是设备能落到的那个值")
	require.Equal(t, "200", far[0].Actual)

	// ⑦ ⛔ 反向守卫 D：设备回一个**不在网格上**的值 ⇒ 必须报。
	// 回读侧刻意不折：两边都折的话 `49` 会被折成 48 吞掉，那是把假差异换成假绿。
	offGrid := diffDeviceConfigBlocks(osd(18, 51), osd(18, 49))
	require.Len(t, offGrid, 1, "回读值不在行网格上说明设备没按预期落位")
	require.Equal(t, "48", offGrid[0].Wanted)
	require.Equal(t, "49", offGrid[0].Actual)
}

// TestDiffDeviceConfigBlocksReportsNestedPathAndValues 差异必须指到具体字段，
// 否则面板只能说"不一致"而不能说"哪一格不一致"。
func TestDiffDeviceConfigBlocksReportsNestedPathAndValues(t *testing.T) {
	wanted := manscdp.DeviceConfigBlocks{
		AlarmReport: &manscdp.AlarmReportBlock{MotionDetection: manscdp.AlarmReportOn, FieldDetection: manscdp.AlarmReportOff},
		// ⛔ 这里的 Y 取 16 的倍数（= 行网格上的值）：本用例要验证的是"路径带下标、
		// 值能读出来"，不是纵向折算 —— 拿 34 这种网格外的值会被 [foldOSDPositionsToRowGrid]
		// 折成 32，把两个语义混在一条断言里。折算本身由
		// TestDiffDeviceConfigBlocksFoldsOSDPositionsToRowGrid 单独覆盖。
		OSDConfig: &manscdp.OSDConfigBlock{Length: 1920, Width: 1080,
			Items: []manscdp.OSDTextItem{{Text: "东门", X: 10, Y: 32}}},
	}
	observed := manscdp.DeviceConfigBlocks{
		// 设备没执行动检，且 OSD 的第一个文本项 Y 与下发不同。
		AlarmReport: &manscdp.AlarmReportBlock{MotionDetection: manscdp.AlarmReportOff, FieldDetection: manscdp.AlarmReportOff},
		OSDConfig: &manscdp.OSDConfigBlock{Length: 1920, Width: 1080,
			Items: []manscdp.OSDTextItem{{Text: "东门", X: 10, Y: 40}}},
	}

	diffs := diffDeviceConfigBlocks(wanted, observed)
	require.Len(t, diffs, 2)

	paths := map[string]string{}
	for _, diff := range diffs {
		paths[diff.Path] = diff.Wanted + "→" + diff.Actual
	}
	require.Contains(t, paths, "AlarmReport.motionDetection")
	require.Equal(t, "1→0", paths["AlarmReport.motionDetection"])
	require.Contains(t, paths, "OSDConfig.items[0].y", "数组元素必须带下标，否则多元素的配置指不清是哪一项")
	require.Equal(t, "32→40", paths["OSDConfig.items[0].y"])

	text := formatDeviceConfigDiff(diffs)
	require.Contains(t, text, "AlarmReport.motionDetection=1(实际 0)")
	require.NotContains(t, text, "失败", "不一致是信息不是错误：设备能力边界也会造成它")
}

// TestFormatDeviceConfigDiffCapsOutput 差异条数必须有上限：
// error_message 是要落库的短文本，一整份 OSD 配置逐格展开会变成一面看不完的墙。
func TestFormatDeviceConfigDiffCapsOutput(t *testing.T) {
	total := deviceConfigDiffLimit + 5
	diffs := make([]deviceConfigDiff, 0, total)
	for index := 0; index < total; index++ {
		diffs = append(diffs, deviceConfigDiff{
			ConfigType: manscdp.ConfigTypeOSDConfig,
			Path:       "OSDConfig.items[" + strconv.Itoa(index) + "].x",
			Wanted:     "1", Actual: "2",
		})
	}
	text := formatDeviceConfigDiff(diffs)
	require.Equal(t, deviceConfigDiffLimit, strings.Count(text, "实际"),
		"只应列出前 deviceConfigDiffLimit 格")
	require.Contains(t, text, "仅列出前")
	require.Contains(t, text, strconv.Itoa(total), "必须报出差异总数，否则读者不知道被截掉了多少")
}

// TestMarkDeviceConfigReconcileMismatchOnlyForDerivedReads 只有"由下发派生出来的回读"
// 才判 mismatch：操作员手点的「读取设备配置」没有可比的意图。
func TestMarkDeviceConfigReconcileMismatchOnlyForDerivedReads(t *testing.T) {
	service, db := newDeviceConfigTestService(t)
	wanted := manscdp.DeviceConfigBlocks{
		FrameMirror: &manscdp.FrameMirrorBlock{Value: manscdp.FrameMirrorLeftRight},
	}
	parent := createDeviceConfigOperation(t, db, struct {
		OperationID string
		Action      string
		CmdType     string
		DeviceID    uint
		TargetCode  string
		Trigger     *string
		PayloadJSON string
	}{OperationID: "dc-parent", Action: actionApplyDeviceConfig,
		CmdType: manscdp.CmdDeviceConfig, DeviceID: 1, TargetCode: "C1",
		PayloadJSON: deviceConfigPayloadFor(t, wanted)})

	// 手动读取（没有 trigger）→ 即使值不同也不许标 mismatch。
	manual := createDeviceConfigOperation(t, db, struct {
		OperationID string
		Action      string
		CmdType     string
		DeviceID    uint
		TargetCode  string
		Trigger     *string
		PayloadJSON string
	}{OperationID: "dc-manual", Action: actionRefreshDeviceConfigs,
		CmdType: manscdp.CmdConfigDownload, DeviceID: 1, TargetCode: "C1"})
	require.NoError(t, service.applyDeviceConfigReadResponse(context.Background(), manual, "c", "1",
		configResponse(5, "C1", "<FrameMirror>2</FrameMirror>")))
	var manualRow gbmodels.GbPTZOperation
	require.NoError(t, db.Where("id = ?", manual.ID).Limit(1).Find(&manualRow).Error)
	require.Empty(t, manualRow.ErrorCode, "手动读取不该被判成 mismatch")

	// 派生读取 → 值不同必须标 mismatch，且子 operation 仍是 accepted（设备确实接受了命令）。
	trigger := parent.OperationID
	derived := createDeviceConfigOperation(t, db, struct {
		OperationID string
		Action      string
		CmdType     string
		DeviceID    uint
		TargetCode  string
		Trigger     *string
		PayloadJSON string
	}{OperationID: "dc-derived", Action: actionRefreshDeviceConfigs,
		CmdType: manscdp.CmdConfigDownload, DeviceID: 1, TargetCode: "C1", Trigger: &trigger})
	require.NoError(t, service.applyDeviceConfigReadResponse(context.Background(), derived, "c", "2",
		configResponse(5, "C1", "<FrameMirror>2</FrameMirror>")))

	var derivedRow gbmodels.GbPTZOperation
	require.NoError(t, db.Where("id = ?", derived.ID).Limit(1).Find(&derivedRow).Error)
	require.Equal(t, ptzErrorDeviceConfigReconcileMismatch, derivedRow.ErrorCode)
	require.Equal(t, gbmodels.PTZOperationAccepted, derivedRow.Status)
	require.Contains(t, derivedRow.ErrorMessage, "FrameMirror.value")
}

// TestDeriveDeviceConfigReconcileStateRecognizesFamilyMismatchCode 状态推导必须认识
// **配置家族自己的**不一致码。
//
// ⛔ 这正是"同一件事写两份判定"的典型受伤点：码有两个，而推导函数原先只认视频参数那个。
// 漏认的表现是面板显示 read_ok（一切正常），实际回读值与下发值不一致 —— 静默的假绿灯。
func TestDeriveDeviceConfigReconcileStateRecognizesFamilyMismatchCode(t *testing.T) {
	hasData := true
	latest := &gbmodels.GbPTZOperation{
		Status: gbmodels.PTZOperationAccepted, ErrorCode: ptzErrorDeviceConfigReconcileMismatch,
		ResponseHasData: &hasData, ErrorMessage: "回读值与下发值不一致: X=1(实际 2)",
	}
	state := DeriveDeviceConfigReconcileState(latest)
	require.Equal(t, DeviceConfigStateMismatch, state.State)

	for _, code := range []string{ptzErrorVideoParamReconcileMismatch, ptzErrorDeviceConfigReconcileMismatch} {
		require.True(t, isReconcileMismatchCode(code), "两种不一致码都必须被判成 mismatch")
	}
	require.False(t, isReconcileMismatchCode(""))
	require.False(t, isReconcileMismatchCode("SOME_OTHER"))

	// 设备回了 OK 但没带元素 → type_absent（能力问题），与 mismatch 分开。
	absent := false
	require.Equal(t, DeviceConfigStateTypeAbsent,
		DeriveDeviceConfigReconcileState(&gbmodels.GbPTZOperation{
			Status: gbmodels.PTZOperationAccepted, ResponseHasData: &absent,
		}).State)
	require.Equal(t, DeviceConfigStateNeverRead, DeriveDeviceConfigReconcileState(nil).State)
	require.Equal(t, DeviceConfigStatePending, DeriveDeviceConfigReconcileState(&gbmodels.GbPTZOperation{
		Status: gbmodels.PTZOperationSent,
	}).State)
}

// ---- 类型规范化 ----

func TestNormalizeDeviceConfigTypesOrdersDedupesAndRejects(t *testing.T) {
	got, err := normalizeDeviceConfigTypes([]string{"OSDConfig", " FrameMirror ", "OSDConfig", ""})
	require.NoError(t, err)
	// ⛔ 顺序必须是 ConfigTypeOrder 的顺序，不是入参顺序：报文元素顺序抖动会让
	// 抓包比对与日志 diff 误报"报文变了"。
	require.Equal(t, []string{manscdp.ConfigTypeFrameMirror, manscdp.ConfigTypeOSDConfig}, got)

	_, err = normalizeDeviceConfigTypes([]string{"VideoParamAttribute"})
	require.Error(t, err, "VideoParamAttribute 有自己的通道，通用通道不许认它")
	_, err = normalizeDeviceConfigTypes(nil)
	require.Error(t, err, "空类型列表没有语义，必须拒发")
	_, err = normalizeDeviceConfigTypes([]string{"  "})
	require.Error(t, err)
	_, err = normalizeDeviceConfigTypes([]string{"SVACEncodeConfig"})
	require.Error(t, err, "平台还没落地 SVAC 配置族，读请求不许认它")
}

// TestNormalizeDeviceConfigTypesAcceptsSnapShotConfig 钉住抓拍配置**能读**。
//
// ⛔ 这条用例的来历（2026-09-20）：`SnapShotConfig` 早就有常量、但**零消费者**，
// 于是读请求里带它会被"未知的配置类型"拒发 —— 抓拍配置"配了之后无法回读当前值"
// 正是从这一行开始的。查询侧的合法取值出自 A.2.4.7 的明文列举（图像抓拍配置：SnapShotConfig）。
func TestNormalizeDeviceConfigTypesAcceptsSnapShotConfig(t *testing.T) {
	got, err := normalizeDeviceConfigTypes([]string{manscdp.ConfigTypeSnapShotConfig})
	require.NoError(t, err)
	require.Equal(t, []string{manscdp.ConfigTypeSnapShotConfig}, got)

	// ⛔ 排在最末：它与 A.2.3.2.12（下发）和 A.2.6.9（应答）里抓拍元素的位置一致，
	// 而 ConfigTypeOrder 是对外契约（构建顺序 / PresentConfigTypes / 差异列表三处同源）。
	require.Equal(t, manscdp.ConfigTypeSnapShotConfig, manscdp.ConfigTypeOrder[len(manscdp.ConfigTypeOrder)-1])
}

// TestValidateDeviceConfigChannelTypesRejectsSnapshotThroughGenericAPI 钉住
// 「抓拍配置不能从通用下发接口进来」。
//
// ⛔ 这条是**平台策略**、不是协议结论：A.2.3.2.12 允许下发 `<SnapShotConfig>`，
// 但它的 `UploadURL` 是**设备往哪里 POST 图像**的地址。通用接口若收客户端给的值，
// 等于允许任何持"设备配置下发"权限的账号把摄像头画面推到他自己的服务器上。
// 抓拍配置只走抓拍会话（SessionID 与带令牌的上传地址由平台生成）。
//
// ⛔ 反向锚点：**其它类型必须照常放行**，否则这条守卫会把整个配置族的下发一起锁死。
func TestValidateDeviceConfigChannelTypesRejectsSnapshotThroughGenericAPI(t *testing.T) {
	err := validateDeviceConfigChannelTypes([]string{manscdp.ConfigTypeSnapShotConfig})
	require.Error(t, err)
	require.Contains(t, err.Error(), manscdp.ConfigTypeSnapShotConfig)
	require.Contains(t, err.Error(), "抓拍会话", "错误文案必须告诉调用方正确的入口")

	// 混在一组里也必须整条拒绝，不能"只丢掉那一块"。
	require.Error(t, validateDeviceConfigChannelTypes([]string{
		manscdp.ConfigTypeOSDConfig, manscdp.ConfigTypeSnapShotConfig,
	}))

	for _, allowed := range []string{
		manscdp.ConfigTypeBasicParam, manscdp.ConfigTypeVideoRecordPlan,
		manscdp.ConfigTypeVideoAlarmRecord, manscdp.ConfigTypePictureMask,
		manscdp.ConfigTypeFrameMirror, manscdp.ConfigTypeAlarmReport, manscdp.ConfigTypeOSDConfig,
	} {
		require.NoError(t, validateDeviceConfigChannelTypes([]string{allowed}),
			"%s 是本通道的正常下发类型，不该被守卫挡住", allowed)
	}
}

// ---- 两条读链路的分派（「两道门禁」的位置） ----

// TestOnPTZMessageRoutesConfigDownloadByAction 两条读链路共用 `ConfigDownload` 这个
// CmdType，靠 action 分流。这条用例同时钉住两件事：
//
//  1. 配置家族的读必须落在 gb_device_config；
//  2. **视频参数（已上线）的读仍然落在 gb_device_video_param**，不能被通用容器截走。
//
// ⛔ 第 2 点是本仓栽过三次的同一类错（"同一件事写两份判定，改一处忘一处"）：
// 分派漏掉 A-5 那一支的后果是它的值被写进另一张表，面板从此读不到新数据，
// 而两侧日志都不报错 —— 典型的静默功能失效。所以这里**必须**做跨链路的互斥断言，
// 只断言"家族那条通了"是不够的。
func TestOnPTZMessageRoutesConfigDownloadByAction(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbPTZOperation{}, &gbmodels.GbPTZOperationAttempt{},
		&gbmodels.GbDeviceConfig{}, &gbmodels.GbDeviceVideoParam{}))
	service, err := NewService(db, &fakeTrackedSender{}, videoParamTestNow)
	require.NoError(t, err)

	// ---- 配置家族的读 ----
	family := createDeviceConfigOperation(t, db, struct {
		OperationID string
		Action      string
		CmdType     string
		DeviceID    uint
		TargetCode  string
		Trigger     *string
		PayloadJSON string
	}{OperationID: "route-family", Action: actionRefreshDeviceConfigs,
		CmdType: manscdp.CmdConfigDownload, DeviceID: 1, TargetCode: "C1"})
	require.NoError(t, service.OnPTZMessage(context.Background(), "D", "call-f", "1",
		configResponse(5, "C1", "<FrameMirror>1</FrameMirror>")))

	require.Len(t, loadDeviceConfigs(t, db, 1, "C1"), 1, "配置家族的读必须落 gb_device_config")

	var familyVideoParams []gbmodels.GbDeviceVideoParam
	require.NoError(t, db.Find(&familyVideoParams).Error)
	require.Empty(t, familyVideoParams, "配置家族的读绝不该写进视频参数表")
	require.Equal(t, gbmodels.PTZOperationAccepted, reloadPTZStatus(t, db, family.ID))

	// ---- 视频参数（A-5）的读：同一 CmdType、不同 action ----
	legacy := createDeviceConfigOperation(t, db, struct {
		OperationID string
		Action      string
		CmdType     string
		DeviceID    uint
		TargetCode  string
		Trigger     *string
		PayloadJSON string
	}{OperationID: "route-legacy", Action: actionRefreshVideoParams,
		CmdType: manscdp.CmdConfigDownload, DeviceID: 1, TargetCode: "C1"})
	legacyBody := []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\r\n" +
		"<Response><CmdType>ConfigDownload</CmdType><SN>5</SN><DeviceID>C1</DeviceID><Result>OK</Result>" +
		"<VideoParamAttribute><Item><StreamNumber>0</StreamNumber><VideoFormat>2</VideoFormat>" +
		"<Resolution>2</Resolution><FrameRate>25</FrameRate><BitRateType>1</BitRateType>" +
		"<VideoBitRate>2048</VideoBitRate></Item></VideoParamAttribute></Response>")
	require.NoError(t, service.OnPTZMessage(context.Background(), "D", "call-l", "2", legacyBody))

	var legacyParams []gbmodels.GbDeviceVideoParam
	require.NoError(t, db.Where("device_id = ?", 1).Find(&legacyParams).Error)
	require.Len(t, legacyParams, 1, "视频参数的读必须仍然落 gb_device_video_param")

	require.Len(t, loadDeviceConfigs(t, db, 1, "C1"), 1,
		"视频参数的读绝不该在 gb_device_config 里多出一行")
	require.Equal(t, gbmodels.PTZOperationAccepted, reloadPTZStatus(t, db, legacy.ID))
}

// TestOnPTZMessageRoutesDeviceConfigAckByAction 写入 ack 同样靠 action 分流，
// 两条支路都必须排上各自的对账子 operation（都走 ConfigDownload）。
func TestOnPTZMessageRoutesDeviceConfigAckByAction(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbPTZOperation{}, &gbmodels.GbPTZOperationAttempt{}, &gbmodels.GbDeviceConfig{}))
	service, err := NewService(db, &fakeTrackedSender{}, videoParamTestNow)
	require.NoError(t, err)

	blocks := manscdp.DeviceConfigBlocks{FrameMirror: &manscdp.FrameMirrorBlock{Value: manscdp.FrameMirrorLeftRight}}
	operation := createDeviceConfigOperation(t, db, struct {
		OperationID string
		Action      string
		CmdType     string
		DeviceID    uint
		TargetCode  string
		Trigger     *string
		PayloadJSON string
	}{OperationID: "route-ack", Action: actionApplyDeviceConfig,
		CmdType: manscdp.CmdDeviceConfig, DeviceID: 1, TargetCode: "C1",
		PayloadJSON: deviceConfigPayloadFor(t, blocks)})

	ack := []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\r\n" +
		"<Response><CmdType>DeviceConfig</CmdType><SN>5</SN><DeviceID>C1</DeviceID><Result>OK</Result></Response>")
	require.NoError(t, service.OnPTZMessage(context.Background(), "D", "call-a", "1", ack))

	var reloaded gbmodels.GbPTZOperation
	require.NoError(t, db.Where("id = ?", operation.ID).Limit(1).Find(&reloaded).Error)
	require.Equal(t, gbmodels.PTZOperationAccepted, reloaded.Status)
	require.NotNil(t, reloaded.ReconcileOperationID, "写入 ack 必须排上回读对账")

	var child gbmodels.GbPTZOperation
	require.NoError(t, db.Where("operation_id = ?", *reloaded.ReconcileOperationID).Limit(1).Find(&child).Error)
	require.Equal(t, actionRefreshDeviceConfigs, child.Action,
		"对账子 operation 必须走配置家族的回读 action，否则它会被分派到 A-5 那条链路")
}

// ⛔⛔ 回归锚点（2026-09-20 海康真机）：**抓拍会话（action=`snapshot_config`）的写入 ack
// 必须走配置族那一支**。这是"抓拍整条链路静默失效"的第二个断面。
//
// 走错支路**不报任何错**，后果比报错严重得多：A-5 那条支路派生出的回读去查
// `VideoParamAttribute`，而父 payload 里装的是 `SnapShotConfig` ——
// [diffDeviceConfigBlocks] 只比"双方都在场"的块，两边一个都对不上 ⇒ 差异 0 条 ⇒ 判 read_ok。
// 面板显示"抓拍配置已生效"，实际设备那边一块配置都没配。
func TestOnPTZMessageRoutesSnapshotConfigAckToBlockFamily(t *testing.T) {
	service, db := newDeviceConfigTestService(t)

	snapNum, interval := 3, 3
	uploadURL := "http://192.168.10.120:8280/api/gb28181/device-snapshots/uploads/tok/"
	sessionID := strings.Repeat("s", 32)
	blocks := manscdp.DeviceConfigBlocks{SnapShot: &manscdp.SnapShotBlock{
		SnapNum: &snapNum, Interval: &interval, UploadURL: &uploadURL, SessionID: &sessionID,
	}}
	operation := createDeviceConfigOperation(t, db, struct {
		OperationID string
		Action      string
		CmdType     string
		DeviceID    uint
		TargetCode  string
		Trigger     *string
		PayloadJSON string
	}{OperationID: "route-snapshot-ack", Action: ActionSnapshotConfig,
		CmdType: manscdp.CmdDeviceConfig, DeviceID: 1, TargetCode: "C1",
		PayloadJSON: deviceConfigPayloadFor(t, blocks)})

	ack := []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\r\n" +
		"<Response><CmdType>DeviceConfig</CmdType><SN>5</SN><DeviceID>C1</DeviceID><Result>OK</Result></Response>")
	require.NoError(t, service.OnPTZMessage(context.Background(), "D", "call-snap", "1", ack))

	var reloaded gbmodels.GbPTZOperation
	require.NoError(t, db.Where("id = ?", operation.ID).Limit(1).Find(&reloaded).Error)
	require.Equal(t, gbmodels.PTZOperationAccepted, reloaded.Status)
	require.NotNil(t, reloaded.ReconcileOperationID, "抓拍配置的写入 ack 必须排上回读对账")

	var child gbmodels.GbPTZOperation
	require.NoError(t, db.Where("operation_id = ?", *reloaded.ReconcileOperationID).Limit(1).Find(&child).Error)
	require.Equal(t, actionRefreshDeviceConfigs, child.Action)

	var payload struct {
		ConfigTypes []string `json:"configTypes"`
	}
	require.NoError(t, json.Unmarshal([]byte(child.PayloadJSON), &payload))
	require.Equal(t, []string{manscdp.ConfigTypeSnapShotConfig}, payload.ConfigTypes,
		"对账必须回读 SnapShotConfig；回读 VideoParamAttribute 等于没对账（差异恒为 0 条）")
}
