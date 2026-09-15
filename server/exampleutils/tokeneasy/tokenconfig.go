package tokeneasy

import (
	"go.uber.org/zap"
	"sync"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/ymlconfig"
)

var (
	tokenConfig app.YmlConfigInterf
	configOnce  sync.Once
)

// GetTokenConfig 获取token配置实例
func GetTokenConfig() app.YmlConfigInterf {
	configOnce.Do(func() {
		tokenConfig = ymlconfig.CreateYamlFactory("./config", "exampletoken")
		// 监听配置文件变化
		tokenConfig.ConfigFileChangeListen(func() {
			app.ZapLog.Named("auth.member").Info("token配置文件变化，重新加载", zap.String("event", "auth.member.config_changed"))
		})
	})
	return tokenConfig
}
