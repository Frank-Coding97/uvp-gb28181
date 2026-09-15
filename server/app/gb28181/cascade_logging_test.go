package gb28181

import (
	"errors"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// C05 的验收面：cascade 链路的日志必须
//   ① 字段名一律 snake_case（工具/门禁/人 grep 才能把同一台平台的两组日志串起来）；
//   ② 定位字段在场，且**判定在认定平台之前还是之后**——之前靠 peer，之后加 platform_id；
//   ③ 同一事件只有一种字段集（forward_failed 两个写法已合并）。

func observeCascadeLogs(t *testing.T) *observer.ObservedLogs {
	t.Helper()
	core, observed := observer.New(zap.DebugLevel)
	previous := app.ZapLog
	app.ZapLog = zap.New(core)
	t.Cleanup(func() { app.ZapLog = previous })
	return observed
}

func requireNoCamelCaseFields(t *testing.T, observed *observer.ObservedLogs) {
	t.Helper()
	for _, entry := range observed.All() {
		for key := range entry.ContextMap() {
			require.NotRegexp(t, `[A-Z]`, key, "cascade 日志字段名必须 snake_case，event=%v", entry.ContextMap()["event"])
		}
	}
}

// 认定平台**之前**失败：platform_id 必须缺席（打 0 会被当成"已带定位字段"），
// 此时唯一的身份线索是 peer。
func TestCascadeVideoFailureBeforeIdentificationKeepsPeerOnly(t *testing.T) {
	observed := observeCascadeLogs(t)
	h, _, _ := newCascadeVideoTestRuntime(t)

	req := cascadeTestRequestFrom("10.8.0.2", 15060, "0200000001")
	// From 换成配置里没有的上级 —— 严格匹配与身份匹配都会落空。
	req.ReplaceHeader(sip.NewHeader("From", "<sip:35020000002000000001@3502000000>;tag=upper"))
	tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
	defer tx.Terminate()
	go h.Handle(req, tx)

	require.Equal(t, 403, cascadeAwaitResponse(t, tx).StatusCode)

	entries := observed.FilterField(zap.String("event", "cascade.video.failed")).All()
	require.Len(t, entries, 1)
	fields := entries[0].ContextMap()
	require.NotContains(t, fields, "platform_id", "未认定平台时不该出现 platform_id")
	require.Equal(t, "same-call-id", fields["call_id"])
	require.Equal(t, "10.8.0.2:15060/udp from=35020000002000000001", fields["peer"])
	require.Contains(t, fields, "error", "失败原因必须留下")
	requireNoCamelCaseFields(t, observed)
}

// 认定平台**之后**失败：platform_id 必须在场 —— 这时运维要定位的是"哪台平台"。
func TestCascadeVideoFailureAfterIdentificationCarriesPlatform(t *testing.T) {
	observed := observeCascadeLogs(t)
	h, _, _ := newCascadeVideoTestRuntime(t)

	// 严格匹配 upper0（192.168.10.220:15060），但请求的是没共享给它的通道。
	req := cascadeTestRequestFrom("192.168.10.220", 15060, "0200000001")
	req.Recipient.User = "34020000001320000099"
	req.ReplaceHeader(sip.NewHeader("To", "<sip:34020000001320000099@3402000000>"))
	req.ReplaceHeader(sip.NewHeader("Subject", "34020000001320000099:0200000001,34020000002000000001:0"))
	tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 10)}
	defer tx.Terminate()
	go h.Handle(req, tx)

	require.Equal(t, 404, cascadeAwaitResponse(t, tx).StatusCode)

	entries := observed.FilterField(zap.String("event", "cascade.video.failed")).All()
	require.Len(t, entries, 1)
	fields := entries[0].ContextMap()
	require.NotEmpty(t, fields["platform_id"], "认定平台之后必须给出 platform_id")
	require.Equal(t, "same-call-id", fields["call_id"])
	require.Equal(t, "192.168.10.220:15060/udp from=34020000002000000001", fields["peer"])
	requireNoCamelCaseFields(t, observed)
}

// forward_failed 曾经有两个写法（硬编码 reason / 带 err），同名事件两种字段集
// 会让聚合统计与排障都不确定该读哪个键。合并后：reason_code 必有，error 只在转发失败时出现。
func TestCascadeControlForwardFailureUsesSingleShape(t *testing.T) {
	observed := observeCascadeLogs(t)
	ctx := t.Context()

	logCascadeControlForwardFailed(ctx, 7, "34020000001320000010", cascadeControlReasonPTZUnavailable, nil)
	logCascadeControlForwardFailed(ctx, 7, "34020000001320000010", cascadeControlReasonForwardRejected, errors.New("channel is not shared to platform 7"))

	entries := observed.FilterField(zap.String("event", "cascade.control.forward_failed")).All()
	require.Len(t, entries, 2)
	for _, entry := range entries {
		fields := entry.ContextMap()
		require.Equal(t, uint64(7), fields["platform_id"])
		require.Equal(t, "34020000001320000010", fields["channel_id"])
		require.NotEmpty(t, fields["reason_code"])
	}
	require.Equal(t, cascadeControlReasonPTZUnavailable, entries[0].ContextMap()["reason_code"])
	require.NotContains(t, entries[0].ContextMap(), "error", "reason_code 已说明原因时不该留空 error 字段")
	require.Equal(t, cascadeControlReasonForwardRejected, entries[1].ContextMap()["reason_code"])
	require.Contains(t, entries[1].ContextMap(), "error")
	requireNoCamelCaseFields(t, observed)
}

// 级联目录查询失败多数发生在"认定平台之前"，platform_id 拿不到；
// 这时必须靠 call_id（与 SIP 报文对上的关联键）+ peer 定位。
func TestCascadeCatalogQueryFailureKeepsCallIDAndPeer(t *testing.T) {
	observed := observeCascadeLogs(t)
	db := cascadeInviteTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.GbCascadePlatform{}))
	handler := newCascadeCatalogHandler(repository.NewGormRepository(db), newCascadePlatformClientFactory(nil, nil, time.Second))

	req := sip.NewRequest(sip.MESSAGE, sip.Uri{User: "34020000002000000002", Host: "192.168.10.106"})
	req.AppendHeader(sip.NewHeader("Via", "SIP/2.0/UDP 10.8.0.2:8160;branch=z9hG4bK-catalog-1"))
	req.AppendHeader(sip.NewHeader("From", "<sip:35020000002000000001@3502000000>;tag=upper"))
	req.AppendHeader(sip.NewHeader("To", "<sip:34020000002000000002@3402000000>"))
	req.AppendHeader(sip.NewHeader("Call-ID", "catalog-call-id-1"))
	req.AppendHeader(&sip.CSeqHeader{SeqNo: 1, MethodName: sip.MESSAGE})
	req.SetSource("10.8.0.2:8160")
	req.SetTransport("UDP")
	req.SetBody([]byte("<?xml version=\"1.0\" encoding=\"GB2312\"?>\r\n<Query>\r\n<CmdType>Catalog</CmdType>\r\n<SN>1</SN>\r\n<DeviceID>34020000002000000002</DeviceID>\r\n</Query>\r\n"))
	tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 4)}
	defer tx.Terminate()

	require.True(t, handler(req, tx))

	entries := observed.FilterField(zap.String("event", "cascade.catalog.query_failed")).All()
	require.Len(t, entries, 1)
	fields := entries[0].ContextMap()
	require.Equal(t, "catalog-call-id-1", fields["call_id"])
	require.Equal(t, "10.8.0.2:8160/udp from=35020000002000000001", fields["peer"])
	require.EqualValues(t, 403, fields["status"])
	requireNoCamelCaseFields(t, observed)
}
