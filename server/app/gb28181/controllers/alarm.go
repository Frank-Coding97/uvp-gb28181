package controllers

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const maxAlarmDeleteBatch = 100

type alarmListItem struct {
	ID string `json:"id"`
}

type alarmEnumValue struct {
	Value *int   `json:"value"`
	Label string `json:"label"`
}

func formatAlarmID(id uint64) string {
	return strconv.FormatUint(id, 10)
}

func parseAlarmID(raw string) (uint64, error) {
	if raw == "" || strings.TrimSpace(raw) != raw {
		return 0, fmt.Errorf("告警 ID 必须是十进制正整数")
	}
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("告警 ID 必须是十进制正整数")
	}
	return id, nil
}

func normalizeAlarmIDs(rawIDs []string) ([]uint64, []string, error) {
	if len(rawIDs) == 0 || len(rawIDs) > maxAlarmDeleteBatch {
		return nil, nil, fmt.Errorf("告警 ID 数量必须在 1-%d 之间", maxAlarmDeleteBatch)
	}
	values := make([]uint64, 0, len(rawIDs))
	stringsOut := make([]string, 0, len(rawIDs))
	seen := make(map[uint64]struct{}, len(rawIDs))
	for _, raw := range rawIDs {
		id, err := parseAlarmID(raw)
		if err != nil {
			return nil, nil, err
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		values = append(values, id)
		stringsOut = append(stringsOut, formatAlarmID(id))
	}
	return values, stringsOut, nil
}

func alarmPriority(value *int) alarmEnumValue {
	return alarmEnum(value, map[int]string{
		1: "一级警情",
		2: "二级警情",
		3: "三级警情",
		4: "四级警情",
	})
}

func alarmMethod(value *int) alarmEnumValue {
	return alarmEnum(value, map[int]string{
		1: "电话报警",
		2: "设备报警",
		3: "短信报警",
		4: "GPS报警",
		5: "视频报警",
		6: "设备故障报警",
		7: "其他报警",
	})
}

func alarmType(method, value *int) alarmEnumValue {
	if value == nil {
		return alarmEnumValue{Label: "未知"}
	}
	labels := map[int]string(nil)
	if method != nil {
		switch *method {
		case 2:
			labels = map[int]string{
				1: "视频丢失报警",
				2: "设备防拆报警",
				3: "存储设备磁盘满报警",
				4: "设备高温报警",
				5: "设备低温报警",
			}
		case 5:
			labels = map[int]string{
				1:  "人工视频报警",
				2:  "运动目标检测报警",
				3:  "遗留物检测报警",
				4:  "物体移除检测报警",
				5:  "绊线检测报警",
				6:  "入侵检测报警",
				7:  "逆行检测报警",
				8:  "徘徊检测报警",
				9:  "流量统计报警",
				10: "密度检测报警",
				11: "视频异常检测报警",
				12: "快速移动报警",
				13: "图像遮挡报警（2022）",
			}
		case 6:
			labels = map[int]string{
				1: "存储设备磁盘故障报警",
				2: "存储设备风扇故障报警",
			}
		}
	}
	return alarmEnum(value, labels)
}

func alarmEnum(value *int, labels map[int]string) alarmEnumValue {
	if value == nil {
		return alarmEnumValue{Label: "未知"}
	}
	label, exists := labels[*value]
	if !exists {
		label = fmt.Sprintf("未知(%d)", *value)
	}
	return alarmEnumValue{Value: value, Label: label}
}

func parseAlarmTimeRange(rawFrom, rawTo string) (*time.Time, *time.Time, error) {
	if rawFrom == "" && rawTo == "" {
		return nil, nil, nil
	}
	if rawFrom == "" || rawTo == "" {
		return nil, nil, fmt.Errorf("告警时间范围必须同时提供起止时间")
	}
	from, err := time.Parse(time.RFC3339, rawFrom)
	if err != nil {
		return nil, nil, fmt.Errorf("告警开始时间必须是 RFC3339")
	}
	to, err := time.Parse(time.RFC3339, rawTo)
	if err != nil {
		return nil, nil, fmt.Errorf("告警结束时间必须是 RFC3339")
	}
	if from.After(to) {
		return nil, nil, fmt.Errorf("告警开始时间不能晚于结束时间")
	}
	return &from, &to, nil
}

func containsLikePattern(value string) string {
	escaped := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(value)
	return "%" + escaped + "%"
}
