package handler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestLoggingCatalogResponseProcessedCarriesSIPCorrelation 锁住 catalog 链路的
// `device_id` / `call_id` / `cseq` 三件套。
//
// C06 之前这三个值走的是 `traceFields ...zap.Field` + `.With(...)`：**运行时在，
// 字段名在日志语句里读不到** —— 扫描脚本判它"无定位"，门禁也验证不了。
// 改成显式参数之后，它们必须在**每一条** catalog 日志上都在场，包括这条成功路径：
// 通道没出来的排查顺序就是"注册成功 → 有没有发 Catalog 查询 → 应答回来了没有"，
// 而把应答和 SIP 报文对上靠的正是 call_id（`gb_sip_trace_message` 也按它串）。
func TestLoggingCatalogResponseProcessedCarriesSIPCorrelation(t *testing.T) {
	_, sink := t14Runtime(t)

	const deviceID = "34020000001320000077"
	body := []byte(`<Response>
<CmdType>Catalog</CmdType>
<SN>12</SN>
<DeviceID>34020000001320000077</DeviceID>
<SumNum>1</SumNum>
<DeviceList Num="1">
<Item><DeviceID>34020000001320000078</DeviceID><Name>通道 1</Name><Status>ON</Status></Item>
</DeviceList>
</Response>`)

	HandleCatalogResponse(context.Background(), body, deviceID, "call-catalog", "12")

	row := t14RecordWithEvent(t, sink, "gb28181.catalog.response_processed")
	require.Equal(t, deviceID, row["device_id"])
	require.Equal(t, "call-catalog", row["call_id"])
	require.Equal(t, "12", row["cseq"])
	require.Equal(t, float64(1), row["item_count"])
	require.Equal(t, true, row["complete"])
}

// TestLoggingCatalogParseFailureKeepsEnvelopeIdentity 锁住"报文解析失败时仍能说出是谁发的"。
//
// 应答体坏掉 ≠ 不知道是谁发的：MESSAGE 信封（From / Call-ID / CSeq）在进入
// `HandleCatalogResponse` 之前就已经解开过了。所以解析失败这条 WARN 必须带上信封上的
// device_id / call_id —— 否则一台设备反复发坏 Catalog 应答时，日志里只有一串
// "解析失败"，没有任何线索指向那台设备。
func TestLoggingCatalogParseFailureKeepsEnvelopeIdentity(t *testing.T) {
	_, sink := t14Runtime(t)

	HandleCatalogResponse(context.Background(), []byte(`<Response><CmdType>Catalog</CmdType>`),
		"device-bad-catalog", "call-bad", "77")

	row := t14RecordWithEvent(t, sink, "gb28181.catalog.response_parse_failed")
	require.Equal(t, "device-bad-catalog", row["device_id"])
	require.Equal(t, "call-bad", row["call_id"])
	require.Equal(t, "77", row["cseq"])
	require.Equal(t, "catalog_response_invalid", row["reason_code"])
}
