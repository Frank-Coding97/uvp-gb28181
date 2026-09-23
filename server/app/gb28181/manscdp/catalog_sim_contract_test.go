package manscdp_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
)

const simChannelID = "34020000001320000010"

// 跨仓契约核对：把**模拟器实际产出的目录报文**喂给平台的 MANSCDP 解析器，
// 确认 `<Longitude>` / `<Latitude>` 真的能被取到。
//
// 为什么值得单独钉一条：这两个元素是 2026-09-21 才在模拟器侧加上的，元素位置由
// CatalogNotifyBuilder 决定（标准要求排在 `<Status>` 之后、`<Info>` 之前）。解析器按
// 标签名匹配、不看顺序，所以位置错通常仍能解出来 —— 但"能解出来"这件事必须实测，
// 不能靠推断：平台侧 `catalog/upsert.go` 拿不到坐标就**静默不发**，
// 表现为"通道坐标永远是 0"，而日志里一切正常。
//
// 四种报文形态各来一条（两版标准 × NOTIFY/Response），因为它们在平台侧走**两条不同的
// 解析入口**（ParseCatalogNotify / ParseCatalogResponse），其中 Response 那条路径
// 依赖 CatalogItem.normalize() 归一化嵌套字段 —— 少了任何一条都可能只坏一半。
//
// ## 夹具来源（`testdata/sim-catalog-*.xml`，逐字节取自模拟器真实输出）
//
// 生成方式：在模拟器仓 `~/code/uvp/uvp-gb28181-sim` 打一份四个形态的报文出来，
// 拷进本目录。报文"长什么样"由模拟器侧的
// `shared/src/commonTest/.../CatalogInstallPositionTest.kt` 逐条钉住（元素位置、
// 两版差异、0 值不发、非设备节点不发）—— 本用例只负责验"这份形态平台吃得下"。
// 两侧分工明确：改模拟器输出 → 模拟器那条先红；改平台解析 → 本用例先红。
func TestParseCatalog_AcceptsSimulatorInstallPosition(t *testing.T) {
	cases := []struct {
		name string
		path string
		// itemOf 从对应解析结果里取出视频通道那一项
		itemOf func(t *testing.T, body []byte) manscdp.CatalogItem
	}{
		{"NOTIFY-2016", "testdata/sim-catalog-V2016.xml", itemFromNotify},
		{"NOTIFY-2022", "testdata/sim-catalog-V2022.xml", itemFromNotify},
		{"Response-2016", "testdata/sim-catalog-response-V2016.xml", itemFromResponse},
		{"Response-2022", "testdata/sim-catalog-response-V2022.xml", itemFromResponse},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, err := os.ReadFile(tc.path)
			require.NoError(t, err, "夹具缺失(%s) —— 它不是可选样本，是这条契约的另一半", tc.path)
			item := tc.itemOf(t, body)
			assert.InDelta(t, 116.404, item.Longitude, 1e-6, "经度未解析出来")
			assert.InDelta(t, 39.915, item.Latitude, 1e-6, "纬度未解析出来")

			// 顺带确认"坐标已解析 → 有可用坐标"这条判据成立：平台侧 catalog/upsert.go
			// 正是用它决定要不要覆盖 gb_channel 的坐标列。判据若挂了，
			// 报文解析得再对也不会落库。
			assert.True(t, item.Longitude != 0 || item.Latitude != 0, "会被判成'设备未声明坐标'")
		})
	}
}

func itemFromNotify(t *testing.T, body []byte) manscdp.CatalogItem {
	t.Helper()
	notify, err := manscdp.ParseCatalogNotify(body)
	require.NoError(t, err, "Catalog NOTIFY 解析失败")
	for _, item := range notify.DeviceList.Items {
		if item.DeviceID == simChannelID {
			return item
		}
	}
	require.Failf(t, "样本里没有视频通道节点", "期望 %s，实际 SN=%d 共 %d 项", simChannelID, notify.SN, len(notify.DeviceList.Items))
	return manscdp.CatalogItem{}
}

func itemFromResponse(t *testing.T, body []byte) manscdp.CatalogItem {
	t.Helper()
	resp, err := manscdp.ParseCatalogResponse(body)
	require.NoError(t, err, "Catalog Response 解析失败")
	for _, item := range resp.DeviceList.Items {
		if item.DeviceID == simChannelID {
			return item
		}
	}
	require.Failf(t, "样本里没有视频通道节点", "期望 %s，共 %d 项", simChannelID, len(resp.DeviceList.Items))
	return manscdp.CatalogItem{}
}
