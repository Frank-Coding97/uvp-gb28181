package sdp

import "strings"

// AudioCodec 是国标对讲/广播支持的 G.711 家族编码。
//
// 只收 G.711A(PCMA) 与 G.711U(PCMU) 两种：
//   - 二者都是 8kHz / 单声道 / 64kbps，差别只在压扩表（A-law vs μ-law），
//     ZLM 转发不转码，浏览器 WebRTC 也能直接推；
//   - 载荷类型是国标写死的静态值：PCMA=8、PCMU=0。
//
// ⛔ 不接受动态载荷类型（例如 `a=rtpmap:96 PCMA/8000`）：国标对声音频就这两个
// 静态 PT，放宽会让「平台发的 PT 与设备解码器对不上」变成静默故障。
// ⛔ AAC（mpeg4-generic）不在这里：源流来自浏览器 WHIP（推不了 AAC），
// ZLM 的 startSendRtp 也不转码，要支持只能另做转码旁路。
type AudioCodec string

const (
	AudioCodecPCMA AudioCodec = "pcma"
	AudioCodecPCMU AudioCodec = "pcmu"
)

// DefaultAudioCodec 是历史行为：平台一直只发 G.711A。
const DefaultAudioCodec = AudioCodecPCMA

// NormalizeAudioCodec 把用户/配置输入归一成受支持的编码。
//
// 空串按默认 G.711A 处理（设备字段的默认值就是 pcma），非法值返回 false。
func NormalizeAudioCodec(value string) (AudioCodec, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "pcma", "g711a", "g711_a", "a-law", "alaw":
		return AudioCodecPCMA, true
	case "pcmu", "g711u", "g711_u", "u-law", "ulaw":
		return AudioCodecPCMU, true
	default:
		return "", false
	}
}

func (c AudioCodec) Valid() bool {
	return c == AudioCodecPCMA || c == AudioCodecPCMU
}

// normalize 把零值/非法值收回默认编码，用于构造 SDP 的兜底。
func (c AudioCodec) normalize() AudioCodec {
	if !c.Valid() {
		return DefaultAudioCodec
	}
	return c
}

// PayloadType 返回国标静态载荷类型：PCMA=8、PCMU=0。
func (c AudioCodec) PayloadType() int {
	if c == AudioCodecPCMU {
		return 0
	}
	return 8
}

// EncodingName 返回 rtpmap 里的编码名（PCMA / PCMU）。
func (c AudioCodec) EncodingName() string {
	if c == AudioCodecPCMU {
		return "PCMU"
	}
	return "PCMA"
}

// RTPMap 返回 `a=rtpmap` 冒号后的值，例如 PCMA/8000。
func (c AudioCodec) RTPMap() string {
	return c.EncodingName() + "/8000"
}

// FLine 返回 SDP 的媒体描述行（GB/T 28181 附录 G 的音频段）：
// 第 2 段是音频编码格式（1=G.711A、2=G.711U），第 3 段采样率 8kHz，第 4 段单声道。
func (c AudioCodec) FLine() string {
	if c == AudioCodecPCMU {
		return "v/////a/2/8/1"
	}
	return "v/////a/1/8/1"
}

// Label 是给现场看的名称，用在「设备不支持该编码」这类错误文案里。
func (c AudioCodec) Label() string {
	if c == AudioCodecPCMU {
		return "G.711U（PCMU/8000）"
	}
	return "G.711A（PCMA/8000）"
}

// ParseG711RTPMap 判断一条 `a=rtpmap` 是否是国标静态 G.711 映射。
//
// payload 形如 "a=rtpmap:8"，encoding 形如 "PCMA/8000" 或 "PCMA/8000/1"。
// 单声道可以省略声道数，也可以显式写 /1；写成 /2 或别的采样率一律不接受。
func ParseG711RTPMap(payload, encoding string) (AudioCodec, bool) {
	pt := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(payload)), "a=rtpmap:"))
	fields := strings.Split(strings.TrimSpace(encoding), "/")
	if len(fields) != 2 && len(fields) != 3 {
		return "", false
	}
	if len(fields) == 3 && fields[2] != "1" {
		return "", false
	}
	if fields[1] != "8000" {
		return "", false
	}
	switch {
	case pt == "8" && strings.EqualFold(fields[0], "PCMA"):
		return AudioCodecPCMA, true
	case pt == "0" && strings.EqualFold(fields[0], "PCMU"):
		return AudioCodecPCMU, true
	default:
		return "", false
	}
}
