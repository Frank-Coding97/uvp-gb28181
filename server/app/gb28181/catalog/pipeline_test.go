package catalog_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/catalog"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func newPipelineTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbCatalogNode{},
		&gbmodels.GbChannelMount{},
		&gbmodels.GbAnomalyRecord{},
		&gbmodels.GbChannel{},
		&gbmodels.GbDevice{},
		&gbmodels.GbAlarmResource{},
		&gbmodels.GbAlarmResourceParent{},
		&gbmodels.GbAlarmBinding{},
	))
	return db
}

func TestPipeline_IngestRequiresOwnerDept(t *testing.T) {
	p := catalog.New(newPipelineTestDB(t))

	err := p.Ingest(context.Background(), catalog.Sender{}, []catalog.CatalogItem{
		{DeviceID: "37011200001310000001", CivilCode: "370112"},
	})

	require.Error(t, err)
	assert.True(t, errors.Is(err, catalog.ErrOwnerDeptRequired))
}

func TestPipeline_IngestWithOwnerDept(t *testing.T) {
	db := newPipelineTestDB(t)
	p := catalog.New(db)

	require.NoError(t, p.Ingest(context.Background(), catalog.Sender{OwnerDeptID: 10}, []catalog.CatalogItem{
		{DeviceID: "37011200001310000001", Name: "通道", CivilCode: "370112", StatusOn: true},
	}))

	var channel gbmodels.GbChannel
	require.NoError(t, db.Where("channel_id = ?", "37011200001310000001").First(&channel).Error)
	assert.EqualValues(t, 10, channel.OwnerDeptID)
	assert.True(t, channel.AudioEnabled, "新建通道默认应开启音频")
}

func TestPipeline_IngestPersistsAlarmResourceAndMultipleParents(t *testing.T) {
	db := newPipelineTestDB(t)
	p := catalog.New(db)
	deviceCode := "34020000001180000001"
	channelCode := "34020000001310000001"
	alarmCode := "34020000001340000001"
	device := &gbmodels.GbDevice{DeviceID: deviceCode, OwnerDeptID: 10}
	require.NoError(t, db.Create(device).Error)

	require.NoError(t, p.Ingest(context.Background(), catalog.Sender{OwnerDeptID: 10, SourceDeviceID: deviceCode}, []catalog.CatalogItem{
		{DeviceID: channelCode, Name: "录像通道", ParentID: deviceCode, StatusOn: true},
		{DeviceID: alarmCode, Name: "门磁报警", ParentID: channelCode + "/" + deviceCode, StatusOn: true},
	}))

	var resource gbmodels.GbAlarmResource
	require.NoError(t, db.Where("device_id = ? AND alarm_code = ?", device.ID, alarmCode).First(&resource).Error)
	assert.Equal(t, gbmodels.AlarmResourceInput, resource.ResourceType)
	assert.Equal(t, channelCode+"/"+deviceCode, resource.RawParentIDs)

	var parents []gbmodels.GbAlarmResourceParent
	require.NoError(t, db.Where("alarm_resource_id = ?", resource.ID).Order("id").Find(&parents).Error)
	require.Len(t, parents, 2)
	assert.Equal(t, channelCode, parents[0].ParentCode)
	assert.Equal(t, deviceCode, parents[1].ParentCode)

	var alarmChannelCount int64
	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Where("channel_id = ?", alarmCode).Count(&alarmChannelCount).Error)
	assert.Zero(t, alarmChannelCount, "报警输入不能伪装成可播放通道")

	var node gbmodels.GbCatalogNode
	require.NoError(t, db.Where("code = ?", alarmCode).First(&node).Error)
	assert.Equal(t, gbmodels.NodeTypeAlarmInput, node.NodeType)
	require.NotNil(t, node.AlarmResourceID)
	assert.Equal(t, resource.ID, *node.AlarmResourceID)
	assert.False(t, node.Anomaly)
}

func TestPipeline_IngestHVRAsDeviceNotChannel(t *testing.T) {
	db := newPipelineTestDB(t)
	p := catalog.New(db)
	hvrCode := "34020000001300000001"

	require.NoError(t, p.Ingest(context.Background(), catalog.Sender{OwnerDeptID: 10}, []catalog.CatalogItem{{DeviceID: hvrCode, Name: "HVR"}}))

	var deviceCount, channelCount int64
	require.NoError(t, db.Model(&gbmodels.GbDevice{}).Where("device_id = ?", hvrCode).Count(&deviceCount).Error)
	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Where("channel_id = ?", hvrCode).Count(&channelCount).Error)
	assert.EqualValues(t, 1, deviceCount)
	assert.Zero(t, channelCount)
}

func TestPipeline_DelIsScopedByOwnerDept(t *testing.T) {
	db := newPipelineTestDB(t)
	p := catalog.New(db)

	for _, deptID := range []uint{10, 20} {
		parentID := deptID
		channel := &gbmodels.GbChannel{
			DeviceID:    "340200000020000000" + uintStr(deptID),
			ChannelID:   "370112000013100000" + uintStr(deptID),
			OwnerDeptID: deptID,
		}
		require.NoError(t, db.Create(channel).Error)
		require.NoError(t, db.Create(&gbmodels.GbChannelMount{
			OwnerDeptID:  deptID,
			ChannelID:    channel.ID,
			ParentNodeID: parentID,
		}).Error)
		require.NoError(t, db.Create(&gbmodels.GbCatalogNode{
			OwnerDeptID: deptID,
			NodeType:    gbmodels.NodeTypeVirtualOrg,
			Path:        "/",
			Name:        "同编码节点",
			Code:        "UVP-PRIVATE-X0001",
			ParentID:    &parentID,
			ChannelID:   &channel.ID,
		}).Error)
	}

	require.NoError(t, p.IngestDelta(
		context.Background(),
		catalog.Sender{OwnerDeptID: 10},
		"DEL",
		catalog.CatalogItem{DeviceID: "UVP-PRIVATE-X0001"},
	))

	var dept10Count, dept20Count int64
	require.NoError(t, db.Model(&gbmodels.GbCatalogNode{}).Where("owner_dept_id = ?", 10).Count(&dept10Count).Error)
	require.NoError(t, db.Model(&gbmodels.GbCatalogNode{}).Where("owner_dept_id = ?", 20).Count(&dept20Count).Error)
	assert.EqualValues(t, 0, dept10Count)
	assert.EqualValues(t, 1, dept20Count)

	var channelCount, mountCount int64
	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Where("owner_dept_id = ?", 10).Count(&channelCount).Error)
	require.NoError(t, db.Model(&gbmodels.GbChannelMount{}).Where("owner_dept_id = ?", 10).Count(&mountCount).Error)
	assert.EqualValues(t, 0, channelCount)
	assert.EqualValues(t, 0, mountCount)
}

func TestPipeline_DelKeepsMultiMountedChannel(t *testing.T) {
	db := newPipelineTestDB(t)
	p := catalog.New(db)
	parentA := uint(100)
	parentB := uint(200)
	channel := &gbmodels.GbChannel{
		DeviceID:    "34020000002000000010",
		ChannelID:   "37011200001310000010",
		OwnerDeptID: 10,
	}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannelMount{
		OwnerDeptID:  10,
		ChannelID:    channel.ID,
		ParentNodeID: parentA,
		IsPrimary:    true,
	}).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannelMount{
		OwnerDeptID:  10,
		ChannelID:    channel.ID,
		ParentNodeID: parentB,
	}).Error)
	require.NoError(t, db.Create(&gbmodels.GbCatalogNode{
		OwnerDeptID: 10,
		NodeType:    gbmodels.NodeTypeChannel,
		Path:        "/100/1/",
		Name:        "主挂载节点",
		Code:        "37011200001310000010",
		ParentID:    &parentA,
		ChannelID:   &channel.ID,
	}).Error)

	require.NoError(t, p.IngestDelta(
		context.Background(),
		catalog.Sender{OwnerDeptID: 10},
		"DEL",
		catalog.CatalogItem{DeviceID: "37011200001310000010"},
	))

	var channelCount, mountCount int64
	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Where("id = ?", channel.ID).Count(&channelCount).Error)
	require.NoError(t, db.Model(&gbmodels.GbChannelMount{}).Where("channel_id = ?", channel.ID).Count(&mountCount).Error)
	assert.EqualValues(t, 1, channelCount)
	assert.EqualValues(t, 1, mountCount)

	var remaining gbmodels.GbChannelMount
	require.NoError(t, db.Where("channel_id = ?", channel.ID).First(&remaining).Error)
	assert.EqualValues(t, parentB, remaining.ParentNodeID)
}

func uintStr(value uint) string {
	return fmt.Sprintf("%d", value)
}

func TestPipeline_PathHelpers(t *testing.T) {
	assert.Equal(t, "/12/47/189/", catalog.BuildPath("/12/47/", 189))
	assert.Equal(t, uint8(0), catalog.DepthFromPath("/"))
	assert.Equal(t, uint8(3), catalog.DepthFromPath("/12/47/189/"))
}

func TestPipeline_IngestFiveLevelCatalog(t *testing.T) {
	db := newPipelineTestDB(t)
	p := catalog.New(db)

	items := []catalog.CatalogItem{
		{DeviceID: "37011200001310000001", Name: "通道 1", CivilCode: "370112", StatusOn: true},
		{DeviceID: "37011200001310000002", Name: "通道 2", CivilCode: "370112", StatusOn: true},
		{DeviceID: "37011200001310000003", Name: "通道 3", CivilCode: "370112", StatusOn: false},
		{DeviceID: "37011200001310000004", Name: "通道 4", CivilCode: "370112"},
		{DeviceID: "37011200001310000005", Name: "通道 5", CivilCode: "370112"},
		{DeviceID: "37011200001310000006", Name: "通道 6", CivilCode: "370112"},
	}
	require.NoError(t, p.Ingest(context.Background(), catalog.Sender{OwnerDeptID: 1}, items))

	var totalNodes int64
	require.NoError(t, db.Model(&gbmodels.GbCatalogNode{}).Count(&totalNodes).Error)
	assert.GreaterOrEqual(t, totalNodes, int64(6+3))

	var channelNodes []gbmodels.GbCatalogNode
	require.NoError(t, db.Where("node_type = ?", gbmodels.NodeTypeChannel).Find(&channelNodes).Error)
	assert.Len(t, channelNodes, 6)
	for _, node := range channelNodes {
		assert.NotEmpty(t, node.Path)
		assert.Contains(t, node.Path, "/")
	}

	var primaryCount int64
	require.NoError(t, db.Model(&gbmodels.GbChannelMount{}).Where("is_primary = ?", true).Count(&primaryCount).Error)
	assert.EqualValues(t, 6, primaryCount)

	var channelCount int64
	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Count(&channelCount).Error)
	assert.EqualValues(t, 6, channelCount)
}

func TestPipeline_IngestIsIdempotent(t *testing.T) {
	db := newPipelineTestDB(t)
	p := catalog.New(db)
	sender := catalog.Sender{OwnerDeptID: 1, SourceDeviceID: "34020000002000000001"}
	item := catalog.CatalogItem{
		DeviceID:  "37011200001310000001",
		Name:      "通道",
		CivilCode: "370112",
		StatusOn:  true,
	}

	require.NoError(t, p.Ingest(context.Background(), sender, []catalog.CatalogItem{item}))
	var nodesBefore, mountsBefore int64
	require.NoError(t, db.Model(&gbmodels.GbCatalogNode{}).Count(&nodesBefore).Error)
	require.NoError(t, db.Model(&gbmodels.GbChannelMount{}).Count(&mountsBefore).Error)

	require.NoError(t, p.Ingest(context.Background(), sender, []catalog.CatalogItem{item}))
	var nodesAfter, mountsAfter int64
	require.NoError(t, db.Model(&gbmodels.GbCatalogNode{}).Count(&nodesAfter).Error)
	require.NoError(t, db.Model(&gbmodels.GbChannelMount{}).Count(&mountsAfter).Error)
	assert.Equal(t, nodesBefore, nodesAfter)
	assert.Equal(t, mountsBefore, mountsAfter)
}

func TestPipeline_IngestAnomalyFallback(t *testing.T) {
	db := newPipelineTestDB(t)
	p := catalog.New(db)

	items := []catalog.CatalogItem{
		{DeviceID: "UVP-PRIVATE-X0001", Name: "私有通道", StatusOn: true},
		{DeviceID: "12345", Name: "短码节点"},
	}
	require.NoError(t, p.Ingest(context.Background(), catalog.Sender{OwnerDeptID: 1}, items))

	var anomalyNodes, records int64
	require.NoError(t, db.Model(&gbmodels.GbCatalogNode{}).Where("anomaly = ?", true).Count(&anomalyNodes).Error)
	require.NoError(t, db.Model(&gbmodels.GbAnomalyRecord{}).Count(&records).Error)
	assert.EqualValues(t, 2, anomalyNodes)
	assert.EqualValues(t, 2, records)

	var sample gbmodels.GbCatalogNode
	require.NoError(t, db.Where("anomaly = ?", true).First(&sample).Error)
	assert.NotEmpty(t, sample.RawCode)
	assert.NotEmpty(t, sample.AnomalyReason)
}

func TestPipeline_IngestDeltaAddUpdateDel(t *testing.T) {
	db := newPipelineTestDB(t)
	p := catalog.New(db)
	ctx := context.Background()
	sender := catalog.Sender{OwnerDeptID: 1, SourceDeviceID: "34020000002000000001"}
	item := catalog.CatalogItem{
		DeviceID:  "37011200001310000001",
		Name:      "原名",
		CivilCode: "370112",
		StatusOn:  true,
	}

	require.NoError(t, p.IngestDelta(ctx, sender, "ADD", item))
	var node gbmodels.GbCatalogNode
	require.NoError(t, db.Where("code = ?", item.DeviceID).First(&node).Error)
	assert.Equal(t, "原名", node.Name)

	item.Name = "新名"
	require.NoError(t, p.IngestDelta(ctx, sender, "UPDATE", item))
	var channel gbmodels.GbChannel
	require.NoError(t, db.Where("channel_id = ?", item.DeviceID).First(&channel).Error)
	assert.Equal(t, "新名", channel.Name)

	require.NoError(t, p.IngestDelta(ctx, sender, "DEL", item))
	var leftover int64
	require.NoError(t, db.Model(&gbmodels.GbCatalogNode{}).Where("code = ?", item.DeviceID).Count(&leftover).Error)
	assert.EqualValues(t, 0, leftover)
}

func TestPipeline_IngestDeltaUpdatesChannelStatus(t *testing.T) {
	db := newPipelineTestDB(t)
	p := catalog.New(db)
	ctx := context.Background()
	sender := catalog.Sender{OwnerDeptID: 1, SourceDeviceID: "34020000002000000001"}
	item := catalog.CatalogItem{DeviceID: "37011200001310000001", Name: "入口", CivilCode: "370112", StatusOn: true}
	require.NoError(t, p.Ingest(ctx, sender, []catalog.CatalogItem{item}))

	for _, action := range []string{"OFF", "VLOST", "DEFECT"} {
		require.NoError(t, p.IngestDelta(ctx, sender, action, catalog.CatalogItem{DeviceID: item.DeviceID}))
		var channel gbmodels.GbChannel
		require.NoError(t, db.Where("channel_id = ?", item.DeviceID).First(&channel).Error)
		assert.Equal(t, gbmodels.ChannelStatusOffline, channel.Status, action)

		require.NoError(t, p.IngestDelta(ctx, sender, "ON", catalog.CatalogItem{DeviceID: item.DeviceID}))
		require.NoError(t, db.Where("channel_id = ?", item.DeviceID).First(&channel).Error)
		assert.Equal(t, gbmodels.ChannelStatusOnline, channel.Status, action)
	}
}

func TestPipeline_IngestRejectsUnknownDeltaAction(t *testing.T) {
	p := catalog.New(newPipelineTestDB(t))
	err := p.IngestDelta(context.Background(), catalog.Sender{OwnerDeptID: 1}, "BROKEN", catalog.CatalogItem{DeviceID: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown delta action")
}

func TestPipeline_IngestDepthChain(t *testing.T) {
	db := newPipelineTestDB(t)
	p := catalog.New(db)
	require.NoError(t, p.Ingest(context.Background(), catalog.Sender{OwnerDeptID: 1}, []catalog.CatalogItem{
		{DeviceID: "37011200001310000001", Name: "通道", CivilCode: "370112", StatusOn: true},
	}))

	type row struct {
		Code  string
		Depth uint8
	}
	var rows []row
	require.NoError(t, db.Model(&gbmodels.GbCatalogNode{}).
		Select("code, depth").
		Where("node_type = ? OR node_type = ?", gbmodels.NodeTypeCivilCode, gbmodels.NodeTypeChannel).
		Order("depth, code").
		Find(&rows).Error)

	want := map[string]uint8{
		"37":                   0,
		"3701":                 1,
		"370112":               2,
		"37011200001310000001": 3,
	}
	got := map[string]uint8{}
	for _, row := range rows {
		got[row.Code] = row.Depth
	}
	for code, expected := range want {
		assert.Equal(t, expected, got[code], "节点 %s depth 不匹配", code)
	}
}

func TestDepthFromPathHandlesEmptyPath(t *testing.T) {
	assert.Equal(t, uint8(0), catalog.DepthFromPath(""))
	assert.Equal(t, uint8(1), catalog.DepthFromPath("/12/"))
}
