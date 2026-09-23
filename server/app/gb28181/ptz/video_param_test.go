package ptz

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func videoParamTestNow() time.Time {
	return time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
}

func newVideoParamTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbPTZOperation{}, &gbmodels.GbDeviceVideoParam{}))
	service, err := NewService(db, &fakeTrackedSender{}, videoParamTestNow)
	require.NoError(t, err)
	return service, db
}

// createVideoParamOperation 造一条"已发出、等应答"的 operation。
// ⛔ transport_deadline_at / deadline_at 必须设：应答落地的 CAS 要求
// operation 仍在有效窗口内（见 ptzResponseOperationUpdate），
// 不设的话 applyPTZResponseTransition 会静默不生效。
func createVideoParamOperation(t *testing.T, db *gorm.DB, operationID string, deviceID uint, targetCode, cmdType string) gbmodels.GbPTZOperation {
	t.Helper()
	deadline := videoParamTestNow().Add(time.Minute)
	operation := gbmodels.GbPTZOperation{
		OperationID: operationID, IdempotencyKey: operationID,
		DeviceID: deviceID, DeviceCode: "D", ChannelID: 7, ChannelCode: targetCode,
		CmdType: cmdType, Action: actionRefreshVideoParams,
		SN: 5, Status: gbmodels.PTZOperationSent, ResponseRequired: true,
		TransportDeadlineAt: &deadline, DeadlineAt: &deadline,
		CreatedAt: videoParamTestNow(),
	}
	require.NoError(t, db.Create(&operation).Error)
	return operation
}

func videoParamStrPtr(value string) *string { return &value }

func videoParam(stream int, format, resolution, frameRate, bitRateType string, bitRate *string) manscdp.VideoParamItem {
	return manscdp.VideoParamItem{
		StreamNumber: stream, VideoFormat: format, Resolution: resolution,
		FrameRate: frameRate, BitRateType: bitRateType, VideoBitRate: bitRate,
	}
}

func loadVideoParams(t *testing.T, db *gorm.DB, deviceID uint, targetCode string) []gbmodels.GbDeviceVideoParam {
	t.Helper()
	var list []gbmodels.GbDeviceVideoParam
	require.NoError(t, db.Where("device_id = ? AND target_code = ?", deviceID, targetCode).
		Order("stream_number").Find(&list).Error)
	return list
}

// ---- 落库 ----

func TestPersistVideoParamsStoresOneRowPerStream(t *testing.T) {
	service, db := newVideoParamTestService(t)
	operation := createVideoParamOperation(t, db, "vp-1", 1, "C1", manscdp.CmdConfigDownload)

	require.NoError(t, service.persistVideoParamsWithDB(context.Background(), db, operation, []manscdp.VideoParamItem{
		videoParam(0, manscdp.VideoFormatH264, manscdp.Resolution720P, "25", manscdp.BitRateTypeCBR, videoParamStrPtr("2048")),
		videoParam(1, manscdp.VideoFormatH265, manscdp.ResolutionCIF, "15", manscdp.BitRateTypeVBR, nil),
	}, "body"))

	list := loadVideoParams(t, db, 1, "C1")
	require.Len(t, list, 2)
	require.Equal(t, 0, list[0].StreamNumber)
	require.Equal(t, manscdp.VideoFormatH264, list[0].VideoFormat)
	require.Equal(t, manscdp.Resolution720P, list[0].Resolution)
	require.Equal(t, "25", list[0].FrameRate)
	require.Equal(t, manscdp.BitRateTypeCBR, list[0].BitRateType)
	require.NotNil(t, list[0].VideoBitRate)
	require.Equal(t, "2048", *list[0].VideoBitRate)
	require.Equal(t, operation.ID, list[0].SourceOperationSeq)
	require.Equal(t, 5, list[0].SourceSN)

	require.Equal(t, 1, list[1].StreamNumber)
	require.Nil(t, list[1].VideoBitRate, "VBR 时码率本就该缺席")
}

// ⛔ 落库必须存**附录 G 的码值**，不做码值→人读串的转换。
// 一旦这里存成人读串，对账时比的就是两套表示，设备回 "2" 而库里是 "H.264"
// 会永远判成不一致 —— 而且这种"永远不一致"最难归因，因为它看着像设备不听话。
func TestPersistVideoParamsKeepsAppendixGCodes(t *testing.T) {
	service, db := newVideoParamTestService(t)
	operation := createVideoParamOperation(t, db, "vp-1", 1, "C1", manscdp.CmdConfigDownload)
	require.NoError(t, service.persistVideoParamsWithDB(context.Background(), db, operation, []manscdp.VideoParamItem{
		videoParam(0, "2", "5", "25", "1", videoParamStrPtr("0")),
	}, "body"))

	row := loadVideoParams(t, db, 1, "C1")[0]
	require.Equal(t, "2", row.VideoFormat)
	require.Equal(t, "5", row.Resolution)
	require.Equal(t, "1", row.BitRateType)
	require.NotNil(t, row.VideoBitRate)
	require.Equal(t, "0", *row.VideoBitRate, "设备给了个 0 就必须是 0，与'没给'是两件事")
}

func TestPersistVideoParamsKeepsNewerOperationWhenResponsesArriveOutOfOrder(t *testing.T) {
	service, db := newVideoParamTestService(t)
	older := createVideoParamOperation(t, db, "vp-older", 1, "C1", manscdp.CmdConfigDownload)
	newer := createVideoParamOperation(t, db, "vp-newer", 1, "C1", manscdp.CmdConfigDownload)
	require.Greater(t, newer.ID, older.ID)

	require.NoError(t, service.persistVideoParamsWithDB(context.Background(), db, newer, []manscdp.VideoParamItem{
		videoParam(0, "5", "6", "30", "2", nil),
	}, "newer"))
	// 迟到的旧应答：整批放弃，不能把上面那行覆盖回去。
	require.NoError(t, service.persistVideoParamsWithDB(context.Background(), db, older, []manscdp.VideoParamItem{
		videoParam(0, manscdp.VideoFormatH264, manscdp.ResolutionCIF, "15", manscdp.BitRateTypeCBR, videoParamStrPtr("512")),
	}, "older"))

	list := loadVideoParams(t, db, 1, "C1")
	require.Len(t, list, 1)
	require.Equal(t, "5", list[0].VideoFormat)
	require.Equal(t, newer.ID, list[0].SourceOperationSeq)
}

func TestPersistVideoParamsRemovesStreamsThatDisappeared(t *testing.T) {
	service, db := newVideoParamTestService(t)
	first := createVideoParamOperation(t, db, "vp-first", 1, "C1", manscdp.CmdConfigDownload)
	require.NoError(t, service.persistVideoParamsWithDB(context.Background(), db, first, []manscdp.VideoParamItem{
		videoParam(0, "2", "5", "25", "2", nil),
		videoParam(1, "2", "2", "15", "2", nil),
	}, "first"))

	second := createVideoParamOperation(t, db, "vp-second", 1, "C1", manscdp.CmdConfigDownload)
	require.NoError(t, service.persistVideoParamsWithDB(context.Background(), db, second, []manscdp.VideoParamItem{
		videoParam(0, "2", "5", "25", "2", nil),
	}, "second"))

	list := loadVideoParams(t, db, 1, "C1")
	require.Len(t, list, 1, "子码流被关掉了，必须从缓存里消失，否则前端会一直显示一条不存在的码流")
	require.Equal(t, 0, list[0].StreamNumber)
}

// 带元素但一条 Item 都没有 = 设备明确回了空配置 ⇒ 清空缓存。
// ⛔ 与"设备没带这个元素"（type_absent）是两件事，后者刻意不清（见对应测试）。
func TestPersistVideoParamsEmptiesCacheWhenDeviceReportsEmptyBlock(t *testing.T) {
	service, db := newVideoParamTestService(t)
	first := createVideoParamOperation(t, db, "vp-first", 1, "C1", manscdp.CmdConfigDownload)
	require.NoError(t, service.persistVideoParamsWithDB(context.Background(), db, first, []manscdp.VideoParamItem{
		videoParam(0, "2", "5", "25", "2", nil),
	}, "first"))

	second := createVideoParamOperation(t, db, "vp-second", 1, "C1", manscdp.CmdConfigDownload)
	require.NoError(t, service.persistVideoParamsWithDB(context.Background(), db, second, nil, "empty"))

	require.Empty(t, loadVideoParams(t, db, 1, "C1"))
}

func TestPersistVideoParamsDoesNotTouchOtherTargets(t *testing.T) {
	service, db := newVideoParamTestService(t)
	deviceTarget := createVideoParamOperation(t, db, "vp-device", 1, "DEVICE", manscdp.CmdConfigDownload)
	channelTarget := createVideoParamOperation(t, db, "vp-channel", 1, "CHANNEL", manscdp.CmdConfigDownload)
	require.NoError(t, service.persistVideoParamsWithDB(context.Background(), db, deviceTarget, []manscdp.VideoParamItem{
		videoParam(0, "2", "5", "25", "2", nil),
	}, "device"))
	require.NoError(t, service.persistVideoParamsWithDB(context.Background(), db, channelTarget, []manscdp.VideoParamItem{
		videoParam(0, "2", "2", "15", "2", nil),
		videoParam(1, "2", "1", "15", "2", nil),
	}, "channel"))

	// ⛔ 这就是"清理消失的码流"最容易写错的地方：如果按"不在本次列表里"删，
	// 上面 DEVICE 那一行会被这一轮 CHANNEL 的写入误删。
	require.Len(t, loadVideoParams(t, db, 1, "DEVICE"), 1)
	require.Len(t, loadVideoParams(t, db, 1, "CHANNEL"), 2)
}

func TestPersistVideoParamsRejectsOperationWithoutIdentity(t *testing.T) {
	service, db := newVideoParamTestService(t)
	err := service.persistVideoParamsWithDB(context.Background(), db,
		gbmodels.GbPTZOperation{ID: 0, DeviceID: 1}, nil, "body")
	require.Error(t, err, "没有可靠序列就不能写，否则 CAS 无从谈起")
}

// ---- 读应答 ----

func TestApplyConfigDownloadResponsePersistsRows(t *testing.T) {
	service, db := newVideoParamTestService(t)
	operation := createVideoParamOperation(t, db, "vp-read", 1, "C1", manscdp.CmdConfigDownload)

	body := `<Response><CmdType>ConfigDownload</CmdType><SN>5</SN><DeviceID>C1</DeviceID><Result>OK</Result>` +
		`<VideoParamAttribute Num="1"><Item><StreamNumber>0</StreamNumber><VideoFormat>2</VideoFormat>` +
		`<Resolution>5</Resolution><FrameRate>25</FrameRate><BitRateType>1</BitRateType>` +
		`<VideoBitRate>2048</VideoBitRate></Item></VideoParamAttribute></Response>`
	require.NoError(t, service.applyConfigDownloadResponse(context.Background(), operation, "call", "1", []byte(body)))

	list := loadVideoParams(t, db, 1, "C1")
	require.Len(t, list, 1)
	require.Equal(t, "5", list[0].Resolution)

	var stored gbmodels.GbPTZOperation
	require.NoError(t, db.Where("operation_id = ?", "vp-read").Limit(1).Find(&stored).Error)
	require.Equal(t, gbmodels.PTZOperationAccepted, stored.Status)
	require.NotNil(t, stored.ResponseHasData)
	require.True(t, *stored.ResponseHasData)
}

// ⭐ 本卡最关键的一条协议约定：`Result=OK` 但**没带**请求的配置类型，
// 等价于"设备不支持该配置类型"（2016 设备正是这个样子）。
// 它落成 accepted + response_has_data=false，而不是 rejected —— 设备没做错任何事。
func TestApplyConfigDownloadResponseTreatsAbsentTypeAsConclusion(t *testing.T) {
	service, db := newVideoParamTestService(t)
	operation := createVideoParamOperation(t, db, "vp-read", 1, "C1", manscdp.CmdConfigDownload)

	body := `<Response><CmdType>ConfigDownload</CmdType><SN>5</SN><DeviceID>C1</DeviceID><Result>OK</Result></Response>`
	require.NoError(t, service.applyConfigDownloadResponse(context.Background(), operation, "call", "1", []byte(body)))

	var stored gbmodels.GbPTZOperation
	require.NoError(t, db.Where("operation_id = ?", "vp-read").Limit(1).Find(&stored).Error)
	require.Equal(t, gbmodels.PTZOperationAccepted, stored.Status)
	require.NotNil(t, stored.ResponseHasData)
	require.False(t, *stored.ResponseHasData, "回读为空 = 设备不支持该配置类型，这是最可靠的判据")
	require.NotEmpty(t, stored.DeviceError)
}

func TestApplyConfigDownloadTypeAbsentKeepsPreviouslyReadRows(t *testing.T) {
	service, db := newVideoParamTestService(t)
	first := createVideoParamOperation(t, db, "vp-first", 1, "C1", manscdp.CmdConfigDownload)
	require.NoError(t, service.persistVideoParamsWithDB(context.Background(), db, first, []manscdp.VideoParamItem{
		videoParam(0, "2", "5", "25", "2", nil),
	}, "first"))

	second := createVideoParamOperation(t, db, "vp-absent", 1, "C1", manscdp.CmdConfigDownload)
	body := `<Response><CmdType>ConfigDownload</CmdType><SN>5</SN><DeviceID>C1</DeviceID><Result>OK</Result></Response>`
	require.NoError(t, service.applyConfigDownloadResponse(context.Background(), second, "call", "1", []byte(body)))

	// ⛔ type_absent 说的是"设备这次没给这个类型"，不是"配置被清空了"。
	// 用一次偶发的漏带元素去删掉好数据，比留着旧数据更糟。
	require.Len(t, loadVideoParams(t, db, 1, "C1"), 1, "设备没带元素不等于配置被清空")
}

func TestApplyConfigDownloadResponseRejectsMalformedBody(t *testing.T) {
	service, db := newVideoParamTestService(t)
	operation := createVideoParamOperation(t, db, "vp-read", 1, "C1", manscdp.CmdConfigDownload)
	require.NoError(t, service.applyConfigDownloadResponse(context.Background(), operation, "call", "1",
		[]byte(`<Response><CmdType>Catalog</CmdType><SN>5</SN><DeviceID>C1</DeviceID></Response>`)))

	var stored gbmodels.GbPTZOperation
	require.NoError(t, db.Where("operation_id = ?", "vp-read").Limit(1).Find(&stored).Error)
	require.Equal(t, gbmodels.PTZOperationRejected, stored.Status)
	require.Equal(t, ptzErrorProtocolInvalid, stored.ErrorCode)
}

// ---- 写应答：ack 不是终态 ----

func TestApplyDeviceConfigResponseQueuesReconcileOperation(t *testing.T) {
	service, db := newVideoParamTestService(t)
	operation := createVideoParamOperation(t, db, "vp-apply", 1, "C1", manscdp.CmdDeviceConfig)
	payloadJSON, err := canonicalPayload(map[string]interface{}{"items": []manscdp.VideoParamItem{
		videoParam(0, "2", "6", "25", "1", videoParamStrPtr("2048")),
	}})
	require.NoError(t, err)
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Where("id = ?", operation.ID).
		Update("payload_json", payloadJSON).Error)
	operation.PayloadJSON = payloadJSON

	body := `<Response><CmdType>DeviceConfig</CmdType><SN>5</SN><DeviceID>C1</DeviceID><Result>OK</Result></Response>`
	require.NoError(t, service.applyDeviceConfigResponse(context.Background(), operation, "call", "1", []byte(body)))

	var parent gbmodels.GbPTZOperation
	require.NoError(t, db.Where("operation_id = ?", "vp-apply").Limit(1).Find(&parent).Error)
	require.Equal(t, gbmodels.PTZOperationAccepted, parent.Status)
	require.NotNil(t, parent.ReconcileOperationID,
		"⛔ Result=OK 不许当终态：写入应答没有回显，必须自动追加一次回读对账")

	var child gbmodels.GbPTZOperation
	require.NoError(t, db.Where("operation_id = ?", *parent.ReconcileOperationID).Limit(1).Find(&child).Error)
	require.Equal(t, manscdp.CmdConfigDownload, child.CmdType)
	require.Equal(t, actionRefreshVideoParams, child.Action)
	require.Equal(t, gbmodels.PTZOperationQueued, child.Status)
	require.Equal(t, videoParamReconcileAttempts, child.MaxAttempts)
	require.NotNil(t, child.TriggerOperationID)
	require.Equal(t, "vp-apply", *child.TriggerOperationID)
	// 子 operation 继承父的 profile：这是 scheduler 重试时重建报文的依据。
	require.Equal(t, parent.ProfileVersion, child.ProfileVersion)
}

func TestApplyDeviceConfigResponseRejectsDeviceError(t *testing.T) {
	service, db := newVideoParamTestService(t)
	operation := createVideoParamOperation(t, db, "vp-apply", 1, "C1", manscdp.CmdDeviceConfig)
	body := `<Response><CmdType>DeviceConfig</CmdType><SN>5</SN><DeviceID>C1</DeviceID><Result>ERROR</Result></Response>`
	require.NoError(t, service.applyDeviceConfigResponse(context.Background(), operation, "call", "1", []byte(body)))

	var parent gbmodels.GbPTZOperation
	require.NoError(t, db.Where("operation_id = ?", "vp-apply").Limit(1).Find(&parent).Error)
	require.Equal(t, gbmodels.PTZOperationRejected, parent.Status)
	require.Equal(t, ptzErrorDeviceRejected, parent.ErrorCode)
	require.Nil(t, parent.ReconcileOperationID, "设备明确拒绝时不必再回读：它已经给了结论")
}

func TestApplyDeviceConfigResponseRejectsAckWithoutResult(t *testing.T) {
	service, db := newVideoParamTestService(t)
	operation := createVideoParamOperation(t, db, "vp-apply", 1, "C1", manscdp.CmdDeviceConfig)
	body := `<Response><CmdType>DeviceConfig</CmdType><SN>5</SN><DeviceID>C1</DeviceID></Response>`
	require.NoError(t, service.applyDeviceConfigResponse(context.Background(), operation, "call", "1", []byte(body)))

	var parent gbmodels.GbPTZOperation
	require.NoError(t, db.Where("operation_id = ?", "vp-apply").Limit(1).Find(&parent).Error)
	require.Equal(t, gbmodels.PTZOperationRejected, parent.Status,
		"写应答的全部意义就是传 Result，缺了它这份应答没有信息量")
	require.Equal(t, ptzErrorProtocolInvalid, parent.ErrorCode)
}

// ---- 对账 ----

func TestMarkVideoParamReconcileMismatchFlagsDerivedQuery(t *testing.T) {
	service, db := newVideoParamTestService(t)
	parent := createVideoParamOperation(t, db, "vp-apply", 1, "C1", manscdp.CmdDeviceConfig)
	payloadJSON, err := canonicalPayload(map[string]interface{}{"items": []manscdp.VideoParamItem{
		videoParam(0, "2", "6", "25", "1", videoParamStrPtr("2048")),
	}})
	require.NoError(t, err)
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Where("id = ?", parent.ID).
		Update("payload_json", payloadJSON).Error)

	child := createVideoParamOperation(t, db, "vp-reconcile", 1, "C1", manscdp.CmdConfigDownload)
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Where("id = ?", child.ID).
		Update("trigger_operation_id", "vp-apply").Error)
	child.TriggerOperationID = videoParamStrPtr("vp-apply")

	// 设备回了 VBR 无码率 + 720P：与下发的 CBR 2048 + 1080P 逐格不同。
	// ⭐ 走**完整链路**（落 accepted → 落库 → 对账）而不是直接调对账函数：
	// 对账的终态 CAS 要求 operation 已是 accepted，跳过前一步就会静默不标，
	// 那样测试会"通过"但什么也没验证。
	body := `<Response><CmdType>ConfigDownload</CmdType><SN>5</SN><DeviceID>C1</DeviceID><Result>OK</Result>` +
		`<VideoParamAttribute Num="1"><Item><StreamNumber>0</StreamNumber><VideoFormat>2</VideoFormat>` +
		`<Resolution>5</Resolution><FrameRate>25</FrameRate><BitRateType>2</BitRateType></Item></VideoParamAttribute></Response>`
	require.NoError(t, service.applyConfigDownloadResponse(context.Background(), child, "call", "1", []byte(body)))

	var stored gbmodels.GbPTZOperation
	require.NoError(t, db.Where("operation_id = ?", "vp-reconcile").Limit(1).Find(&stored).Error)
	require.Equal(t, gbmodels.PTZOperationAccepted, stored.Status)
	require.Equal(t, ptzErrorVideoParamReconcileMismatch, stored.ErrorCode)
	require.Contains(t, stored.ErrorMessage, "Resolution")
	require.Contains(t, stored.ErrorMessage, "BitRateType")

	// 对账的输入是落库的回读值，顺带确认它真的落了。
	list := loadVideoParams(t, db, 1, "C1")
	require.Len(t, list, 1)
	require.Equal(t, "5", list[0].Resolution)
}

func TestMarkVideoParamReconcileMismatchIgnoresManualReads(t *testing.T) {
	_, db := newVideoParamTestService(t)
	operation := createVideoParamOperation(t, db, "vp-read", 1, "C1", manscdp.CmdConfigDownload)

	// 手动点「读取设备参数」触发的回读没有可比的意图，不该被判成 mismatch。
	require.NoError(t, schedulerTransaction(context.Background(), db, func(tx *gorm.DB) error {
		return markVideoParamReconcileMismatch(tx, operation, []manscdp.VideoParamItem{
			videoParam(0, "2", "5", "25", "2", nil),
		})
	}))

	var stored gbmodels.GbPTZOperation
	require.NoError(t, db.Where("operation_id = ?", "vp-read").Limit(1).Find(&stored).Error)
	require.Empty(t, stored.ErrorCode)
}

func TestMarkVideoParamReconcileMismatchStaysQuietWhenValuesMatch(t *testing.T) {
	service, db := newVideoParamTestService(t)
	parent := createVideoParamOperation(t, db, "vp-apply", 1, "C1", manscdp.CmdDeviceConfig)
	payloadJSON, err := canonicalPayload(map[string]interface{}{"items": []manscdp.VideoParamItem{
		videoParam(0, "2", "6", "25", "1", videoParamStrPtr("2048")),
	}})
	require.NoError(t, err)
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Where("id = ?", parent.ID).
		Update("payload_json", payloadJSON).Error)

	child := createVideoParamOperation(t, db, "vp-reconcile", 1, "C1", manscdp.CmdConfigDownload)
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Where("id = ?", child.ID).
		Update("trigger_operation_id", "vp-apply").Error)
	child.TriggerOperationID = videoParamStrPtr("vp-apply")

	// 同一条链路，但回读值与下发值逐格相同 ⇒ 不该标错。
	body := `<Response><CmdType>ConfigDownload</CmdType><SN>5</SN><DeviceID>C1</DeviceID><Result>OK</Result>` +
		`<VideoParamAttribute Num="1"><Item><StreamNumber>0</StreamNumber><VideoFormat>2</VideoFormat>` +
		`<Resolution>6</Resolution><FrameRate>25</FrameRate><BitRateType>1</BitRateType>` +
		`<VideoBitRate>2048</VideoBitRate></Item></VideoParamAttribute></Response>`
	require.NoError(t, service.applyConfigDownloadResponse(context.Background(), child, "call", "1", []byte(body)))

	var stored gbmodels.GbPTZOperation
	require.NoError(t, db.Where("operation_id = ?", "vp-reconcile").Limit(1).Find(&stored).Error)
	require.Equal(t, gbmodels.PTZOperationAccepted, stored.Status, "先确认这轮真走到了对账那一步")
	require.Empty(t, stored.ErrorCode, "逐格相同就是一致，不该误报")
}

// ---- 纯函数：对账差异 ----

func TestDiffVideoParamItemsPassesWhenValuesMatch(t *testing.T) {
	want := []manscdp.VideoParamItem{
		videoParam(0, "2", "5", "25", "1", videoParamStrPtr("2048")),
	}
	got := []manscdp.VideoParamItem{
		videoParam(0, "2", "5", "25", "1", videoParamStrPtr("2048")),
	}
	require.Empty(t, diffVideoParamItems(want, got))
}

func TestDiffVideoParamItemsReportsEachField(t *testing.T) {
	want := []manscdp.VideoParamItem{videoParam(0, "2", "6", "25", "1", videoParamStrPtr("2048"))}
	cases := map[string]manscdp.VideoParamItem{
		"VideoFormat":  videoParam(0, "5", "6", "25", "1", videoParamStrPtr("2048")),
		"Resolution":   videoParam(0, "2", "5", "25", "1", videoParamStrPtr("2048")),
		"FrameRate":    videoParam(0, "2", "6", "30", "1", videoParamStrPtr("2048")),
		"BitRateType":  videoParam(0, "2", "6", "25", "2", videoParamStrPtr("2048")),
		"VideoBitRate": videoParam(0, "2", "6", "25", "1", videoParamStrPtr("1024")),
	}
	for field, got := range cases {
		t.Run(field, func(t *testing.T) {
			diffs := diffVideoParamItems(want, []manscdp.VideoParamItem{got})
			require.Len(t, diffs, 1)
			require.Equal(t, field, diffs[0].Field)
			require.Equal(t, 0, diffs[0].StreamNumber)
		})
	}
}

// ⛔ 条件必选字段的"缺席"必须与"值为 0"区分开：
// 前端选了 VBR 就不该发码率，设备回读里没有码率是**一致**，不是差异；
// 但"下发没给 vs 回读给了 0"是两回事，必须报出来。
func TestDiffVideoParamItemsTreatsAbsentBitRateDifferentlyFromZero(t *testing.T) {
	vbrWithoutBitRate := videoParam(0, "2", "6", "25", "2", nil)
	require.Empty(t, diffVideoParamItems([]manscdp.VideoParamItem{vbrWithoutBitRate},
		[]manscdp.VideoParamItem{vbrWithoutBitRate}), "两边都没给码率 = 一致")

	diffs := diffVideoParamItems([]manscdp.VideoParamItem{vbrWithoutBitRate},
		[]manscdp.VideoParamItem{videoParam(0, "2", "6", "25", "2", videoParamStrPtr("0"))})
	require.Len(t, diffs, 1)
	require.Equal(t, "VideoBitRate", diffs[0].Field)
	require.Equal(t, "(未提供)", diffs[0].Wanted)
	require.Equal(t, "0", diffs[0].Actual)
}

func TestDiffVideoParamItemsReportsMissingStream(t *testing.T) {
	diffs := diffVideoParamItems(
		[]manscdp.VideoParamItem{
			videoParam(0, "2", "6", "25", "1", videoParamStrPtr("2048")),
			videoParam(1, "2", "5", "25", "1", videoParamStrPtr("1024")),
		},
		[]manscdp.VideoParamItem{videoParam(0, "2", "6", "25", "1", videoParamStrPtr("2048"))},
	)
	require.Len(t, diffs, 1)
	require.Equal(t, 1, diffs[0].StreamNumber)
	require.Equal(t, "回读中不存在", diffs[0].Actual,
		"命令被接受了、但那个码流根本没出现在回读里 —— 这是最值得暴露的一种差异")
}

func TestFormatVideoParamDiffDescribesEachDifference(t *testing.T) {
	message := formatVideoParamDiff([]videoParamDiff{
		{StreamNumber: 0, Field: "Resolution", Wanted: "6", Actual: "5"},
	})
	require.Contains(t, message, "码流 0")
	require.Contains(t, message, "Resolution")
	require.Contains(t, message, "6")
	require.Contains(t, message, "5")
	require.Contains(t, message, "设备已接受命令，但回读值不一致")
	require.Empty(t, formatVideoParamDiff(nil))
}

// ---- payload 往返 ----

func TestDecodeVideoParamPayloadRoundTripsThroughCanonicalPayload(t *testing.T) {
	items := []manscdp.VideoParamItem{
		videoParam(0, "2", "5", "25", "1", videoParamStrPtr("2048")),
		videoParam(1, "5", "2", "15", "2", nil),
	}
	payloadJSON, err := canonicalPayload(map[string]interface{}{"items": items})
	require.NoError(t, err)

	decoded, err := decodeVideoParamPayload(payloadJSON)
	require.NoError(t, err)
	require.Len(t, decoded, 2)
	require.Equal(t, items[0], decoded[0])
	require.Nil(t, decoded[1].VideoBitRate, "条件必选字段的缺席必须能穿过 payload 往返不变形")
}

func TestDecodeVideoParamPayloadToleratesEmptyInput(t *testing.T) {
	decoded, err := decodeVideoParamPayload("")
	require.NoError(t, err)
	require.Nil(t, decoded)
}

// ---- 面板四态判据（纯函数）----

func TestDeriveVideoParamReconcileStateCoversEveryOutcome(t *testing.T) {
	hasData := true
	noData := false

	for _, tc := range []struct {
		name   string
		latest *gbmodels.GbPTZOperation
		want   string
	}{
		{"从未回读", nil, VideoParamStateNeverRead},
		{"排队中", &gbmodels.GbPTZOperation{Status: gbmodels.PTZOperationQueued}, VideoParamStatePending},
		{"已发送", &gbmodels.GbPTZOperation{Status: gbmodels.PTZOperationSent}, VideoParamStatePending},
		{"回读成功且带数据", &gbmodels.GbPTZOperation{Status: gbmodels.PTZOperationAccepted, ResponseHasData: &hasData}, VideoParamStateReadOK},
		{"设备未返回该配置类型", &gbmodels.GbPTZOperation{Status: gbmodels.PTZOperationAccepted, ResponseHasData: &noData}, VideoParamStateTypeAbsent},
		{"回读值不一致", &gbmodels.GbPTZOperation{Status: gbmodels.PTZOperationAccepted, ResponseHasData: &hasData, ErrorCode: ptzErrorVideoParamReconcileMismatch}, VideoParamStateMismatch},
		{"设备拒绝", &gbmodels.GbPTZOperation{Status: gbmodels.PTZOperationRejected, ErrorCode: ptzErrorDeviceRejected}, VideoParamStateFailed},
		{"超时", &gbmodels.GbPTZOperation{Status: gbmodels.PTZOperationTimeout}, VideoParamStateFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := DeriveVideoParamReconcileState(tc.latest)
			require.Equal(t, tc.want, got.State)
		})
	}

	// mismatch 的前提是 accepted：设备拒绝了命令就不可能"值不一致"，
	// 落成 failed 才不会把"没拿到答案"说成"拿到了答案说不行"。
	t.Run("拒绝优先于不一致", func(t *testing.T) {
		got := DeriveVideoParamReconcileState(&gbmodels.GbPTZOperation{
			Status: gbmodels.PTZOperationRejected, ErrorCode: ptzErrorVideoParamReconcileMismatch,
		})
		require.Equal(t, VideoParamStateFailed, got.State)
	})

	// type_absent 与 read_ok 的差别只在 response_has_data 上，不能靠列表空不空来猜。
	t.Run("accepted 但没有 responseHasData 时按有数据处理", func(t *testing.T) {
		got := DeriveVideoParamReconcileState(&gbmodels.GbPTZOperation{Status: gbmodels.PTZOperationAccepted})
		require.Equal(t, VideoParamStateReadOK, got.State)
	})
}

func TestDeriveVideoParamReconcileStateCarriesOperationFacts(t *testing.T) {
	hasData := false
	completedAt := videoParamTestNow()
	applyOp := "apply-1"
	got := DeriveVideoParamReconcileState(&gbmodels.GbPTZOperation{
		OperationID:        "op-refresh-1",
		Status:             gbmodels.PTZOperationAccepted,
		ResponseHasData:    &hasData,
		DeviceError:        "no VideoParamAttribute element",
		CompletedAt:        &completedAt,
		TriggerOperationID: &applyOp,
	})
	require.Equal(t, "op-refresh-1", got.OperationID)
	require.Equal(t, string(gbmodels.PTZOperationAccepted), got.Status)
	require.NotNil(t, got.ResponseHasData)
	require.False(t, *got.ResponseHasData)
	// ⛔ type_absent 的判定理由走 device_error（设备侧原话），
	// 对账不一致的逐格差异走 error_message —— 两条路径的文本不能互相顶替，
	// 否则前端要么说不出"为什么空"，要么把"设备没带元素"说成"值对不上"。
	require.Equal(t, "no VideoParamAttribute element", got.DeviceError)
	require.Empty(t, got.ErrorMessage)
	require.NotNil(t, got.CompletedAt)
	require.True(t, got.DerivedFromApply, "带 trigger_operation_id 的回读 = 由下发派生的对账")

	// 手动「读取设备参数」触发的回读没有 trigger，不参与 mismatch 判定。
	manual := DeriveVideoParamReconcileState(&gbmodels.GbPTZOperation{
		OperationID: "op-manual", Status: gbmodels.PTZOperationAccepted, ResponseHasData: &hasData,
	})
	require.False(t, manual.DerivedFromApply)
}
