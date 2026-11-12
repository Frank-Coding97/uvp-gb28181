package gb28181

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
)

type cascadeCatalogAckTx struct {
	*siptest.ServerTxRecorder
	statuses []int
	err      error
}

func (tx *cascadeCatalogAckTx) Respond(response *sip.Response) error {
	tx.statuses = append(tx.statuses, response.StatusCode)
	if tx.err != nil {
		return tx.err
	}
	return tx.ServerTxRecorder.Respond(response)
}

func TestCascadeCatalogAcknowledgesBeforeSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name              string
		changePlatform    func(*model.GbCascadePlatform)
		changeRequest     func(*sip.Request)
		ackErr            error
		snapshotErr       error
		clientUnavailable bool
		wantStatus        int
		wantSnapshot      bool
		wantMessages      int
		wantFailure       bool
	}{
		{name: "valid query", wantStatus: 200, wantSnapshot: true, wantMessages: 1},
		{
			name: "unknown upstream", wantStatus: 403, wantFailure: true,
			changeRequest: func(req *sip.Request) { req.From().Address.User = "unknown" },
		},
		{
			name: "disabled upstream", wantStatus: 403, wantFailure: true,
			changePlatform: func(p *model.GbCascadePlatform) { p.Enabled = false },
		},
		{
			name: "invalid SN", wantStatus: 400, wantFailure: true,
			changeRequest: func(req *sip.Request) {
				req.SetBody([]byte(strings.Replace(string(req.Body()), "<SN>91</SN>", "<SN>0</SN>", 1)))
			},
		},
		{
			name: "empty DeviceID", wantStatus: 400, wantFailure: true,
			changeRequest: func(req *sip.Request) {
				req.SetBody([]byte(strings.Replace(string(req.Body()), catalogBusinessLocalID, "", 1)))
			},
		},
		{
			name: "acknowledgement fails", wantStatus: 200,
			ackErr: errors.New("SIP response transport failed"),
		},
		{
			name: "snapshot fails after acknowledgement", wantStatus: 200, wantSnapshot: true, wantFailure: true,
			snapshotErr: errors.New("catalog database unavailable"),
		},
		{
			name: "client fails after acknowledgement", wantStatus: 200, wantSnapshot: true, wantFailure: true,
			clientUnavailable: true,
		},
		{
			name: "planning fails after acknowledgement", wantStatus: 200, wantSnapshot: true, wantFailure: true,
			changePlatform: func(p *model.GbCascadePlatform) { p.CatalogBatchSize = -1 },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observed := observeCascadeLogs(t)
			db := cascadeInviteTestDB(t)
			require.NoError(t, db.AutoMigrate(
				&model.GbCascadePlatform{}, &model.GbCascadeDeviceProjection{}, &model.GbCascadeChannelProjection{},
				&model.GbCascadeBusinessNode{}, &model.GbCascadeBusinessBinding{},
			))
			platform := model.GbCascadePlatform{
				Name: "upstream", UpstreamServerID: "35020000002000000001", UpstreamDomain: "3502000000",
				Host: "192.168.10.220", Port: 15060, LocalDeviceID: catalogBusinessLocalID, LocalDomain: "3402000000",
				LocalSIPIP: "192.168.10.106", LocalSIPPort: 5060, Transport: "UDP", ProfileOverride: model.CascadeProfileOverride2016,
				PublishPlatform: true, CatalogBatchSize: 1, Enabled: true, ConfigRevision: 1, ProjectionRevision: 3,
			}
			if tc.changePlatform != nil {
				tc.changePlatform(&platform)
			}
			require.NoError(t, db.Create(&platform).Error)
			req := newCascadeCatalogRequest(platform, []byte("<Query><CmdType>Catalog</CmdType><SN>91</SN><DeviceID>"+catalogBusinessLocalID+"</DeviceID></Query>"))
			if tc.changeRequest != nil {
				tc.changeRequest(req)
			}
			tx := &cascadeCatalogAckTx{ServerTxRecorder: siptest.NewServerTxRecorder(req), err: tc.ackErr}
			defer tx.Terminate()
			snapshotStarted := false
			require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:catalog_ack_before_snapshot", func(query *gorm.DB) {
				if query.Statement.Table != (model.GbCascadeDeviceProjection{}).TableName() {
					return
				}
				snapshotStarted = true
				// Check ordering at the database boundary, without a wall-clock deadline.
				require.Equal(t, []int{sip.StatusOK}, tx.statuses, "SIP 200 must precede catalog projection reads")
				if tc.snapshotErr != nil {
					query.AddError(tc.snapshotErr)
				}
			}))
			transport := &catalogBusinessTransport{}
			clients := newCascadePlatformClientFactory(transport, nil, time.Second)
			if tc.clientUnavailable {
				clients = nil
			}
			handler := newCascadeCatalogHandler(repository.NewGormRepository(db), clients)

			require.True(t, handler(req, tx))
			require.Equal(t, []int{tc.wantStatus}, tx.statuses, "send exactly one final SIP response")
			require.Equal(t, tc.wantSnapshot, snapshotStarted)
			require.Len(t, transport.requests, tc.wantMessages)
			failures := observed.FilterField(zap.String("event", "cascade.catalog.query_failed")).All()
			if tc.wantFailure {
				require.Len(t, failures, 1)
				require.NotEmpty(t, failures[0].ContextMap()["error"])
				require.Equal(t, "business-catalog-call", failures[0].ContextMap()["call_id"])
			} else {
				require.Empty(t, failures)
			}
		})
	}
}
