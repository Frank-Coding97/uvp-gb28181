package ptz

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

func TestCascadeConfigReadLifecycle(t *testing.T) {
	for _, scenario := range []string{"empty response", "explicit error", "cancelled", "retired"} {
		t.Run(scenario, func(t *testing.T) {
			service, db := newPTZQueryTestService(t, &fakeTrackedSender{})
			connection, err := db.DB()
			require.NoError(t, err)
			connection.SetMaxOpenConns(1)
			defer service.Retire()
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			type result struct {
				body []byte
				err  error
			}
			resultCh := make(chan result, 1)
			done := make(chan struct{})
			go func() {
				defer close(done)
				body, err := service.ConfigForCascade(ctx, testTarget(), "FrameMirror", "cascade-config")
				resultCh <- result{body, err}
			}()
			t.Cleanup(func() {
				cancel()
				<-done
			})
			var operation gbmodels.GbPTZOperation
			// This fixture has no scheduler; response lifecycle tests use the persisted operation.
			require.Eventually(t, func() bool {
				return db.Where("idempotency_key=?", "cascade-config").Take(&operation).Error == nil
			}, time.Second, 5*time.Millisecond)
			if scenario == "cancelled" {
				cancel()
			} else if scenario == "retired" {
				service.Retire()
			} else {
				result := "OK"
				if scenario == "explicit error" {
					result = "ERROR"
				}
				body := []byte(fmt.Sprintf(`<Response><CmdType>ConfigDownload</CmdType><SN>%d</SN><DeviceID>C</DeviceID><Result>%s</Result></Response>`, operation.SN, result))
				require.NoError(t, service.OnPTZMessage(ctx, "D", "config-response", "2", body))
			}
			got := <-resultCh
			if scenario == "empty response" {
				require.NoError(t, got.err)
				require.NotContains(t, string(got.body), "<FrameMirror>")
				got.body[0] = '!'
				body, err := service.ConfigForCascade(ctx, testTarget(), "FrameMirror", "cascade-config")
				require.NoError(t, err)
				require.Equal(t, byte('<'), body[0], "replay uses an isolated copy of the exact operation response")
				_, err = service.ConfigForCascade(ctx, testTarget(), "BasicParam", "cascade-config")
				require.Error(t, err, "the same identity cannot be rebound to another configuration type")
				var count int64
				require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Count(&count).Error)
				require.EqualValues(t, 1, count)
			} else {
				require.Error(t, got.err)
				require.Empty(t, got.body)
				service.cascadeQueryMu.Lock()
				count := len(service.cascadeQueries)
				service.cascadeQueryMu.Unlock()
				require.Zero(t, count, "failed or cancelled callers release query waiters")
			}
		})
	}
}
