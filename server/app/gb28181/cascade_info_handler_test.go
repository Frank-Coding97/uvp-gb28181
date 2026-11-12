package gb28181

import (
	"context"
	"encoding/xml"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/catalog"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/sipclient"
	"uvplatform.com/uvp-gb28181/app/gb28181/civilcode"
	gbconfig "uvplatform.com/uvp-gb28181/app/gb28181/config"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

type cascadeInfoTransport struct{ requests []*sip.Request }

func (f *cascadeInfoTransport) Do(_ context.Context, req *sip.Request) (*sip.Response, error) {
	f.requests = append(f.requests, req)
	return sip.NewResponseFromRequest(req, 200, "OK", nil), nil
}
func (f *cascadeInfoTransport) DoDigestAuth(ctx context.Context, req *sip.Request, _ *sip.Response, _ sipclient.Credentials) (*sip.Response, error) {
	return f.Do(ctx, req)
}

func TestCascadeInfoHandlerPublishesLocalMetadataAndRejectsUnknownTarget(t *testing.T) {
	db := cascadeInviteTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.GbCascadePlatform{}, &model.GbCascadeDeviceProjection{}, &model.GbCascadeChannelProjection{}, &model.GbCascadeBusinessNode{}, &model.GbCascadeBusinessBinding{}))
	p := model.GbCascadePlatform{Name: "remote alias", UpstreamServerID: "34020000002000000001", UpstreamDomain: "3402000000", LocalDeviceID: "34020000002000000002", LocalDomain: "3402000000", Host: "192.0.2.10", Port: 5060, Transport: "UDP", LocalSIPIP: "192.0.2.20", LocalSIPPort: 5062, Enabled: true, PublishPlatform: true}
	require.NoError(t, db.Create(&p).Error)
	transport := &cascadeInfoTransport{}
	handler := newCascadeInfoHandler(repository.NewGormRepository(db), newCascadePlatformClientFactory(transport, nil, time.Second))
	request := func(target string) *sip.Request {
		req := sip.NewRequest(sip.MESSAGE, sip.Uri{User: target, Host: p.LocalDomain})
		params := sip.NewParams()
		params.Add("branch", sip.GenerateBranch())
		req.AppendHeader(&sip.ViaHeader{ProtocolName: "SIP", ProtocolVersion: "2.0", Transport: "UDP", Host: p.Host, Port: p.Port, Params: params})
		callID := sip.CallIDHeader("info-test")
		req.AppendHeader(&callID)
		req.AppendHeader(&sip.CSeqHeader{SeqNo: 1, MethodName: sip.MESSAGE})
		req.AppendHeader(&sip.FromHeader{Address: sip.Uri{User: p.UpstreamServerID}})
		req.AppendHeader(&sip.ToHeader{Address: sip.Uri{User: target}})
		req.SetSource("192.0.2.10:5060")
		req.SetTransport("UDP")
		body, err := manscdp.MarshalProfiledXML(protocol.ProfileFor(protocol.Version2016), struct {
			XMLName  xml.Name `xml:"Query"`
			CmdType  string   `xml:"CmdType"`
			SN       int      `xml:"SN"`
			DeviceID string   `xml:"DeviceID"`
		}{CmdType: manscdp.CmdDeviceInfo, SN: 3, DeviceID: target})
		require.NoError(t, err)
		req.SetBody(body)
		return req
	}
	req := request(p.LocalDeviceID)
	tx := siptest.NewServerTxRecorder(req)
	defer tx.Terminate()
	require.True(t, handler(req, tx))
	require.Len(t, transport.requests, 1)
	var response struct {
		DeviceName string `xml:"DeviceName"`
		Channel    int    `xml:"Channel"`
		SN         int    `xml:"SN"`
	}
	require.NoError(t, manscdp.DecodeProfiledXML(protocol.ProfileFor(protocol.Version2016), transport.requests[0].Body(), &response))
	require.Equal(t, gbconfig.DefaultCascadeSettings().PlatformName, response.DeviceName)
	require.Equal(t, 3, response.SN)
	require.Zero(t, response.Channel)
	req = request("34020000001329999999")
	tx2 := siptest.NewServerTxRecorder(req)
	defer tx2.Terminate()
	require.True(t, handler(req, tx2))
	require.Len(t, transport.requests, 1, "unknown target must not disclose metadata")
	snapshot, err := buildCascadeCatalogSnapshot(context.Background(), repository.NewGormRepository(db), &p)
	require.NoError(t, err)
	require.Len(t, snapshot.Items, 1)
	require.Equal(t, catalog.CatalogItemPlatform, snapshot.Items[0].Kind)
	require.Equal(t, gbconfig.DefaultCascadeSettings().PlatformName, snapshot.Items[0].Name)

	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &civilcode.SysCivilCode{}))
	require.NoError(t, db.Table("gb_device").Create(map[string]any{"id": 10, "device_id": "source-device", "status": 1}).Error)
	require.NoError(t, db.Table("gb_channel").Create(map[string]any{"id": 20, "device_id": "source-device", "channel_id": "source-channel", "status": 1, "manufacturer": "CameraVendor", "model": "CameraModel", "civil_code": "340203"}).Error)
	require.NoError(t, db.Table("gb_channel").Create(map[string]any{"id": 21, "device_id": "source-device", "channel_id": "private-channel", "status": 1, "civil_code": "440100"}).Error)
	d := model.GbCascadeDeviceProjection{PlatformID: p.ID, SourceDeviceID: 10, PublishedDeviceID: "34020000001320000001", Active: true}
	require.NoError(t, db.Create(&d).Error)
	c := model.GbCascadeChannelProjection{PlatformID: p.ID, DeviceProjectionID: d.ID, SourceChannelID: 20, PublishedChannelID: "34020000001320000011", Name: "shared camera", ParentOverride: p.LocalDeviceID, Active: true}
	require.NoError(t, db.Create(&c).Error)
	p.PublishCivil = true
	require.NoError(t, db.Model(&p).Update("publish_civil", true).Error)
	snapshot, err = buildCascadeCatalogSnapshot(context.Background(), repository.NewGormRepository(db), &p)
	require.NoError(t, err)
	require.True(t, snapshot.HasPublishedID("340203"))
	require.False(t, snapshot.HasPublishedID("440100"), "unshared regions must stay private")
	req = request(c.PublishedChannelID)
	tx3 := siptest.NewServerTxRecorder(req)
	defer tx3.Terminate()
	require.True(t, handler(req, tx3))
	require.Len(t, transport.requests, 2)
	var channelInfo struct {
		Name         string `xml:"DeviceName"`
		Manufacturer string `xml:"Manufacturer"`
		Model        string `xml:"Model"`
	}
	require.NoError(t, manscdp.DecodeProfiledXML(protocol.ProfileFor(protocol.Version2016), transport.requests[1].Body(), &channelInfo))
	require.Equal(t, "shared camera", channelInfo.Name)
	require.Equal(t, "CameraVendor", channelInfo.Manufacturer)
	require.Equal(t, "CameraModel", channelInfo.Model)
}
