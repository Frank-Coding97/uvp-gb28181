package sdp

import (
	"strings"
)

// ParseAnswerSSRC 取出 SDP 里 y= 声明行的 SSRC。
//
// GB/T 28181 允许被叫方不同意主叫在 y= 里指定的 SSRC:它可以改用自己生成的
// SSRC,并把实际使用的值写进 200 OK 的 y= 回告主叫。WVP 的
// useCustomSsrcForParentInvite 默认开启,走的正是这条路。主叫若不读这个字段、
// 仍按自己请求的值在收流端做校验,对端推来的每一包都会被判为
// "ssrc mismatched" 丢弃 —— SIP 层全绿,画面永远是黑的。
//
// 返回空串表示对端没有声明 SSRC(此时应当沿用主叫请求值)。
func ParseAnswerSSRC(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if !strings.HasPrefix(line, "y=") {
			continue
		}
		if value := strings.TrimSpace(strings.TrimPrefix(line, "y=")); value != "" {
			return value
		}
	}
	return ""
}
