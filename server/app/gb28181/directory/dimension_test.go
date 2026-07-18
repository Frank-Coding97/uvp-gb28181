package directory

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// RED: 测试 Node 结构体能正确序列化
func TestNodeStructure(t *testing.T) {
	node := Node{
		ID:          "test-001",
		Name:        "测试节点",
		NodeType:    "device",
		ParentID:    nil,
		ChannelID:   stringPtr("34020000001320000001"),
		DeviceID:    stringPtr("34020000001310000001"),
		CivilCode:   "370100",
		MountCount:  2,
		ChannelCount: 5,
	}

	assert.Equal(t, "test-001", node.ID)
	assert.Equal(t, "测试节点", node.Name)
	assert.Equal(t, "device", node.NodeType)
	assert.Nil(t, node.ParentID)
	assert.NotNil(t, node.ChannelID)
	assert.Equal(t, "34020000001320000001", *node.ChannelID)
}

// RED: 测试 Dimension 接口定义存在
func TestDimensionInterface(t *testing.T) {
	// 编译时检查:任何实现 Dimension 的类型都应该有 GetRoots / GetChildren 方法
	var _ Dimension = (*mockDimension)(nil)
}

// mock 实现用于测试接口定义
type mockDimension struct{}

func (m *mockDimension) GetRoots(withCounts bool) ([]Node, error) {
	return []Node{
		{ID: "root-1", Name: "根节点1", NodeType: "civil_code"},
		{ID: "root-2", Name: "根节点2", NodeType: "civil_code"},
	}, nil
}

func (m *mockDimension) GetChildren(parentID string, withCounts bool) ([]Node, error) {
	return []Node{
		{ID: "child-1", Name: "子节点1", NodeType: "channel", ParentID: &parentID},
	}, nil
}

func stringPtr(s string) *string {
	return &s
}
