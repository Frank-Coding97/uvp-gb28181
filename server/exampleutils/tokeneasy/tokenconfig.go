package tokeneasy

import (
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/ymlconfig"
	"sync"
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
			app.ZapLog.Info("token配置文件变化，重新加载")
		})
	})
	return tokenConfig
}
