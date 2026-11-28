package catalog_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/gb28181/catalog"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// 场景来源：2026-10-26 级联验收（我方作上级 / WVP 作下级）。WVP 把共享通道的
// 自定义编号从 `34020000011320000099` 改成 `37010301021320000002` 后**只发 UPDATE**，
// 旧编号没有任何 DEL，于是我方 gb_channel 里两条并存，点旧编号必 404。
func TestPipeline_PruneChannelsRemovesStaleChannels(t *testing.T) {
	db := newPipelineTestDB(t)
	pipeline := catalog.New(db)
	const platform = "35020000002000000009"
	sender := catalog.Sender{OwnerDeptID: 10, SourceDeviceID: platform}
	keepCode := "37010301021320000002"
	staleCode := "34020000011320000099"

	require.NoError(t, pipeline.Ingest(context.Background(), sender, []catalog.CatalogItem{
		{DeviceID: keepCode, Name: "海康相机-一楼大厅", ParentID: platform, StatusOn: true},
		{DeviceID: staleCode, Name: "海康相机-一楼大厅(旧编号)", ParentID: platform, StatusOn: true},
	}))
	require.EqualValues(t, 2, countChannels(t, db, platform))

	pruned, err := pipeline.PruneChannels(context.Background(), sender, []string{keepCode})
	require.NoError(t, err)
	assert.Equal(t, 1, pruned)

	var remain []string
	require.NoError(t, db.Model(&gbmodels.GbChannel{}).
		Where("device_id = ?", platform).Order("channel_id").Pluck("channel_id", &remain).Error)
	assert.Equal(t, []string{keepCode}, remain, "对端仍提供的通道必须保留")
}

// 空快照不能当"全删"执行：对端只报设备层（或本次报 0 条）时无法区分
// 「通道被全部撤掉」与「这次没报通道层」，宁漏不误。
func TestPipeline_PruneChannelsSkipsEmptyKeepList(t *testing.T) {
	db := newPipelineTestDB(t)
	pipeline := catalog.New(db)
	const platform = "35020000002000000009"
	sender := catalog.Sender{OwnerDeptID: 10, SourceDeviceID: platform}

	require.NoError(t, pipeline.Ingest(context.Background(), sender, []catalog.CatalogItem{
		{DeviceID: "37010301021320000002", Name: "通道一", ParentID: platform, StatusOn: true},
	}))

	pruned, err := pipeline.PruneChannels(context.Background(), sender, nil)
	require.NoError(t, err)
	assert.Zero(t, pruned)
	assert.EqualValues(t, 1, countChannels(t, db, platform), "keepCodes 为空时必须一条都不删")
}

// 剪枝必须按来源隔离：同一台下级平台下可能有多个来源上报的同名编码，
// 也可能是别的设备的通道，不能顺着编码误删。
func TestPipeline_PruneChannelsScopesToSourceDevice(t *testing.T) {
	db := newPipelineTestDB(t)
	pipeline := catalog.New(db)
	const platformA = "35020000002000000009"
	const platformB = "35020000002000000010"
	senderA := catalog.Sender{OwnerDeptID: 10, SourceDeviceID: platformA}
	senderB := catalog.Sender{OwnerDeptID: 10, SourceDeviceID: platformB}

	require.NoError(t, pipeline.Ingest(context.Background(), senderA, []catalog.CatalogItem{
		{DeviceID: "37010301021320000002", Name: "A 的通道", ParentID: platformA, StatusOn: true},
	}))
	require.NoError(t, pipeline.Ingest(context.Background(), senderB, []catalog.CatalogItem{
		{DeviceID: "37010301021320000003", Name: "B 的通道", ParentID: platformB, StatusOn: true},
	}))

	// A 的快照里没有 A 的任何通道 ⇒ A 的被剪；B 的通道不受影响。
	pruned, err := pipeline.PruneChannels(context.Background(), senderA, []string{"37010301021320000099"})
	require.NoError(t, err)
	assert.Equal(t, 1, pruned)
	assert.EqualValues(t, 0, countChannels(t, db, platformA))
	assert.EqualValues(t, 1, countChannels(t, db, platformB), "剪 A 不能误伤 B")
}

func countChannels(t *testing.T, db *gorm.DB, platform string) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Where("device_id = ?", platform).Count(&count).Error)
	return count
}
