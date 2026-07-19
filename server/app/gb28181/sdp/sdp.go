package sdp

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// SSRC 国标格式:10 位十进制
// 首位:0=实时流 1=回放流
// 中间 8 位:取国标编码(domain)的中间 8 位(地市/设备编码段)
// 末 1 位:流序号(同一通道多路区分)
const (
	SSRCRealtime = "0" // 实时流首位
	SSRCPlayback = "1" // 回放流首位
)

var ssrcSeq uint32

// GenRealtimeSSRC 生成实时流 SSRC
// domain = SIP 域(20位编码或10位域),取其中段 8 位
func GenRealtimeSSRC(domain string) string {
	mid := extractMid8(domain)
	seq := atomic.AddUint32(&ssrcSeq, 1) % 10
	return SSRCRealtime + mid + fmt.Sprintf("%d", seq)
}

// extractMid8 从国标编码取中间 8 位(第 4-11 位,即地市/区县段);不足则随机补
func extractMid8(code string) string {
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, code)
	if len(digits) >= 11 {
		return digits[3:11]
	}
	// 不足:随机 8 位兜底
	return fmt.Sprintf("%08d", rand.Intn(100000000))
}

// PlayParams 实时点播 SDP 构造参数
type PlayParams struct {
	ServerID string // 平台国标编码(o= 行)
	RecvIP   string // 收流 IP(ZLM 地址)
	RecvPort int    // 收流端口(ZLM RTP 端口)
	SSRC     string // 媒体流 SSRC
	TCPMode  bool   // true=TCP被动收流, false=UDP
}

// RecordingParams describes a historical playback or download RTP receiver.
type RecordingParams struct {
	ServerID      string
	RecvIP        string
	RecvPort      int
	SSRC          string
	TCPMode       bool
	StartTime     time.Time
	EndTime       time.Time
	DownloadSpeed float64
}

// BuildPlaySDP 构造实时点播 SDP(平台作主叫,s=Play)
// 遵循 GB/T 28181 附录 SDP 格式:PS 封装(96/97/98) + H264/H265
func BuildPlaySDP(p PlayParams) string {
	proto := "RTP/AVP"
	var b strings.Builder
	b.WriteString("v=0\r\n")
	b.WriteString(fmt.Sprintf("o=%s 0 0 IN IP4 %s\r\n", p.ServerID, p.RecvIP))
	b.WriteString("s=Play\r\n")
	b.WriteString(fmt.Sprintf("c=IN IP4 %s\r\n", p.RecvIP))
	b.WriteString("t=0 0\r\n")
	b.WriteString(fmt.Sprintf("m=video %d %s 96 98 97\r\n", p.RecvPort, proto))
	b.WriteString("a=recvonly\r\n")
	b.WriteString("a=rtpmap:96 PS/90000\r\n") // PS 封装(国标主流)
	b.WriteString("a=rtpmap:98 H264/90000\r\n")
	b.WriteString("a=rtpmap:97 MPEG4/90000\r\n")
	if p.TCPMode {
		b.WriteString("a=setup:passive\r\n")
		b.WriteString("a=connection:new\r\n")
	}
	// y= 行:国标扩展,声明 SSRC(10位)
	b.WriteString(fmt.Sprintf("y=%s\r\n", p.SSRC))
	return b.String()
}

// BuildPlaybackSDP builds a GB28181 historical playback SDP offer.
func BuildPlaybackSDP(p RecordingParams) (string, error) {
	return buildRecordingSDP("Playback", p, false)
}

// BuildDownloadSDP builds a GB28181 recording download SDP offer.
func BuildDownloadSDP(p RecordingParams) (string, error) {
	return buildRecordingSDP("Download", p, true)
}

func buildRecordingSDP(sessionName string, p RecordingParams, download bool) (string, error) {
	if strings.TrimSpace(p.ServerID) == "" || strings.TrimSpace(p.RecvIP) == "" {
		return "", fmt.Errorf("SDP 缺少 ServerID 或收流地址")
	}
	if p.RecvPort < 1 || p.RecvPort > 65535 {
		return "", fmt.Errorf("RTP 端口非法: %d", p.RecvPort)
	}
	if !validPlaybackSSRC(p.SSRC) {
		return "", fmt.Errorf("回放 SSRC 必须为 10 位数字且首位为 1")
	}
	if p.StartTime.IsZero() || p.EndTime.IsZero() || !p.EndTime.After(p.StartTime) {
		return "", fmt.Errorf("录像时间范围非法")
	}
	if download {
		if p.DownloadSpeed == 0 {
			p.DownloadSpeed = 1
		}
		if p.DownloadSpeed != 1 && p.DownloadSpeed != 2 && p.DownloadSpeed != 4 {
			return "", fmt.Errorf("不支持的下载速度: %g", p.DownloadSpeed)
		}
	}

	proto := "RTP/AVP"
	var b strings.Builder
	b.WriteString("v=0\r\n")
	b.WriteString(fmt.Sprintf("o=%s 0 0 IN IP4 %s\r\n", p.ServerID, p.RecvIP))
	b.WriteString("s=" + sessionName + "\r\n")
	b.WriteString(fmt.Sprintf("c=IN IP4 %s\r\n", p.RecvIP))
	b.WriteString(fmt.Sprintf("t=%s %s\r\n", formatRecordingTime(p.StartTime), formatRecordingTime(p.EndTime)))
	b.WriteString(fmt.Sprintf("m=video %d %s 96 98 97\r\n", p.RecvPort, proto))
	b.WriteString("a=recvonly\r\n")
	b.WriteString("a=rtpmap:96 PS/90000\r\n")
	b.WriteString("a=rtpmap:98 H264/90000\r\n")
	b.WriteString("a=rtpmap:97 MPEG4/90000\r\n")
	if p.TCPMode {
		b.WriteString("a=setup:passive\r\n")
		b.WriteString("a=connection:new\r\n")
	}
	if download {
		b.WriteString("a=downloadspeed:" + strconv.FormatFloat(p.DownloadSpeed, 'f', -1, 64) + "\r\n")
	}
	b.WriteString(fmt.Sprintf("y=%s\r\n", p.SSRC))
	return b.String(), nil
}

func validPlaybackSSRC(ssrc string) bool {
	if len(ssrc) != 10 || ssrc[0] != '1' {
		return false
	}
	for _, r := range ssrc {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func formatRecordingTime(value time.Time) string {
	return value.Format("20060102T150405")
}
