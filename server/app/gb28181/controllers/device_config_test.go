package controllers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// TestBuildDeviceConfigEntriesAbsentFollowsRequested `absentTypes` 必须按
// **这一次问过的类型**算差集（2026-09-19 修）。
//
// 库里的行是**历次读取累积**的，与"这次问了什么"无关。修复前实现是
// 「全部 8 组减已落库」，于是 `configTypes=PictureMask` 这种收窄查询会把另外
// 7 个**没问过**的类型一并报成 absent —— 而前端拿到 absent 就显示
// 「设备未返回该类型」并禁用编辑（§7.3「没有事实就不给编辑」），
// 等于**凭空造出 7 个假问题**，还让整组控件点不动。
func TestBuildDeviceConfigEntriesAbsentFollowsRequested(t *testing.T) {
	observedAt := time.Date(2026, 9, 19, 18, 40, 0, 0, time.UTC)
	rows := []gbmodels.GbDeviceConfig{
		{
			ConfigType:  manscdp.ConfigTypePictureMask,
			PayloadJSON: `{"on":0,"regions":[]}`,
			ObservedAt:  observedAt,
		},
	}
	controller := &DeviceMgmtController{}

	// ① 只问 PictureMask，库里正好有 ⇒ 一个 absent 都不该有。
	_, _, absent := controller.buildDeviceConfigEntries(rows, []string{manscdp.ConfigTypePictureMask})
	require.Empty(t, absent, "问过的类型库里都有值，不能报 absent")

	// ② 问两个、库里只有其中一个 ⇒ 只有缺席的那个进 absent。
	_, _, absent = controller.buildDeviceConfigEntries(rows,
		[]string{manscdp.ConfigTypePictureMask, manscdp.ConfigTypeFrameMirror})
	require.Equal(t, []string{manscdp.ConfigTypeFrameMirror}, absent,
		"absent 只能包含「这次问过、且库里没有」的类型")

	// ③ 缺省（前端不带 configTypes ⇒ 全部 8 组）：除 PictureMask 外都缺席。
	//    ⛔ 这一条钉住"收窄修复没有把缺省行为改坏" —— 缺省时 absent 应当真的列出
	//    其余 7 组（它们确实没数据），否则「没读到」会被误显示成「设备报的 0」。
	_, _, absent = controller.buildDeviceConfigEntries(rows, manscdp.ConfigTypeOrder)
	require.Len(t, absent, len(manscdp.ConfigTypeOrder)-1,
		"缺省查询要如实报出其余没有数据的类型")
	require.NotContains(t, absent, manscdp.ConfigTypePictureMask)

	// ④ 传了个空 requested（调用方不该这么传）：不报 absent，也别 panic。
	_, _, absent = controller.buildDeviceConfigEntries(rows, nil)
	require.Empty(t, absent)
}

// TestBuildDeviceConfigEntriesDropsInvalidPayload 落库的 payload 不是合法 JSON 时
// 该行不进 list，也不算"库里有值"。
//
// ⛔ 这条守的是 absent 的**真源**：判定"有没有值"必须与"能不能展示"同一口径。
// 两处各判一次（一处看行数、一处看 JSON 合法性）就会出现"列表里没有、也不在 absent 里"
// 的类型 —— 那类类型在界面上会**彻底消失**，既不报错也没提示。
func TestBuildDeviceConfigEntriesDropsInvalidPayload(t *testing.T) {
	observedAt := time.Date(2026, 9, 19, 18, 40, 0, 0, time.UTC)
	rows := []gbmodels.GbDeviceConfig{
		{
			ConfigType:  manscdp.ConfigTypeOSDConfig,
			PayloadJSON: `{"length":704,`, // 截断的 JSON
			ObservedAt:  observedAt,
		},
	}
	controller := &DeviceMgmtController{}

	entries, _, absent := controller.buildDeviceConfigEntries(rows, []string{manscdp.ConfigTypeOSDConfig})
	require.Empty(t, entries, "非法 JSON 不能进列表")
	require.Equal(t, []string{manscdp.ConfigTypeOSDConfig}, absent,
		"展示不了的行等于没有值，必须落进 absent，否则该类型在界面上会彻底消失")
}
