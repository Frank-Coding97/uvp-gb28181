package catalog_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/gb28181/catalog"
	gbconfig "uvplatform.com/uvp-gb28181/app/gb28181/config"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

type pipelineTestYAML struct {
	values map[string]interface{}
}

func (c *pipelineTestYAML) ConfigFileChangeListen(...func()) {}
func (c *pipelineTestYAML) Get(key string) interface{}       { return c.values[key] }
func (c *pipelineTestYAML) GetString(key string) string {
	value, _ := c.values[key].(string)
	return value
}
func (c *pipelineTestYAML) GetBool(key string) bool {
	value, _ := c.values[key].(bool)
	return value
}
func (c *pipelineTestYAML) GetInt(string) int                 { return 0 }
func (c *pipelineTestYAML) GetInt32(string) int32             { return 0 }
func (c *pipelineTestYAML) GetInt64(string) int64             { return 0 }
func (c *pipelineTestYAML) GetFloat64(string) float64         { return 0 }
func (c *pipelineTestYAML) GetDuration(string) time.Duration  { return 0 }
func (c *pipelineTestYAML) GetStringSlice(string) []string    { return nil }
func (c *pipelineTestYAML) GetUintSlice(string) []uint        { return nil }
func (c *pipelineTestYAML) Set(key string, value interface{}) { c.values[key] = value }
func (c *pipelineTestYAML) SaveConfig() error                 { return nil }

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

func TestPipeline_DeviceCatalogNodeUsesSenderOwnerWithoutMutatingOtherOwnersDevice(t *testing.T) {
	db := newPipelineTestDB(t)
	p := catalog.New(db)
	deviceID := "37011200001170000021"
	sender := catalog.Sender{OwnerDeptID: 10, SourceDeviceID: "37011200001170000001"}
	existing := &gbmodels.GbDevice{
		DeviceID: deviceID, Name: "其他归属设备", Manufacturer: "原厂商", Model: "原型号", OwnerDeptID: 20,
	}
	require.NoError(t, db.Create(existing).Error)

	require.NoError(t, p.Ingest(context.Background(), sender, []catalog.CatalogItem{{
		DeviceID: deviceID, Name: "新上报名称", Manufacturer: "新厂商", Model: "新型号", CivilCode: "370112",
	}}))

	var device gbmodels.GbDevice
	require.NoError(t, db.Where("device_id = ?", deviceID).First(&device).Error)
	assert.EqualValues(t, 20, device.OwnerDeptID)
	assert.Equal(t, "其他归属设备", device.Name)
	assert.Equal(t, "原厂商", device.Manufacturer)
	assert.Equal(t, "原型号", device.Model)

	var node gbmodels.GbCatalogNode
	require.NoError(t, db.Where("owner_dept_id = ? AND source_device_id = ? AND node_type = ? AND code = ?",
		sender.OwnerDeptID, sender.SourceDeviceID, gbmodels.NodeTypeDevice, deviceID).First(&node).Error)
	assert.EqualValues(t, 10, node.OwnerDeptID)
}

func TestPipeline_IngestKeepsDeviceParentAndBusinessGroupRelation(t *testing.T) {
	db := newPipelineTestDB(t)
	p := catalog.New(db)
	deviceID := "37011200001170000001"
	groupID := "37011200002150000001"
	channelID := "37011200001320000001"
	sender := catalog.Sender{OwnerDeptID: 10, SourceDeviceID: deviceID}
	require.NoError(t, p.Ingest(context.Background(), sender, []catalog.CatalogItem{
		{DeviceID: deviceID, Name: "NVR", CivilCode: "370112", StatusOn: true},
		{DeviceID: groupID, Name: "重点场所", CivilCode: "370112", ParentID: deviceID},
		{DeviceID: channelID, Name: "校门", CivilCode: "370112", ParentID: deviceID, BusinessGroupID: groupID, StatusOn: true},
	}))
	var channelNode gbmodels.GbCatalogNode
	require.NoError(t, db.Where("code = ?", channelID).First(&channelNode).Error)
	var groupNode gbmodels.GbCatalogNode
	require.NoError(t, db.Where("code = ?", groupID).First(&groupNode).Error)
	var deviceNode gbmodels.GbCatalogNode
	require.NoError(t, db.Where("code = ?", deviceID).First(&deviceNode).Error)
	assert.Equal(t, deviceNode.ID, *groupNode.ParentID)
	assert.Equal(t, groupNode.ID, *channelNode.ParentID)
}

func TestPipeline_IngestPrefersVirtualParentFromParentIDForBusinessChannel(t *testing.T) {
	db := newPipelineTestDB(t)
	p := catalog.New(db)
	deviceID := "37011200001170000001"
	groupID := "37011200002150000001"
	virtualID := "37011200002160000001"
	nestedVirtualID := "37011200002160000002"
	channelID := "37011200001320000001"
	sender := catalog.Sender{OwnerDeptID: 10, SourceDeviceID: deviceID}
	require.NoError(t, p.Ingest(context.Background(), sender, []catalog.CatalogItem{
		{DeviceID: deviceID, Name: "下级平台", CivilCode: "370112", StatusOn: true},
		{DeviceID: groupID, Name: "重点场所", CivilCode: "370112", ParentID: deviceID},
		{DeviceID: virtualID, Name: "门岗", CivilCode: "370112", BusinessGroupID: groupID},
		{DeviceID: nestedVirtualID, Name: "东门", CivilCode: "370112", ParentID: virtualID, BusinessGroupID: groupID},
		{DeviceID: channelID, Name: "校门", CivilCode: "370112", ParentID: deviceID + "/" + nestedVirtualID, BusinessGroupID: groupID, StatusOn: true},
	}))
	var channelNode gbmodels.GbCatalogNode
	require.NoError(t, db.Where("code = ?", channelID).First(&channelNode).Error)
	var virtualNode gbmodels.GbCatalogNode
	require.NoError(t, db.Where("code = ?", virtualID).First(&virtualNode).Error)
	var nestedVirtualNode gbmodels.GbCatalogNode
	require.NoError(t, db.Where("code = ?", nestedVirtualID).First(&nestedVirtualNode).Error)
	var groupNode gbmodels.GbCatalogNode
	require.NoError(t, db.Where("code = ?", groupID).First(&groupNode).Error)
	assert.Equal(t, groupNode.ID, *virtualNode.ParentID, "顶层 216 通过 BusinessGroupID 挂到 215")
	assert.Equal(t, virtualNode.ID, *nestedVirtualNode.ParentID, "嵌套 216 通过 ParentID 保留目录层级")
	assert.Equal(t, nestedVirtualNode.ID, *channelNode.ParentID, "复合 ParentID 中的 216 优先于设备编码")
}

func TestPipeline_IngestUsesConfiguredTransportOnlyForNewChannels(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	testConfig := &pipelineTestYAML{values: map[string]interface{}{
		gbconfig.DefaultChannelStreamTransportConfigKey: "UDP",
	}}
	app.ConfigYml = testConfig
	db := newPipelineTestDB(t)
	p := catalog.New(db)
	sender := catalog.Sender{OwnerDeptID: 10, SourceDeviceID: "34020000001180000001"}

	require.NoError(t, p.Ingest(context.Background(), sender, []catalog.CatalogItem{
		{DeviceID: "37011200001310000001", Name: "入口", StatusOn: true},
	}))
	var first gbmodels.GbChannel
	require.NoError(t, db.Where("channel_id = ?", "37011200001310000001").First(&first).Error)
	require.Equal(t, "UDP", first.StreamTransport)

	testConfig.values[gbconfig.DefaultChannelStreamTransportConfigKey] = "TCP-Active"
	require.NoError(t, p.Ingest(context.Background(), sender, []catalog.CatalogItem{
		{DeviceID: "37011200001310000001", Name: "入口更新", StatusOn: true},
		{DeviceID: "37011200001310000002", Name: "出口", StatusOn: true},
	}))

	var channels []gbmodels.GbChannel
	require.NoError(t, db.Order("channel_id").Find(&channels).Error)
	require.Len(t, channels, 2)
	require.Equal(t, "UDP", channels[0].StreamTransport, "已有通道不能被新默认值覆盖")
	require.Equal(t, "TCP-Active", channels[1].StreamTransport)
}

func TestPipeline_IngestUsesConfiguredAudioOnlyForNewChannels(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	testConfig := &pipelineTestYAML{values: map[string]interface{}{
		gbconfig.DefaultChannelAudioEnabledConfigKey: false,
	}}
	app.ConfigYml = testConfig
	db := newPipelineTestDB(t)
	p := catalog.New(db)
	sender := catalog.Sender{OwnerDeptID: 10, SourceDeviceID: "34020000001180000001"}

	require.NoError(t, p.Ingest(context.Background(), sender, []catalog.CatalogItem{
		{DeviceID: "37011200001310000001", Name: "入口", StatusOn: true},
	}))
	var first gbmodels.GbChannel
	require.NoError(t, db.Where("channel_id = ?", "37011200001310000001").First(&first).Error)
	require.False(t, first.AudioEnabled)

	testConfig.values[gbconfig.DefaultChannelAudioEnabledConfigKey] = true
	require.NoError(t, p.Ingest(context.Background(), sender, []catalog.CatalogItem{
		{DeviceID: "37011200001310000001", Name: "入口更新", StatusOn: true},
		{DeviceID: "37011200001310000002", Name: "出口", StatusOn: true},
	}))

	var channels []gbmodels.GbChannel
	require.NoError(t, db.Order("channel_id").Find(&channels).Error)
	require.Len(t, channels, 2)
	require.False(t, channels[0].AudioEnabled, "已有通道不能被新默认值覆盖")
	require.True(t, channels[1].AudioEnabled)
}

func TestPipeline_IngestUsesPlaybackDefaultsForNewChannels(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })

	tests := []struct {
		name           string
		onDemandLive   bool
		cloudRecording bool
		wantState      string
	}{
		{name: "按需且不录像", onDemandLive: true, cloudRecording: false, wantState: gbmodels.CloudRecordingStateDisabled},
		{name: "按需且录像", onDemandLive: true, cloudRecording: true, wantState: gbmodels.CloudRecordingStateWaiting},
		{name: "常驻且不录像", onDemandLive: false, cloudRecording: false, wantState: gbmodels.CloudRecordingStateDisabled},
		{name: "常驻且录像", onDemandLive: false, cloudRecording: true, wantState: gbmodels.CloudRecordingStateWaiting},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app.ConfigYml = &pipelineTestYAML{values: map[string]interface{}{
				gbconfig.DefaultChannelOnDemandLiveConfigKey:   test.onDemandLive,
				gbconfig.DefaultChannelCloudRecordingConfigKey: test.cloudRecording,
			}}
			db := newPipelineTestDB(t)
			p := catalog.New(db)
			channelID := fmt.Sprintf("370112000013100000%02d", index+1)
			require.NoError(t, p.Ingest(context.Background(), catalog.Sender{
				OwnerDeptID: 10, SourceDeviceID: "34020000001180000001",
			}, []catalog.CatalogItem{{DeviceID: channelID, Name: test.name, StatusOn: true}}))

			var channel gbmodels.GbChannel
			require.NoError(t, db.Where("channel_id = ?", channelID).First(&channel).Error)
			require.Equal(t, test.onDemandLive, channel.OnDemandLive)
			require.Equal(t, test.cloudRecording, channel.CloudRecordingEnabled)
			require.Equal(t, test.wantState, channel.CloudRecordingState)
		})
	}
}

func TestPipeline_IngestDoesNotOverwriteExistingPlaybackPolicy(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	testConfig := &pipelineTestYAML{values: map[string]interface{}{
		gbconfig.DefaultChannelOnDemandLiveConfigKey:   false,
		gbconfig.DefaultChannelCloudRecordingConfigKey: true,
	}}
	app.ConfigYml = testConfig
	db := newPipelineTestDB(t)
	p := catalog.New(db)
	sender := catalog.Sender{OwnerDeptID: 10, SourceDeviceID: "34020000001180000001"}

	require.NoError(t, p.Ingest(context.Background(), sender, []catalog.CatalogItem{
		{DeviceID: "37011200001310000001", Name: "入口", StatusOn: true},
	}))
	var first gbmodels.GbChannel
	require.NoError(t, db.Where("channel_id = ?", "37011200001310000001").First(&first).Error)
	require.False(t, first.OnDemandLive)
	require.True(t, first.CloudRecordingEnabled)
	require.Equal(t, gbmodels.CloudRecordingStateWaiting, first.CloudRecordingState)

	testConfig.values[gbconfig.DefaultChannelOnDemandLiveConfigKey] = true
	testConfig.values[gbconfig.DefaultChannelCloudRecordingConfigKey] = false
	require.NoError(t, p.Ingest(context.Background(), sender, []catalog.CatalogItem{
		{DeviceID: "37011200001310000001", Name: "入口更新", StatusOn: true},
		{DeviceID: "37011200001310000002", Name: "出口", StatusOn: true},
	}))

	var channels []gbmodels.GbChannel
	require.NoError(t, db.Order("channel_id").Find(&channels).Error)
	require.Len(t, channels, 2)
	require.False(t, channels[0].OnDemandLive, "已有通道按需直播不能被全局默认值覆盖")
	require.True(t, channels[0].CloudRecordingEnabled, "已有通道云端录像不能被全局默认值覆盖")
	require.Equal(t, gbmodels.CloudRecordingStateWaiting, channels[0].CloudRecordingState)
	require.True(t, channels[1].OnDemandLive)
	require.False(t, channels[1].CloudRecordingEnabled)
	require.Equal(t, gbmodels.CloudRecordingStateDisabled, channels[1].CloudRecordingState)
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

func TestPipeline_ChannelMoveRemovesStaleCatalogMounts(t *testing.T) {
	db := newPipelineTestDB(t)
	p := catalog.New(db)
	sender := catalog.Sender{OwnerDeptID: 10, SourceDeviceID: "34020000001170000001"}
	groupA := "34020000002160000001"
	groupB := "34020000002160000002"
	channelID := "34020000001320000001"

	require.NoError(t, p.Ingest(context.Background(), sender, []catalog.CatalogItem{
		{DeviceID: groupA, Name: "目录 A", CivilCode: "370103"},
		{DeviceID: groupB, Name: "目录 B", CivilCode: "370103"},
		{DeviceID: channelID, Name: "通道", CivilCode: "370103", ParentID: groupA, StatusOn: true},
	}))
	require.NoError(t, p.IngestDelta(context.Background(), sender, "UPDATE", catalog.CatalogItem{
		DeviceID: channelID, Name: "通道", BusinessGroupID: groupB, StatusOn: true,
	}))

	var channel gbmodels.GbChannel
	require.NoError(t, db.Where("device_id = ? AND channel_id = ?", sender.SourceDeviceID, channelID).First(&channel).Error)
	var mounts []gbmodels.GbChannelMount
	require.NoError(t, db.Where("owner_dept_id = ? AND channel_id = ?", sender.OwnerDeptID, channel.ID).Find(&mounts).Error)
	require.Len(t, mounts, 1, "通道移动后同一来源只保留当前目录挂载")

	var parent gbmodels.GbCatalogNode
	require.NoError(t, db.Where("id = ?", mounts[0].ParentNodeID).First(&parent).Error)
	assert.Equal(t, groupB, parent.Code)
	var node gbmodels.GbCatalogNode
	require.NoError(t, db.Where("owner_dept_id = ? AND source_device_id = ? AND code = ?", sender.OwnerDeptID, sender.SourceDeviceID, channelID).First(&node).Error)
	assert.Equal(t, parent.ID, *node.ParentID)
}

func TestPipeline_SameChannelCodeFromDifferentSourcesIsolated(t *testing.T) {
	db := newPipelineTestDB(t)
	p := catalog.New(db)
	channelID := "34020000001320000001"
	senderA := catalog.Sender{OwnerDeptID: 10, SourceDeviceID: "34020000001170000001"}
	senderB := catalog.Sender{OwnerDeptID: 10, SourceDeviceID: "34020000001170000002"}
	groupA := "34020000002160000001"
	groupB := "34020000002160000002"

	require.NoError(t, p.Ingest(context.Background(), senderA, []catalog.CatalogItem{
		{DeviceID: groupA, Name: "来源 A 目录", CivilCode: "370103"},
		{DeviceID: channelID, Name: "A 通道", CivilCode: "370103", ParentID: groupA, StatusOn: true},
	}))
	require.NoError(t, p.Ingest(context.Background(), senderB, []catalog.CatalogItem{
		{DeviceID: groupB, Name: "来源 B 目录", CivilCode: "370103"},
		{DeviceID: channelID, Name: "B 通道", CivilCode: "370103", ParentID: groupB, StatusOn: true},
	}))

	require.NoError(t, p.IngestDelta(context.Background(), senderA, "UPDATE", catalog.CatalogItem{
		DeviceID: channelID, ParentID: groupA, StatusOn: false,
	}))

	var channelB gbmodels.GbChannel
	require.NoError(t, db.Where("device_id = ? AND channel_id = ?", senderB.SourceDeviceID, channelID).First(&channelB).Error)
	var nodeB gbmodels.GbCatalogNode
	require.NoError(t, db.Where("owner_dept_id = ? AND source_device_id = ? AND code = ?", senderB.OwnerDeptID, senderB.SourceDeviceID, channelID).First(&nodeB).Error)
	assert.Equal(t, gbmodels.ChannelStatusOnline, channelB.Status)
	assert.Equal(t, groupB, catalogNodeCode(t, db, *nodeB.ParentID))

	require.NoError(t, p.IngestDelta(context.Background(), senderA, "DEL", catalog.CatalogItem{DeviceID: channelID}))
	var remaining int64
	require.NoError(t, db.Model(&gbmodels.GbCatalogNode{}).
		Where("owner_dept_id = ? AND source_device_id = ? AND code = ?", senderB.OwnerDeptID, senderB.SourceDeviceID, channelID).
		Count(&remaining).Error)
	assert.EqualValues(t, 1, remaining)
}

func TestPipeline_LateBusinessParentsRepairPathWithoutDuplicates(t *testing.T) {
	db := newPipelineTestDB(t)
	p := catalog.New(db)
	sender := catalog.Sender{OwnerDeptID: 10, SourceDeviceID: "37010301022000000007"}
	deviceID := sender.SourceDeviceID
	groupID := "37010301022150000007"
	virtualID := "37010301022160000008"
	channelID := "34020000001320000010"

	require.NoError(t, p.Ingest(context.Background(), sender, []catalog.CatalogItem{
		{DeviceID: channelID, Name: "后置摄像头", CivilCode: "370103", ParentID: virtualID, StatusOn: true},
	}))
	require.NoError(t, p.Ingest(context.Background(), sender, []catalog.CatalogItem{
		{DeviceID: virtualID, Name: "东门", ParentID: groupID, BusinessGroupID: groupID},
	}))
	require.NoError(t, p.Ingest(context.Background(), sender, []catalog.CatalogItem{
		{DeviceID: groupID, Name: "校园安防", ParentID: deviceID, CivilCode: "370103"},
		{DeviceID: deviceID, Name: "下级平台", CivilCode: "370103"},
	}))

	for _, code := range []string{deviceID, groupID, virtualID, channelID} {
		var count int64
		require.NoError(t, db.Model(&gbmodels.GbCatalogNode{}).
			Where("owner_dept_id = ? AND source_device_id = ? AND code = ?", sender.OwnerDeptID, sender.SourceDeviceID, code).
			Count(&count).Error)
		assert.EqualValues(t, 1, count, "节点 %s 不应因乱序产生重复", code)
	}

	var deviceNode, groupNode, virtualNode, channelNode gbmodels.GbCatalogNode
	require.NoError(t, db.Where("owner_dept_id = ? AND source_device_id = ? AND code = ?", sender.OwnerDeptID, sender.SourceDeviceID, deviceID).First(&deviceNode).Error)
	require.NoError(t, db.Where("owner_dept_id = ? AND source_device_id = ? AND code = ?", sender.OwnerDeptID, sender.SourceDeviceID, groupID).First(&groupNode).Error)
	require.NoError(t, db.Where("owner_dept_id = ? AND source_device_id = ? AND code = ?", sender.OwnerDeptID, sender.SourceDeviceID, virtualID).First(&virtualNode).Error)
	require.NoError(t, db.Where("owner_dept_id = ? AND source_device_id = ? AND code = ?", sender.OwnerDeptID, sender.SourceDeviceID, channelID).First(&channelNode).Error)
	assert.Equal(t, deviceNode.ID, *groupNode.ParentID)
	assert.Equal(t, groupNode.ID, *virtualNode.ParentID)
	assert.Equal(t, virtualNode.ID, *channelNode.ParentID)
	assert.Equal(t, catalog.BuildPath(virtualNode.Path, channelNode.ID), channelNode.Path)
}

func TestPipeline_StatusOnlyUpdatePreservesDirectoryPlacement(t *testing.T) {
	db := newPipelineTestDB(t)
	p := catalog.New(db)
	sender := catalog.Sender{OwnerDeptID: 10, SourceDeviceID: "34020000001170000001"}
	groupID := "34020000002160000001"
	channelID := "34020000001320000001"
	require.NoError(t, p.Ingest(context.Background(), sender, []catalog.CatalogItem{
		{DeviceID: groupID, Name: "目录", CivilCode: "370103"},
		{DeviceID: channelID, Name: "通道", CivilCode: "370103", ParentID: groupID, StatusOn: true},
	}))
	var before gbmodels.GbCatalogNode
	require.NoError(t, db.Where("owner_dept_id = ? AND source_device_id = ? AND code = ?", sender.OwnerDeptID, sender.SourceDeviceID, channelID).First(&before).Error)
	require.NoError(t, p.IngestDelta(context.Background(), sender, "UPDATE", catalog.CatalogItem{DeviceID: channelID, StatusOn: false}))
	var after gbmodels.GbCatalogNode
	require.NoError(t, db.Where("id = ?", before.ID).First(&after).Error)
	assert.Equal(t, before.ParentID, after.ParentID)
	assert.Equal(t, before.Path, after.Path)
	var channel gbmodels.GbChannel
	require.NoError(t, db.Where("device_id = ? AND channel_id = ?", sender.SourceDeviceID, channelID).First(&channel).Error)
	assert.Equal(t, groupID, channel.ParentID)
	assert.Equal(t, gbmodels.ChannelStatusOffline, channel.Status)
}

func TestPipeline_LegacyDuplicateCatalogNodesAreCollapsedOnRefresh(t *testing.T) {
	db := newPipelineTestDB(t)
	p := catalog.New(db)
	sender := catalog.Sender{OwnerDeptID: 10, SourceDeviceID: "34020000001170000001"}
	channelID := "34020000001320000001"
	channel := &gbmodels.GbChannel{OwnerDeptID: 10, DeviceID: sender.SourceDeviceID, ChannelID: channelID, Name: "通道"}
	require.NoError(t, db.Create(channel).Error)
	parentA := &gbmodels.GbCatalogNode{OwnerDeptID: 10, NodeType: gbmodels.NodeTypeVirtualOrg, Code: "34020000002160000001", Name: "旧目录 A", Path: "/1/", Source: gbmodels.NodeSourceCatalog, SourceDeviceID: sender.SourceDeviceID}
	parentB := &gbmodels.GbCatalogNode{OwnerDeptID: 10, NodeType: gbmodels.NodeTypeVirtualOrg, Code: "34020000002160000002", Name: "旧目录 B", Path: "/2/", Source: gbmodels.NodeSourceCatalog, SourceDeviceID: sender.SourceDeviceID}
	require.NoError(t, db.Create(parentA).Error)
	require.NoError(t, db.Create(parentB).Error)
	channelNodeA := &gbmodels.GbCatalogNode{OwnerDeptID: 10, NodeType: gbmodels.NodeTypeChannel, Code: channelID, Name: "通道 A", Path: "/1/3/", ParentID: &parentA.ID, ChannelID: &channel.ID, Source: gbmodels.NodeSourceCatalog, SourceDeviceID: sender.SourceDeviceID}
	channelNodeB := &gbmodels.GbCatalogNode{OwnerDeptID: 10, NodeType: gbmodels.NodeTypeChannel, Code: channelID, Name: "通道 B", Path: "/2/4/", ParentID: &parentB.ID, ChannelID: &channel.ID, Source: gbmodels.NodeSourceCatalog, SourceDeviceID: sender.SourceDeviceID}
	require.NoError(t, db.Create(channelNodeA).Error)
	require.NoError(t, db.Create(channelNodeB).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannelMount{OwnerDeptID: 10, ChannelID: channel.ID, ParentNodeID: parentA.ID, MountSource: gbmodels.MountSourceCatalog, IsPrimary: true}).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannelMount{OwnerDeptID: 10, ChannelID: channel.ID, ParentNodeID: parentB.ID, MountSource: gbmodels.MountSourceCatalog}).Error)
	manualParent := &gbmodels.GbCatalogNode{OwnerDeptID: 10, NodeType: gbmodels.NodeTypeVirtualOrg, Code: "34020000002160000003", Name: "人工目录", Path: "/5/", Source: gbmodels.NodeSourceManual}
	require.NoError(t, db.Create(manualParent).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannelMount{OwnerDeptID: 10, ChannelID: channel.ID, ParentNodeID: manualParent.ID, MountSource: gbmodels.MountSourceManual}).Error)
	manualChannel := &gbmodels.GbCatalogNode{OwnerDeptID: 10, NodeType: gbmodels.NodeTypeChannel, Code: channelID, Name: "人工通道节点", Path: "/5/6/", ParentID: &manualParent.ID, ChannelID: &channel.ID, Source: gbmodels.NodeSourceManual, SourceDeviceID: sender.SourceDeviceID}
	require.NoError(t, db.Create(manualChannel).Error)
	otherSource := &gbmodels.GbCatalogNode{OwnerDeptID: 10, NodeType: gbmodels.NodeTypeChannel, Code: channelID, Name: "其他来源通道", Path: "/7/8/", Source: gbmodels.NodeSourceCatalog, SourceDeviceID: "34020000001170000002"}
	require.NoError(t, db.Create(otherSource).Error)

	require.NoError(t, p.IngestDelta(context.Background(), sender, "UPDATE", catalog.CatalogItem{DeviceID: channelID, ParentID: parentB.Code, Name: "最新通道", StatusOn: true}))

	var sourceNodes []gbmodels.GbCatalogNode
	require.NoError(t, db.Where("owner_dept_id = ? AND source_device_id = ? AND node_type = ? AND code = ?", 10, sender.SourceDeviceID, gbmodels.NodeTypeChannel, channelID).
		Where("(source = ? OR source = ?)", gbmodels.NodeSourceCatalog, "").Find(&sourceNodes).Error)
	require.Len(t, sourceNodes, 1, "重报时应收敛同来源历史重复目录项")
	assert.Equal(t, parentB.ID, *sourceNodes[0].ParentID)
	assert.Equal(t, "最新通道", sourceNodes[0].Name)
	var mounts []gbmodels.GbChannelMount
	require.NoError(t, db.Where("owner_dept_id = ? AND channel_id = ?", 10, channel.ID).Find(&mounts).Error)
	require.Len(t, mounts, 2, "保留当前 Catalog 挂载和人工挂载")
	var manualMountCount int64
	require.NoError(t, db.Model(&gbmodels.GbChannelMount{}).Where("owner_dept_id = ? AND channel_id = ? AND mount_source = ?", 10, channel.ID, gbmodels.MountSourceManual).Count(&manualMountCount).Error)
	assert.EqualValues(t, 1, manualMountCount)
	require.NoError(t, db.Where("id = ?", manualChannel.ID).First(&manualChannel).Error, "人工 Catalog 节点不能被收敛")
	var otherSourceNode gbmodels.GbCatalogNode
	require.NoError(t, db.Where("owner_dept_id = ? AND source_device_id = ? AND node_type = ? AND code = ?", 10, otherSource.SourceDeviceID, gbmodels.NodeTypeChannel, channelID).First(&otherSourceNode).Error, "其他来源节点不能被收敛")
}

func catalogNodeCode(t *testing.T, db *gorm.DB, id uint) string {
	t.Helper()
	var node gbmodels.GbCatalogNode
	require.NoError(t, db.Where("id = ?", id).First(&node).Error)
	return node.Code
}
