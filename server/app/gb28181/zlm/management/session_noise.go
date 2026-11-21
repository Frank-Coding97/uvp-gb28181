package management

import (
	"net"
	"strings"

	"uvplatform.com/uvp-gb28181/app/gb28181/zlm"
)

// isPlatformControlSession 判定一条 ZLM 会话是「平台自己在调 HTTP API」的短连接。
//
// 平台每轮 overview 会并发请求 getStatistic / getAllSession / getMediaList /
// getThreadsLoad / getWorkThreadsLoad，每个请求都会在 ZLM 的会话表里留下一条
// 「回环地址 + HttpSession」，几百毫秒后关闭。把它们算进「网络会话」会带来两个
// 后果：数字随采样节拍抖动；卡片数字（快照时刻）与明细表（点开时刻）永远对不上。
//
// 只排除回环来源，经反代或真实 IP 接入的 HTTP-FLV 观众不受影响。
func isPlatformControlSession(session zlm.Session) bool {
	if !isLoopbackAddress(session.PeerIP) {
		return false
	}
	typeID := strings.ToLower(strings.TrimSpace(session.TypeID))
	protocol := strings.ToLower(strings.TrimSpace(session.Type))
	return strings.Contains(typeID, "http") || protocol == "http"
}

func isLoopbackAddress(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	// IPv6 带 zone 的写法（fe80::1%lo0）不是回环，但 ::1%lo0 要能识别。
	if index := strings.LastIndex(host, "%"); index >= 0 {
		host = host[:index]
	}
	parsed := net.ParseIP(host)
	return parsed != nil && parsed.IsLoopback()
}

// businessSessionCount 统计需要展示给运维的会话数：剔除平台自身控制连接。
func businessSessionCount(sessions []zlm.Session) int {
	count := 0
	for _, session := range sessions {
		if isPlatformControlSession(session) {
			continue
		}
		count++
	}
	return count
}

// visibleSessions 返回剔除平台自身控制连接后的会话列表，顺序保持不变。
func visibleSessions(sessions []zlm.Session) []zlm.Session {
	if len(sessions) == 0 {
		return sessions
	}
	filtered := make([]zlm.Session, 0, len(sessions))
	for _, session := range sessions {
		if isPlatformControlSession(session) {
			continue
		}
		filtered = append(filtered, session)
	}
	return filtered
}
