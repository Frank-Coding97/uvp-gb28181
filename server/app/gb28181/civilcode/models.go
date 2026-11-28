// Package civilcode 行政区划字典 (GB/T 2260 6 位级)
// Q2 决议:Go embed + modood Administrative-divisions-of-China JSON,
// 启动 SeedIfEmpty 幂等,~3500 条 6 位级数据。
package civilcode

import "time"

// SysCivilCode 行政区划字典
// 表小(~3500 行 × ~60 字节 ≈ 200KB),进程内全表缓存 sync.Map
type SysCivilCode struct {
	Code       string    `gorm:"column:code;size:6;primaryKey" json:"code"`
	Name       string    `gorm:"column:name;size:64;not null" json:"name"`
	ShortName  string    `gorm:"column:short_name;size:32" json:"shortName"`
	ParentCode string    `gorm:"column:parent_code;size:6;index:idx_parent_code,priority:1" json:"parentCode"`
	Level      int8      `gorm:"column:level;not null;index:idx_parent_code,priority:2" json:"level"`
	Pinyin     string    `gorm:"column:pinyin;size:64;index:idx_pinyin" json:"pinyin"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// TableName 表名
func (SysCivilCode) TableName() string { return "sys_civil_code" }

// CivilCodeLevel 行政区级
const (
	LevelProvince int8 = 1 // 省 / 自治区 / 直辖市
	LevelCity     int8 = 2 // 市 / 自治州 / 盟
	LevelCounty   int8 = 3 // 区 / 县 / 市辖区
)
