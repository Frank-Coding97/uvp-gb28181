package manscdp_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
)

// hikSnapshotFileIDs 是海康 `37010301021320000002` 一次 3 张抓拍的**实际文件名**
// （与 `snapshot_test.go` 里 `hikUploadSnapShotFinishedBody` 的 `SnapShotFileID` 同源）。
//
// ⛔ 注意长度：只有第一张是标准 41 位，后两张是 **40 位** —— 真机的序列码不补零。
// 这组样例的作用就是钉住"平台不能按 41 位硬校验"，否则 3 张里有 2 张会被判非合规。
var hikSnapshotFileIDs = []string{
	"37010301021320000002022026092013305816101",
	"3701030102132000000202202609201331014002",
	"3701030102132000000202202609201331044003",
}

// TestParseSnapshotFileIDReadsRealDeviceAnchors 用真机文件名钉住分段与拍摄时刻。
//
// ⛔ 关键性质是"三张严格递增且间隔与 SnapShotConfig 的 Interval 一致"：
// 这只有在"时间 17 位 = 秒 14 + 毫秒 3、序列码不定长"的读法下才成立。
// 若改成"从后往前切 2 位当序列码"，第二、三张会把时间字段最后一位吃掉
// （13:31:01.400 读成 13:31:01.40 → 也就是 400ms 变 40ms），
// 单看某一个绝对值看不出来，只有连拍序列的间隔会明显错乱。
func TestParseSnapshotFileIDReadsRealDeviceAnchors(t *testing.T) {
	for _, test := range []struct {
		name, input, captured, sequence string
		compliant                       bool
	}{
		{"第一张(41位)", hikSnapshotFileIDs[0], "2026-09-20T13:30:58.161", "01", true},
		{"第二张(40位)", hikSnapshotFileIDs[1], "2026-09-20T13:31:01.400", "2", false},
		{"第三张(40位)", hikSnapshotFileIDs[2], "2026-09-20T13:31:04.400", "3", false},
		// E-3 真机端到端那次抓拍落盘的文件名（`…14333016801`）。
		{"上传落盘", "37010301021320000002022026092014333016801", "2026-09-20T14:33:30.168", "01", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := manscdp.ParseSnapshotFileID(test.input)
			require.NoError(t, err)
			require.Equal(t, "37010301021320000002", parsed.DeviceCode)
			require.Equal(t, "02", parsed.ImageCode)
			require.Equal(t, test.sequence, parsed.Sequence)
			require.Equal(t, test.captured, parsed.CapturedAt.Format("2006-01-02T15:04:05.000"))
			require.Equal(t, test.input, parsed.Raw)
			require.Equal(t, test.compliant, parsed.StandardCompliant())
		})
	}

	// 连拍序列：严格递增，且两两间隔在合理范围（这组实拍的 Interval 是 3 秒，故间隔约 3s）。
	var moments []time.Time
	for _, id := range hikSnapshotFileIDs {
		parsed, err := manscdp.ParseSnapshotFileID(id)
		require.NoError(t, err)
		moments = append(moments, parsed.CapturedAt)
	}
	for index := 1; index < len(moments); index++ {
		gap := moments[index].Sub(moments[index-1])
		require.Greater(t, gap, time.Duration(0), "第 %d 张应晚于前一张", index+1)
		require.Less(t, gap, 10*time.Second, "第 %d 张的间隔应在 10 秒内", index+1)
	}
}

// TestParseSnapshotFileIDAcceptsUploadFileName 上传的文件名带 `.jpg`，完成通知里的标识不带；
// 两者是**同一样东西的两种出现形式**，必须解析出同一个时刻（否则按时间排序会因来源而异）。
func TestParseSnapshotFileIDAcceptsUploadFileName(t *testing.T) {
	identifier := "37010301021320000002022026092014333016801"
	fromNotify, err := manscdp.ParseSnapshotFileID(identifier)
	require.NoError(t, err)
	fromUpload, err := manscdp.ParseSnapshotFileID(identifier + ".jpg")
	require.NoError(t, err)
	require.Equal(t, fromNotify.CapturedAt, fromUpload.CapturedAt)
	require.Equal(t, fromNotify.Raw, fromUpload.Raw, "Raw 应是去掉后缀后的标识，两种形态一致")
}

// TestParseSnapshotFileIDStandardCompliance 只判"是否严格 41 位"，与"能否解析"分开：
// 40 位真机文件名能解析（StandardCompliant=false），42 位则连解析都不做。
func TestParseSnapshotFileIDStandardCompliance(t *testing.T) {
	// 39 位（时间字段刚好凑齐、序列码为空）能解析但非合规 —— 下界是 39。
	parsed, err := manscdp.ParseSnapshotFileID("370103010213200000020220260920143330168")
	require.NoError(t, err)
	require.Equal(t, "2026-09-20T14:33:30.168", parsed.CapturedAt.Format("2006-01-02T15:04:05.000"))
	require.Empty(t, parsed.Sequence)
	require.False(t, parsed.StandardCompliant())
}

// TestParseSnapshotFileIDRejectsNonCompliant 长度越界与非数字一律报错
// （调用方据此回落接收时刻，**不**因此拒收图片）—— 这里只钉住"确实判成不可解析"。
func TestParseSnapshotFileIDRejectsNonCompliant(t *testing.T) {
	for _, test := range []struct {
		name, input, fragment string
	}{
		{"长度不足", "37010301021320000002022026092014333016", "长度"},
		{"长度超出", "370103010213200000020220260920143330168011", "长度"},
		{"含非数字", "3701030102132000000A022026092014333016801", "不是数字"},
		{"月份非法", "37010301021320000002022026132014333016801", "时间字段"},
		{"小时非法", "37010301021320000002022026092024333016801", "时间字段"},
		{"空串", "", "长度"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := manscdp.ParseSnapshotFileID(test.input)
			require.Error(t, err)
			require.Contains(t, err.Error(), test.fragment)
		})
	}
}
