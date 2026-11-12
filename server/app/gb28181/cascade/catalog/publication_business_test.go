package catalog

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

func TestBusinessPublicationOmitsPhysicalDevicesAndPreservesPlayback(t *testing.T) {
	for _, assigned := range []int{0, 5, 10} {
		for _, publishPlatform := range []bool{false, true} {
			t.Run(fmt.Sprintf("assigned=%d/platform=%t", assigned, publishPlatform), func(t *testing.T) {
				p := model.GbCascadePlatform{ID: 9, LocalDeviceID: "34020000002000000002", PublishGroup: true, PublishPlatform: publishPlatform}
				root := model.GbCascadeBusinessNode{PlatformID: p.ID, Code: "34020000002150000001", Kind: "group"}
				org := model.GbCascadeBusinessNode{PlatformID: p.ID, Code: "34020000002160000001", Kind: "organization", ParentCode: root.Code}
				projection := repository.ProjectionSnapshot{Platform: p, Nodes: []model.GbCascadeBusinessNode{root, org}}
				for i := 0; i < 6; i++ {
					projection.Devices = append(projection.Devices, model.GbCascadeDeviceProjection{
						ID: uint64(i + 1), PlatformID: p.ID, SourceDeviceID: uint64(100 + i),
						PublishedDeviceID: fmt.Sprintf("34020000001320000%03d", i+1), Parental: 1, Active: true,
					})
				}
				for i := 0; i < 10; i++ {
					channel := model.GbCascadeChannelProjection{
						PlatformID: p.ID, DeviceProjectionID: uint64(i%6 + 1), SourceChannelID: uint64(200 + i),
						PublishedChannelID: fmt.Sprintf("34020000001320001%03d", i+1), PTZAllowed: true, Active: true,
					}
					projection.Channels = append(projection.Channels, channel)
					if i < assigned {
						projection.Bindings = append(projection.Bindings, model.GbCascadeBusinessBinding{
							PlatformID: p.ID, SourceChannelID: channel.SourceChannelID, NodeCode: org.Code,
						})
					}
				}
				snapshot, err := Build(projection, SourceFacts{})
				require.NoError(t, err)
				require.Equal(t, 19, snapshot.SumNum)
				originalItems := append([]CatalogItem(nil), snapshot.Items...)
				out := snapshot.Publish(p, PublicationSettings{})
				require.Equal(t, 13, out.SumNum, "ten channels plus the platform, business group and organization")
				require.Len(t, out.Items, 13)
				expectedParents := map[string]string{}
				for i, channel := range projection.Channels {
					parent := p.LocalDeviceID
					if i < assigned {
						parent += "/" + org.Code
					}
					expectedParents[channel.PublishedChannelID] = parent
					target, ok := out.LookupPublishedChannel(channel.PublishedChannelID)
					require.True(t, ok)
					require.Equal(t, PublishedChannelTarget{
						PlatformID: p.ID, SourceDeviceID: projection.Devices[i%6].SourceDeviceID,
						SourceChannelID: channel.SourceChannelID, PTZAllowed: true,
					}, target)
				}
				for _, item := range out.Items {
					require.NotEqual(t, CatalogItemDevice, item.Kind)
					if parent, ok := expectedParents[item.ID]; ok {
						require.Equal(t, parent, item.ParentID)
					}
					if item.ParentID != "" {
						for _, parent := range strings.Split(item.ParentID, "/") {
							require.True(t, out.HasPublishedID(parent), "no dangling parent: %s", parent)
						}
					}
				}
				for _, version := range []protocol.Version{protocol.Version2016, protocol.Version2022} {
					profile := protocol.ProfileFor(version)
					query, err := manscdp.BuildCatalogQueryWithProfile(profile, p.LocalDeviceID, 1)
					require.NoError(t, err)
					responses, err := PlanCatalogResponses(profile, p.LocalDeviceID, out, 1, query)
					require.NoError(t, err)
					require.Len(t, responses, 13)
					for _, response := range responses {
						var decoded struct {
							SumNum int `xml:"SumNum"`
							Items  []struct {
								ID       string `xml:"DeviceID"`
								ParentID string `xml:"ParentID"`
							} `xml:"DeviceList>Item"`
						}
						require.NoError(t, manscdp.DecodeProfiledXML(profile, response.Body, &decoded))
						require.Equal(t, 13, decoded.SumNum)
						require.Len(t, decoded.Items, 1)
						if parent, ok := expectedParents[decoded.Items[0].ID]; ok {
							require.Equal(t, parent, decoded.Items[0].ParentID)
						}
					}
					query, err = manscdp.BuildCatalogQueryWithProfile(profile, org.Code, 2)
					require.NoError(t, err)
					responses, err = PlanCatalogResponses(profile, p.LocalDeviceID, out, 64, query)
					require.NoError(t, err)
					require.Equal(t, assigned+1, responses[0].SumNum, "organization queries retain only assigned channels")
					for _, device := range projection.Devices {
						require.False(t, out.HasPublishedID(device.PublishedDeviceID))
						query, err = manscdp.BuildCatalogQueryWithProfile(profile, device.PublishedDeviceID, 3)
						require.NoError(t, err)
						_, err = PlanCatalogResponses(profile, p.LocalDeviceID, out, 64, query)
						require.ErrorIs(t, err, ErrUnknownCatalogTarget)
					}
				}
				require.Equal(t, originalItems, snapshot.Items, "publication must not mutate the internal snapshot")
				for _, device := range projection.Devices {
					require.True(t, snapshot.HasPublishedID(device.PublishedDeviceID))
				}
			})
		}
	}
}
