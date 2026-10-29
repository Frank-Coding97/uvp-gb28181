package models

// ServerConfig 服务器配置参数
type SystemConfig struct {
	SystemLogo      string `json:"systemLogo" yaml:"SystemLogo"`
	SystemIcon      string `json:"systemIcon" yaml:"SystemIcon"`
	SystemName      string `json:"systemName" yaml:"SystemName"`
	SystemCopyright string `json:"systemCopyright" yaml:"SystemCopyright"`
	SystemRecordNo  string `json:"systemRecordNo" yaml:"SystemRecordNo"`
	DefaultUsername string `json:"defaultUsername" yaml:"DefaultUsername"`
	DefaultPassword string `json:"defaultPassword" yaml:"DefaultPassword"`
}

type SafeConfig struct {
	LoginLockThreshold int  `json:"loginLockThreshold" yaml:"LoginLockThreshold"`
	LoginLockExpire    int  `json:"loginLockExpire" yaml:"LoginLockExpire"`
	LoginLockDuration  int  `json:"loginLockDuration" yaml:"LoginLockDuration"`
	MinPasswordLength  int  `json:"minPasswordLength" yaml:"MinPasswordLength"`
	RequireSpecialChar bool `json:"requireSpecialChar" yaml:"RequireSpecialChar"`
}

// CaptchaConfig 验证码配置参数
type CaptchaConfig struct {
	Open   bool `json:"open" yaml:"open"`
	Length int  `json:"length" yaml:"length"`
}

// LogCleanupConfig exposes retention fields as pointers so omitted or null
// values in an update request can be rejected instead of silently becoming 0.
type LogCleanupConfig struct {
	SIPRetentionDays       *int `json:"sipRetentionDays" yaml:"sipRetentionDays"`
	OperationRetentionDays *int `json:"operationRetentionDays" yaml:"operationRetentionDays"`
	LoginRetentionDays     *int `json:"loginRetentionDays" yaml:"loginRetentionDays"`
	JobRetentionDays       *int `json:"jobRetentionDays" yaml:"jobRetentionDays"`
	PlaybackRetentionDays  *int `json:"playbackRetentionDays" yaml:"playbackRetentionDays"`
	SchedulerRetentionDays *int `json:"schedulerRetentionDays" yaml:"schedulerRetentionDays"`
	Configured             bool `json:"configured,omitempty" yaml:"configured,omitempty"`
}

// ConfigRequest 配置请求参数
type ConfigRequest struct {
	System     *SystemConfig     `json:"system,omitempty" yaml:"System,omitempty"`
	Safe       *SafeConfig       `json:"safe,omitempty" yaml:"Safe,omitempty"`
	Captcha    *CaptchaConfig    `json:"captcha,omitempty" yaml:"Captcha,omitempty"`
	LogCleanup *LogCleanupConfig `json:"logCleanup,omitempty" yaml:"logCleanup,omitempty"`
}
