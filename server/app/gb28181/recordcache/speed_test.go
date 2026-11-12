package recordcache

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMaxSpeedFromPayload 钉住「设备上报的下载档位怎么取值」。
//
// ⛔ 这一层的三种错法都不会报错，只会静默退化成"没有能力记录"⇒ 一律用 4 倍速：
// ① 咬定键盘名（历史值里既有 XML 元素名 DownloadSpeed、也有 DTO 的 downloadSpeed）；
// ② 不认 `/` 分隔的多值（`1/2/4` 取整体会解析失败）；
// ③ 把非法档位（如设备乱报的 `3`）当成可用值发进 SDP。
func TestMaxSpeedFromPayload(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    int
	}{
		{"XML 元素名大驼峰", `{"DownloadSpeed":"1/2/4","Resolution":"1/2"}`, 4},
		{"DTO 小驼峰", `{"downloadSpeed":"1/2/4"}`, 4},
		{"取最大档", `{"DownloadSpeed":"1/2/4/8"}`, 8},
		{"值里带空格", `{"DownloadSpeed":"2 / 4"}`, 4},
		{"只报一档", `{"DownloadSpeed":"2"}`, 2},
		{"非法档位被剔除", `{"DownloadSpeed":"3"}`, 0},
		{"非法与合法混合只取合法", `{"DownloadSpeed":"3/4"}`, 4},
		{"数字形态", `{"DownloadSpeed":8}`, 8},
		{"数组形态", `{"downloadSpeed":[1,2]}`, 2},
		{"没有该字段", `{"Resolution":"1/2"}`, 0},
		{"空对象", `{}`, 0},
		{"空串", ``, 0},
		{"坏 JSON", `{"DownloadSpeed":`, 0},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.want, maxSpeedFromPayload(testCase.payload))
		})
	}
}

// TestDownloadSpeedOptionsAreTheOnlyLegalGears 钉住合法档位表本身。
// 倍速是 SDP 的 `a=downloadspeed` 取值，发别的值设备行为未定义。
func TestDownloadSpeedOptionsAreTheOnlyLegalGears(t *testing.T) {
	require.Equal(t, []int{1, 2, 4, 8}, DownloadSpeedOptions)
	for _, legal := range DownloadSpeedOptions {
		require.True(t, validDownloadSpeed(legal))
	}
	for _, illegal := range []int{0, 3, 5, 6, 7, 16, -1} {
		require.False(t, validDownloadSpeed(illegal), "%d 不是合法档位", illegal)
	}
	require.True(t, validDownloadSpeed(DefaultDownloadSpeed), "兜底值本身必须是合法档位")
}
