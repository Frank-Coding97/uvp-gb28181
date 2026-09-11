package catalog

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	cascadeprotocol "uvplatform.cn/uvp-gb28181/app/gb28181/cascade/protocol"
	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	baseprotocol "uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

func TestPlanCatalogResponsesParsesQueryBatchesAndUsesProfiledXML(t *testing.T) {
	snapshot := Snapshot{
		PlatformID: 1,
		Revision:   7,
		SumNum:     3,
		Items: []CatalogItem{
			{ID: "34020000001320000001", Kind: CatalogItemDevice, Name: "device", Manufacturer: "UVP", Model: "M1", Owner: "owner", CivilCode: "340200", Address: "gate", Parental: 1, Secrecy: 0, Status: ResourceStatusOnline},
			{ID: "34020000001320000011", ParentID: "34020000001320000001", Kind: CatalogItemChannel, Name: "front", Status: ResourceStatusOnline},
			{ID: "34020000001320000012", ParentID: "34020000001320000001", Kind: CatalogItemChannel, Name: "back", Status: ResourceStatusOffline},
		},
	}
	for _, version := range []baseprotocol.Version{baseprotocol.Version2016, baseprotocol.Version2022} {
		t.Run(string(version), func(t *testing.T) {
			profile := baseprotocol.ProfileFor(version)
			queryBody, err := manscdp.BuildCatalogQueryWithProfile(profile, "34020000002000000001", 42)
			require.NoError(t, err)
			responses, err := PlanCatalogResponses(profile, "34020000002000000001", snapshot, 2, queryBody)
			require.NoError(t, err)
			require.Len(t, responses, 2)

			for index, response := range responses {
				require.Equal(t, 42, response.SN)
				require.Equal(t, 3, response.SumNum)
				parsed, err := cascadeprotocol.ParseMANSCDP(version, response.Body)
				require.NoError(t, err)
				require.NoError(t, cascadeprotocol.ValidateMANSCDPFixture(parsed))
				require.Equal(t, 42, parsed.SN)
				require.Equal(t, 3, *parsed.SumNum)
				require.Equal(t, []string{"2", "1"}[index], parsed.DeviceListNum)
				require.Empty(t, parsed.DeviceListnum)

				var decoded manscdp.CatalogResponse
				require.NoError(t, manscdp.DecodeProfiledXML(profile, response.Body, &decoded))
				require.Equal(t, 42, decoded.SN)
				require.Equal(t, 3, decoded.SumNum)
				require.Len(t, decoded.DeviceList.Items, []int{2, 1}[index])
			}
			first := string(responses[0].Body)
			require.Contains(t, first, "<Address>gate</Address>")
			require.Contains(t, first, "<Parental>1</Parental>")
			require.Contains(t, first, "<Secrecy>0</Secrecy>")
			require.NotContains(t, first, "<PTZType>")
			require.NotContains(t, first, "<Longitude>")
			require.NotContains(t, first, "<Latitude>")
			require.NotContains(t, first, "<Event>")
		})
	}
}

func TestPlanCatalogResponsesEmptyHasNoDeviceListAndValidatesTargetAndBatch(t *testing.T) {
	profile := baseprotocol.ProfileFor(baseprotocol.Version2016)
	queryBody := []byte(`<?xml version="1.0" encoding="GB2312"?><Query><CmdType>Catalog</CmdType><SN>1</SN><DeviceID>34020000002000000001</DeviceID></Query>`)
	responses, err := PlanCatalogResponses(profile, "34020000002000000001", Snapshot{PlatformID: 1, Revision: 2}, 10, queryBody)
	require.NoError(t, err)
	require.Len(t, responses, 1)
	require.Equal(t, 0, responses[0].SumNum)
	require.NotContains(t, string(responses[0].Body), "DeviceList")
	parsed, err := cascadeprotocol.ParseMANSCDP(baseprotocol.Version2016, responses[0].Body)
	require.NoError(t, err)
	require.Equal(t, 0, *parsed.SumNum)
	require.Zero(t, parsed.DeviceListSize)

	_, err = PlanCatalogResponses(profile, "34020000002000000009", Snapshot{}, 10, queryBody)
	require.ErrorIs(t, err, ErrUnknownCatalogTarget)
	_, err = PlanCatalogResponses(profile, "34020000002000000001", Snapshot{}, 0, queryBody)
	require.ErrorIs(t, err, ErrInvalidCatalogBatch)
	_, err = PlanCatalogResponses(profile, "34020000002000000001", Snapshot{}, 1, []byte(`<Query><CmdType>DeviceInfo</CmdType><SN>1</SN><DeviceID>34020000002000000001</DeviceID></Query>`))
	require.ErrorIs(t, err, ErrInvalidCatalogQuery)
}

func TestSendCatalogResponsesStopsAtFirstFailure(t *testing.T) {
	first := CatalogResponse{Body: []byte("first")}
	second := CatalogResponse{Body: []byte("second")}
	sender := &catalogSenderFake{failAt: 2}

	result, err := SendCatalogResponses(context.Background(), sender, []CatalogResponse{first, second})
	require.Error(t, err)
	require.Equal(t, 1, result.SentBatches)
	require.False(t, result.Completed)
	require.Equal(t, [][]byte{first.Body, second.Body}, sender.bodies)
}

func TestSendCatalogResponsesWaitsForPriorBatch(t *testing.T) {
	sender := &blockingCatalogSender{firstStarted: make(chan struct{}), releaseFirst: make(chan struct{}), secondCalled: make(chan struct{})}
	type sendOutcome struct {
		result CatalogSendResult
		err    error
	}
	done := make(chan sendOutcome, 1)
	go func() {
		result, err := SendCatalogResponses(context.Background(), sender, []CatalogResponse{{Body: []byte("first")}, {Body: []byte("second")}})
		done <- sendOutcome{result: result, err: err}
	}()

	<-sender.firstStarted
	select {
	case <-sender.secondCalled:
		t.Fatal("second response started before first response completed")
	default:
	}
	close(sender.releaseFirst)
	result := <-done
	require.NoError(t, result.err)
	require.Equal(t, 2, result.result.SentBatches)
	require.True(t, result.result.Completed)
}

type catalogSenderFake struct {
	failAt int
	bodies [][]byte
}

type blockingCatalogSender struct {
	mu           sync.Mutex
	calls        int
	firstStarted chan struct{}
	releaseFirst chan struct{}
	secondCalled chan struct{}
}

func (s *blockingCatalogSender) SendCatalogResponse(_ context.Context, _ []byte) error {
	s.mu.Lock()
	s.calls++
	call := s.calls
	s.mu.Unlock()
	if call == 1 {
		close(s.firstStarted)
		<-s.releaseFirst
		return nil
	}
	close(s.secondCalled)
	return nil
}

func (s *catalogSenderFake) SendCatalogResponse(_ context.Context, body []byte) error {
	s.bodies = append(s.bodies, append([]byte(nil), body...))
	if len(s.bodies) == s.failAt {
		return errors.New("second batch failed")
	}
	return nil
}

func TestParseCatalogQueryRejectsMalformedInput(t *testing.T) {
	_, err := ParseCatalogQuery(baseprotocol.ProfileFor(baseprotocol.Version2022), []byte(`<Query><CmdType>Catalog</CmdType><SN>0</SN><DeviceID></DeviceID></Query>`))
	require.ErrorIs(t, err, ErrInvalidCatalogQuery)
	_, err = ParseCatalogQuery(baseprotocol.ProfileFor(baseprotocol.Version2022), []byte(`<Response><CmdType>Catalog</CmdType><SN>1</SN><DeviceID>34020000002000000001</DeviceID></Response>`))
	require.ErrorIs(t, err, ErrInvalidCatalogQuery)

	query, err := ParseCatalogQuery(baseprotocol.ProfileFor(baseprotocol.Version2022), []byte(`<?xml version="1.0" encoding="GB18030"?><Query><CmdType>Catalog</CmdType><SN>7</SN><DeviceID>34020000002000000001</DeviceID></Query>`))
	require.NoError(t, err)
	require.Equal(t, 7, query.SN)
	require.True(t, strings.HasPrefix(query.DeviceID, "340200"))
}

func TestCatalogByteLimitSplitsWithoutLosingTotalOrItems(t *testing.T) {
	profile := baseprotocol.ProfileFor(baseprotocol.Version2016)
	body, err := manscdp.BuildCatalogQueryWithProfile(profile, "34020000002000000001", 42)
	require.NoError(t, err)
	items := []CatalogItem{{ID: "34020000001320000001", Name: strings.Repeat("名", 20)}, {ID: "34020000001320000002", Name: strings.Repeat("名", 20)}}
	one, err := PlanCatalogResponses(profile, "34020000002000000001", Snapshot{Items: items}, 1, body)
	require.NoError(t, err)
	limit := len(one[0].Body)
	batches, err := PlanCatalogResponsesWithinLimit(profile, "34020000002000000001", Snapshot{Items: items}, 100, body, func(b []byte) bool { return len(b) <= limit })
	require.NoError(t, err)
	require.Len(t, batches, 2)
	for _, b := range batches {
		require.Equal(t, 2, b.SumNum)
		require.Equal(t, 42, b.SN)
		require.LessOrEqual(t, len(b.Body), limit)
	}
	_, err = PlanCatalogResponsesWithinLimit(profile, "34020000002000000001", Snapshot{Items: items}, 100, body, func([]byte) bool { return false })
	require.ErrorContains(t, err, "TCP")
}
