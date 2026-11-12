package devicequery

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	appmodels "uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

const (
	queryRootDept     = uint(10)
	queryChildDept    = uint(11)
	queryOtherDept    = uint(20)
	queryRootDevice   = "34020000002000000010"
	queryChildDevice  = "34020000002000000011"
	queryOtherDevice  = "34020000002000000020"
	queryRootChannel  = "37011200001310000010"
	queryChildChannel = "37011200001310000011"
	queryOtherChannel = "37011200001310000020"
)

func newDeviceQueryDB(t *testing.T) (*gorm.DB, resource.DepartmentScope, uint, uint, uint) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.AutoMigrate(
		&appmodels.SysDepartment{}, &gbmodels.GbDevice{}, &gbmodels.GbChannel{},
		&gbmodels.GbAlarmEvent{}, &gbmodels.GbDeviceStatusEvent{},
		&gbmodels.GbPTZState{}, &gbmodels.GbPTZHomePosition{},
		&gbmodels.GbPTZCruiseTrack{}, &gbmodels.GbDeviceTargetTrack{}, &gbmodels.GbPTZOperation{},
	))
	active := int8(1)
	parent := queryRootDept
	for _, department := range []appmodels.SysDepartment{
		{BaseModel: appmodels.BaseModel{ID: queryRootDept}, Name: "root", Status: &active},
		{BaseModel: appmodels.BaseModel{ID: queryChildDept}, ParentID: &parent, Name: "child", Status: &active},
		{BaseModel: appmodels.BaseModel{ID: queryOtherDept}, Name: "other", Status: &active},
	} {
		require.NoError(t, db.Create(&department).Error)
	}
	for _, device := range []gbmodels.GbDevice{
		{DeviceID: queryRootDevice, Name: "root-device", OwnerDeptID: queryRootDept},
		{DeviceID: queryChildDevice, Name: "child-device", OwnerDeptID: queryChildDept},
		{DeviceID: queryOtherDevice, Name: "other-device", OwnerDeptID: queryOtherDept},
	} {
		require.NoError(t, db.Create(&device).Error)
	}
	for _, channel := range []gbmodels.GbChannel{
		{DeviceID: queryRootDevice, ChannelID: queryRootChannel, Name: "root-channel", OwnerDeptID: queryRootDept, PTZType: 1},
		{DeviceID: queryChildDevice, ChannelID: queryChildChannel, Name: "child-channel", OwnerDeptID: queryChildDept, PTZType: 1},
		{DeviceID: queryOtherDevice, ChannelID: queryOtherChannel, Name: "other-channel", OwnerDeptID: queryOtherDept, PTZType: 1},
	} {
		require.NoError(t, db.Create(&channel).Error)
	}
	var deviceID, channelID, childDeviceID uint
	require.NoError(t, db.Model(&gbmodels.GbDevice{}).Select("id").Where("device_id = ?", queryRootDevice).Scan(&deviceID).Error)
	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Select("id").Where("channel_id = ?", queryRootChannel).Scan(&channelID).Error)
	require.NoError(t, db.Model(&gbmodels.GbDevice{}).Select("id").Where("device_id = ?", queryChildDevice).Scan(&childDeviceID).Error)
	return db, resource.DepartmentScope{OwnerDeptID: queryRootDept, DataScope: resource.DataScopeDepartment}, deviceID, channelID, childDeviceID
}

func TestDeviceAlarmsUseScopedDeviceAndSafePagedDTO(t *testing.T) {
	db, scope, rootDeviceID, rootChannelID, childDeviceID := newDeviceQueryDB(t)
	now := time.Now().UTC().Truncate(time.Second)
	channelID := rootChannelID
	for _, event := range []gbmodels.GbAlarmEvent{
		{DeviceID: rootDeviceID, ChannelID: &channelID, SourceCode: queryRootChannel, ReceivedAt: now.Add(time.Second), AlarmTime: &now, Description: "first", RawDigest: "secret-digest", RawSummary: "secret-raw", DedupeKey: "secret-key", CallID: "private-call"},
		{DeviceID: rootDeviceID, SourceCode: queryRootDevice, ReceivedAt: now, AlarmTime: &now, Description: "second", RawDigest: "secret-digest-2", RawSummary: "secret-raw-2", DedupeKey: "secret-key-2"},
		{DeviceID: childDeviceID, SourceCode: queryChildChannel, Description: "child-alarm"},
	} {
		require.NoError(t, db.Create(&event).Error)
	}

	service := New(db)
	page, err := service.ListDeviceAlarmsInScope(context.Background(), scope, queryRootDevice, resource.DeviceListOptions{Page: 1, PageSize: 1})
	require.NoError(t, err)
	assert.Equal(t, int64(2), page.Total)
	assert.Equal(t, 1, page.Page)
	assert.Equal(t, 1, page.PageSize)
	require.Len(t, page.Items, 1)
	assert.Equal(t, "first", page.Items[0].Description)
	assert.Equal(t, queryRootChannel, *page.Items[0].ChannelCode)

	encoded, err := json.Marshal(page)
	require.NoError(t, err)
	for _, hidden := range []string{"secret-digest", "secret-raw", "secret-key", "private-call", `"deviceId":`, `"channelId":`} {
		assert.NotContains(t, string(encoded), hidden)
	}

	childScope := resource.DepartmentScope{OwnerDeptID: queryRootDept, DataScope: resource.DataScopeDepartmentAndChildren}
	childPage, err := service.ListDeviceAlarmsInScope(context.Background(), childScope, queryChildDevice, resource.DeviceListOptions{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), childPage.Total)
	_, err = service.ListDeviceAlarmsInScope(context.Background(), scope, queryChildDevice, resource.DeviceListOptions{})
	assert.ErrorIs(t, err, resource.ErrResourceNotFound)
}

func TestDeviceStatusEventsUseResumableCursorAndOmitNetworkDetails(t *testing.T) {
	db, scope, rootDeviceID, _, childDeviceID := newDeviceQueryDB(t)
	base := time.Now().UTC().Truncate(time.Second)
	for i, event := range []gbmodels.GbDeviceStatusEvent{
		{DeviceID: rootDeviceID, DeviceCode: queryRootDevice, EventType: gbmodels.DeviceEventRegisterOnline, ToStatus: 1, Source: gbmodels.DeviceEventSourceRegister, OccurredAt: base, IP: "10.0.0.1", Port: 5060, Transport: "TCP", Detail: "private detail"},
		{DeviceID: rootDeviceID, DeviceCode: queryRootDevice, EventType: gbmodels.DeviceEventHeartbeatTimeout, FromStatus: int8ptr(1), ToStatus: 0, Source: gbmodels.DeviceEventSourceOfflineScanner, OccurredAt: base.Add(time.Second), IP: "10.0.0.2", Port: 5061, Transport: "UDP", Detail: "private detail 2"},
		{DeviceID: rootDeviceID, DeviceCode: queryRootDevice, EventType: gbmodels.DeviceEventHeartbeatRecovered, FromStatus: int8ptr(0), ToStatus: 1, Source: gbmodels.DeviceEventSourceKeepalive, OccurredAt: base.Add(2 * time.Second)},
		{DeviceID: childDeviceID, DeviceCode: queryChildDevice, EventType: gbmodels.DeviceEventRegisterOnline, ToStatus: 1, Source: gbmodels.DeviceEventSourceRegister, OccurredAt: base},
	} {
		event.ID = uint64(i + 1)
		require.NoError(t, db.Create(&event).Error)
	}

	service := New(db)
	first, err := service.ListDeviceStatusEventsInScope(context.Background(), scope, queryRootDevice, 2, "")
	require.NoError(t, err)
	require.Len(t, first.Items, 2)
	assert.Equal(t, gbmodels.DeviceEventRegisterOnline, first.Items[0].EventType)
	assert.True(t, first.HasMore)
	require.NotEmpty(t, first.NextCursor)

	second, err := service.ListDeviceStatusEventsInScope(context.Background(), scope, queryRootDevice, 2, first.NextCursor)
	require.NoError(t, err)
	require.Len(t, second.Items, 1)
	assert.False(t, second.HasMore)
	assert.Equal(t, gbmodels.DeviceEventHeartbeatRecovered, second.Items[0].EventType)

	encoded, err := json.Marshal(first)
	require.NoError(t, err)
	for _, hidden := range []string{`"ip"`, `"port"`, `"transport"`, `private detail`, `"deviceId":`} {
		assert.NotContains(t, string(encoded), hidden)
	}
	childScope := resource.DepartmentScope{OwnerDeptID: queryRootDept, DataScope: resource.DataScopeDepartmentAndChildren}
	_, err = service.ListDeviceStatusEventsInScope(context.Background(), childScope, queryChildDevice, 2, first.NextCursor)
	assert.ErrorIs(t, err, resource.ErrInvalidListOptions, "cursor must be bound to the device that created it")
	_, err = service.ListDeviceStatusEventsInScope(context.Background(), scope, queryOtherDevice, 2, "")
	assert.ErrorIs(t, err, resource.ErrResourceNotFound)
}

func TestChannelPTZSnapshotsAreScopedAndPreserveUnknownFreshness(t *testing.T) {
	db, scope, rootDeviceID, rootChannelID, _ := newDeviceQueryDB(t)
	observedAt := time.Now().UTC().Truncate(time.Second)
	pan, tilt, zoom := 12.5, -3.25, 4.0
	require.NoError(t, db.Create(&gbmodels.GbPTZState{
		DeviceID: rootDeviceID, DeviceCode: queryRootDevice, ChannelID: rootChannelID, ChannelCode: queryRootChannel,
		Pan: &pan, Tilt: &tilt, Zoom: &zoom, ReceivedAt: observedAt, SourceSN: 91, RawSummary: "private xml",
	}).Error)
	service := New(db)
	snapshot, err := service.GetChannelPreciseStatusInScope(context.Background(), scope, queryRootDevice, queryRootChannel)
	require.NoError(t, err)
	assert.Equal(t, gbmodels.PTZFreshnessFresh, snapshot.Freshness)
	require.NotNil(t, snapshot.State)
	assert.Equal(t, pan, *snapshot.State.Pan)
	assert.Equal(t, observedAt, *snapshot.ObservedAt)
	encoded, err := json.Marshal(snapshot)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "sourceSn")
	assert.NotContains(t, string(encoded), "rawSummary")
	assert.NotContains(t, string(encoded), "deviceId")

	_, err = service.GetChannelPreciseStatusInScope(context.Background(), scope, queryRootDevice, queryRootChannel+"1")
	assert.ErrorIs(t, err, resource.ErrResourceNotFound)
	_, err = service.GetChannelPreciseStatusInScope(context.Background(), scope, queryOtherDevice, queryRootChannel)
	assert.ErrorIs(t, err, resource.ErrResourceNotFound)
}

func TestHomePositionCruiseAndTargetTrackReturnCacheSnapshots(t *testing.T) {
	db, scope, rootDeviceID, rootChannelID, _ := newDeviceQueryDB(t)
	now := time.Now().UTC().Truncate(time.Second)
	resetTime, presetID := 30, 4
	require.NoError(t, db.Create(&gbmodels.GbPTZHomePosition{
		DeviceID: rootDeviceID, ChannelID: rootChannelID, ChannelCode: queryRootChannel, Enabled: true,
		ResetTime: &resetTime, PresetID: &presetID, ConfirmedAt: now, Source: gbmodels.PTZHomePositionSourceDeviceQuery,
		Verification: gbmodels.PTZHomePositionVerificationVerified, SourceOperationID: strptr("internal-operation"), RawSummary: "private xml",
	}).Error)
	enabled := true
	require.NoError(t, db.Create(&gbmodels.GbPTZCruiseTrack{
		DeviceID: rootDeviceID, ChannelID: rootChannelID, TrackID: 2, Name: "north", Enabled: &enabled,
		DetailJSON: `{"trackId":2,"name":"north","sumNum":1,"cruisePoints":[{"presetIndex":4,"stayTime":10,"speed":120}]}`,
		UpdatedAt:  now, LastOperationID: "private-operation", RawSummary: "private xml",
	}).Error)
	arealength, areawidth, midpointx, midpointy, lengthx, lengthy := 1920, 1080, 500, 400, 100, 80
	require.NoError(t, db.Create(&gbmodels.GbDeviceTargetTrack{
		DeviceID: rootDeviceID, ChannelID: rootChannelID, TargetCode: queryRootChannel, Mode: gbmodels.TargetTrackModeManual,
		AreaLength: &arealength, AreaWidth: &areawidth, AreaMidPointX: &midpointx, AreaMidPointY: &midpointy,
		AreaLengthX: &lengthx, AreaLengthY: &lengthy, CommandedAt: now, SourceOperationSeq: 9,
		SourceSN: 91, SourceOperationID: strptr("internal-operation"), RawSummary: "private xml",
	}).Error)

	service := New(db)
	home, err := service.GetChannelHomePositionInScope(context.Background(), scope, queryRootDevice, queryRootChannel)
	require.NoError(t, err)
	assert.Equal(t, gbmodels.PTZFreshnessFresh, home.Freshness)
	require.NotNil(t, home.HomePosition)
	assert.True(t, home.HomePosition.Enabled)
	encoded, err := json.Marshal(home)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "internal-operation")
	assert.NotContains(t, string(encoded), "rawSummary")

	tracks, err := service.ListChannelCruiseTracksInScope(context.Background(), scope, queryRootDevice, queryRootChannel)
	require.NoError(t, err)
	assert.Equal(t, gbmodels.PTZFreshnessFresh, tracks.Freshness)
	require.Len(t, tracks.Items, 1)
	require.NotNil(t, tracks.Items[0].Detail)
	assert.Equal(t, 4, tracks.Items[0].Detail.CruisePoints[0].PresetIndex)
	assert.Equal(t, "device_cache", tracks.Items[0].Source)
	assert.NotNil(t, tracks.Items[0].ObservedAt)
	encoded, err = json.Marshal(tracks)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "private-operation")
	assert.NotContains(t, string(encoded), "private xml")
	assert.NotContains(t, string(encoded), "deviceId")

	track, err := service.GetChannelTargetTrackInScope(context.Background(), scope, queryRootDevice, queryRootChannel)
	require.NoError(t, err)
	assert.False(t, track.DeviceAcknowledged)
	assert.False(t, track.ResponseRequired)
	require.NotNil(t, track.Intent)
	assert.Equal(t, gbmodels.TargetTrackModeManual, track.Intent.Mode)
	encoded, err = json.Marshal(track)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "internal-operation")
	assert.NotContains(t, string(encoded), "sourceOperationSeq")
	assert.NotContains(t, string(encoded), "rawSummary")
	_, err = service.GetChannelTargetTrackInScope(context.Background(), scope, queryOtherDevice, queryRootChannel)
	assert.ErrorIs(t, err, resource.ErrResourceNotFound)
}

func TestEmptyAndStalePTZCachesAreNotPresentedAsCurrent(t *testing.T) {
	db, scope, rootDeviceID, rootChannelID, _ := newDeviceQueryDB(t)
	service := New(db)
	service.now = func() time.Time { return time.Now().UTC() }
	precise, err := service.GetChannelPreciseStatusInScope(context.Background(), scope, queryRootDevice, queryRootChannel)
	require.NoError(t, err)
	assert.Equal(t, gbmodels.PTZFreshnessUnknown, precise.Freshness)
	assert.Nil(t, precise.State)

	staleAt := time.Now().Add(-2 * time.Minute)
	require.NoError(t, db.Create(&gbmodels.GbPTZHomePosition{
		DeviceID: rootDeviceID, ChannelID: rootChannelID, ChannelCode: queryRootChannel, Enabled: false,
		ConfirmedAt: staleAt, Source: gbmodels.PTZHomePositionSourceDeviceQuery,
		Verification: gbmodels.PTZHomePositionVerificationUnverified,
	}).Error)
	home, err := service.GetChannelHomePositionInScope(context.Background(), scope, queryRootDevice, queryRootChannel)
	require.NoError(t, err)
	assert.Equal(t, gbmodels.PTZFreshnessStale, home.Freshness)

	require.NoError(t, db.Create(&gbmodels.GbPTZCruiseTrack{
		DeviceID: rootDeviceID, ChannelID: rootChannelID, TrackID: 8, Name: "pending",
		DetailJSON: `{"trackId":8,"source":"reconcile-pending"}`, RawSummary: "reconcile-pending", UpdatedAt: time.Now(),
	}).Error)
	tracks, err := service.ListChannelCruiseTracksInScope(context.Background(), scope, queryRootDevice, queryRootChannel)
	require.NoError(t, err)
	assert.Equal(t, gbmodels.PTZFreshnessUnknown, tracks.Freshness, "platform optimistic write is not a device observation")
	require.Len(t, tracks.Items, 1)
	assert.Equal(t, "platform_pending_reconcile", tracks.Items[0].Source)
	assert.Nil(t, tracks.Items[0].ObservedAt)
}

func int8ptr(value int8) *int8    { return &value }
func strptr(value string) *string { return &value }
