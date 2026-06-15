package main

import (
	"uvplatform.cn/uvp-gb28181/app/routes"
	"uvplatform.cn/uvp-gb28181/app/utils/ginhelper"
	_ "uvplatform.cn/uvp-gb28181/bootstrap"

	_ "uvplatform.cn/uvp-gb28181/docs/swagger" // swagger docs
	_ "uvplatform.cn/uvp-gb28181/plugins"
)

// @title UVP-GB28181 API
// @version 1.0
// @description UVP 国标 GB28181 上级平台 API 文档
// @termsOfService https://github.com/Frank-Coding97/uvp-gb28181

// @contact.name UVP Support
// @contact.url https://github.com/Frank-Coding97/uvp-gb28181/issues

// @license.name MIT
// @license.url https://github.com/Frank-Coding97/uvp-gb28181/blob/main/LICENSE

// @host localhost:8080
// @BasePath /api
func main() {
	// 获取Gin引擎实例
	engine := ginhelper.GetEngine()
	// 初始化系统路由
	routes.InitRoutes(engine)
	// 初始化插件路由
	ginhelper.InitPluginRoutes(engine)
	// 启动服务器
	_ = ginhelper.StartServer(engine)

}
