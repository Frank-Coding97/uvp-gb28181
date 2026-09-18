// Package handler/sipkey 提取 SIP 报文里用于 metrics 配对的 (Call-ID, CSeq) key
package handler

import (
	"strconv"

	"github.com/emiago/sipgo/sip"
)

// sipPairKey 从 SIP request 抽取 (Call-ID, CSeq) 用于 metrics 配对
// CallID 为空时返回空串 + 0(metrics 会跳过)
func sipPairKey(req *sip.Request) (string, string) {
	var callID, cseq string
	if h := req.CallID(); h != nil {
		callID = string(*h)
	}
	if h := req.CSeq(); h != nil {
		cseq = strconv.FormatUint(uint64(h.SeqNo), 10)
	}
	return callID, cseq
}

// sipPairKeyFromBody Catalog Response 类场景 — Request 本身就是 MESSAGE,
// 也用 (CallID, CSeq) 作为 metrics 配对 key
func sipPairKeyFromMessage(req *sip.Request) (string, string) {
	return sipPairKey(req)
}
