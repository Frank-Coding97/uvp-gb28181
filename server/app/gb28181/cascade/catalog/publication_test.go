package catalog

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

func TestPublicationUsesAuthorizedSourceChannelMetadata(t *testing.T) {
	p := model.GbCascadePlatform{ID: 1, LocalDeviceID: "34020000002000000001", PublishCivil: true}
	d := model.GbCascadeDeviceProjection{ID: 1, PlatformID: 1, SourceDeviceID: 10, PublishedDeviceID: "34020000001320000001", Active: true}
	c := model.GbCascadeChannelProjection{PlatformID: 1, DeviceProjectionID: 1, SourceChannelID: 20, PublishedChannelID: "34020000001320000011", Name: "published name", Active: true}
	facts := SourceFacts{ChannelMetadata: map[uint64]ChannelMetadata{
		20:  {Manufacturer: "CameraVendor", Model: "CameraModel", Owner: "CameraOwner", CivilCode: "340203"},
		999: {Manufacturer: "unauthorized", CivilCode: "440104"},
	}}
	snapshot, err := Build(repository.ProjectionSnapshot{Platform: p, Devices: []model.GbCascadeDeviceProjection{d}, Channels: []model.GbCascadeChannelProjection{c}}, facts)
	require.NoError(t, err)
	for _, version := range []protocol.Version{protocol.Version2016, protocol.Version2022} {
		profile := protocol.ProfileFor(version)
		out := snapshot.Publish(p, PublicationSettings{CivilCode: "110101"})
		query, err := manscdp.BuildCatalogQueryWithProfile(profile, p.LocalDeviceID, 10)
		require.NoError(t, err)
		responses, err := PlanCatalogResponses(profile, p.LocalDeviceID, out, 64, query)
		require.NoError(t, err)
		var decoded struct {
			Items []struct {
				ID           string `xml:"DeviceID"`
				Name         string `xml:"Name"`
				Manufacturer string `xml:"Manufacturer"`
				Model        string `xml:"Model"`
				Owner        string `xml:"Owner"`
				CivilCode    string `xml:"CivilCode"`
			} `xml:"DeviceList>Item"`
		}
		require.NoError(t, manscdp.DecodeProfiledXML(profile, responses[0].Body, &decoded))
		found := false
		var ids []string
		for _, item := range decoded.Items {
			ids = append(ids, item.ID)
			if item.ID != c.PublishedChannelID {
				continue
			}
			found = true
			require.Equal(t, "published name", item.Name)
			require.Equal(t, "CameraVendor", item.Manufacturer)
			require.Equal(t, "CameraModel", item.Model)
			require.Equal(t, "CameraOwner", item.Owner)
			require.Equal(t, "340203", item.CivilCode)
		}
		require.True(t, found)
		require.Equal(t, []string{"34", "3402", "340203", c.PublishedChannelID}, ids, "unshared metadata must not create administrative directories")
		require.NotContains(t, string(responses[0].Body), "unauthorized")
	}
}

func TestPublicationMetadataSourceIsIndependentOfParentOverride(t *testing.T) {
	p := model.GbCascadePlatform{ID: 1, LocalDeviceID: "34020000002000000001", PublishPlatform: true}
	d := model.GbCascadeDeviceProjection{ID: 1, PlatformID: 1, SourceDeviceID: 10, PublishedDeviceID: "34020000001320000001", Manufacturer: "CameraVendor", CivilCode: "340203", Address: "camera address", Secrecy: 1, Active: true}
	c := model.GbCascadeChannelProjection{PlatformID: 1, DeviceProjectionID: 1, SourceChannelID: 20, PublishedChannelID: "34020000001320000011", ParentOverride: p.LocalDeviceID, Active: true}
	snapshot, err := Build(repository.ProjectionSnapshot{Platform: p, Devices: []model.GbCascadeDeviceProjection{d}, Channels: []model.GbCascadeChannelProjection{c}}, SourceFacts{})
	require.NoError(t, err)
	out := snapshot.Publish(p, PublicationSettings{CivilCode: "110101"})
	found := false
	for _, item := range out.Items {
		if item.Kind != CatalogItemChannel {
			continue
		}
		found = true
		require.Equal(t, "CameraVendor", item.Manufacturer)
		require.Equal(t, "340203", item.CivilCode)
		require.Equal(t, "camera address", item.Address)
		require.Equal(t, 1, item.Secrecy)
	}
	require.True(t, found)
}

func TestPublicationUsesLocalMetadataAndOnlyAuthorizedChannels(t *testing.T) {
	p := model.GbCascadePlatform{ID: 1, Name: "upstream alias", LocalDeviceID: "34020000002000000001", PublishPlatform: true, PublishCivil: true}
	d := model.GbCascadeDeviceProjection{ID: 1, PlatformID: 1, SourceDeviceID: 10, PublishedDeviceID: "34020000001320000001", Manufacturer: "CameraVendor", CivilCode: "340203", Active: true}
	c := model.GbCascadeChannelProjection{PlatformID: 1, DeviceProjectionID: 1, SourceChannelID: 20, PublishedChannelID: "34020000001320000011", Active: true}
	s, err := Build(repository.ProjectionSnapshot{Platform: p, Devices: []model.GbCascadeDeviceProjection{d}, Channels: []model.GbCascadeChannelProjection{c}}, SourceFacts{})
	require.NoError(t, err)
	settings := PublicationSettings{PlatformName: "local platform", Manufacturer: "UVP", Model: "Gateway", CivilCode: "340200", Address: "platform address", CivilName: func(code string) string { return "region " + code }}
	out := s.Publish(p, settings)
	require.Equal(t, 5, out.SumNum)
	require.Equal(t, "local platform", out.Items[0].Name)
	require.Equal(t, "UVP", out.Items[0].Manufacturer)
	require.Equal(t, "platform address", out.Items[0].Address)
	require.False(t, out.HasPublishedID(d.PublishedDeviceID))
	var channel CatalogItem
	for _, item := range out.Items {
		if item.ID == c.PublishedChannelID {
			channel = item
		}
	}
	require.Equal(t, "CameraVendor", channel.Manufacturer)
	require.Equal(t, "340203", channel.CivilCode)
	require.Equal(t, p.LocalDeviceID, channel.ParentID)
	_, ok := out.LookupPublishedChannel(c.PublishedChannelID)
	require.True(t, ok)
	_, ok = out.LookupPublishedChannel("34020000001329999999")
	require.False(t, ok)
	for _, version := range []protocol.Version{protocol.Version2016, protocol.Version2022} {
		profile := protocol.ProfileFor(version)
		query, err := manscdp.BuildCatalogQueryWithProfile(profile, p.LocalDeviceID, 10)
		require.NoError(t, err)
		batches, err := PlanCatalogResponses(profile, p.LocalDeviceID, out, 64, query)
		require.NoError(t, err)
		var decoded struct {
			Items []struct {
				ID           string  `xml:"DeviceID"`
				Manufacturer *string `xml:"Manufacturer"`
				CivilCode    *string `xml:"CivilCode"`
			} `xml:"DeviceList>Item"`
		}
		require.NoError(t, manscdp.DecodeProfiledXML(profile, batches[0].Body, &decoded))
		for _, item := range decoded.Items {
			if len(item.ID) < 20 {
				require.Nil(t, item.Manufacturer)
				require.Nil(t, item.CivilCode)
			}
		}
	}
	p.PublishPlatform, p.PublishCivil = false, false
	flat := s.Publish(p, settings)
	require.Equal(t, 1, flat.SumNum)
	require.Empty(t, flat.Items[0].ParentID)
	require.Equal(t, 3, len(s.Items), "publication does not mutate the input")
}

func TestPublicationCivilQueryIsScopedToPublishedRegions(t *testing.T) {
	s := Snapshot{Items: []CatalogItem{{ID: "340203", Kind: CatalogItemCivil}, {ID: "440100", Kind: CatalogItemCivil}, {ID: "camera-a", Kind: CatalogItemChannel, CivilCode: "340203"}, {ID: "camera-b", Kind: CatalogItemChannel, CivilCode: "440100"}}}
	profile := protocol.ProfileFor(protocol.Version2022)
	query, err := manscdp.BuildCatalogQueryWithProfile(profile, "340203", 11)
	require.NoError(t, err)
	batches, err := PlanCatalogResponses(profile, "platform", s, 64, query)
	require.NoError(t, err)
	require.Equal(t, 2, batches[0].SumNum)
	require.Contains(t, string(batches[0].Body), "camera-a")
	require.False(t, strings.Contains(string(batches[0].Body), "camera-b"))
	query, _ = manscdp.BuildCatalogQueryWithProfile(profile, "110101", 12)
	_, err = PlanCatalogResponses(profile, "platform", s, 64, query)
	require.ErrorIs(t, err, ErrUnknownCatalogTarget)
}
