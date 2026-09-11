package handler

import (
	"strconv"
	"testing"

	"github.com/emiago/sipgo/sip"

	"uvplatform.cn/uvp-gb28181/app/gb28181/metrics"
)

// T1.5-U1: txKindFromCmd 各 CmdType 映射
func TestTxKindFromCmd(t *testing.T) {
	cases := []struct {
		cmd  string
		want metrics.TxKind
	}{
		{"Keepalive", metrics.TxKeepalive},
		{"Catalog", metrics.TxCatalog},
		{"Alarm", metrics.TxAlarm},
		{"Unknown", metrics.TxUnknown},
		{"", metrics.TxUnknown},
	}
	for _, c := range cases {
		if got := txKindFromCmd(c.cmd); got != c.want {
			t.Errorf("txKindFromCmd(%q) = %v, want %v", c.cmd, got, c.want)
		}
	}
}

// T1.4-U6 + T1.5-U?: sipPairKey 缺 Call-ID 不 panic,返回空
func TestSipPairKey_MissingHeaders(t *testing.T) {
	uri := sip.Uri{}
	sip.ParseUri("sip:test@127.0.0.1", &uri)
	req := sip.NewRequest(sip.MESSAGE, uri)
	// 不附 CallID/CSeq 头,pairKey 应该返回 sipgo 自动生成的(非空)或空字符串
	callID, cseq := sipPairKey(req)
	// 不严格断言值,关键是不 panic;sipgo NewRequest 会自动补 Call-ID/CSeq
	_, _ = callID, cseq
}

// T1.4-U?: sipPairKey 完整请求返回非空
func TestSipPairKey_FullRequest(t *testing.T) {
	uri := sip.Uri{}
	sip.ParseUri("sip:test@127.0.0.1", &uri)
	req := sip.NewRequest(sip.REGISTER, uri)
	// 模拟 SIP transport 已经填好的头(入向请求是设备发的,必有 Call-ID/CSeq)
	cid := sip.CallIDHeader("test-call-id-1234")
	req.AppendHeader(&cid)
	req.AppendHeader(&sip.CSeqHeader{SeqNo: 7, MethodName: sip.REGISTER})

	callID, cseq := sipPairKey(req)
	if callID != "test-call-id-1234" {
		t.Errorf("callID=%q, want test-call-id-1234", callID)
	}
	if cseq != "7" {
		t.Errorf("cseq=%q, want 7", cseq)
	}
	// CSeq 是数字
	if _, err := strconv.Atoi(cseq); err != nil {
		t.Errorf("cseq=%q not a number: %v", cseq, err)
	}
}
