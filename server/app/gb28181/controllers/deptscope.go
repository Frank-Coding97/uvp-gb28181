package controllers

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/utils/datascope"
)

func ownerDeptScope(c *gin.Context) func(*gorm.DB) *gorm.DB {
	return datascope.OwnerDeptScope(c, "owner_dept_id")
}

// visibleScope 设备可见性过滤(归属 OR 共享),用于设备/通道表查询
// (两表均有 20 位设备编码列 device_id,目录树/异常记录等无编码列的资源仍用 ownerDeptScope)。
func visibleScope(c *gin.Context) func(*gorm.DB) *gorm.DB {
	return datascope.VisibilityScope(c, "owner_dept_id", "device_id")
}

func transactionalVisibleScope(c *gin.Context) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.Scopes(datascope.VisibilityScopeWithDB(c, db, "owner_dept_id", "device_id"))
	}
}

// aliasedChannelVisibleScope 与 visibleScope 语义相同（归属 OR 共享），只是列名带**表别名**，
// 供"主表 JOIN gb_channel"的查询使用（当前唯一使用者：图像库列表 ListSnapshots）。
//
// ⛔ 联表查询**必须**带别名，不能复用 visibleScope：`gb_channel_snapshot` 自己也有一个名为
// `device_id` 的列（语义是**平台主键**，而 gb_channel.device_id 是 **20 位国标编码**）。
// 不带前缀的 `device_id` 在联表里是歧义列，轻则 SQL 报错、重则匹配到错的那一列 ——
// 后者会让数据范围过滤静默失效（越权看到别人的画面）。
//
// ⛔ 因此这里查的是 `ch.*`（gb_channel 的列），不是 `s.*`。
func aliasedChannelVisibleScope(c *gin.Context) func(*gorm.DB) *gorm.DB {
	return datascope.VisibilityScope(c, "ch.owner_dept_id", "ch.device_id")
}
