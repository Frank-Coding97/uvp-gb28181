package gb28181

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/catalog"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/sipclient"
	"uvplatform.com/uvp-gb28181/app/gb28181/civilcode"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.com/uvp-gb28181/app/gb28181/uac"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/internal/authoritytest"
)

type cascadeStatusTransport struct {
	requests chan *sip.Request
}

func (f *cascadeStatusTransport) Do(_ context.Context, req *sip.Request) (*sip.Response, error) {
	f.requests <- req
	return sip.NewResponseFromRequest(req, 200, "OK", nil), nil
}

func (f *cascadeStatusTransport) DoDigestAuth(ctx context.Context, req *sip.Request, _ *sip.Response, _ sipclient.Credentials) (*sip.Response, error) {
	return f.Do(ctx, req)
}

func TestCascadeMessageHandlerRespondsToDeviceStatusQueryForPublishedPlatform(t *testing.T) {
	db := cascadeInviteTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&model.GbCascadePlatform{}, &model.GbCascadeDeviceProjection{}, &model.GbCascadeChannelProjection{},
		&model.GbCascadeBusinessNode{}, &model.GbCascadeBusinessBinding{},
	))
	platform := model.GbCascadePlatform{
		Name: "status upstream", UpstreamServerID: "34020000002000000001", UpstreamDomain: "3402000000",
		LocalDeviceID: "34020000002000000002", LocalDomain: "3402000000", Host: "192.0.2.10", Port: 5060,
		Transport: "UDP", LocalSIPIP: "192.0.2.20", LocalSIPPort: 5062, Enabled: true,
		PublishPlatform: true, EffectiveVersion: "2022",
	}
	require.NoError(t, db.Create(&platform).Error)

	transport := &cascadeStatusTransport{requests: make(chan *sip.Request, 1)}
	clients := newCascadePlatformClientFactory(transport, nil, time.Second)
	handler := newCascadeMessageHandler(repository.NewGormRepository(db), clients)
	profile := protocol.ProfileFor(protocol.Version2022)
	body, err := manscdp.BuildDeviceStatusQueryWithProfile(profile, platform.LocalDeviceID, 47)
	require.NoError(t, err)
	req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, body)
	tx := siptest.NewServerTxRecorder(req)
	defer tx.Terminate()

	require.True(t, handler(req, tx))
	require.Eventually(t, func() bool { return len(tx.Result()) == 1 }, time.Second, 5*time.Millisecond)
	require.Equal(t, 200, tx.Result()[0].StatusCode)

	var response *sip.Request
	select {
	case response = <-transport.requests:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for DeviceStatus response")
	}
	var decoded struct {
		CmdType  string `xml:"CmdType"`
		SN       int    `xml:"SN"`
		DeviceID string `xml:"DeviceID"`
		Result   string `xml:"Result"`
		Online   string `xml:"Online"`
		Status   string `xml:"Status"`
	}
	require.NoError(t, manscdp.DecodeProfiledXML(profile, response.Body(), &decoded))
	require.Equal(t, "DeviceStatus", decoded.CmdType)
	require.Equal(t, 47, decoded.SN)
	require.Equal(t, platform.LocalDeviceID, decoded.DeviceID)
	require.Equal(t, "OK", decoded.Result)
	require.Equal(t, "ONLINE", decoded.Online)
	require.Equal(t, "OK", decoded.Status)
}

func newCascadeStatusRequest(platform model.GbCascadePlatform, requestTarget, toTarget string, body []byte) *sip.Request {
	req := sip.NewRequest(sip.MESSAGE, sip.Uri{User: requestTarget, Host: platform.LocalDomain})
	params := sip.NewParams()
	params.Add("branch", sip.GenerateBranch())
	req.AppendHeader(&sip.ViaHeader{ProtocolName: "SIP", ProtocolVersion: "2.0", Transport: "UDP", Host: platform.Host, Port: platform.Port, Params: params})
	callID := sip.CallIDHeader("cascade-status-test")
	req.AppendHeader(&callID)
	req.AppendHeader(&sip.CSeqHeader{SeqNo: 1, MethodName: sip.MESSAGE})
	req.AppendHeader(&sip.FromHeader{Address: sip.Uri{User: platform.UpstreamServerID}})
	req.AppendHeader(&sip.ToHeader{Address: sip.Uri{User: toTarget}})
	req.SetSource("192.0.2.10:5060")
	req.SetTransport("UDP")
	req.SetBody(body)
	return req
}

func TestCascadeDeviceStatusQueriesOnlyPublishedResourcesAndRewritesIdentity(t *testing.T) {
	for _, version := range []string{"2016", "2022"} {
		for _, targetID := range []string{"34020000001320000012"} {
			t.Run(version+"/"+targetID, func(t *testing.T) {
				_, platform, store := cascadeDeviceStatusFixture(t, version)
				transport := &cascadeStatusTransport{requests: make(chan *sip.Request, 1)}
				clients := newCascadePlatformClientFactory(transport, nil, time.Second)
				queries := make(chan cascadeDeviceStatusTarget, 1)
				handler := newCascadeDeviceStatusHandlerWithQuery(store, clients, func(_ context.Context, target cascadeDeviceStatusTarget, _ string) (catalog.DeviceStatusFact, error) {
					queries <- target
					return catalog.DeviceStatusFact{Online: false, Working: true}, nil
				})
				profile := protocol.ProfileFor(protocol.Version(version))
				body, err := manscdp.BuildDeviceStatusQueryWithProfile(profile, targetID, 71)
				require.NoError(t, err)
				req := newCascadeStatusRequest(platform, targetID, targetID, body)
				tx := siptest.NewServerTxRecorder(req)
				defer tx.Terminate()

				require.True(t, handler(req, tx))
				require.Equal(t, 200, tx.Result()[0].StatusCode)
				select {
				case target := <-queries:
					require.Equal(t, targetID, target.PublishedID)
					require.Equal(t, uint64(10), target.SourceDeviceID)
					require.Equal(t, uint64(20), target.SourceChannelID)
					require.Equal(t, gbmodels.ControlTargetScopeChannel, target.TargetScope)
				case <-time.After(time.Second):
					t.Fatal("timed out waiting for downstream DeviceStatus query")
				}

				response := awaitCascadeStatusResponse(t, transport.requests)
				var decoded cascadeDeviceStatusResponseTestXML
				require.NoError(t, manscdp.DecodeProfiledXML(profile, response.Body(), &decoded))
				require.Equal(t, 71, decoded.SN)
				require.Equal(t, targetID, decoded.DeviceID)
				require.Equal(t, "OK", decoded.Result)
				require.Equal(t, "OFFLINE", decoded.Online)
				require.Equal(t, "OK", decoded.Status)
				require.NotContains(t, string(response.Body()), "source-only-device")
				require.NotContains(t, string(response.Body()), "source-only-channel")
			})
		}
	}
}

func TestCascadeDeviceStatusBindsSourceAuthorizationEpoch(t *testing.T) {
	for _, test := range []struct {
		name             string
		epoch, completed int
		allowed          bool
		otherOwner       bool
	}{
		{"current", 7, 7, true, false}, {"missing", 0, 0, false, false}, {"cleanup pending", 8, 7, false, false},
		{"owner mismatch", 7, 7, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, _, _ := cascadeDeviceStatusFixture(t, "2022")
			require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN access_epoch BIGINT").Error)
			require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN cleanup_completed_epoch BIGINT").Error)
			require.NoError(t, db.Exec("UPDATE gb_device SET access_epoch=?, cleanup_completed_epoch=? WHERE id=10", test.epoch, test.completed).Error)
			if test.otherOwner {
				require.NoError(t, db.Exec("UPDATE gb_channel SET owner_dept_id=1 WHERE id=20").Error)
			}
			service := &cascadeStatusEpochOperations{}
			_, err := queryCascadeDeviceStatusWith(t.Context(), cascadeDeviceStatusTarget{
				PublishedID: "34020000001320000012", SourceDeviceID: 10, SourceChannelID: 20, TargetScope: gbmodels.ControlTargetScopeChannel,
			}, "epoch-test", db, service)
			require.Error(t, err)
			require.Equal(t, test.allowed, service.called)
			if test.allowed {
				require.EqualValues(t, 7, service.target.DeviceEpoch)
			}
		})
	}
}

type cascadeStatusEpochOperations struct {
	target ptz.Target
	called bool
}

func (*cascadeStatusEpochOperations) RequiresDeviceAuthorization() bool { return true }
func (f *cascadeStatusEpochOperations) Execute(_ context.Context, target ptz.Target, _ ptz.Command) (gbmodels.GbPTZOperation, error) {
	f.called, f.target = true, target
	return gbmodels.GbPTZOperation{}, fmt.Errorf("test stopped after authorization capture")
}
func (*cascadeStatusEpochOperations) GetOperation(context.Context, string) (gbmodels.GbPTZOperation, error) {
	return gbmodels.GbPTZOperation{}, fmt.Errorf("unexpected poll")
}

func TestCascadeDeviceStatusRejectsTargetMismatchesBeforeQuery(t *testing.T) {
	tests := []struct {
		name          string
		requestTarget string
		toTarget      string
		queryTarget   string
	}{
		{name: "request URI", requestTarget: "34020000001320000011", toTarget: "34020000001320000012", queryTarget: "34020000001320000012"},
		{name: "To", requestTarget: "34020000001320000012", toTarget: "34020000001320000011", queryTarget: "34020000001320000012"},
		{name: "XML DeviceID", requestTarget: "34020000001320000012", toTarget: "34020000001320000012", queryTarget: "34020000001320000011"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, platform, store := cascadeDeviceStatusFixture(t, "2022")
			transport := &cascadeStatusTransport{requests: make(chan *sip.Request, 1)}
			queryCalls := 0
			handler := newCascadeDeviceStatusHandlerWithQuery(store, newCascadePlatformClientFactory(transport, nil, time.Second), func(context.Context, cascadeDeviceStatusTarget, string) (catalog.DeviceStatusFact, error) {
				queryCalls++
				return catalog.DeviceStatusFact{Online: true, Working: true}, nil
			})
			body, err := manscdp.BuildDeviceStatusQueryWithProfile(protocol.ProfileFor(protocol.Version2022), test.queryTarget, 72)
			require.NoError(t, err)
			req := newCascadeStatusRequest(platform, test.requestTarget, test.toTarget, body)
			tx := siptest.NewServerTxRecorder(req)
			defer tx.Terminate()

			require.True(t, handler(req, tx))
			require.Equal(t, 403, tx.Result()[0].StatusCode)
			require.Zero(t, queryCalls)
			select {
			case <-transport.requests:
				t.Fatal("mismatched target must not produce a response")
			default:
			}
		})
	}
}

func TestCascadeDeviceStatusReturnsBusinessErrorWhenQueryFactsAreMissing(t *testing.T) {
	_, platform, store := cascadeDeviceStatusFixture(t, "2016")
	transport := &cascadeStatusTransport{requests: make(chan *sip.Request, 1)}
	handler := newCascadeDeviceStatusHandlerWithQuery(store, newCascadePlatformClientFactory(transport, nil, time.Second), func(context.Context, cascadeDeviceStatusTarget, string) (catalog.DeviceStatusFact, error) {
		return catalog.DeviceStatusFact{}, fmt.Errorf("device status facts unavailable")
	})
	profile := protocol.ProfileFor(protocol.Version2016)
	body, err := manscdp.BuildDeviceStatusQueryWithProfile(profile, "34020000001320000012", 73)
	require.NoError(t, err)
	req := newCascadeStatusRequest(platform, "34020000001320000012", "34020000001320000012", body)
	tx := siptest.NewServerTxRecorder(req)
	defer tx.Terminate()

	require.True(t, handler(req, tx))
	require.Equal(t, 200, tx.Result()[0].StatusCode)
	response := awaitCascadeStatusResponse(t, transport.requests)
	var decoded cascadeDeviceStatusResponseTestXML
	require.NoError(t, manscdp.DecodeProfiledXML(profile, response.Body(), &decoded))
	require.Equal(t, 73, decoded.SN)
	require.Equal(t, "34020000001320000012", decoded.DeviceID)
	require.Equal(t, "ERROR", decoded.Result)
	require.Empty(t, decoded.Online)
	require.Empty(t, decoded.Status)
}

func TestCascadeDeviceStatusDoesNotSendAfterAuthorizationRevisionChanges(t *testing.T) {
	for _, test := range []struct {
		name    string
		updates map[string]interface{}
	}{
		{"projection changed", map[string]interface{}{"projection_revision": 2}},
		{"config changed", map[string]interface{}{"config_revision": 2}},
		{"disabled", map[string]interface{}{"enabled": false}},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, platform, store := cascadeDeviceStatusFixture(t, "2022")
			transport := &cascadeStatusTransport{requests: make(chan *sip.Request, 1)}
			started, release := make(chan struct{}), make(chan struct{})
			handler := newCascadeDeviceStatusHandlerWithQuery(store, newCascadePlatformClientFactory(transport, nil, time.Second), func(context.Context, cascadeDeviceStatusTarget, string) (catalog.DeviceStatusFact, error) {
				close(started)
				<-release
				return catalog.DeviceStatusFact{Online: true, Working: true}, nil
			})
			profile := protocol.ProfileFor(protocol.Version2022)
			body, err := manscdp.BuildDeviceStatusQueryWithProfile(profile, "34020000001320000012", 74)
			require.NoError(t, err)
			req := newCascadeStatusRequest(platform, "34020000001320000012", "34020000001320000012", body)
			tx := siptest.NewServerTxRecorder(req)
			defer tx.Terminate()

			require.True(t, handler(req, tx))
			require.Equal(t, 200, tx.Result()[0].StatusCode)
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("downstream query did not start")
			}
			require.NoError(t, db.Model(&platform).Updates(test.updates).Error)
			close(release)
			select {
			case response := <-transport.requests:
				t.Fatalf("response leaked after projection revision changed: %s", response.Body())
			case <-time.After(150 * time.Millisecond):
			}

		})
	}
}

func TestCascadeDeviceStatusFactsRequireTheExactOperation(t *testing.T) {
	db := cascadeInviteTestDB(t)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDeviceControlState{}))
	operationID := "status-operation-current"
	operation := gbmodels.GbPTZOperation{
		ID: 42, OperationID: operationID, DeviceID: 10, DeviceCode: "source-only-device", ChannelID: 20,
		ChannelCode: "source-only-channel", TargetScope: gbmodels.ControlTargetScopeChannel,
		TargetCode: "source-only-channel", SN: 75, Status: gbmodels.PTZOperationAccepted, DeviceResult: "OK",
	}
	online, selfTest := gbmodels.DeviceOnlineStateOffline, gbmodels.DeviceSelfTestOK
	sourceOperationID := operationID
	require.NoError(t, db.Create(&gbmodels.GbDeviceControlState{
		DeviceID: 10, ChannelID: 20, TargetScope: gbmodels.ControlTargetScopeChannel, TargetCode: "source-only-channel",
		OnlineState: &online, SelfTestState: &selfTest, RecordState: gbmodels.ControlStateUnknown,
		Freshness: gbmodels.ControlStateFresh, ObservedAt: time.Now(), Source: "device_status", SourceSN: 75,
		SourceOperationID: &sourceOperationID, SourceOperationSeq: 42, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}).Error)

	fact, err := executeCascadeDeviceStatusQuery(context.Background(), db, &cascadeStatusOperationsFake{operation: operation}, ptz.Target{}, ptz.Command{ResponseRequired: true})
	require.NoError(t, err)
	require.False(t, fact.Online)
	require.True(t, fact.Working)
	require.Nil(t, fact.Record)

	for _, mutate := range []func(*gbmodels.GbPTZOperation){
		func(op *gbmodels.GbPTZOperation) { op.OperationID = "other-operation" },
		func(op *gbmodels.GbPTZOperation) { op.ID++ },
		func(op *gbmodels.GbPTZOperation) { op.SN++ },
		func(op *gbmodels.GbPTZOperation) { op.DeviceID++ },
		func(op *gbmodels.GbPTZOperation) { op.TargetCode = "other-target" },
		func(op *gbmodels.GbPTZOperation) { op.TargetScope = gbmodels.ControlTargetScopeDevice },
	} {
		other := operation
		mutate(&other)
		_, err = executeCascadeDeviceStatusQuery(t.Context(), db, &cascadeStatusOperationsFake{operation: other}, ptz.Target{}, ptz.Command{ResponseRequired: true})
		require.Error(t, err, "unrelated cached facts must not satisfy this operation")
	}
}

type cascadeStatusOperationsFake struct {
	operation gbmodels.GbPTZOperation
}

func (*cascadeStatusOperationsFake) RequiresDeviceAuthorization() bool { return false }

func (f *cascadeStatusOperationsFake) Execute(_ context.Context, _ ptz.Target, command ptz.Command) (gbmodels.GbPTZOperation, error) {
	if !command.ResponseRequired {
		return gbmodels.GbPTZOperation{}, fmt.Errorf("DeviceStatus query must wait for a response")
	}
	return f.operation, nil
}

func (f *cascadeStatusOperationsFake) GetOperation(_ context.Context, operationID string) (gbmodels.GbPTZOperation, error) {
	if operationID != f.operation.OperationID {
		return gbmodels.GbPTZOperation{}, fmt.Errorf("unexpected operation id %q", operationID)
	}
	return f.operation, nil
}

type cascadeDeviceStatusResponseTestXML struct {
	SN       int    `xml:"SN"`
	DeviceID string `xml:"DeviceID"`
	Result   string `xml:"Result"`
	Online   string `xml:"Online"`
	Status   string `xml:"Status"`
}

func cascadeDeviceStatusFixture(t *testing.T, version string, suppliedDB ...*gorm.DB) (*gorm.DB, model.GbCascadePlatform, *repository.GormRepository) {
	t.Helper()
	var db *gorm.DB
	if len(suppliedDB) == 0 {
		db = cascadeInviteTestDB(t)
	} else {
		db = suppliedDB[0]
	}
	require.NoError(t, db.AutoMigrate(
		&model.GbCascadePlatform{}, &model.GbCascadeDeviceProjection{}, &model.GbCascadeChannelProjection{},
		&model.GbCascadeBusinessNode{}, &model.GbCascadeBusinessBinding{},
		&model.GbCascadeAlarmDelivery{}, &gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &civilcode.SysCivilCode{},
	))
	platform := model.GbCascadePlatform{
		Name: "status upstream", UpstreamServerID: "34020000002000000001", UpstreamDomain: "3402000000",
		LocalDeviceID: "34020000002000000002", LocalDomain: "3402000000", Host: "192.0.2.10", Port: 5060,
		Transport: "UDP", LocalSIPIP: "192.0.2.20", LocalSIPPort: 5062, Enabled: true,
		PublishPlatform: true, EffectiveVersion: version, ConfigRevision: 1, ProjectionRevision: 1,
	}
	require.NoError(t, db.Create(&platform).Error)
	require.NoError(t, db.Table("gb_device").Create(map[string]interface{}{
		"id": 10, "device_id": "source-only-device", "ip": "192.0.2.30", "port": 5060,
		"transport": "UDP", "effective_version": "2016", "status": 1, "owner_dept_id": 0,
	}).Error)
	require.NoError(t, db.Table("gb_channel").Create(map[string]interface{}{
		"id": 20, "device_id": "source-only-device", "channel_id": "source-only-channel", "status": 1, "owner_dept_id": 0,
	}).Error)
	deviceProjection := model.GbCascadeDeviceProjection{
		PlatformID: platform.ID, SourceDeviceID: 10, PublishedDeviceID: "34020000001320000011", Active: true,
	}
	require.NoError(t, db.Create(&deviceProjection).Error)
	channelProjection := model.GbCascadeChannelProjection{
		PlatformID: platform.ID, DeviceProjectionID: deviceProjection.ID, SourceChannelID: 20,
		PublishedChannelID: "34020000001320000012", ParentOverride: platform.LocalDeviceID, Active: true,
	}
	require.NoError(t, db.Create(&channelProjection).Error)
	return db, platform, repository.NewGormRepository(db)
}

func awaitCascadeStatusResponse(t *testing.T, requests <-chan *sip.Request) *sip.Request {
	t.Helper()
	select {
	case request := <-requests:
		return request
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for DeviceStatus response")
		return nil
	}
}

func TestCascadeDeviceStatusAuthorizedEndToEnd(t *testing.T) {
	for _, scenario := range []struct {
		name, version string
		revokeSQL     string
	}{
		{"2016", "2016", ""}, {"2022", "2022", ""},
		{"source revoked during query", "2022", "UPDATE gb_device SET access_epoch=2, cleanup_completed_epoch=2 WHERE id=10"},
		{"channel removed during query", "2022", "UPDATE gb_channel SET deleted_at=CURRENT_TIMESTAMP WHERE id=20"},
		{"channel owner changed during query", "2022", "UPDATE gb_channel SET owner_dept_id=1 WHERE id=20"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			db := authoritytest.OpenSQLite(t, filepath.Join(t.TempDir(), "cascade-status.sqlite"))
			_, platform, store := cascadeDeviceStatusFixture(t, scenario.version, db)
			require.NoError(t, db.AutoMigrate(&gbmodels.GbPTZOperation{}, &gbmodels.GbPTZOperationAttempt{}, &gbmodels.GbDeviceControlState{}, &playauth.DeviceOperationIntent{}))
			for _, statement := range []string{
				"ALTER TABLE gb_device ADD COLUMN access_epoch BIGINT DEFAULT 1",
				"ALTER TABLE gb_device ADD COLUMN cleanup_completed_epoch BIGINT DEFAULT 1",
				"ALTER TABLE gb_device ADD COLUMN legacy_revoked_before DATETIME NULL",
				"UPDATE gb_device SET device_id='34020000001320000001' WHERE id=10",
				"UPDATE gb_channel SET device_id='34020000001320000001', channel_id='34020000001320000002' WHERE id=20",
			} {
				require.NoError(t, db.Exec(statement).Error)
			}
			authority := authoritytest.Register(t, db, "")
			intents, err := playauth.NewAuthorizedDeviceOperationIntentStore(db, authority)
			require.NoError(t, err)
			barrier, err := playauth.NewAuthorizedDeviceOperationBarrier(playauth.NewDeviceSecurityStore(db), authority)
			require.NoError(t, err)
			downstream := make(chan []byte, 1)
			sender := cascadeRecordSenderFunc(func(_ context.Context, device, address, transport string, body []byte) (uac.TrackedMessageResult, error) {
				downstream <- append([]byte(nil), body...)
				return uac.TrackedMessageResult{CallID: "downstream-status", CSeq: "1", StatusCode: 200, Attempted: true}, nil
			})
			service, err := ptz.NewAuthorizedService(db, sender, time.Now, intents, barrier)
			require.NoError(t, err)
			t.Cleanup(service.Retire)
			oldDB, oldService, oldConfig := app.GormDbMysql, ptzService, app.ConfigYml
			app.GormDbMysql, ptzService, app.ConfigYml = db, service, cascadeInviteTestConfig{}
			t.Cleanup(func() { app.GormDbMysql, ptzService, app.ConfigYml = oldDB, oldService, oldConfig })
			scheduler := ptz.NewScheduler(service, ptz.WithSchedulerInterval(5*time.Millisecond))
			scheduler.Start(t.Context())
			t.Cleanup(scheduler.Stop)
			transport := &cascadeStatusTransport{requests: make(chan *sip.Request, 1)}
			handler := newCascadeMessageHandler(store, newCascadePlatformClientFactory(transport, nil, time.Second))
			profile := protocol.ProfileFor(protocol.Version(scenario.version))
			body, err := manscdp.BuildDeviceStatusQueryWithProfile(profile, "34020000001320000012", 97)
			require.NoError(t, err)
			req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, body)
			tx := siptest.NewServerTxRecorder(req)
			defer tx.Terminate()
			require.True(t, handler(req, tx))
			require.Equal(t, 200, tx.Result()[0].StatusCode)
			var query []byte
			select {
			case query = <-downstream:
			case <-time.After(2 * time.Second):
				t.Fatal("no downstream query")
			}
			var head struct {
				SN       int    `xml:"SN"`
				DeviceID string `xml:"DeviceID"`
				CmdType  string `xml:"CmdType"`
			}
			require.NoError(t, manscdp.DecodeProfiledXML(protocol.ProfileFor(protocol.Version2016), query, &head))
			require.Equal(t, "DeviceStatus", head.CmdType)
			require.Equal(t, "34020000001320000002", head.DeviceID)
			var operation gbmodels.GbPTZOperation
			require.Eventually(t, func() bool {
				err := db.Where("sn=?", head.SN).Take(&operation).Error
				return err == nil && operation.Status == gbmodels.PTZOperationSent
			}, time.Second, 5*time.Millisecond)
			require.NotNil(t, operation.DeviceEpoch)
			require.EqualValues(t, 1, *operation.DeviceEpoch)
			require.NotNil(t, operation.DeviceIntentID)
			if scenario.revokeSQL != "" {
				require.NoError(t, db.Exec(scenario.revokeSQL).Error)
			}
			response := []byte(fmt.Sprintf(`<Response><CmdType>DeviceStatus</CmdType><SN>%d</SN><DeviceID>%s</DeviceID><Result>OK</Result><Online>OFFLINE</Online><Status>ERROR</Status><Encode>ON</Encode><Record>OFF</Record><Alarmstatus Num="1"><Item><DeviceID>34020000001340000001</DeviceID><DutyStatus>ALARM</DutyStatus></Item></Alarmstatus></Response>`, head.SN, head.DeviceID))
			require.NoError(t, service.OnPTZMessage(t.Context(), "34020000001320000001", "device-status-response", "2", response))
			upstream := awaitCascadeStatusResponse(t, transport.requests)
			var decoded cascadeDeviceStatusResponseTestXML
			require.NoError(t, manscdp.DecodeProfiledXML(profile, upstream.Body(), &decoded))
			require.Equal(t, 97, decoded.SN)
			require.Equal(t, "34020000001320000012", decoded.DeviceID)
			if scenario.revokeSQL != "" {
				require.Equal(t, "ERROR", decoded.Result)
				require.Empty(t, decoded.Online)
				require.Empty(t, decoded.Status)
				return
			}
			require.Equal(t, "OK", decoded.Result)
			require.Equal(t, "OFFLINE", decoded.Online)
			require.Equal(t, "ERROR", decoded.Status)
			require.Contains(t, string(upstream.Body()), "<Encode>ON</Encode>")
			require.Contains(t, string(upstream.Body()), "<Record>OFF</Record>")
			require.NotContains(t, string(upstream.Body()), "Alarmstatus")
			require.NotContains(t, string(upstream.Body()), "34020000001320000001")
			require.NotContains(t, string(upstream.Body()), "34020000001320000002")
			require.NotContains(t, string(upstream.Body()), "34020000001340000001")
		})
	}
}

func TestCascadeDeviceStatusRejectsUnauthorizedQueries(t *testing.T) {
	for _, test := range []struct {
		name    string
		mutate  func(*testing.T, *gorm.DB, *model.GbCascadePlatform, *sip.Request)
		target  string
		handled bool
		status  int
	}{
		{name: "hidden device", target: "34020000001320000011", handled: true, status: 403},
		{name: "unpublished", target: "34020000001320000099", handled: true, status: 403},
		{name: "disabled", handled: true, status: 403, mutate: func(t *testing.T, db *gorm.DB, p *model.GbCascadePlatform, _ *sip.Request) {
			require.NoError(t, db.Model(p).Update("enabled", false).Error)
		}},
		{name: "different source", mutate: func(_ *testing.T, _ *gorm.DB, _ *model.GbCascadePlatform, req *sip.Request) {
			req.SetSource("192.0.2.99:5060")
		}},
		{name: "different identity", mutate: func(_ *testing.T, _ *gorm.DB, _ *model.GbCascadePlatform, req *sip.Request) {
			req.From().Address.User = "34020000002000000099"
		}},
		{name: "other upstream", handled: true, status: 403, mutate: func(t *testing.T, db *gorm.DB, p *model.GbCascadePlatform, req *sip.Request) {
			other := *p
			other.ID = 0
			other.Name = "other upstream"
			other.UpstreamServerID = "34020000002000000099"
			require.NoError(t, db.Create(&other).Error)
			req.From().Address.User = other.UpstreamServerID
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, platform, store := cascadeDeviceStatusFixture(t, "2022")
			target := test.target
			if target == "" {
				target = "34020000001320000012"
			}
			body, err := manscdp.BuildDeviceStatusQueryWithProfile(protocol.ProfileFor(protocol.Version2022), target, 98)
			require.NoError(t, err)
			req := newCascadeStatusRequest(platform, target, target, body)
			if test.mutate != nil {
				test.mutate(t, db, &platform, req)
			}
			tx := siptest.NewServerTxRecorder(req)
			defer tx.Terminate()
			transport := &cascadeStatusTransport{requests: make(chan *sip.Request, 1)}
			handler := newCascadeDeviceStatusHandlerWithQuery(store, newCascadePlatformClientFactory(transport, nil, time.Second), func(context.Context, cascadeDeviceStatusTarget, string) (catalog.DeviceStatusFact, error) {
				t.Error("unauthorized query reached downstream executor")
				return catalog.DeviceStatusFact{}, fmt.Errorf("unauthorized")
			})
			require.Equal(t, test.handled, handler(req, tx))
			if test.handled {
				require.Len(t, tx.Result(), 1)
				require.Equal(t, test.status, tx.Result()[0].StatusCode)
			} else {
				require.Empty(t, tx.Result())
			}
			require.Empty(t, transport.requests)
		})
	}
}

func TestCascadeDeviceStatusExecutorFailureAndTimeout(t *testing.T) {
	for _, status := range []gbmodels.PTZOperationStatus{gbmodels.PTZOperationRejected, gbmodels.PTZOperationTimeout, gbmodels.PTZOperationCancelled, gbmodels.PTZOperationUnknown, gbmodels.PTZOperationQueued, gbmodels.PTZOperationAccepted} {
		t.Run(string(status), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
			defer cancel()
			_, err := executeCascadeDeviceStatusQuery(ctx, cascadeInviteTestDB(t), &cascadeStatusOperationsFake{operation: gbmodels.GbPTZOperation{OperationID: "failure", Status: status, DeviceResult: "ERROR"}}, ptz.Target{}, ptz.Command{ResponseRequired: true})
			require.Error(t, err)
			if status == gbmodels.PTZOperationQueued {
				require.ErrorIs(t, err, context.DeadlineExceeded)
			}
		})
	}
}

func TestCascadeDeviceStatusRejectsUnknownRequiredFacts(t *testing.T) {
	for _, test := range []struct {
		name             string
		online, selfTest *string
	}{
		{name: "missing online", selfTest: cascadeStatusString(gbmodels.DeviceSelfTestOK)},
		{name: "unknown online", online: cascadeStatusString("unknown"), selfTest: cascadeStatusString(gbmodels.DeviceSelfTestOK)},
		{name: "missing self-test", online: cascadeStatusString(gbmodels.DeviceOnlineStateOnline)},
		{name: "unknown self-test", online: cascadeStatusString(gbmodels.DeviceOnlineStateOnline), selfTest: cascadeStatusString("unknown")},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := cascadeInviteTestDB(t)
			require.NoError(t, db.AutoMigrate(&gbmodels.GbDeviceControlState{}))
			id := "required-facts"
			op := gbmodels.GbPTZOperation{ID: 1, OperationID: id, DeviceID: 10, TargetScope: gbmodels.ControlTargetScopeChannel, TargetCode: "source", SN: 1}
			require.NoError(t, db.Create(&gbmodels.GbDeviceControlState{
				DeviceID: 10, TargetScope: op.TargetScope, TargetCode: op.TargetCode, OnlineState: test.online, SelfTestState: test.selfTest,
				Freshness: gbmodels.ControlStateFresh, Source: "device_status", SourceSN: 1, SourceOperationID: &id, SourceOperationSeq: 1,
			}).Error)
			_, err := cascadeDeviceStatusFactForOperation(t.Context(), db, op)
			require.Error(t, err)
		})
	}
}

func cascadeStatusString(value string) *string { return &value }
