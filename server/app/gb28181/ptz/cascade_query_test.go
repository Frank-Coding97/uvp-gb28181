package ptz

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

type cascadeQueryTestResult struct {
	pages [][]byte
	err   error
}

func TestCascadeQueryRetainsExactResultForRetransmissions(t *testing.T) {
	service, db := newPTZQueryTestService(t, &fakeTrackedSender{})
	connection, err := db.DB()
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	defer service.Retire()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	result := make(chan cascadeQueryTestResult, 2)
	go func() {
		pages, err := service.QueryForCascade(ctx, testTarget(), QueryPreset, 0, "cascade-query")
		result <- cascadeQueryTestResult{pages, err}
	}()
	var operation gbmodels.GbPTZOperation
	require.Eventually(t, func() bool { return db.Where("idempotency_key=?", "cascade-query").Take(&operation).Error == nil }, time.Second, 5*time.Millisecond)
	// A second pending request joins this operation rather than creating a second query.
	go func() {
		pages, err := service.QueryForCascade(ctx, testTarget(), QueryPreset, 0, "cascade-query")
		result <- cascadeQueryTestResult{pages, err}
	}()
	body := []byte(fmt.Sprintf(`<Response><CmdType>PresetQuery</CmdType><SN>%d</SN><DeviceID>C</DeviceID><SumNum>0</SumNum><PresetList Num="0"/></Response>`, operation.SN))
	require.NoError(t, service.OnPTZMessage(ctx, "D", "response", "1", body))
	for range 2 {
		received := <-result
		require.NoError(t, received.err)
		require.Equal(t, [][]byte{body}, received.pages)
		received.pages[0][0] = '!'
	}
	replay, err := service.QueryForCascade(ctx, testTarget(), QueryPreset, 0, "cascade-query")
	require.NoError(t, err)
	require.Equal(t, [][]byte{body}, replay, "caller mutation must not change the retained operation result")
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestCascadeQueryCancellationAndRetirementReleaseWaiters(t *testing.T) {
	for _, retire := range []bool{false, true} {
		t.Run(fmt.Sprint(retire), func(t *testing.T) {
			service, db := newPTZQueryTestService(t, &fakeTrackedSender{})
			connection, err := db.DB()
			require.NoError(t, err)
			connection.SetMaxOpenConns(1)
			defer service.Retire()
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			result := make(chan error, 1)
			go func() {
				_, err := service.QueryForCascade(ctx, testTarget(), QueryPreset, 0, "cancel-query")
				result <- err
			}()
			require.Eventually(t, func() bool {
				var count int64
				return db.Model(&gbmodels.GbPTZOperation{}).Count(&count).Error == nil && count == 1
			}, time.Second, 5*time.Millisecond)
			if retire {
				service.Retire()
			} else {
				cancel()
			}
			require.Error(t, <-result)
			service.cascadeQueryMu.Lock()
			count := len(service.cascadeQueries)
			service.cascadeQueryMu.Unlock()
			require.Zero(t, count)
		})
	}
}
