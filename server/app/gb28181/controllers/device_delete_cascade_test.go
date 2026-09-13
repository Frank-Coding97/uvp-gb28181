package controllers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	cascademodel "uvplatform.cn/uvp-gb28181/app/gb28181/cascade/model"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func newCascadeTestPlatform(name, localDeviceID string) *cascademodel.GbCascadePlatform {
	return &cascademodel.GbCascadePlatform{
		Name: name, UpstreamServerID: "34020000002000000001", UpstreamDomain: "3402000000",
		Host: "192.0.2.1", Port: 5060, LocalDeviceID: localDeviceID, LocalDomain: "3402000000",
		LocalSIPIP: "192.0.2.2", LocalSIPPort: 5060, EffectiveVersion: "2016", Transport: "UDP",
		ConfigRevision: 1, Enabled: true,
	}
}

// 设备删除必须把国标级联的共享投影一并回收。不回收线上会连着出两个问题:
//
//  1. 上级平台目录里留下一台永不下线的幽灵设备(投影还 active,源设备已经不存在);
//  2. 设备重新接入后 gb_device 换了自增 id,而 published_device_id 还是同一个国标号,
//     再共享时撞 uk_cascade_device_published 报 1062 Duplicate entry,接口只能回 409。
//
// 所以这里断言的是"物理删除",不是 active=false:两张投影表的唯一索引不含 deleted_at,
// 留一行停用的行仍然占着 (platform_id, published_*) 键位,等于没回收。
func TestDeviceMgmt_DeleteDeviceRetiresCascadeShareProjections(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	deviceID, channelID, _ := seedDevicesAndChannels(t, db)

	// 旁路设备:它的共享投影不能被动到。
	other := &gbmodels.GbDevice{DeviceID: "34020000002000000009", Name: "旁路设备", SubscribeCapability: gbmodels.SubscribeUnknown}
	require.NoError(t, db.Create(other).Error)

	platform := newCascadeTestPlatform("上级平台", "34020000002000000001")
	require.NoError(t, db.Create(platform).Error)

	shared := &cascademodel.GbCascadeDeviceProjection{
		PlatformID: platform.ID, SourceDeviceID: uint64(deviceID),
		PublishedDeviceID: "34020000002000000001", Name: "测试 NVR", Active: true, Revision: 1,
	}
	require.NoError(t, db.Create(shared).Error)
	require.NoError(t, db.Create(&cascademodel.GbCascadeChannelProjection{
		PlatformID: platform.ID, DeviceProjectionID: shared.ID, SourceChannelID: uint64(channelID),
		PublishedChannelID: "37011200001310000001", Active: true, Revision: 1,
	}).Error)
	require.NoError(t, db.Create(&cascademodel.GbCascadeDeviceProjection{
		PlatformID: platform.ID, SourceDeviceID: uint64(other.ID),
		PublishedDeviceID: "34020000002000000009", Active: true, Revision: 1,
	}).Error)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", "/api/gb28181/device-mgmt/device/"+uintStr(deviceID), nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var deviceProjections, channelProjections int64
	require.NoError(t, db.Unscoped().Model(&cascademodel.GbCascadeDeviceProjection{}).
		Where("source_device_id = ?", uint64(deviceID)).Count(&deviceProjections).Error)
	require.Zero(t, deviceProjections, "被删设备的共享投影必须物理删除")
	require.NoError(t, db.Unscoped().Model(&cascademodel.GbCascadeChannelProjection{}).
		Where("source_channel_id = ?", uint64(channelID)).Count(&channelProjections).Error)
	require.Zero(t, channelProjections, "被删设备名下通道的共享投影也要一起回收")

	var surviving int64
	require.NoError(t, db.Unscoped().Model(&cascademodel.GbCascadeDeviceProjection{}).
		Where("source_device_id = ?", uint64(other.ID)).Count(&surviving).Error)
	require.EqualValues(t, 1, surviving, "别的设备的共享投影不能被误删")

	// 修订号推进:还开着共享弹窗的人提交旧快照时要拿到冲突(409),
	// 而不是把已经删掉的设备重新写回成一条幽灵投影。
	var reloaded cascademodel.GbCascadePlatform
	require.NoError(t, db.First(&reloaded, platform.ID).Error)
	require.EqualValues(t, platform.ProjectionRevision+1, reloaded.ProjectionRevision)
}

// 单独删一路通道时,只回收这一路的共享投影,设备投影保留。
func TestDeviceMgmt_DeleteChannelRetiresItsCascadeShareProjection(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	deviceID, channelID, otherChannelID := seedDevicesAndChannels(t, db)

	platform := newCascadeTestPlatform("上级平台", "34020000002000000001")
	require.NoError(t, db.Create(platform).Error)
	shared := &cascademodel.GbCascadeDeviceProjection{
		PlatformID: platform.ID, SourceDeviceID: uint64(deviceID),
		PublishedDeviceID: "34020000002000000001", Active: true, Revision: 1,
	}
	require.NoError(t, db.Create(shared).Error)
	published := map[uint]string{channelID: "37011200001310000001", otherChannelID: "37011200001310000002"}
	for _, id := range []uint{channelID, otherChannelID} {
		require.NoError(t, db.Create(&cascademodel.GbCascadeChannelProjection{
			PlatformID: platform.ID, DeviceProjectionID: shared.ID, SourceChannelID: uint64(id),
			PublishedChannelID: published[id], Active: true, Revision: 1,
		}).Error)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", "/api/gb28181/device-mgmt/channel/"+uintStr(channelID), nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var retired, remaining int64
	require.NoError(t, db.Unscoped().Model(&cascademodel.GbCascadeChannelProjection{}).
		Where("source_channel_id = ?", uint64(channelID)).Count(&retired).Error)
	require.Zero(t, retired)
	require.NoError(t, db.Unscoped().Model(&cascademodel.GbCascadeChannelProjection{}).
		Where("source_channel_id = ?", uint64(otherChannelID)).Count(&remaining).Error)
	require.EqualValues(t, 1, remaining, "同设备下其它通道的共享投影保留")

	var deviceProjections int64
	require.NoError(t, db.Unscoped().Model(&cascademodel.GbCascadeDeviceProjection{}).
		Where("source_device_id = ?", uint64(deviceID)).Count(&deviceProjections).Error)
	require.EqualValues(t, 1, deviceProjections, "删通道不该动设备投影")
}
