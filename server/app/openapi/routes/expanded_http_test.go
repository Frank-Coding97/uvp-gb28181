package routes

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	gbplay "uvplatform.com/uvp-gb28181/app/gb28181/play"
	gbplayback "uvplatform.com/uvp-gb28181/app/gb28181/playback"
	runtimeptz "uvplatform.com/uvp-gb28181/app/gb28181/ptz"
	appmodels "uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/openapi/adapters"
	"uvplatform.com/uvp-gb28181/app/openapi/auth"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	"uvplatform.com/uvp-gb28181/app/openapi/client"
	"uvplatform.com/uvp-gb28181/app/openapi/cloudrecord"
	"uvplatform.com/uvp-gb28181/app/openapi/contracts"
	"uvplatform.com/uvp-gb28181/app/openapi/models"
	openapiplay "uvplatform.com/uvp-gb28181/app/openapi/play"
	"uvplatform.com/uvp-gb28181/app/openapi/recording"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

const expandedDevice = "34020000002000000010"
const expandedChannel = "34020000001320000010"

type expandedMedia struct {
	sessions []*gbplayback.Session
	calls    int
}

func (s *expandedMedia) StartAuthorized(context.Context, gbplay.AuthorizedRequest) (*gbplay.Result, error) {
	s.calls++
	return nil, nil
}
func (s *expandedMedia) Stop(context.Context, string, string, string) error { s.calls++; return nil }
func (s *expandedMedia) ListForOwner(owner string) []*gbplayback.Session    { return s.sessions }
func (s *expandedMedia) Create(context.Context, gbplayback.CreateRequest) (gbplayback.CreateResult, error) {
	s.calls++
	return gbplayback.CreateResult{}, nil
}
func (s *expandedMedia) PeekForOwner(string, string) (*gbplayback.Session, bool) { return nil, false }
func (s *expandedMedia) GetForOwner(string, string) (*gbplayback.Session, bool) {
	s.calls++
	return nil, false
}
func (s *expandedMedia) Action(context.Context, string, string, gbplayback.ActionRequest) (*gbplayback.Session, error) {
	s.calls++
	return nil, nil
}
func (s *expandedMedia) StopForOwner(context.Context, string, string, string) error {
	s.calls++
	return nil
}

type expandedCommands struct {
	commands  []runtimeptz.Command
	operation gbmodels.GbPTZOperation
}

func (s *expandedCommands) Execute(_ context.Context, target runtimeptz.Target, c runtimeptz.Command) (gbmodels.GbPTZOperation, error) {
	s.commands = append(s.commands, c)
	s.operation = gbmodels.GbPTZOperation{OperationID: "00000000-0000-4000-8000-000000000099", DeviceCode: target.DeviceCode, ChannelCode: target.ChannelCode, ChannelID: target.ChannelID, IdempotencyKey: c.IdempotencyKey, Action: c.Action, Status: gbmodels.PTZOperationQueued, ProfileVersion: string(target.Profile.Version), ResponseRequired: true}
	return s.operation, nil
}
func (s *expandedCommands) GetOperationForClient(context.Context, resource.DepartmentScope, string, string, int64) (gbmodels.GbPTZOperation, error) {
	return s.operation, nil
}

type expandedPTZSpy struct{ calls int }

func (s *expandedPTZSpy) Ready() bool { return true }
func (s *expandedPTZSpy) Handle(context.Context, auth.PTZRequest) (any, error) {
	s.calls++
	return nil, auth.ErrPTZUnavailable
}

type expandedHTTP struct {
	t      *testing.T
	router *gin.Engine
	secret string
	seq    int
}

func (f *expandedHTTP) call(method, path, body string, tamper bool) (int, map[string]any) {
	f.t.Helper()
	f.seq++
	parsed, err := url.Parse(path)
	require.NoError(f.t, err)
	input := auth.SignatureInput{Method: method, Path: parsed.Path, RawQuery: parsed.RawQuery, Body: []byte(body), AccessKey: fmt.Sprintf("uvp_%032x", 1), Timestamp: fmt.Sprint(time.Now().Unix()), Nonce: fmt.Sprintf("%032x", f.seq), Audience: "expanded-http"}
	if method == "POST" {
		input.ContentType = "application/json"
		input.IdempotencyKey = fmt.Sprintf("query-%d", f.seq)
	}
	sig, err := auth.Sign(input, f.secret)
	require.NoError(f.t, err)
	if tamper {
		if method == "POST" {
			body += " "
		} else {
			path = strings.Replace(path, "pageSize=1", "pageSize=2", 1)
		}
	}
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range map[string]string{"Content-Type": input.ContentType, "Idempotency-Key": input.IdempotencyKey, "X-UVP-Sign-Version": "1", "X-UVP-Access-Key": input.AccessKey, "X-UVP-Timestamp": input.Timestamp, "X-UVP-Nonce": input.Nonce, "X-UVP-Signature": sig} {
		if v != "" {
			request.Header.Set(k, v)
		}
	}
	out := httptest.NewRecorder()
	f.router.ServeHTTP(out, request)
	var response map[string]any
	require.NoError(f.t, json.Unmarshal(out.Body.Bytes(), &response), out.Body.String())
	require.Equal(f.t, "no-store", out.Header().Get("Cache-Control"))
	return out.Code, response
}

func TestExpandedOpenAPIRealHMACRoutesReachProductionAdapters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newCatalogRuntimeTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.Client{}, &models.ClientScope{}, &models.Nonce{}, &models.Audit{}, &models.PlayGrant{}, &models.Viewer{}, &appmodels.SysDepartment{}, &gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbAlarmEvent{}))
	require.NoError(t, db.AutoMigrate(&gbmodels.GbRecordingFile{}))
	require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN access_epoch BIGINT NOT NULL DEFAULT 3").Error)
	require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN cleanup_completed_epoch BIGINT NOT NULL DEFAULT 3").Error)
	active := int8(1)
	require.NoError(t, db.Create(&appmodels.SysDepartment{BaseModel: appmodels.BaseModel{ID: 10}, Name: "owned", Status: &active}).Error)
	device := gbmodels.GbDevice{DeviceID: expandedDevice, OwnerDeptID: 10, Status: 1, Manufacturer: "Acme", EffectiveVersion: "2022", IP: "192.0.2.10", Port: 5060, Transport: "UDP"}
	require.NoError(t, db.Create(&device).Error)
	channel := gbmodels.GbChannel{DeviceID: expandedDevice, ChannelID: expandedChannel, OwnerDeptID: 10, Status: 1, AudioEnabled: true}
	require.NoError(t, db.Create(&channel).Error)
	priority := 1
	alarm := gbmodels.GbAlarmEvent{DeviceID: device.ID, SourceCode: expandedChannel, Priority: &priority, Description: "test alarm", ReceivedAt: time.Now().UTC(), DedupeKey: "alarm-one"}
	require.NoError(t, db.Create(&alarm).Error)
	keys, err := client.NewSecretManager(bytes.Repeat([]byte{1}, 32), "test")
	require.NoError(t, err)
	secret := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	ak := fmt.Sprintf("uvp_%032x", 1)
	cipher, iv, err := keys.Encrypt(1, ak, 1, secret)
	require.NoError(t, err)
	require.NoError(t, db.Create(&models.Client{ID: 1, AK: ak, Name: "machine", OwnerDeptID: 10, DataScope: resource.DataScopeDepartment, Status: models.StatusActive, AuthEpoch: 1, SecretCiphertext: cipher, SecretIV: iv, SecretKeyID: "test", SecretVersion: 1, RateLimit: 1000, Burst: 1000}).Error)
	scopes := []string{auth.PlayLiveScope, auth.PlayLiveReadScope, auth.PlayLiveListScope, recording.ScopePlaybackList, recording.ScopeDownloadList, adapters.DeviceStatisticsScope, adapters.ChannelStatisticsScope, adapters.DeviceUpdateScope, adapters.ChannelUpdateScope, "device:alarms", adapters.DeviceAlarmDetailScope, "channel:ptz:preset:refresh", "channel:ptz:home:refresh", "channel:ptz:cruise:refresh", "channel:ptz:position:refresh", "channel:storage-card:refresh", "channel:control-status:refresh", adapters.ChannelQueryOperationScope}
	scopes = append(scopes, cloudrecord.ListScope, cloudrecord.InfoScope, cloudrecord.DatesScope)
	for _, scope := range scopes {
		require.NoError(t, db.Create(&models.ClientScope{ClientID: 1, Scope: scope, Enabled: true, ScopeEpoch: 1}).Error)
	}
	now := time.Now().UTC()
	stream, node, boot, schema, vhost, mediaApp, protocol := "owned-stream", "node-one", strings.Repeat("a", 32), "https", "__defaultVhost__", "rtp", "https-flv"
	generation := uint64(1)
	grant := models.PlayGrant{GrantID: "00000000-0000-4000-8000-000000000001", ClientID: 1, Scope: auth.PlayLiveScope, DeviceID: &device.DeviceID, ChannelID: &channel.ChannelID, ClientEpoch: 1, ScopeEpoch: 1, DeviceEpoch: 3, NodeUUID: &node, BootNonce: &boot, Schema: &schema, VHost: &vhost, App: &mediaApp, Stream: &stream, Protocol: &protocol, MediaGeneration: &generation, State: models.GrantStateIssued, IssuedAt: now, CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(time.Minute)}
	require.NoError(t, db.Create(&grant).Error)
	media := &expandedMedia{}
	snapshot := gbplayback.AuthorizationSnapshot{DevicePK: int64(device.ID), DeviceEpoch: 3, CleanupCompletedEpoch: 3, DeviceCode: expandedDevice, ChannelPK: int64(channel.ID), ChannelCode: expandedChannel}
	media.sessions = []*gbplayback.Session{{ID: "play-one", OwnerID: "openapi-client:1", DeviceID: expandedDevice, ChannelID: fmt.Sprint(channel.ID), Authorization: snapshot, Mode: gbplayback.ModePlayback, CreatedAt: now}, {ID: "download-one", OwnerID: "openapi-client:1", DeviceID: expandedDevice, ChannelID: fmt.Sprint(channel.ID), Authorization: snapshot, Mode: gbplayback.ModeDownload, CreatedAt: now}}
	rec := recording.NewDispatcher(recording.NewGormTargetResolver(db), nil, nil, media, recording.Config{})
	commands := &expandedCommands{}
	bindings := make(map[string]catalogruntime.Adapter)
	for _, reg := range recording.NewAdapterRegistrations(rec) {
		bindings[reg.Key] = reg.Adapter
	}
	for _, reg := range adapters.NewChannelRefreshAdapterRegistrations(adapters.NewGormMaintenanceResolver(db), commands, commands) {
		bindings[reg.Key] = reg.Adapter
	}
	ptz := &expandedPTZSpy{}
	live := openapiplay.NewDispatcher(db, media)
	require.NoError(t, db.AutoMigrate(catalogRuntimeTables(t, true)...))
	t.Setenv("UVP_OPENAPI_MASTER_KEY", base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)))
	settings := runtimeTestSettings{enabled: true, values: map[string]string{"openapi.audience": "expanded-http", "openapi.master_key_id": "test"}, integers: map[string]int{"httpserver.write_timeout": 30}}
	gate, _, err := InitializeRuntimeWithBindings(context.Background(), db, runtimePermissions{}, settings, bindings, ptz, live)
	require.NoError(t, err)
	router := gin.New()
	require.NoError(t, InstallPublicBoundary(router, gate, nil))
	f := expandedHTTP{t: t, router: router, secret: secret}
	target := "deviceId=" + expandedDevice + "&channelId=" + expandedChannel
	for _, path := range []string{"/live/info?" + target, "/live/list?" + target + "&page=1&pageSize=1", "/playback/list?" + target + "&page=1&pageSize=1", "/download/list?" + target + "&page=1&pageSize=1"} {
		status, response := f.call("GET", "/openapi/v1"+path, "", false)
		require.Equal(t, 200, status, response)
		data := response["data"].(map[string]any)
		if strings.Contains(path, "live/info") {
			require.Equal(t, "owned-stream", data["streamId"])
		} else {
			require.EqualValues(t, 1, data["total"])
			require.Len(t, data["items"], 1)
		}
	}
	status, response := f.call("GET", "/openapi/v1/playback/list?"+target+"&pageSize=1", "", true)
	require.Equal(t, 401, status, response)
	require.Zero(t, media.calls, "read endpoints must never start media or renew session leases")
	status, response = f.call("GET", "/openapi/v1/device/statistics?manufacturer=Acme", "", false)
	require.Equal(t, 200, status, response)
	require.EqualValues(t, 1, response["data"].(map[string]any)["total"])
	status, response = f.call("GET", "/openapi/v1/channel/statistics?deviceId="+expandedDevice, "", false)
	require.Equal(t, 200, status, response)
	require.EqualValues(t, 1, response["data"].(map[string]any)["total"])
	status, response = f.call("POST", "/openapi/v1/device/update", fmt.Sprintf(`{"deviceId":%q,"alias":"changed"}`, expandedDevice), false)
	require.Equal(t, 200, status, response)
	require.Equal(t, "changed", response["data"].(map[string]any)["alias"])
	body := fmt.Sprintf(`{"deviceId":%q,"channelId":%q,"audioEnabled":false,"ptzType":0}`, expandedDevice, expandedChannel)
	status, response = f.call("POST", "/openapi/v1/channel/update", body, false)
	require.Equal(t, 200, status, response)
	require.Equal(t, false, response["data"].(map[string]any)["audioEnabled"])
	status, response = f.call("POST", "/openapi/v1/channel/update", body, true)
	require.Equal(t, 401, status, response)
	status, response = f.call("GET", "/openapi/v1/alarm/list?deviceId="+expandedDevice+"&priority=1&sourceCode="+expandedChannel, "", false)
	require.Equal(t, 200, status, response)
	require.EqualValues(t, 1, response["data"].(map[string]any)["total"])
	status, response = f.call("GET", fmt.Sprintf("/openapi/v1/alarm/info?deviceId=%s&alarmId=%d", expandedDevice, alarm.ID), "", false)
	require.Equal(t, 200, status, response)
	require.Equal(t, "test alarm", response["data"].(map[string]any)["description"])
	for _, scope := range scopes {
		if !strings.HasSuffix(scope, ":refresh") {
			continue
		}
		var op contracts.Operation
		for _, candidate := range contracts.Operations() {
			if candidate.Scope == scope {
				op = candidate
			}
		}
		business := fmt.Sprintf(`{"deviceId":%q,"channelId":%q}`, expandedDevice, expandedChannel)
		if scope == "channel:control-status:refresh" {
			business = strings.TrimSuffix(business, "}") + `,"kind":"record"}`
		}
		status, response = f.call("POST", op.ExternalPath, business, false)
		require.Equal(t, 200, status, response)
		require.NotEmpty(t, response["data"].(map[string]any)["operationId"])
	}
	require.Len(t, commands.commands, 6)
	status, response = f.call("GET", "/openapi/v1/query/operation/info?"+target+"&operationId="+commands.operation.OperationID, "", false)
	require.Equal(t, 200, status, response)
	require.Zero(t, ptz.calls, "query refreshes with PTZ paths must reach query adapters, not the dedicated PTZ control plane")
	var stored gbmodels.GbChannel
	require.NoError(t, db.First(&stored, channel.ID).Error)
	require.False(t, stored.AudioEnabled)
	var lease models.PlayGrant
	require.NoError(t, db.Where("grant_id = ?", grant.GrantID).Take(&lease).Error)
	require.Equal(t, grant.ExpiresAt, lease.ExpiresAt)

	status, response = f.call("GET", "/openapi/v1/live/list?"+target+"&ownerDeptId=20", "", false)
	require.Equal(t, 400, status, response)
	status, response = f.call("POST", "/openapi/v1/channel/update", strings.TrimSuffix(body, "}")+`,"ownerDeptId":20}`, false)
	require.Equal(t, 400, status, response)
	require.NoError(t, db.Model(&models.ClientScope{}).Where("client_id = 1 AND scope = ?", auth.PlayLiveListScope).Update("enabled", false).Error)
	status, response = f.call("GET", "/openapi/v1/live/list?"+target, "", false)
	require.Equal(t, 403, status, response)

	otherDepartment := appmodels.SysDepartment{BaseModel: appmodels.BaseModel{ID: 20}, Name: "foreign", Status: &active}
	require.NoError(t, db.Create(&otherDepartment).Error)
	otherDevice, otherChannel := "34020000002000000020", "34020000001320000020"
	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: otherDevice, OwnerDeptID: 20}).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannel{DeviceID: otherDevice, ChannelID: otherChannel, OwnerDeptID: 20}).Error)
	for _, action := range []string{"live/info", "playback/list", "download/list"} {
		status, response = f.call("GET", "/openapi/v1/"+action+"?deviceId="+otherDevice+"&channelId="+otherChannel, "", false)
		require.Equal(t, 404, status, response)
	}
	status, response = f.call("POST", "/openapi/v1/device/update", fmt.Sprintf(`{"deviceId":%q,"alias":"forbidden"}`, otherDevice), false)
	require.Equal(t, 404, status, response)
	status, response = f.call("GET", fmt.Sprintf("/openapi/v1/alarm/info?deviceId=%s&alarmId=%d", otherDevice, alarm.ID), "", false)
	require.Equal(t, 404, status, response)

	fileTime := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	files := []gbmodels.GbRecordingFile{
		{ChannelID: channel.ID, DeviceID: expandedDevice, OwnerDeptID: 10, ChannelCode: expandedChannel, FileKey: "owned-cloud", FileName: "owned.mp4", FilePath: "/private/storage/secret.mp4", URL: "https://private-node/token", NodeID: 99, StartTime: &fileTime, RecordDate: &fileTime, Source: "hook", MetadataState: "complete", DiscoveredAt: fileTime},
		{ChannelID: channel.ID, DeviceID: expandedDevice, OwnerDeptID: 20, FileKey: "foreign-cloud", FileName: "foreign-secret.mp4", StartTime: &fileTime, RecordDate: &fileTime, Source: "hook", MetadataState: "complete", DiscoveredAt: fileTime},
	}
	require.NoError(t, db.Create(&files).Error)
	for _, path := range []string{
		"/cloud/record/list?" + target + "&metadataState=complete&pageSize=1",
		fmt.Sprintf("/cloud/record/info?%s&fileId=%d", target, files[0].ID),
		"/cloud/record/dates?" + target + "&startDate=2026-11-01&endDate=2026-12-01",
	} {
		status, response = f.call("GET", "/openapi/v1"+path, "", false)
		require.Equal(t, 200, status, response)
		data := response["data"].(map[string]any)
		switch {
		case strings.Contains(path, "/list?"):
			require.EqualValues(t, 1, data["total"])
			require.Len(t, data["items"], 1)
		case strings.Contains(path, "/info?"):
			require.Equal(t, "owned.mp4", data["fileName"])
		default:
			dates := data["items"].([]any)
			require.Len(t, dates, 1)
			require.EqualValues(t, 1, dates[0].(map[string]any)["fileCount"])
		}
		encoded, err := json.Marshal(response)
		require.NoError(t, err)
		for _, private := range []string{"private/storage", "private-node", "foreign-secret", "filePath", "nodeId", "fileKey"} {
			require.NotContains(t, string(encoded), private)
		}
	}
	status, response = f.call("GET", "/openapi/v1/cloud/record/list?"+target+"&pageSize=1", "", true)
	require.Equal(t, 401, status, response)
	status, response = f.call("GET", fmt.Sprintf("/openapi/v1/cloud/record/info?%s&fileId=%d", target, files[1].ID), "", false)
	require.Equal(t, 404, status, response)
	status, response = f.call("GET", fmt.Sprintf("/openapi/v1/cloud/record/info?deviceId=%s&channelId=%s&fileId=%d", otherDevice, otherChannel, files[0].ID), "", false)
	require.Equal(t, 404, status, response)
	status, response = f.call("GET", "/openapi/v1/cloud/record/dates?"+target+"&startDate=2026-11-01", "", false)
	require.Equal(t, 400, status, response)
	require.NoError(t, db.Model(&models.ClientScope{}).Where("client_id = 1 AND scope = ?", cloudrecord.ListScope).Update("enabled", false).Error)
	status, response = f.call("GET", "/openapi/v1/cloud/record/list?"+target, "", false)
	require.Equal(t, 403, status, response)
}
