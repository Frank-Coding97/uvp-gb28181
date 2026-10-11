package deviceevents

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
	"uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

const (
	deviceEventsRootDept   = uint(10)
	deviceEventsOtherDept  = uint(20)
	deviceEventsRootDevice = "34020000002000000010"
	deviceEventsOtherCode  = "34020000002000000020"
)

type subscriptionStub struct {
	configureCalls int
	renewCalls     int
	err            error
}

func (stub *subscriptionStub) Configure(_ context.Context, _ *gbmodels.GbDevice, kind gbmodels.SubscriptionKind, enabled *bool, expires, interval *int) (*gbmodels.GbDeviceSubscription, error) {
	stub.configureCalls++
	if stub.err != nil {
		return nil, stub.err
	}
	return &gbmodels.GbDeviceSubscription{DeviceID: 1, Kind: kind, Enabled: enabled != nil && *enabled, Status: gbmodels.SubscriptionStatusActive}, nil
}

func (stub *subscriptionStub) Renew(_ context.Context, _ *gbmodels.GbDevice, kind gbmodels.SubscriptionKind) (*gbmodels.GbDeviceSubscription, error) {
	stub.renewCalls++
	if stub.err != nil {
		return nil, stub.err
	}
	return &gbmodels.GbDeviceSubscription{DeviceID: 1, Kind: kind, Enabled: true, Status: gbmodels.SubscriptionStatusActive}, nil
}

func newDeviceEventsTestService(t *testing.T, upstream SubscriptionLifecycle, overrides ...Config) (*Service, *gorm.DB, resource.DepartmentScope, uint, uint, *time.Time) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.AutoMigrate(
		&appmodels.SysDepartment{}, &gbmodels.GbDevice{}, &gbmodels.GbChannel{},
		&gbmodels.GbDeviceSubscription{}, &gbmodels.GbAlarmEvent{}, &gbmodels.GbDeviceStatusEvent{},
		&gbmodels.GbMobilePositionLatest{}, &gbmodels.GbMobilePositionHistory{}, &OpenAPIDeviceSubscription{},
	))
	active := int8(1)
	for _, department := range []appmodels.SysDepartment{
		{BaseModel: appmodels.BaseModel{ID: deviceEventsRootDept}, Name: "root", Status: &active},
		{BaseModel: appmodels.BaseModel{ID: deviceEventsOtherDept}, Name: "other", Status: &active},
	} {
		require.NoError(t, db.Create(&department).Error)
	}
	online := gbmodels.DeviceStatusOnline
	devices := []gbmodels.GbDevice{
		{DeviceID: deviceEventsRootDevice, OwnerDeptID: deviceEventsRootDept, Status: online},
		{DeviceID: deviceEventsOtherCode, OwnerDeptID: deviceEventsOtherDept, Status: online},
	}
	for index := range devices {
		require.NoError(t, db.Create(&devices[index]).Error)
	}
	var rootID, otherID uint
	require.NoError(t, db.Model(&gbmodels.GbDevice{}).Select("id").Where("device_id = ?", deviceEventsRootDevice).Scan(&rootID).Error)
	require.NoError(t, db.Model(&gbmodels.GbDevice{}).Select("id").Where("device_id = ?", deviceEventsOtherCode).Scan(&otherID).Error)
	now := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	config := Config{
		CursorKey:              []byte("0123456789abcdef0123456789abcdef"),
		Now:                    func() time.Time { return now },
		AlarmEventsEnabled:     func() bool { return true },
		PositionHistoryEnabled: func() bool { return true },
	}
	for _, override := range overrides {
		if override.CursorKey != nil {
			config.CursorKey = override.CursorKey
		}
		if override.CursorTTL != 0 {
			config.CursorTTL = override.CursorTTL
		}
		if override.Now != nil {
			config.Now = override.Now
		}
		if override.AlarmEventsEnabled != nil {
			config.AlarmEventsEnabled = override.AlarmEventsEnabled
		}
		if override.PositionHistoryEnabled != nil {
			config.PositionHistoryEnabled = override.PositionHistoryEnabled
		}
	}
	service := NewService(db, upstream, config)
	return service, db, resource.DepartmentScope{OwnerDeptID: deviceEventsRootDept, DataScope: resource.DataScopeDepartment}, rootID, otherID, &now
}

func TestSubscriptionLifecycleIsClientAndDepartmentScoped(t *testing.T) {
	upstream := &subscriptionStub{}
	service, _, scope, _, _, _ := newDeviceEventsTestService(t, upstream)
	ctx := context.Background()
	created, err := service.CreateSubscription(ctx, Request{OpenAPIClientID: 71, ResourceScope: scope, DeviceID: deviceEventsRootDevice, Kind: EventAlarm, ExpiresSeconds: 600})
	require.NoError(t, err)
	assert.NotEmpty(t, created.ID)
	assert.True(t, created.Enabled)
	assert.Equal(t, 1, upstream.configureCalls)

	read, err := service.GetSubscription(ctx, Request{OpenAPIClientID: 71, ResourceScope: scope, SubscriptionID: created.ID})
	require.NoError(t, err)
	assert.Equal(t, created.ID, read.ID)
	_, err = service.GetSubscription(ctx, Request{OpenAPIClientID: 72, ResourceScope: scope, SubscriptionID: created.ID})
	assert.ErrorIs(t, err, ErrNotFound)

	_, err = service.RenewSubscription(ctx, Request{OpenAPIClientID: 71, ResourceScope: scope, SubscriptionID: created.ID})
	require.NoError(t, err)
	assert.Equal(t, 1, upstream.renewCalls)

	_, err = service.CancelSubscription(ctx, Request{OpenAPIClientID: 71, ResourceScope: scope, SubscriptionID: created.ID})
	require.NoError(t, err)
	_, err = service.PollEvents(ctx, Request{OpenAPIClientID: 71, ResourceScope: scope, SubscriptionID: created.ID, PageSize: 10})
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = service.GetSubscription(ctx, Request{OpenAPIClientID: 71, ResourceScope: resource.DepartmentScope{OwnerDeptID: deviceEventsOtherDept, DataScope: resource.DataScopeDepartment}, SubscriptionID: created.ID})
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestCancelSubscriptionDoesNotDisableSharedSIPSubscription(t *testing.T) {
	service, db, scope, rootID, _, _ := newDeviceEventsTestService(t, &subscriptionStub{})
	shared := gbmodels.GbDeviceSubscription{
		DeviceID: rootID, Kind: gbmodels.SubscriptionKindAlarm,
		Enabled: true, Status: gbmodels.SubscriptionStatusActive,
	}
	require.NoError(t, db.Create(&shared).Error)

	created, err := service.CreateSubscription(context.Background(), Request{
		OpenAPIClientID: 71, ResourceScope: scope, DeviceID: deviceEventsRootDevice,
		Kind: EventAlarm, ExpiresSeconds: 600,
	})
	require.NoError(t, err)
	_, err = service.CancelSubscription(context.Background(), Request{
		OpenAPIClientID: 71, ResourceScope: scope, SubscriptionID: created.ID,
	})
	require.NoError(t, err)

	var persisted gbmodels.GbDeviceSubscription
	require.NoError(t, db.First(&persisted, shared.ID).Error)
	assert.True(t, persisted.Enabled)
	assert.Equal(t, gbmodels.SubscriptionStatusActive, persisted.Status)
}

func TestEventCursorIsStableAndBoundToClientSubscriptionAndDepartment(t *testing.T) {
	service, db, scope, rootID, otherID, now := newDeviceEventsTestService(t, &subscriptionStub{})
	ctx := context.Background()
	sub, err := service.CreateSubscription(ctx, Request{OpenAPIClientID: 71, ResourceScope: scope, DeviceID: deviceEventsRootDevice, Kind: EventDeviceStatus, ExpiresSeconds: maxExpires})
	require.NoError(t, err)
	base := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
	for index, event := range []gbmodels.GbDeviceStatusEvent{
		{DeviceID: rootID, DeviceCode: deviceEventsRootDevice, EventType: gbmodels.DeviceEventRegisterOnline, ToStatus: 1, Source: gbmodels.DeviceEventSourceRegister, OccurredAt: base},
		{DeviceID: rootID, DeviceCode: deviceEventsRootDevice, EventType: gbmodels.DeviceEventHeartbeatTimeout, ToStatus: 0, Source: gbmodels.DeviceEventSourceOfflineScanner, OccurredAt: base.Add(time.Second), Detail: "must not escape"},
		{DeviceID: otherID, DeviceCode: deviceEventsOtherCode, EventType: gbmodels.DeviceEventRegisterOnline, ToStatus: 1, Source: gbmodels.DeviceEventSourceRegister, OccurredAt: base},
	} {
		event.ID = uint64(index + 1)
		require.NoError(t, db.Create(&event).Error)
	}
	first, err := service.PollEvents(ctx, Request{OpenAPIClientID: 71, ResourceScope: scope, SubscriptionID: sub.ID, PageSize: 1})
	require.NoError(t, err)
	require.Len(t, first.Items, 1)
	assert.True(t, first.HasMore)
	require.NotEmpty(t, first.NextCursor)
	second, err := service.PollEvents(ctx, Request{OpenAPIClientID: 71, ResourceScope: scope, SubscriptionID: sub.ID, PageSize: 1, Cursor: first.NextCursor})
	require.NoError(t, err)
	require.Len(t, second.Items, 1)
	assert.NotEqual(t, first.Items[0].ID, second.Items[0].ID)
	assert.False(t, second.HasMore)
	assert.NotContains(t, string(mustJSON(t, second)), "must not escape")

	otherClientSub, err := service.CreateSubscription(ctx, Request{OpenAPIClientID: 72, ResourceScope: scope, DeviceID: deviceEventsRootDevice, Kind: EventDeviceStatus, ExpiresSeconds: 3600})
	require.NoError(t, err)
	_, err = service.PollEvents(ctx, Request{OpenAPIClientID: 72, ResourceScope: scope, SubscriptionID: otherClientSub.ID, PageSize: 1, Cursor: first.NextCursor})
	assert.ErrorIs(t, err, ErrInvalidCursor)
	_, err = service.PollEvents(ctx, Request{OpenAPIClientID: 71, ResourceScope: resource.DepartmentScope{OwnerDeptID: deviceEventsOtherDept, DataScope: resource.DataScopeDepartment}, SubscriptionID: sub.ID, PageSize: 1, Cursor: first.NextCursor})
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = service.PollEvents(ctx, Request{OpenAPIClientID: 71, ResourceScope: scope, SubscriptionID: sub.ID, PageSize: 1, Cursor: "not-a-cursor"})
	assert.ErrorIs(t, err, ErrInvalidCursor)
	*now = now.Add(25 * time.Hour)
	_, err = service.PollEvents(ctx, Request{OpenAPIClientID: 71, ResourceScope: scope, SubscriptionID: sub.ID, PageSize: 1, Cursor: first.NextCursor})
	assert.ErrorIs(t, err, ErrExpiredCursor)
}

func TestUnpersistedSubscriptionKindsAreUnavailable(t *testing.T) {
	upstream := &subscriptionStub{}
	service, _, scope, _, _, _ := newDeviceEventsTestService(t, upstream)
	for _, kind := range []EventKind{EventCatalog, EventPTZPrecisePosition} {
		_, err := service.CreateSubscription(context.Background(), Request{OpenAPIClientID: 71, ResourceScope: scope, DeviceID: deviceEventsRootDevice, Kind: kind, ExpiresSeconds: 600})
		assert.ErrorIs(t, err, ErrUnavailable, string(kind))
	}
	assert.Zero(t, upstream.configureCalls)
}

func TestAlarmAndMobilePositionEventPollingUsePersistedSafePayloads(t *testing.T) {
	upstream := &subscriptionStub{}
	service, db, scope, rootID, _, _ := newDeviceEventsTestService(t, upstream)
	ctx := context.Background()
	base := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)

	alarmSub, err := service.CreateSubscription(ctx, Request{OpenAPIClientID: 71, ResourceScope: scope, DeviceID: deviceEventsRootDevice, Kind: EventAlarm, ExpiresSeconds: 600})
	require.NoError(t, err)
	require.NoError(t, db.Create(&gbmodels.GbAlarmEvent{
		ID: 41, DeviceID: rootID, SourceCode: deviceEventsRootDevice, Description: "door open", RawDigest: "private-digest", RawSummary: "private-xml", DedupeKey: "private-dedupe", CallID: "private-call", ReceivedAt: base,
	}).Error)
	alarmPage, err := service.PollEvents(ctx, Request{OpenAPIClientID: 71, ResourceScope: scope, SubscriptionID: alarmSub.ID, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, alarmPage.Items, 1)
	assert.Equal(t, "door open", alarmPage.Items[0].Alarm.Description)
	assert.NotContains(t, string(mustJSON(t, alarmPage)), "private-digest")
	assert.NotContains(t, string(mustJSON(t, alarmPage)), "private-xml")
	assert.NotContains(t, string(mustJSON(t, alarmPage)), "private-call")

	positionSub, err := service.CreateSubscription(ctx, Request{OpenAPIClientID: 71, ResourceScope: scope, DeviceID: deviceEventsRootDevice, Kind: EventMobilePosition, ExpiresSeconds: 600})
	require.NoError(t, err)
	require.NoError(t, db.Create(&gbmodels.GbMobilePositionHistory{
		ID: 51, DeviceID: rootID, SourceCode: deviceEventsRootDevice, EventTime: base, ReceivedAt: base.Add(time.Second), Longitude: 116.4, Latitude: 39.9,
	}).Error)
	positionPage, err := service.PollEvents(ctx, Request{OpenAPIClientID: 71, ResourceScope: scope, SubscriptionID: positionSub.ID, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, positionPage.Items, 1)
	assert.Equal(t, 116.4, positionPage.Items[0].Position.Longitude)
	assert.Equal(t, 39.9, positionPage.Items[0].Position.Latitude)
	assert.Equal(t, 2, upstream.configureCalls)
	assert.NotContains(t, string(mustJSON(t, positionPage)), "locationType")
}

func TestPositionHistoryDisabledMakesSubscriptionUnavailable(t *testing.T) {
	upstream := &subscriptionStub{}
	service, _, scope, _, _, _ := newDeviceEventsTestService(t, upstream, Config{PositionHistoryEnabled: func() bool { return false }})
	_, err := service.CreateSubscription(context.Background(), Request{OpenAPIClientID: 71, ResourceScope: scope, DeviceID: deviceEventsRootDevice, Kind: EventMobilePosition, ExpiresSeconds: 600})
	assert.ErrorIs(t, err, ErrUnavailable)
	assert.Zero(t, upstream.configureCalls)
}

func TestPositionHistoryReadDisabledIsUnavailable(t *testing.T) {
	service, _, scope, _, _, _ := newDeviceEventsTestService(t, nil, Config{PositionHistoryEnabled: func() bool { return false }})
	_, err := service.ListPositionHistory(context.Background(), Request{OpenAPIClientID: 71, ResourceScope: scope, DeviceID: deviceEventsRootDevice, PageSize: 10})
	assert.ErrorIs(t, err, ErrUnavailable)
}

func TestEventPollingRequiresCursorSigningKey(t *testing.T) {
	service, _, scope, _, _, _ := newDeviceEventsTestService(t, nil, Config{CursorKey: []byte{}})
	sub, err := service.CreateSubscription(context.Background(), Request{OpenAPIClientID: 71, ResourceScope: scope, DeviceID: deviceEventsRootDevice, Kind: EventDeviceStatus, ExpiresSeconds: 600})
	require.NoError(t, err)
	_, err = service.PollEvents(context.Background(), Request{OpenAPIClientID: 71, ResourceScope: scope, SubscriptionID: sub.ID, PageSize: 10})
	assert.ErrorIs(t, err, ErrUnavailable)
}

func TestPositionReadReturnsOnlyPersistedFieldsWithinClientDepartment(t *testing.T) {
	service, db, scope, rootID, otherID, _ := newDeviceEventsTestService(t, nil)
	now := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&gbmodels.GbMobilePositionLatest{DeviceID: rootID, SourceCode: deviceEventsRootDevice, EventTime: now, ReceivedAt: now, Longitude: 116.4, Latitude: 39.9}).Error)
	require.NoError(t, db.Create(&gbmodels.GbMobilePositionLatest{DeviceID: otherID, SourceCode: deviceEventsOtherCode, EventTime: now, ReceivedAt: now, Longitude: 1, Latitude: 2}).Error)
	page, err := service.GetLatestPositions(context.Background(), Request{OpenAPIClientID: 71, ResourceScope: scope, DeviceID: deviceEventsRootDevice})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, deviceEventsRootDevice, page.Items[0].SourceCode)
	assert.Equal(t, 116.4, page.Items[0].Longitude)
	assert.NotContains(t, string(mustJSON(t, page)), "deviceId\":1")
	_, err = service.GetLatestPositions(context.Background(), Request{OpenAPIClientID: 71, ResourceScope: scope, DeviceID: deviceEventsOtherCode})
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestDeviceEventAdaptersUseSeparateScopesAndValidateInvocation(t *testing.T) {
	dispatcher := NewDispatcher(nil)
	registrations := NewAdapterRegistrations(dispatcher)
	require.Len(t, registrations, 7)
	byKey := make(map[string]runtime.Adapter)
	for _, registration := range registrations {
		assert.Equal(t, "v1", registration.ContractVersion)
		byKey[registration.Key] = registration.Adapter
	}
	assert.Len(t, byKey, len(registrations))
	_, err := byKey[SubscriptionCreateAdapterKey].Execute(context.Background(), runtime.Invocation{
		Scope: SubscriptionReadScope, Method: "POST", Value: Request{OpenAPIClientID: 71, ResourceScope: resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}},
	})
	assert.ErrorIs(t, err, ErrInvalidRequest)
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	return encoded
}
