package civilcode

import (
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
//  1. 按完整性判定:表内行数达到数据集行数才算已 seed,部分导入会补插
//  2. 解析 embed JSON,批量 INSERT(每 500 条一批)
//  3. 幂等 upsert:code 主键冲突直接忽略(多实例并发 seed 安全)
func SeedIfEmpty(db *gorm.DB) (seeded int, err error) {
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

	// 完整性不按行数短路:表内存在等量其他数据时缺项不会被发现。
	// 始终对嵌入数据集执行幂等 upsert(code 主键冲突忽略),代价是一次
	// ~3500 行批量写入,换来"每次都补齐缺失项"
	const batch = 500
	result := db.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&rows, batch)
	if result.Error != nil {
		return 0, fmt.Errorf("civilcode: insert batches: %w", result.Error)
	}
	return int(result.RowsAffected), nil
}
