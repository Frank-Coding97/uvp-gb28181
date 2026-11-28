package recordcache

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

// GormSpeedSource 从平台**已回读过的**设备配置里取下载倍速能力。
//
// 数据源是 `gb_device_config` 里 `ConfigType='VideoParamOpt'` 的那一行
// （A.2.1.20 的 VideoParamOptBlock.DownloadSpeed，形如 `1/2/4`）。
// 没有记录时返回 0，由调用方回退到 DefaultDownloadSpeed。
//
// ⚠️ 平台目前不会在设备上线时主动查 VideoParamOpt（订阅项里没有它），
// 所以**没在设备配置页读过这台设备的用户会拿到兜底倍速**。这是刻意的：
// 主动发起 ConfigDownload 要走 PTZ 的异步 operation 通道（重试预算、授权 epoch、
// 对账），把一次"创建缓存任务"绑在一个 SIP 往返上会让接口成功率取决于设备应答速度，
// 而结果还只是个"建议值"。
type GormSpeedSource struct{ db *gorm.DB }

func NewGormSpeedSource(db *gorm.DB) *GormSpeedSource { return &GormSpeedSource{db: db} }

// MaxDownloadSpeed 返回设备支持的**最大合法档位**；无记录 / 解析不出时返回 0。
func (s *GormSpeedSource) MaxDownloadSpeed(ctx context.Context, devicePK int64, targetCode string) int {
	if s == nil || s.db == nil || devicePK == 0 {
		return 0
	}
	var rows []struct{ PayloadJSON string }
	query := s.db.WithContext(ctx).Table("gb_device_config").
		Select("payload_json").
		Where("device_id = ? AND config_type = ?", devicePK, "VideoParamOpt")
	if code := strings.TrimSpace(targetCode); code != "" {
		// target_code 既可能是设备编码也可能是通道编码（标准的 DeviceID 是"查询目标"），
		// 两个都兜住；设备维度那一行通常 target_code = 设备编码。
		query = query.Where("target_code = ? OR target_code = ''", code)
	}
	if err := query.Order("observed_at DESC").Limit(1).Find(&rows).Error; err != nil {
		return 0
	}
	if len(rows) == 0 {
		return 0
	}
	return maxSpeedFromPayload(rows[0].PayloadJSON)
}

// maxSpeedFromPayload 从 payload_json 里挑出最大的合法档位。
//
// ⛔ 键名做**大小写不敏感**匹配：这一列的历史值既有"键 = XML 元素名"（`DownloadSpeed`）
// 也有协议层 DTO 的 json tag（`downloadSpeed`），两种写法都必须能读；
// 咬定一种会让另一半数据静默变成"没有能力记录"。
func maxSpeedFromPayload(payload string) int {
	trimmed := strings.TrimSpace(payload)
	if trimmed == "" {
		return 0
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return 0
	}
	raw, ok := lookupCaseInsensitive(decoded, "downloadspeed")
	if !ok {
		return 0
	}
	return maxDownloadSpeedFromCodes(raw)
}

// maxDownloadSpeedFromCodes 把 `1/2/4`（或数字、数组）解析成最大合法档位。
func maxDownloadSpeedFromCodes(value any) int {
	codes := make([]int, 0, 4)
	switch typed := value.(type) {
	case string:
		for _, part := range strings.Split(typed, "/") {
			if number, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
				codes = append(codes, number)
			}
		}
	case float64:
		codes = append(codes, int(typed))
	case []any:
		for _, item := range typed {
			if number, ok := item.(float64); ok {
				codes = append(codes, int(number))
			}
		}
	}
	legal := codes[:0]
	for _, code := range codes {
		if validDownloadSpeed(code) {
			legal = append(legal, code)
		}
	}
	if len(legal) == 0 {
		return 0
	}
	sort.Ints(legal)
	return legal[len(legal)-1]
}

func lookupCaseInsensitive(values map[string]any, key string) (any, bool) {
	for candidate, value := range values {
		if strings.EqualFold(strings.TrimSpace(candidate), key) {
			return value, true
		}
	}
	return nil, false
}
