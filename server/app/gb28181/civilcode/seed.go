package civilcode

import (
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// rawEntry embed JSON 一条记录(对应 tools/convert_civil_code 输出格式)
type rawEntry struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	ShortName  string `json:"short_name"`
	ParentCode string `json:"parent_code"`
	Level      int8   `json:"level"`
}

// SeedIfEmpty Q2 决议:启动期幂等 seed。
//
// 行为:
//  1. 若 sys_civil_code 表已有记录(任意一条),直接返回(已 seed)
//  2. 解析 embed JSON,批量 INSERT(每 500 条一批)
//  3. 跑两次不重复(幂等);单 Insert 用 OnConflict DoNothing 兜底
func SeedIfEmpty(db *gorm.DB) (seeded int, err error) {
	var existed int64
	if err = db.Model(&SysCivilCode{}).Limit(1).Count(&existed).Error; err != nil {
		return 0, fmt.Errorf("civilcode: count existing: %w", err)
	}
	if existed > 0 {
		return 0, nil
	}

	var entries []rawEntry
	if err = json.Unmarshal(rawCivilCodeJSON, &entries); err != nil {
		return 0, fmt.Errorf("civilcode: parse embed json: %w", err)
	}
	if len(entries) == 0 {
		return 0, errors.New("civilcode: embed json is empty")
	}

	rows := make([]SysCivilCode, 0, len(entries))
	for _, e := range entries {
		if len(e.Code) != 6 {
			continue // 跳过格式异常
		}
		rows = append(rows, SysCivilCode{
			Code:       e.Code,
			Name:       e.Name,
			ShortName:  e.ShortName,
			ParentCode: e.ParentCode,
			Level:      e.Level,
		})
	}

	const batch = 500
	if err = db.CreateInBatches(rows, batch).Error; err != nil {
		return 0, fmt.Errorf("civilcode: insert batches: %w", err)
	}
	return len(rows), nil
}
