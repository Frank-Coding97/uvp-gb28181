package trace

import (
	"net"
	"regexp"
	"strings"

	"github.com/emiago/sipgo/sip"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
)

// BusinessCode is a stable machine-readable classification for a SIP trace.
// The UI label is deliberately kept separate so protocol rules do not leak into Vue.
type BusinessCode string

const (
	BusinessRegister        BusinessCode = "register"
	BusinessKeepalive       BusinessCode = "keepalive"
	BusinessCatalog         BusinessCode = "catalog"
	BusinessDeviceInfo      BusinessCode = "device_info"
	BusinessDeviceStatus    BusinessCode = "device_status"
	BusinessDeviceControl   BusinessCode = "device_control"
	BusinessAlarm           BusinessCode = "alarm"
	BusinessPTZ             BusinessCode = "ptz"
	BusinessRealtimePlay    BusinessCode = "realtime_play"
	BusinessRecordQuery     BusinessCode = "record_query"
	BusinessPlayback        BusinessCode = "playback"
	BusinessDownload        BusinessCode = "download"
	BusinessPlaybackControl BusinessCode = "playback_control"
	BusinessTalk            BusinessCode = "talk"
	BusinessBroadcast       BusinessCode = "broadcast"
	BusinessMobilePosition  BusinessCode = "mobile_position"
	BusinessSubscription    BusinessCode = "subscription"
	BusinessAck             BusinessCode = "ack"
	BusinessHangup          BusinessCode = "hangup"
	BusinessUnknown         BusinessCode = "unknown"
)

type businessClassification struct {
	Code       BusinessCode
	Label      string
	Confidence string
}

var sdpSessionPattern = regexp.MustCompile(`(?im)^s=([^\r\n]*)`)

func classifySIPBusiness(raw []byte) businessClassification {
	unknown := businessClassification{Code: BusinessUnknown, Label: "未知业务", Confidence: "none"}
	message, err := sip.NewParser().ParseSIP(raw)
	if err != nil {
		return unknown
	}
	method := ""
	switch typed := message.(type) {
	case *sip.Request:
		method = strings.ToUpper(typed.Method.String())
	case *sip.Response:
		if cseq := typed.CSeq(); cseq != nil {
			method = strings.ToUpper(cseq.MethodName.String())
		}
	}
	body := message.Body()
	contentType := ""
	subject := ""
	if headers := message.GetHeaders("Content-Type"); len(headers) > 0 {
		contentType = strings.ToLower(headers[0].Value())
	}
	if headers := message.GetHeaders("Subject"); len(headers) > 0 {
		subject = strings.TrimSpace(headers[0].Value())
	}
	if method == "REGISTER" {
		return businessClassification{BusinessRegister, "注册", "high"}
	}
	if method == "BYE" || method == "CANCEL" {
		return businessClassification{BusinessHangup, "挂断", "high"}
	}
	if method == "ACK" {
		return businessClassification{BusinessAck, "会话确认", "high"}
	}
	if method == "SUBSCRIBE" || method == "NOTIFY" {
		return businessClassification{BusinessSubscription, "订阅/通知", "high"}
	}
	if strings.Contains(contentType, "mansrtsp") || strings.HasPrefix(strings.TrimSpace(strings.ToUpper(string(body))), "PLAY RTSP/") ||
		strings.HasPrefix(strings.TrimSpace(strings.ToUpper(string(body))), "PAUSE RTSP/") || strings.HasPrefix(strings.TrimSpace(strings.ToUpper(string(body))), "TEARDOWN RTSP/") {
		command := strings.ToUpper(strings.TrimSpace(strings.SplitN(string(body), " ", 2)[0]))
		if command == "PLAY" || command == "PAUSE" || command == "TEARDOWN" || strings.HasPrefix(command, "RTSP/") {
			return businessClassification{BusinessPlaybackControl, "回放控制", "high"}
		}
	}
	if strings.Contains(contentType, "sdp") || method == "INVITE" {
		session := ""
		if match := sdpSessionPattern.FindSubmatch(body); len(match) == 2 {
			session = strings.ToLower(strings.TrimSpace(string(match[1])))
		}
		switch {
		case strings.Contains(session, "broadcast"):
			return businessClassification{BusinessBroadcast, "语音广播", "high"}
		case strings.Contains(session, "talk"):
			return businessClassification{BusinessTalk, "语音对讲", "high"}
		case strings.Contains(session, "download"):
			return businessClassification{BusinessDownload, "下载", "high"}
		case strings.Contains(session, "playback") || strings.Contains(session, "record"):
			return businessClassification{BusinessPlayback, "回放", "high"}
		case strings.Contains(session, "play") || strings.Contains(session, "live"):
			return businessClassification{BusinessRealtimePlay, "实时点播", "high"}
		case method == "INVITE" && subjectSuggestsRealtime(subject):
			return businessClassification{BusinessRealtimePlay, "实时点播", "medium"}
		}
	}
	if method == "MESSAGE" || method == "INFO" {
		head, err := manscdp.ParseHead(body)
		if err == nil {
			return classifyMANSCDP(head.CmdType, body)
		}
	}
	return unknown
}

func classifyMANSCDP(cmdType string, body []byte) businessClassification {
	cmd := strings.ToLower(strings.TrimSpace(cmdType))
	switch cmd {
	case "keepalive":
		return businessClassification{BusinessKeepalive, "心跳保持", "high"}
	case "catalog":
		return businessClassification{BusinessCatalog, "目录查询", "high"}
	case "deviceinfo":
		return businessClassification{BusinessDeviceInfo, "设备信息", "high"}
	case "devicestatus":
		return businessClassification{BusinessDeviceStatus, "设备状态", "high"}
	case "alarm":
		return businessClassification{BusinessAlarm, "报警", "high"}
	case "devicecontrol":
		return classifyDeviceControl(body)
	case "ptzprecisectrl":
		return businessClassification{BusinessPTZ, "云台精准控制", "high"}
	case "ptzposition", "ptzpreciseposition":
		return businessClassification{BusinessPTZ, "云台位置上报", "high"}
	case "presetquery":
		return businessClassification{BusinessPTZ, "预置位查询", "high"}
	case "homepositionquery":
		return businessClassification{BusinessPTZ, "看守位查询", "high"}
	case "cruisetracklistquery":
		return businessClassification{BusinessPTZ, "巡航轨迹列表查询", "high"}
	case "cruisetrackquery":
		return businessClassification{BusinessPTZ, "巡航轨迹查询", "high"}
	case "ptzprecisestatusquery":
		return businessClassification{BusinessPTZ, "云台精准状态查询", "high"}
	case "recordinfo":
		return businessClassification{BusinessRecordQuery, "录像查询", "high"}
	case "broadcast":
		return businessClassification{BusinessBroadcast, "语音广播", "high"}
	case "mobileposition":
		return businessClassification{BusinessMobilePosition, "移动位置", "high"}
	default:
		return businessClassification{BusinessUnknown, "未知业务", "none"}
	}
}

func classifyDeviceControl(body []byte) businessClassification {
	xmlBody := strings.ToLower(string(body))
	switch {
	case strings.Contains(xmlBody, "<ptzcmd"), strings.Contains(xmlBody, "<ptzprecise"):
		return businessClassification{BusinessPTZ, "云台控制", "high"}
	case strings.Contains(xmlBody, "<recordcmd"):
		return businessClassification{BusinessDeviceControl, "设备录像控制", "high"}
	case strings.Contains(xmlBody, "<guardcmd"):
		return businessClassification{BusinessDeviceControl, "布撤防", "high"}
	case strings.Contains(xmlBody, "<alarmcmd"):
		return businessClassification{BusinessDeviceControl, "报警复位", "high"}
	case strings.Contains(xmlBody, "<teleboot"):
		return businessClassification{BusinessDeviceControl, "远程启动", "high"}
	case strings.Contains(xmlBody, "<dragzoomin"), strings.Contains(xmlBody, "<dragzoomout"):
		return businessClassification{BusinessDeviceControl, "拉框控制", "high"}
	case strings.Contains(xmlBody, "<iframecmd"), strings.Contains(xmlBody, "<ifamecmd"):
		return businessClassification{BusinessDeviceControl, "I 帧请求", "high"}
	case strings.Contains(xmlBody, "<homeposition"):
		return businessClassification{BusinessDeviceControl, "看守位控制", "high"}
	default:
		return businessClassification{BusinessDeviceControl, "设备控制", "high"}
	}
}

func subjectSuggestsRealtime(subject string) bool {
	sender := strings.TrimSpace(strings.SplitN(subject, ",", 2)[0])
	separator := strings.LastIndexByte(sender, ':')
	if separator < 0 || separator == len(sender)-1 {
		return false
	}
	return sender[separator+1] == '0'
}

// extractNationalID returns only the SIP URI user part. It intentionally does
// not validate a vendor's ID length: malformed values remain useful evidence.
func extractNationalID(uri string) string {
	value := strings.TrimSpace(uri)
	value = strings.Trim(value, "<>")
	if at := strings.IndexByte(value, ';'); at >= 0 {
		value = value[:at]
	}
	if scheme := strings.Index(value, ":"); scheme >= 0 && strings.EqualFold(value[:scheme], "sip") {
		value = value[scheme+1:]
	}
	if at := strings.IndexByte(value, '@'); at >= 0 {
		value = value[:at]
	}
	return strings.TrimSpace(value)
}

func resolveDisplayAddr(addr, platformAddr string) string {
	if platformAddr == "" || addr == "" {
		return addr
	}
	if strings.HasPrefix(addr, "[::]:") || strings.HasPrefix(addr, "0.0.0.0:") {
		return platformAddr
	}
	return addr
}

func normalizeStoredAddr(addr string) string {
	if !strings.HasPrefix(addr, "[::]:") && !strings.HasPrefix(addr, "0.0.0.0:") {
		return addr
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return "本机 SIP"
	}
	return "本机 SIP:" + port
}

func fallbackBusinessForMethod(method string) businessClassification {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "REGISTER":
		return businessClassification{BusinessRegister, "注册", "medium"}
	case "SUBSCRIBE", "NOTIFY":
		return businessClassification{BusinessSubscription, "订阅/通知", "medium"}
	case "BYE", "CANCEL":
		return businessClassification{BusinessHangup, "挂断", "medium"}
	case "ACK":
		return businessClassification{BusinessAck, "会话确认", "medium"}
	default:
		return businessClassification{BusinessUnknown, "未知业务", "none"}
	}
}
