package directory

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)

	err = db.AutoMigrate(&models.GbCatalogNode{})
	assert.NoError(t, err)

	return db
}

func TestNativeDimension_GetRoots(t *testing.T) {
	db := setupTestDB(t)
	dim := NewNativeDimension(db, 1)

	// 插入测试数据:1 个根节点(设备) + 1 个子节点(通道)
	rootNode := models.GbCatalogNode{
		OwnerDeptID: 1,
		NodeType:    models.NodeTypeDevice,
		ParentID:    nil,
		Path:        "/1/",
		Depth:       0,
		Name:        "Root Device",
		Code:        "34020000001320000001",
	}
	db.Create(&rootNode)

	childNode := models.GbCatalogNode{
		OwnerDeptID: 1,
		NodeType:    models.NodeTypeChannel,
		ParentID:    &rootNode.ID,
		Path:        "/1/2/",
		Depth:       1,
		Name:        "Channel 1",
		Code:        "34020000001320000001",
	}
	db.Create(&childNode)

	// 测试 GetRoots
	roots, err := dim.GetRoots(false)
	assert.NoError(t, err)
	assert.Len(t, roots, 1)
	assert.Equal(t, "Root Device", roots[0].Name)
	assert.Equal(t, NodeTypeDevice, roots[0].NodeType)
	assert.Nil(t, roots[0].ParentID)
	assert.True(t, roots[0].HasChildren)
	assert.False(t, roots[0].IsLeaf)
}

func TestNativeDimension_GetChildren(t *testing.T) {
	db := setupTestDB(t)
	dim := NewNativeDimension(db, 1)

	// 插入测试数据
	rootNode := models.GbCatalogNode{
		OwnerDeptID: 1,
		NodeType:    models.NodeTypeDevice,
		ParentID:    nil,
		Path:        "/1/",
		Depth:       0,
		Name:        "Root Device",
		Code:        "34020000001320000001",
	}
	db.Create(&rootNode)

	channelID := uint(100)
	childNode := models.GbCatalogNode{
		OwnerDeptID: 1,
		NodeType:    models.NodeTypeChannel,
		ParentID:    &rootNode.ID,
		Path:        "/1/2/",
		Depth:       1,
		Name:        "Channel 1",
		Code:        "34020000001320000001",
		ChannelID:   &channelID,
	}
	db.Create(&childNode)

	// 测试 GetChildren
	children, err := dim.GetChildren("1", false)
	assert.NoError(t, err)
	assert.Len(t, children, 1)
	assert.Equal(t, "Channel 1", children[0].Name)
	assert.Equal(t, NodeTypeChannel, children[0].NodeType)
	assert.Equal(t, "1", *children[0].ParentID)
	assert.Equal(t, "100", *children[0].ChannelID)
	assert.False(t, children[0].HasChildren)
	assert.True(t, children[0].IsLeaf)
}
