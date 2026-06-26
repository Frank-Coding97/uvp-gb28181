package catalog_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"uvplatform.cn/uvp-gb28181/app/gb28181/catalog"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// TestClassify 20 位编码类型识别 + anomaly 兜底
func TestClassify(t *testing.T) {
	cases := []struct {
		name     string
		code     string
		wantType gbmodels.NodeType
		wantAnom bool
	}{
		{"视频通道 131", "34010500001310000001", gbmodels.NodeTypeChannel, false},
		{"报警通道 130", "34010500001300000001", gbmodels.NodeTypeChannel, false},
		{"摄像设备 132", "34010500001320000001", gbmodels.NodeTypeChannel, false},
		{"国标设备 200", "34010500002000000001", gbmodels.NodeTypeDevice, false},
		{"业务分组 215", "34010500002150000001", gbmodels.NodeTypeBizGroup, false},
		{"虚拟组织 216", "34010500002160000001", gbmodels.NodeTypeVirtualOrg, false},
		{"系列设备 111", "34010500001110000001", gbmodels.NodeTypeChannel, false},
		{"未知类型 999 → 兜底", "34010500009990000001", gbmodels.NodeTypeVirtualOrg, true},
		{"长度不足 → 兜底", "34010500", gbmodels.NodeTypeVirtualOrg, true},
		{"含字母 → 兜底", "UVP-PRIVATE-0012", gbmodels.NodeTypeVirtualOrg, true},
		{"空串 → 兜底", "", gbmodels.NodeTypeVirtualOrg, true},
		{"带空格 → trim 后兜底(长度)", "  34010500  ", gbmodels.NodeTypeVirtualOrg, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := catalog.Classify(c.code)
			assert.Equal(t, c.wantType, got.NodeType, "node_type 应匹配")
			assert.Equal(t, c.wantAnom, got.Anomaly, "anomaly 标记应匹配")
			if c.wantAnom {
				assert.NotEmpty(t, got.Reason, "anomaly 必须有 reason")
			}
			assert.Equal(t, c.code, got.RawCode, "RawCode 应保留原始入参")
		})
	}
}

// TestClassify_CivilCodeExtracted 验证前 6 位行政区码提取
func TestClassify_CivilCodeExtracted(t *testing.T) {
	got := catalog.Classify("37011200001310000001") // 济南历城区 370112
	assert.Equal(t, "370112", got.CivilCode)
	assert.False(t, got.Anomaly)
	assert.Equal(t, gbmodels.NodeTypeChannel, got.NodeType)
}

// TestIsCivilCodeNode 6 位行政区码节点识别
func TestIsCivilCodeNode(t *testing.T) {
	assert.True(t, catalog.IsCivilCodeNode("370100"))
	assert.True(t, catalog.IsCivilCodeNode("370112"))
	assert.False(t, catalog.IsCivilCodeNode("3701")) // 长度不对
	assert.False(t, catalog.IsCivilCodeNode("ABC123"))
	assert.False(t, catalog.IsCivilCodeNode(""))
}
