package play

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/openapi/auth"
	"uvplatform.com/uvp-gb28181/app/openapi/models"
)

type liveReadDevice struct {
	ID                    uint `gorm:"primaryKey"`
	DeviceID              string
	OwnerDeptID           uint
	AccessEpoch           int64
	CleanupCompletedEpoch int64
	DeletedAt             *time.Time
}

func (liveReadDevice) TableName() string { return "gb_device" }

type liveReadChannel struct {
	ID          uint `gorm:"primaryKey"`
	DeviceID    string
	ChannelID   string
	OwnerDeptID uint
	DeletedAt   *time.Time
}

func (liveReadChannel) TableName() string { return "gb_channel" }

type liveReadDepartment struct {
	ID        uint `gorm:"primaryKey"`
	ParentID  *uint
	Status    *int8
	DeletedAt *time.Time
}

func (liveReadDepartment) TableName() string { return "sys_department" }

func liveReadDB(t *testing.T) (*gorm.DB, *Dispatcher, auth.PlayRequest) {
	t.Helper()
	db := quotaDB(t, 10)
	require.NoError(t, db.AutoMigrate(&liveReadDevice{}, &liveReadChannel{}, &liveReadDepartment{}))
	require.NoError(t, db.Model(&liveReadDevice{}).Where("id = 1").Update("owner_dept_id", 1).Error)
	active := int8(1)
	require.NoError(t, db.Create(&liveReadDepartment{ID: 1, Status: &active}).Error)
	require.NoError(t, db.Create(&liveReadDepartment{ID: 2, Status: &active}).Error)
	require.NoError(t, db.Create(&liveReadChannel{ID: 1, DeviceID: stopDeviceCode, ChannelID: stopChannelCode, OwnerDeptID: 1}).Error)
	d := NewDispatcher(db, &stopLiveService{})
	d.quota = NewViewerQuota(db, func() time.Time { return quotaNow })
	return db, d, auth.PlayRequest{ClientID: 1, OwnerDeptID: 1, DataScope: models.DataScopeDepartment, DeviceID: stopDeviceCode, ChannelID: stopChannelCode}
}

func liveGrant(n int) models.PlayGrant {
	device, channel, stream, node, boot, schema, vhost, app, protocol := stopDeviceCode, stopChannelCode, fmt.Sprintf("stream-%d", n), "private-node", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "http", "__defaultVhost__", "rtp", "http-flv"
	generation := uint64(1)
	return models.PlayGrant{GrantID: fmt.Sprintf("00000000-0000-4000-8000-%012d", n), ClientID: 1, Scope: auth.PlayLiveScope, DeviceID: &device, ChannelID: &channel, ClientEpoch: 4, ScopeEpoch: 5, DeviceEpoch: 3, NodeUUID: &node, BootNonce: &boot, Schema: &schema, VHost: &vhost, App: &app, Stream: &stream, MediaGeneration: &generation, Protocol: &protocol, State: models.GrantStateIssued, IssuedAt: quotaNow, ExpiresAt: quotaNow.Add(time.Minute), CreatedAt: quotaNow.Add(time.Duration(n) * time.Millisecond), UpdatedAt: quotaNow}
}

func TestLiveReadsDeduplicateCurrentClientChannelWithoutRenewingOrLeakingIdentity(t *testing.T) {
	db, d, request := liveReadDB(t)
	first, latest := liveGrant(1), liveGrant(2)
	foreign := liveGrant(3)
	foreign.ClientID = 2
	stale := liveGrant(4)
	stale.DeviceEpoch = 2
	expired := liveGrant(5)
	expired.ExpiresAt = quotaNow
	revoked := liveGrant(6)
	revoked.State = models.GrantStateRevoked
	for _, grant := range []models.PlayGrant{first, latest, foreign, stale, expired, revoked} {
		require.NoError(t, db.Create(&grant).Error)
	}
	seen := quotaNow.Add(-time.Second)
	viewer := models.Viewer{GrantID: latest.GrantID, NodeUUID: *latest.NodeUUID, BootNonce: *latest.BootNonce, Identifier: "private-viewer", Schema: *latest.Schema, VHost: *latest.VHost, App: *latest.App, Stream: *latest.Stream, MediaGeneration: 1, State: models.ViewerStateActive, LastSeenAt: &seen, CreatedAt: quotaNow, UpdatedAt: quotaNow}
	require.NoError(t, db.Create(&viewer).Error)
	data, err := d.Read(context.Background(), request)
	require.NoError(t, err)
	view := data.(LiveView)
	require.Equal(t, "stream-2", view.StreamID)
	require.Equal(t, "issued", view.AuthorizationState)
	require.Equal(t, "active", view.ViewerState)
	require.Equal(t, &seen, view.LastSeenAt)
	encoded, err := json.Marshal(view)
	require.NoError(t, err)
	for _, private := range []string{latest.GrantID, "private-node", "private-viewer", "bootNonce", "token", "clientId"} {
		require.NotContains(t, string(encoded), private)
	}
	request.DeviceID, request.ChannelID = "", ""
	data, err = d.List(context.Background(), auth.PlayListRequest{PlayRequest: request})
	require.NoError(t, err)
	page := data.(LivePage)
	require.Equal(t, 1, page.Total)
	require.Len(t, page.Items, 1)
	require.Equal(t, "stream-2", page.Items[0].StreamID)
	var stored models.PlayGrant
	require.NoError(t, db.Where("grant_id = ?", latest.GrantID).Take(&stored).Error)
	require.Equal(t, latest.ExpiresAt, stored.ExpiresAt)
	require.Equal(t, latest.UpdatedAt, stored.UpdatedAt)
	var storedViewer models.Viewer
	require.NoError(t, db.Where("grant_id = ?", latest.GrantID).Take(&storedViewer).Error)
	require.Equal(t, &seen, storedViewer.LastSeenAt)
	require.Equal(t, quotaNow, storedViewer.UpdatedAt)
}

func TestLiveReadsFailClosedForCurrentScopeAndEpochChanges(t *testing.T) {
	for _, change := range []string{"device moved", "channel moved", "device epoch", "cleanup pending", "client epoch", "scope epoch", "scope revoked", "client disabled", "department disabled"} {
		t.Run(change, func(t *testing.T) {
			db, d, request := liveReadDB(t)
			grant := liveGrant(1)
			require.NoError(t, db.Create(&grant).Error)
			var result *gorm.DB
			switch change {
			case "device moved":
				result = db.Model(&liveReadDevice{}).Where("id = 1").Update("owner_dept_id", 2)
			case "channel moved":
				result = db.Model(&liveReadChannel{}).Where("id = 1").Update("owner_dept_id", 2)
			case "device epoch":
				result = db.Model(&liveReadDevice{}).Where("id = 1").Updates(map[string]any{"access_epoch": 4, "cleanup_completed_epoch": 4})
			case "cleanup pending":
				result = db.Model(&liveReadDevice{}).Where("id = 1").Update("cleanup_completed_epoch", 2)
			case "client epoch":
				result = db.Model(&models.Client{}).Where("id = 1").Update("auth_epoch", 5)
			case "scope epoch":
				result = db.Model(&models.ClientScope{}).Where("client_id = 1").Update("scope_epoch", 6)
			case "scope revoked":
				result = db.Model(&models.ClientScope{}).Where("client_id = 1").Update("enabled", false)
			case "client disabled":
				result = db.Model(&models.Client{}).Where("id = 1").Update("status", models.StatusDisabled)
			case "department disabled":
				result = db.Model(&liveReadDepartment{}).Where("id = 1").Update("status", 0)
			}
			require.NoError(t, result.Error)
			_, err := d.Read(context.Background(), request)
			require.ErrorIs(t, err, auth.ErrPlayNotFound)
			data, err := d.List(context.Background(), auth.PlayListRequest{PlayRequest: request})
			require.NoError(t, err)
			require.Empty(t, data.(LivePage).Items)
		})
	}
}

func TestLiveReadPaginationAndTargetValidation(t *testing.T) {
	db, d, request := liveReadDB(t)
	first := liveGrant(1)
	require.NoError(t, db.Create(&first).Error)
	secondChannel := "37010301021320000007"
	require.NoError(t, db.Create(&liveReadChannel{ID: 2, DeviceID: stopDeviceCode, ChannelID: secondChannel, OwnerDeptID: 1}).Error)
	second := liveGrant(2)
	second.ChannelID = &secondChannel
	require.NoError(t, db.Create(&second).Error)
	request.ChannelID = ""
	data, err := d.List(context.Background(), auth.PlayListRequest{PlayRequest: request, Page: 2, PageSize: 1})
	require.NoError(t, err)
	page := data.(LivePage)
	require.Equal(t, 2, page.Total)
	require.Equal(t, stopChannelCode, page.Items[0].ChannelID)
	_, err = d.Read(context.Background(), request)
	require.ErrorIs(t, err, auth.ErrPlayInvalid)
	_, err = d.List(context.Background(), auth.PlayListRequest{PlayRequest: request, PageSize: 101})
	require.ErrorIs(t, err, auth.ErrPlayInvalid)
	request.DeviceID = "bad"
	_, err = d.List(context.Background(), auth.PlayListRequest{PlayRequest: request})
	require.ErrorIs(t, err, auth.ErrPlayInvalid)
}

func TestLiveReadHidesAmbiguousTargetsAndUnmatchedViewerTuple(t *testing.T) {
	db, d, request := liveReadDB(t)
	grant := liveGrant(1)
	require.NoError(t, db.Create(&grant).Error)
	viewer := models.Viewer{GrantID: grant.GrantID, NodeUUID: *grant.NodeUUID, BootNonce: *grant.BootNonce, Identifier: "private-viewer", Schema: *grant.Schema, VHost: *grant.VHost, App: *grant.App, Stream: "different-stream", MediaGeneration: 1, State: models.ViewerStateActive, CreatedAt: quotaNow, UpdatedAt: quotaNow}
	require.NoError(t, db.Create(&viewer).Error)
	data, err := d.Read(context.Background(), request)
	require.NoError(t, err)
	require.Empty(t, data.(LiveView).ViewerState, "a viewer for a different immutable media tuple is not this grant's observation")
	require.NoError(t, db.Create(&liveReadChannel{ID: 2, DeviceID: stopDeviceCode, ChannelID: stopChannelCode, OwnerDeptID: 1}).Error)
	_, err = d.Read(context.Background(), request)
	require.ErrorIs(t, err, auth.ErrPlayNotFound)
}

func TestLiveRuntimeRootForwardsReadsAndFailsClosedWithoutAReader(t *testing.T) {
	db, d, request := liveReadDB(t)
	grant := liveGrant(1)
	require.NoError(t, db.Create(&grant).Error)
	root := NewRuntimeRoot()
	_, err := root.Read(context.Background(), request)
	require.ErrorIs(t, err, auth.ErrPlayUnavailable)
	require.NoError(t, root.Replace(d))
	data, err := root.Read(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, "stream-1", data.(LiveView).StreamID)
	data, err = root.List(context.Background(), auth.PlayListRequest{PlayRequest: request})
	require.NoError(t, err)
	require.Equal(t, 1, data.(LivePage).Total)
	require.NoError(t, root.Replace(&runtimeRootDispatcher{}))
	_, err = root.Read(context.Background(), request)
	require.ErrorIs(t, err, auth.ErrPlayUnavailable)
	_, err = root.List(context.Background(), auth.PlayListRequest{PlayRequest: request})
	require.ErrorIs(t, err, auth.ErrPlayUnavailable)
}
