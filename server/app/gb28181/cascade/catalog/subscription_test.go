package catalog

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

func TestSubscriptionScopeAndDelta(t *testing.T) {
	const local = "34020000002000000001"
	a := CatalogItem{ID: "34020000001320000001", Kind: CatalogItemChannel, Name: "入口", Status: ResourceStatusOnline}
	b := CatalogItem{ID: "34020000001320000002", Kind: CatalogItemChannel, Name: "出口", Status: ResourceStatusOffline}
	items, err := SubscriptionItems(Snapshot{Items: []CatalogItem{a, b}}, local, a.ID)
	require.NoError(t, err)
	require.Equal(t, []CatalogItem{a}, items)
	_, err = SubscriptionItems(Snapshot{Items: []CatalogItem{a, b}}, local, "source-device")
	require.Error(t, err)
	renamed := a
	renamed.Name = "新入口"
	renamed.Status = ResourceStatusOffline
	changes := DiffCatalog([]CatalogItem{a, b}, []CatalogItem{renamed})
	require.Len(t, changes, 3)
	require.Equal(t, "DEL", changes[0].Event)
	require.Equal(t, CatalogItem{ID: b.ID}, changes[0].Item)
	require.Equal(t, "UPDATE", changes[1].Event)
	require.Equal(t, "OFF", changes[2].Event)
	require.Empty(t, DiffCatalog([]CatalogItem{a, b}, []CatalogItem{a, b}))
}

func TestSubscriptionNotifyBatchesUseBatchTotalAndOriginalSN(t *testing.T) {
	for _, version := range []protocol.Version{protocol.Version2016, protocol.Version2022} {
		profile := protocol.ProfileFor(version)
		changes := []CatalogChange{{Item: CatalogItem{ID: "34020000001320000001", Kind: CatalogItemChannel, Name: "测试<&>", Status: ResourceStatusOnline}, Event: "ADD"}, {Item: CatalogItem{ID: "34020000001320000002"}, Event: "DEL"}}
		bodies, err := PlanCatalogNotifications(profile, "34020000002000000001", 99, changes, 1, nil)
		require.NoError(t, err)
		require.Len(t, bodies, 2)
		for _, body := range bodies {
			parsed, err := manscdp.ParseCatalogNotify(body.Body)
			require.NoError(t, err)
			require.Equal(t, 99, parsed.SN)
			require.Equal(t, 1, parsed.SumNum)
			require.Equal(t, 1, parsed.DeviceList.Num)
			require.NotEmpty(t, parsed.DeviceList.Items[0].Event)
		}
		empty, err := PlanCatalogNotifications(profile, "34020000002000000001", 99, nil, 1, nil)
		require.NoError(t, err)
		require.Len(t, empty, 1)
		parsed, err := manscdp.ParseCatalogNotify(empty[0].Body)
		require.NoError(t, err)
		require.Zero(t, parsed.SumNum)
		_, err = PlanCatalogNotifications(profile, "34020000002000000001", 99, changes, 1, func([]byte) bool { return false })
		require.Error(t, err)
	}
}

func TestSubscriptionMetadataFollows2022AppendixNExample(t *testing.T) {
	for _, version := range []protocol.Version{protocol.Version2016, protocol.Version2022} {
		bodies, err := PlanCatalogNotifications(protocol.ProfileFor(version), "34020000002000000001", 1, []CatalogChange{{Item: CatalogItem{ID: "34020000001320000001", Kind: CatalogItemChannel, Name: "入口", ParentID: "34020000002000000001", BusinessGroupID: "34020000002150000001", Status: ResourceStatusOnline}, Event: "ADD"}}, 1, nil)
		require.NoError(t, err)
		body := string(bodies[0].Body)
		require.Contains(t, body, "<RegisterWay>1</RegisterWay>")
		require.Less(t, strings.Index(body, "<Event>"), strings.Index(body, "<Name>"))
		require.Less(t, strings.Index(body, "<ParentID>"), strings.Index(body, "<RegisterWay>"))
		require.Less(t, strings.Index(body, "<Status>"), strings.Index(body, "<BusinessGroupID>"))
		if version == protocol.Version2022 {
			require.NotContains(t, body, "<Owner>")
		}
	}
}

func TestSubscriptionBusinessAndAdministrativeScopes(t *testing.T) {
	const local = "34020000002000000001"
	group := CatalogItem{ID: "34020000002150000001", Kind: CatalogItemBusinessGroup}
	org := CatalogItem{ID: "34020000002160000001", Kind: CatalogItemVirtualOrganization, BusinessGroupID: group.ID}
	nested := CatalogItem{ID: "34020000002160000002", Kind: CatalogItemVirtualOrganization, ParentID: org.ID, BusinessGroupID: group.ID}
	region := CatalogItem{ID: "340203", Kind: CatalogItemCivil}
	a := CatalogItem{ID: "34020000001320000001", Kind: CatalogItemChannel, ParentID: local + "/" + nested.ID, CivilCode: region.ID}
	b := CatalogItem{ID: "34020000001320000002", Kind: CatalogItemChannel, ParentID: local, CivilCode: "440100"}
	snapshot := Snapshot{Items: []CatalogItem{group, org, nested, region, a, b}}
	for _, scenario := range []struct {
		target string
		ids    []string
	}{
		{group.ID, []string{group.ID, org.ID, nested.ID, a.ID}},
		{org.ID, []string{org.ID, nested.ID, a.ID}},
		{region.ID, []string{region.ID, a.ID}},
		{a.ID, []string{a.ID}},
	} {
		items, err := SubscriptionItems(snapshot, local, scenario.target)
		require.NoError(t, err)
		var ids []string
		for _, item := range items {
			ids = append(ids, item.ID)
		}
		require.Equal(t, scenario.ids, ids)
	}
}

func TestSubscriptionAddedResourceAndAcknowledgementAreIdempotent(t *testing.T) {
	item := CatalogItem{ID: "34020000001320000001", Name: "channel", Kind: CatalogItemChannel, Status: ResourceStatusOnline}
	changes := DiffCatalog(nil, []CatalogItem{item})
	require.Equal(t, []CatalogChange{{Item: item, Event: "ADD"}}, changes)
	confirmed := ApplyCatalogChanges(nil, changes)
	require.Equal(t, []CatalogItem{item}, confirmed)
	require.Equal(t, confirmed, ApplyCatalogChanges(confirmed, changes))
	require.Empty(t, DiffCatalog(confirmed, []CatalogItem{item}))
	require.Empty(t, ApplyCatalogChanges(confirmed, []CatalogChange{{Item: CatalogItem{ID: item.ID}, Event: "DEL"}}))
}
