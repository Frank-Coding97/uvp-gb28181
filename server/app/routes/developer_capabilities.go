package routes

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// The standalone SQLite release does not support development schema tools.
// Register the guard after the existing authentication and authorization chain.
func requireDeveloperTools() gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.EqualFold(strings.TrimSpace(app.ConfigYml.GetString("gormv2.usedbtype")), "sqlite") {
			c.AbortWithStatusJSON(http.StatusNotImplemented, gin.H{
				"code":    1,
				"message": "Windows 单机版不支持开发代码生成及插件结构管理",
				"data":    nil,
			})
			return
		}
		c.Next()
	}
}

func registerDeveloperRoutes(protected *gin.RouterGroup) {
	// 代码生成配置路由组
	sysGen := protected.Group("/sysGen", requireDeveloperTools())
	{
		// 代码生成配置列表（分页查询）
		sysGen.GET("/list", sysGenControllers.List)
		// 批量创建代码生成配置
		sysGen.POST("/batchInsert", sysGenControllers.BatchInsert)
		// 根据ID获取代码生成配置详情
		sysGen.GET("/:id", sysGenControllers.GetByID)
		// 根据ID更新代码生成配置和字段信息
		sysGen.PUT("/update", sysGenControllers.Update)
		// 根据ID删除代码生成配置和字段信息
		sysGen.DELETE("/:id", sysGenControllers.Delete)
		// 根据ID刷新字段信息
		sysGen.PUT("/refreshFields", sysGenControllers.RefreshFields)
	}

	// 代码生成路由组
	codeGen := protected.Group("/codegen", requireDeveloperTools())
	{
		// 获取数据库列表
		codeGen.GET("/databases", codeGenControllers.GetDatabases)
		// 获取指定数据库中的表
		codeGen.GET("/tables", codeGenControllers.GetTables)
		// 获取指定表的字段信息
		codeGen.GET("/columns", codeGenControllers.GetTableColumns)
		// 生成代码
		codeGen.POST("/generate", codeGenControllers.GenerateCode)
		// 预览代码
		codeGen.GET("/preview", codeGenControllers.PreviewCode)
		// 生成菜单
		codeGen.POST("/insertmenuandapi", codeGenControllers.InsertMenuAndApiData)
	}

	// 插件管理路由组
	pluginsManager := protected.Group("/pluginsmanager", requireDeveloperTools())
	{
		// 获取所有插件导出配置
		pluginsManager.GET("/exports", pluginsManagerControllers.GetPluginsExport)
		// 导出插件为压缩包
		pluginsManager.POST("/export", pluginsManagerControllers.ExportPlugin)
		// 导入插件
		pluginsManager.POST("/import", pluginsManagerControllers.ImportPlugin)
		// 卸载插件
		pluginsManager.DELETE("/uninstall", pluginsManagerControllers.UninstallPlugin)
	}
}
