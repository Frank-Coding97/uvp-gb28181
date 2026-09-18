package controllers

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func videoParamIntPtr(value int) *int       { return &value }
func videoParamStrPtr(value string) *string { return &value }

func TestBuildVideoParamItemsKeepsZeroStreamNumber(t *testing.T) {
	items, failure := buildVideoParamItems([]videoParamItemRequest{{
		StreamNumber: videoParamIntPtr(0),
		VideoFormat:  "2", Resolution: "5", FrameRate: "25", BitRateType: "1",
		VideoBitRate: videoParamStrPtr("2048"),
	}})
	require.Empty(t, failure)
	require.Len(t, items, 1)
	// ⛔ 0 号是合法的主码流编号。这一条挡的是"把 0 当零值丢掉"这类改法：
	// 用 *int 而不是 int 就是为了这个，写成 int 时"没填"和"填了 0"分辨不出来。
	require.Equal(t, 0, items[0].StreamNumber)
	require.Equal(t, "2", items[0].VideoFormat)
	require.NotNil(t, items[0].VideoBitRate)
	require.Equal(t, "2048", *items[0].VideoBitRate)
}

func TestBuildVideoParamItemsRejectsMissingStreamNumber(t *testing.T) {
	_, failure := buildVideoParamItems([]videoParamItemRequest{{VideoFormat: "2"}})
	require.NotEmpty(t, failure, "码流编号缺失必须报错，否则整批配置会落到错误的码流上")
	require.Contains(t, failure, "码流编号")
}

func TestBuildVideoParamItemsRejectsEmptyAndOversizedBatch(t *testing.T) {
	_, failure := buildVideoParamItems(nil)
	require.NotEmpty(t, failure)

	oversized := make([]videoParamItemRequest, 0, videoParamMaxStreams+1)
	for i := 0; i <= videoParamMaxStreams; i++ {
		oversized = append(oversized, videoParamItemRequest{StreamNumber: videoParamIntPtr(i)})
	}
	_, failure = buildVideoParamItems(oversized)
	require.NotEmpty(t, failure, "超出工程护栏的码流数量应拒发")
}

// ⛔ 空串与 nil 在协议层是两件事：nil = 不发这个元素（VBR 下的正确形态），
// 空串 = 发了个空元素。前端传 "" 时应归一成"没填"。
func TestBuildVideoParamItemsNormalizesBlankOptionalToNil(t *testing.T) {
	items, failure := buildVideoParamItems([]videoParamItemRequest{{
		StreamNumber: videoParamIntPtr(1), BitRateType: "2",
		VideoBitRate: videoParamStrPtr("   "),
	}})
	require.Empty(t, failure)
	require.Nil(t, items[0].VideoBitRate, "空白可选值必须归一成 nil，不能发一个空元素出去")

	items, failure = buildVideoParamItems([]videoParamItemRequest{{
		StreamNumber: videoParamIntPtr(1), BitRateType: "2",
	}})
	require.Empty(t, failure)
	require.Nil(t, items[0].VideoBitRate)
}

func TestBuildVideoParamItemsTrimsTextFields(t *testing.T) {
	items, failure := buildVideoParamItems([]videoParamItemRequest{{
		StreamNumber: videoParamIntPtr(0),
		VideoFormat:  " 2 ", Resolution: " 1920x1080 ", FrameRate: " 25 ", BitRateType: " 2 ",
	}})
	require.Empty(t, failure)
	require.Equal(t, "2", items[0].VideoFormat)
	require.Equal(t, "1920x1080", items[0].Resolution)
	require.Equal(t, "25", items[0].FrameRate)
	require.Equal(t, "2", items[0].BitRateType)
}

// 结构校验不看取值：越界值要留到 manscdp.ValidateVideoParamItems 判，
// 控制器里再写一套就是第二个真源（两边迟早会漂）。
func TestBuildVideoParamItemsLeavesRangeValidationToProtocolLayer(t *testing.T) {
	items, failure := buildVideoParamItems([]videoParamItemRequest{{
		StreamNumber: videoParamIntPtr(0),
		VideoFormat:  "99", Resolution: "nonsense", FrameRate: "999", BitRateType: "7",
	}})
	require.Empty(t, failure, "控制器只做结构校验，取值范围归协议层")
	require.Len(t, items, 1)
	require.Equal(t, "99", items[0].VideoFormat)
}
