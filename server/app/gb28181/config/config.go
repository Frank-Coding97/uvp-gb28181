package config

import "uvplatform.cn/uvp-gb28181/app/global/app"

// Config GB28181 国标平台配置
type Config struct {
	Enabled bool
	SIP     SIPConfig
	Device  DeviceConfig
	ZLM     ZLMConfig
	Media   MediaConfig
}

// ZLMConfig ZLMediaKit 媒体服务器配置(数据面)
type ZLMConfig struct {
	Host     string // ZLM 地址
	HTTPPort int    // ZLM HTTP API 端口
	Secret   string // API secret
	RTPPort  int    // RTP 单端口收流
}

// MediaConfig 媒体/Hook 配置
type MediaConfig struct {
	HookHost                string // ZLM Hook 回调可达的本机地址
	HookPort                int    // 后端 HTTP 端口(Hook 端点)
	StreamNoneReaderTimeout int    // 无人观看断流秒数
	RTPServerTimeout        int    // RTP 收流超时(秒)
}

// SIPConfig SIP 服务配置
type SIPConfig struct {
	IP        string
	Port      int
	Transport []string // 信令传输: udp / tcp
	Domain    string   // SIP 域(前 10 位行政区划)
	ServerID  string   // 平台国标编码(20 位)
	Password  string   // 统一接入密码
}

// DeviceConfig 设备相关配置
type DeviceConfig struct {
	KeepaliveInterval     int // 心跳周期(秒)
	KeepaliveTimeoutCount int // 连续丢失阈值
	KeepaliveGraceSeconds int // 离线判定宽限缓冲(秒),避开边界误判
	OfflineScanInterval   int // 离线扫描周期(秒)
}

// Load 从全局 ConfigYml 读取 gb28181 配置
func Load() Config {
	c := app.ConfigYml
	return Config{
		Enabled: c.GetBool("gb28181.enabled"),
		SIP: SIPConfig{
			IP:        c.GetString("gb28181.sip.ip"),
			Port:      c.GetInt("gb28181.sip.port"),
			Transport: c.GetStringSlice("gb28181.sip.transport"),
			Domain:    c.GetString("gb28181.sip.domain"),
			ServerID:  c.GetString("gb28181.sip.serverid"),
			Password:  c.GetString("gb28181.sip.password"),
		},
		Device: DeviceConfig{
			KeepaliveInterval:     c.GetInt("gb28181.device.keepalive_interval"),
			KeepaliveTimeoutCount: c.GetInt("gb28181.device.keepalive_timeout_count"),
			KeepaliveGraceSeconds: c.GetInt("gb28181.device.keepalive_grace_seconds"),
			OfflineScanInterval:   c.GetInt("gb28181.device.offline_scan_interval"),
		},
		ZLM: ZLMConfig{
			Host:     c.GetString("gb28181.zlm.host"),
			HTTPPort: c.GetInt("gb28181.zlm.httpport"),
			Secret:   c.GetString("gb28181.zlm.secret"),
			RTPPort:  c.GetInt("gb28181.zlm.rtpport"),
		},
		Media: MediaConfig{
			HookHost:                c.GetString("gb28181.media.hookhost"),
			HookPort:                c.GetInt("gb28181.media.hookport"),
			StreamNoneReaderTimeout: c.GetInt("gb28181.media.streamnonereadertimeout"),
			RTPServerTimeout:        c.GetInt("gb28181.media.rtpservertimeout"),
		},
	}
}
