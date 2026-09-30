package grant

import (
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
)

// installNotDataMask 复刻生产环境的全局查询回调。
//
// ⛔ 生产 gorm 实例在 app/utils/gormhelper/client.go 里注册了
// MaskNotDataError（见 app/utils/gormhelper/hook.go），把 Statement.RaiseErrorOnNotFound
// 恒置为 false —— 即"查询无数据不报错"。
//
// 自建测试库如果不装这个回调，单测就活在"First 会返回 gorm.ErrRecordNotFound"的
// 理想世界里：任何 `First(&x) + errors.Is(err, gorm.ErrRecordNotFound)` 形式的
// 存在性判断都会在单测里绿、在真机上静默走错分支。
// 共享授权"首次共享一条也不写库"就是这么漏掉的，故测试库统一装上。
func installNotDataMask(db *gorm.DB) *gorm.DB {
	_ = db.Callback().Query().Before("gorm:query").Register("disable_raise_record_not_found", gormhelper.MaskNotDataError)
	return db
}
