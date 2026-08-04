package controllers

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAlarmIDContract(t *testing.T) {
	const largeID = uint64(9007199254740993)
	require.Equal(t, "9007199254740993", formatAlarmID(largeID))

	encoded, err := json.Marshal(alarmListItem{ID: formatAlarmID(largeID)})
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"id":"9007199254740993"`)

	parsed, err := parseAlarmID("2006")
	require.NoError(t, err)
	require.Equal(t, uint64(2006), parsed)

	for _, raw := range []string{"", "0", "-1", "1.5", "1e3", " 1 ", "18446744073709551616"} {
		_, err := parseAlarmID(raw)
		require.Error(t, err, raw)
	}
}

func TestAlarmIDBatchNormalization(t *testing.T) {
	values, stringsOut, err := normalizeAlarmIDs([]string{"3", "1", "3", "2"})
	require.NoError(t, err)
	require.Equal(t, []uint64{3, 1, 2}, values)
	require.Equal(t, []string{"3", "1", "2"}, stringsOut)

	_, _, err = normalizeAlarmIDs(nil)
	require.Error(t, err)

	tooMany := make([]string, maxAlarmDeleteBatch+1)
	for i := range tooMany {
		tooMany[i] = formatAlarmID(uint64(i + 1))
	}
	_, _, err = normalizeAlarmIDs(tooMany)
	require.Error(t, err)
}

func TestAlarmEnumLabels(t *testing.T) {
	require.Equal(t, alarmEnumValue{Value: nil, Label: "未知"}, alarmPriority(nil))
	for value, label := range map[int]string{1: "一级警情", 2: "二级警情", 3: "三级警情", 4: "四级警情"} {
		value := value
		require.Equal(t, alarmEnumValue{Value: &value, Label: label}, alarmPriority(&value))
	}
	unknown := 99
	require.Equal(t, "未知(99)", alarmPriority(&unknown).Label)

	for value, label := range map[int]string{
		1: "电话报警", 2: "设备报警", 3: "短信报警", 4: "GPS报警",
		5: "视频报警", 6: "设备故障报警", 7: "其他报警",
	} {
		value := value
		require.Equal(t, label, alarmMethod(&value).Label)
	}
	require.Equal(t, "未知(99)", alarmMethod(&unknown).Label)

	methodDevice, methodVideo, methodFault := 2, 5, 6
	typeOne, typeThirteen := 1, 13
	require.Equal(t, "视频丢失报警", alarmType(&methodDevice, &typeOne).Label)
	require.Equal(t, "人工视频报警", alarmType(&methodVideo, &typeOne).Label)
	require.Equal(t, "存储设备磁盘故障报警", alarmType(&methodFault, &typeOne).Label)
	require.Equal(t, "图像遮挡报警（2022）", alarmType(&methodVideo, &typeThirteen).Label)
	require.Equal(t, "未知(13)", alarmType(&methodDevice, &typeThirteen).Label)
	require.Equal(t, "未知", alarmType(nil, nil).Label)
}

func TestAlarmQueryValidation(t *testing.T) {
	from := "2026-08-04T10:00:00+08:00"
	to := "2026-08-04T11:00:00+08:00"
	fromTime, toTime, err := parseAlarmTimeRange(from, to)
	require.NoError(t, err)
	require.True(t, fromTime.Before(*toTime))

	for _, input := range [][2]string{{from, ""}, {"", to}, {"bad", to}, {to, from}} {
		_, _, err := parseAlarmTimeRange(input[0], input[1])
		require.Error(t, err, input)
	}
	fromTime, toTime, err = parseAlarmTimeRange("", "")
	require.NoError(t, err)
	require.Nil(t, fromTime)
	require.Nil(t, toTime)

	require.Equal(t, `%100\%\_\\中文%`, containsLikePattern(`100%_\中文`))
	require.Equal(t, `%normal%`, containsLikePattern("normal"))
}
