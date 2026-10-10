package setup

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"
)

// YAMLSIPSource 从 legacy YAML 里读 SIP 字段的抽象接口.
// 生产用 app.ConfigYml, test 用 mock.
type YAMLSIPSource interface {
	GetString(string) string
	GetInt(string) int
}

// MigrateYAMLToDB 一次性升级迁移:老 stack 里 YAML 已配好 SIP 段,首次升到 DB 为权威源的版本时,
// 把 YAML 里的 SIP 段搬进 gb_sip_config 表.
//
// 触发条件(全部满足才 seed):
//   - gb_sip_config 表空(没落过 DB 权威源)
//   - YAML 里 gb28181.sip.* 完整(deploymentmode/ip/port/domain/serverid/password 都有值)
//   - gb_device 表存在且有数据(证明是老 stack 升级,不是新装)
//
// 返回值:
//   - migrated=true 说明本次触发了迁移并 seed 了一行
//   - migrated=false 且 err==nil 说明判据未命中,不需要迁移(新装场景/已迁移过)
//   - err!=nil 说明迁移过程本身出错(DB 异常等)
//
// 幂等:重复调用无副作用. 新装用户 gb_device 表空 → 直接 skip. 升级用户第二次跑 → gb_sip_config
// 有数据 → skip.
func MigrateYAMLToDB(ctx context.Context, db *gorm.DB, yaml YAMLSIPSource) (bool, error) {
	if db == nil {
		return false, errors.New("db is nil")
	}
	if yaml == nil {
		return false, nil
	}

	existing, err := NewSIPConfigRepository(db).Get(ctx)
	if err != nil {
		return false, err
	}
	if existing != nil {
		return false, nil // 已经有 DB 权威源,不迁移
	}

	// 判 old-stack:gb_device 表存在且有数据.
	// 表不存在或查询失败 → 视为新装,不 seed.
	var deviceCount int64
	if err := db.WithContext(ctx).Table("gb_device").Count(&deviceCount).Error; err != nil {
		return false, nil // 表不存在或首次装机
	}
	if deviceCount == 0 {
		return false, nil // 全新装机,不 seed
	}

	// 从 YAML 读候选,字段不齐则跳过(新版本部署可能已经清空 YAML SIP 段)
	candidate := SaveSIPConfigRequest{
		DeploymentMode: DeploymentMode(strings.TrimSpace(yaml.GetString("gb28181.sip.deploymentmode"))),
		ListenIP:       strings.TrimSpace(yaml.GetString("gb28181.sip.ip")),
		AdvertiseIP:    strings.TrimSpace(yaml.GetString("gb28181.sip.advertiseip")),
		Port:           yaml.GetInt("gb28181.sip.port"),
		Domain:         strings.TrimSpace(yaml.GetString("gb28181.sip.domain")),
		ServerID:       strings.TrimSpace(yaml.GetString("gb28181.sip.serverid")),
	}
	if !candidate.DeploymentMode.Valid() {
		candidate.DeploymentMode = DeploymentLAN
	}
	// listenIP 是具体 IP 时把它当 advertiseIP,复现 legacy 语义
	if candidate.AdvertiseIP == "" && candidate.ListenIP != "" && candidate.ListenIP != wildcardIPv4 {
		candidate.AdvertiseIP = candidate.ListenIP
	}
	// SDP IP 是必填项,但 legacy YAML 段里没有这个键 —— 依次从 ZLM 的
	// sdpip / receivehost / playbackhost 推导。推导不出可用值时
	// ValidateSIPConfigRequest 会拒绝,于是不 seed、用户走引导页补填,
	// 而不是把一个 127.0.0.1 静默固化进库(那会让点播永远没有画面)。
	candidate.SDPIP = legacySDPIP(yaml)
	pw := yaml.GetString("gb28181.sip.password")
	candidate.Password = &pw

	// 用正式 validator 判合法,不合法就不 seed(用户改天配好会主动登录走引导页)
	if err := ValidateSIPConfigRequest(candidate, false); err != nil {
		return false, nil
	}

	row := SIPConfig{
		ID:             SingletonID,
		DeploymentMode: candidate.DeploymentMode,
		ListenIP:       candidate.ListenIP,
		AdvertiseIP:    candidate.AdvertiseIP,
		Port:           candidate.Port,
		Domain:         candidate.Domain,
		ServerID:       candidate.ServerID,
		SDPIP:          candidate.SDPIP,
		Password:       pw,
	}
	if err := NewSIPConfigRepository(db).Save(ctx, &row); err != nil {
		return false, err
	}
	return true, nil
}

// legacySDPIP 从 legacy YAML 的 gb28181.zlm 段推导 SDP 地址。
//
// 刻意不读 zlm.host:那是 ZLM 的 API 地址,单机部署就是 127.0.0.1,
// 推导它等于把「设备推流给平台自己」写死进库。
func legacySDPIP(yaml YAMLSIPSource) string {
	for _, key := range []string{"gb28181.zlm.sdpip", "gb28181.zlm.receivehost", "gb28181.zlm.playbackhost"} {
		if host := strings.TrimSpace(yaml.GetString(key)); host != "" {
			return host
		}
	}
	return ""
}
