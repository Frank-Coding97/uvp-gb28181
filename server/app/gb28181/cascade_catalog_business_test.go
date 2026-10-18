package gb28181

import (
	"context"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/catalog"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/sipclient"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	basemodels "uvplatform.com/uvp-gb28181/app/models"
)

const (
	catalogBusinessLocalID = "34020000002000000001"
	catalogBusinessRootID  = "34020000002150000001"
	catalogBusinessOrgID   = "34020000002160000001"
)

type catalogBusinessTransport struct {
	requests []*sip.Request
}

func (t *catalogBusinessTransport) Do(_ context.Context, request *sip.Request) (*sip.Response, error) {
	t.requests = append(t.requests, request.Clone())
	return sip.NewResponse(sip.StatusOK, "OK"), nil
}

func (t *catalogBusinessTransport) DoDigestAuth(_ context.Context, _ *sip.Request, _ *sip.Response, _ sipclient.Credentials) (*sip.Response, error) {
	return sip.NewResponse(sip.StatusOK, "OK"), nil
}

func TestCascadeCatalogHandlerAndPusherUseBusinessSnapshot(t *testing.T) {
	db := cascadeInviteTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&model.GbCascadePlatform{}, &model.GbCascadeDeviceProjection{}, &model.GbCascadeChannelProjection{},
		&model.GbCascadeBusinessNode{}, &model.GbCascadeBusinessBinding{}, &gbmodels.GbDevice{}, &gbmodels.GbChannel{},
	))
	platform := model.GbCascadePlatform{
		Name: "business upstream", UpstreamServerID: "35020000002000000001", UpstreamDomain: "3502000000",
		Host: "192.168.10.220", Port: 15060, LocalDeviceID: catalogBusinessLocalID, LocalDomain: "3402000000",
		LocalSIPIP: "192.168.10.106", LocalSIPPort: 5060, Transport: "UDP", ProfileOverride: model.CascadeProfileOverride2016,
		PublishPlatform: true, PublishGroup: true, Enabled: true, ConfigRevision: 1, ProjectionRevision: 3,
	}
	platform.ID = 7
	require.NoError(t, db.Create(&platform).Error)
	require.NoError(t, db.Create(&model.GbCascadeBusinessNode{ID: 10, PlatformID: platform.ID, Code: catalogBusinessRootID, Name: "root", Kind: "group"}).Error)
	require.NoError(t, db.Create(&model.GbCascadeBusinessNode{ID: 11, PlatformID: platform.ID, Code: catalogBusinessOrgID, Name: "org", Kind: "organization", ParentCode: catalogBusinessRootID}).Error)
	device := gbmodels.GbDevice{BaseModel: basemodels.BaseModel{ID: 301}, DeviceID: "37010301021180000007", Name: "physical device", Status: 1}
	channel := gbmodels.GbChannel{ID: 401, DeviceID: device.DeviceID, ChannelID: "34020000001320000011", Name: "assigned channel", Status: 1}
	unmounted := gbmodels.GbChannel{ID: 402, DeviceID: device.DeviceID, ChannelID: "34020000001320000012", Name: "unmounted channel", Status: 1}
	require.NoError(t, db.Create(&device).Error)
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&unmounted).Error)
	require.NoError(t, db.Create(&model.GbCascadeDeviceProjection{ID: 20, PlatformID: platform.ID, SourceDeviceID: uint64(device.ID), PublishedDeviceID: device.DeviceID, Name: device.Name, Active: true}).Error)
	require.NoError(t, db.Create(&model.GbCascadeChannelProjection{ID: 30, PlatformID: platform.ID, DeviceProjectionID: 20, SourceChannelID: uint64(channel.ID), PublishedChannelID: channel.ChannelID, Name: channel.Name, Active: true}).Error)
	require.NoError(t, db.Create(&model.GbCascadeChannelProjection{ID: 31, PlatformID: platform.ID, DeviceProjectionID: 20, SourceChannelID: uint64(unmounted.ID), PublishedChannelID: unmounted.ChannelID, Name: unmounted.Name, Active: true}).Error)
	require.NoError(t, db.Create(&model.GbCascadeBusinessBinding{ID: 40, PlatformID: platform.ID, SourceChannelID: uint64(channel.ID), NodeCode: catalogBusinessOrgID}).Error)

	transport := &catalogBusinessTransport{}
	clients := newCascadePlatformClientFactory(transport, nil, time.Second)
	store := repository.NewGormRepository(db)
	snapshot, err := buildCascadeCatalogSnapshot(t.Context(), store, &platform)
	require.NoError(t, err)
	require.Len(t, snapshot.Items, 5)
	require.Equal(t, []catalog.CatalogItemKind{catalog.CatalogItemPlatform, catalog.CatalogItemBusinessGroup, catalog.CatalogItemVirtualOrganization, catalog.CatalogItemChannel, catalog.CatalogItemChannel}, []catalog.CatalogItemKind{
		snapshot.Items[0].Kind, snapshot.Items[1].Kind, snapshot.Items[2].Kind, snapshot.Items[3].Kind, snapshot.Items[4].Kind,
	})
	require.Equal(t, catalogBusinessLocalID+"/"+catalogBusinessOrgID, snapshot.Items[3].ParentID)
	require.Equal(t, catalogBusinessLocalID, snapshot.Items[4].ParentID, "unmounted shared channels use the platform as their parent")
	require.False(t, snapshot.HasPublishedID(device.DeviceID))

	queryBody, err := manscdp.BuildCatalogQueryWithProfile(protocol.ProfileFor(protocol.Version2016), catalogBusinessRootID, 91)
	require.NoError(t, err)
	req := newCascadeCatalogRequest(platform, queryBody)
	tx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(req), make(chan *sip.Response, 4)}
	defer tx.Terminate()
	handler := newCascadeCatalogHandler(store, clients)
	require.True(t, handler(req, tx))
	require.Equal(t, sip.StatusOK, (<-tx.responses).StatusCode)
	var queryIDs []string
	for _, request := range transport.requests {
		var response manscdp.CatalogResponse
		require.NoError(t, manscdp.DecodeProfiledXML(protocol.ProfileFor(protocol.Version2016), request.Body(), &response))
		require.Equal(t, 91, response.SN)
		require.Equal(t, 3, response.SumNum)
		queryIDs = append(queryIDs, catalogResponseItemIDs(response)...)
		var wire struct {
			DeviceList struct {
				Items []struct {
					DeviceID        string `xml:"DeviceID"`
					ParentID        string `xml:"ParentID"`
					BusinessGroupID string `xml:"BusinessGroupID"`
					Info            struct {
						BusinessGroupID string `xml:"BusinessGroupID"`
					} `xml:"Info"`
				} `xml:"Item"`
			} `xml:"DeviceList"`
		}
		require.NoError(t, manscdp.DecodeProfiledXML(protocol.ProfileFor(protocol.Version2016), request.Body(), &wire))
		for _, item := range wire.DeviceList.Items {
			if item.DeviceID == catalogBusinessOrgID {
				require.Empty(t, item.BusinessGroupID, "2016 BusinessGroupID must not appear at Item level")
				require.Equal(t, catalogBusinessRootID, item.Info.BusinessGroupID)
			}
			if item.DeviceID == channel.ChannelID {
				require.Equal(t, catalogBusinessLocalID+"/"+catalogBusinessOrgID, item.ParentID)
				require.Emptyf(t, item.BusinessGroupID, "channel %s should not declare BusinessGroupID", item.DeviceID)
				require.Emptyf(t, item.Info.BusinessGroupID, "channel %s should not declare BusinessGroupID in Info", item.DeviceID)
			}
		}
	}
	require.Equal(t, []string{catalogBusinessRootID, catalogBusinessOrgID, channel.ChannelID}, queryIDs)

	// A platform may address a published business node directly in both the
	// Request-URI and To header. The XML target is still scoped to that node.
	transport.requests = nil
	directOrgQuery, err := manscdp.BuildCatalogQueryWithProfile(protocol.ProfileFor(protocol.Version2016), catalogBusinessOrgID, 92)
	require.NoError(t, err)
	directOrgReq := newCascadeCatalogRequest(platform, directOrgQuery)
	directOrgReq.Recipient.User = catalogBusinessOrgID
	directOrgReq.To().Address.User = catalogBusinessOrgID
	directOrgTx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(directOrgReq), make(chan *sip.Response, 4)}
	defer directOrgTx.Terminate()
	require.True(t, handler(directOrgReq, directOrgTx))
	require.Equal(t, sip.StatusOK, (<-directOrgTx.responses).StatusCode)
	var directIDs []string
	for _, request := range transport.requests {
		var response manscdp.CatalogResponse
		require.NoError(t, manscdp.DecodeProfiledXML(protocol.ProfileFor(protocol.Version2016), request.Body(), &response))
		require.Equal(t, 2, response.SumNum)
		directIDs = append(directIDs, catalogResponseItemIDs(response)...)
	}
	require.Equal(t, []string{catalogBusinessOrgID, channel.ChannelID}, directIDs)

	// Conflicting non-container targets must not broaden the response to the
	// business root. A SIP 200 is only the MESSAGE acknowledgement; no Catalog
	// response is sent for the rejected scoped target.
	transport.requests = nil
	conflictQuery, err := manscdp.BuildCatalogQueryWithProfile(protocol.ProfileFor(protocol.Version2016), catalogBusinessRootID, 93)
	require.NoError(t, err)
	conflictReq := newCascadeCatalogRequest(platform, conflictQuery)
	conflictReq.Recipient.User = catalogBusinessOrgID
	conflictReq.To().Address.User = catalogBusinessOrgID
	conflictTx := &cascadeInviteTestTx{siptest.NewServerTxRecorder(conflictReq), make(chan *sip.Response, 4)}
	defer conflictTx.Terminate()
	require.True(t, handler(conflictReq, conflictTx))
	require.Equal(t, sip.StatusOK, (<-conflictTx.responses).StatusCode)
	require.Empty(t, transport.requests)

	transport.requests = nil
	items, batches, err := newCascadeCatalogPusher(store, clients).PushCatalog(t.Context(), platform.ID)
	require.NoError(t, err)
	require.Equal(t, 5, items)
	require.Equal(t, len(transport.requests), batches)
	var pushedIDs []string
	for _, request := range transport.requests {
		var pushed manscdp.CatalogResponse
		require.NoError(t, manscdp.DecodeProfiledXML(protocol.ProfileFor(protocol.Version2016), request.Body(), &pushed))
		require.Equal(t, 5, pushed.SumNum)
		require.Equal(t, catalogBusinessLocalID, pushed.DeviceID)
		pushedIDs = append(pushedIDs, catalogResponseItemIDs(pushed)...)
	}
	require.Equal(t, []string{catalogBusinessLocalID, catalogBusinessRootID, catalogBusinessOrgID, channel.ChannelID, unmounted.ChannelID}, pushedIDs)
}

func TestBuildCascadeCatalogSnapshotKeepsLegacyFlatOutputForPublishGroupWithoutTree(t *testing.T) {
	db := cascadeInviteTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&model.GbCascadePlatform{}, &model.GbCascadeDeviceProjection{}, &model.GbCascadeChannelProjection{},
		&model.GbCascadeBusinessNode{}, &model.GbCascadeBusinessBinding{}, &gbmodels.GbDevice{}, &gbmodels.GbChannel{},
	))
	platform := model.GbCascadePlatform{
		ID: 9, Name: "legacy", UpstreamServerID: "35020000002000000009", UpstreamDomain: "3502000000", Host: "192.168.10.220", Port: 15060,
		LocalDeviceID: catalogBusinessLocalID, LocalDomain: "3402000000", LocalSIPIP: "192.168.10.106", LocalSIPPort: 5060,
		Transport: "UDP", PublishGroup: true, Enabled: true, ConfigRevision: 1, ProjectionRevision: 0,
	}
	require.NoError(t, db.Create(&platform).Error)
	device := gbmodels.GbDevice{BaseModel: basemodels.BaseModel{ID: 501}, DeviceID: "37010301021180000008", Status: 1}
	channel := gbmodels.GbChannel{ID: 601, DeviceID: device.DeviceID, ChannelID: "34020000001320000021", Status: 1}
	require.NoError(t, db.Create(&device).Error)
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.GbCascadeDeviceProjection{ID: 51, PlatformID: platform.ID, SourceDeviceID: uint64(device.ID), PublishedDeviceID: device.DeviceID, Active: true}).Error)
	require.NoError(t, db.Create(&model.GbCascadeChannelProjection{ID: 61, PlatformID: platform.ID, DeviceProjectionID: 51, SourceChannelID: uint64(channel.ID), PublishedChannelID: channel.ChannelID, Active: true}).Error)
	snapshot, err := buildCascadeCatalogSnapshot(t.Context(), repository.NewGormRepository(db), &platform)
	require.NoError(t, err)
	require.Equal(t, []catalog.CatalogItem{{ID: channel.ChannelID, Kind: catalog.CatalogItemChannel, Name: channel.Name, Status: catalog.ResourceStatusOnline}}, snapshot.Items)
}

func newCascadeCatalogRequest(platform model.GbCascadePlatform, body []byte) *sip.Request {
	req := sip.NewRequest(sip.MESSAGE, sip.Uri{User: platform.LocalDeviceID, Host: platform.LocalDomain})
	req.AppendHeader(sip.NewHeader("Via", "SIP/2.0/UDP 192.168.10.220:15060;branch=z9hG4bK-business-catalog"))
	req.AppendHeader(&sip.FromHeader{Address: sip.Uri{User: platform.UpstreamServerID, Host: platform.UpstreamDomain}})
	req.AppendHeader(&sip.ToHeader{Address: sip.Uri{User: platform.LocalDeviceID, Host: platform.LocalDomain}})
	req.AppendHeader(sip.NewHeader("Call-ID", "business-catalog-call"))
	req.AppendHeader(&sip.CSeqHeader{SeqNo: 1, MethodName: sip.MESSAGE})
	req.SetSource("192.168.10.220:15060")
	req.SetTransport("UDP")
	req.SetBody(body)
	return req
}

func catalogResponseItemIDs(response manscdp.CatalogResponse) []string {
	ids := make([]string, 0, len(response.DeviceList.Items))
	for _, item := range response.DeviceList.Items {
		ids = append(ids, item.DeviceID)
	}
	return ids
}
