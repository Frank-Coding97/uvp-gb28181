//go:build openapi_live

package integration

import (
	"context"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	openapiclient "uvplatform.cn/uvp-gb28181/app/openapi/client"
	"uvplatform.cn/uvp-gb28181/app/openapi/media"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

// Recreates the configured runner, database connection and factory, not the
// whole API process. ZLM and all survivor sockets remain the same processes.
func TestOpenAPIConfiguredRevocationDurableRecovery(t *testing.T) {
	f := newActiveRevocationFixture(t)
	stream := "durable-revocation"
	f.probe.publish(t, stream)
	generation, publisher := f.probe.generation(stream), f.publisher(t)
	players := make([]*probePlayer, 3)
	events := make([]probeEvent, 3)
	for i, label := range []string{"A", "B", "U"} {
		players[i] = f.probe.player(t, "https-flv", stream, label)
		require.Eventually(t, func() bool {
			var found bool
			events[i], found = f.probe.event("/play", label)
			return found
		}, time.Second, 10*time.Millisecond)
	}
	a, b := f.client(t, "A"), f.client(t, "B")
	aViewer, bViewer := f.bind(t, a, "https-flv", events[0]), f.bind(t, b, "https-flv", events[1])
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	_, err := f.service.SetStatus(ctx, a.ID, models.StatusDisabled, a.RowVersion, 7)
	require.NoError(t, err)
	_, err = f.service.LoadVerificationMaterial(ctx, a.AK)
	require.ErrorIs(t, err, openapiclient.ErrAuthenticationFailed)
	original, err := os.ReadFile(f.bindingsPath)
	require.NoError(t, err)
	badPin := f.probe.controlPin
	badPin[0] ^= 1
	badConfig := strings.Replace(string(original), hex.EncodeToString(f.probe.controlPin[:]), hex.EncodeToString(badPin[:]), 1)
	require.NotEqual(t, string(original), badConfig)
	require.NoError(t, os.WriteFile(f.bindingsPath, []byte(badConfig), 0600))
	type report struct {
		result media.RevocationTickResult
		err    error
	}
	start := func() (func(), <-chan report) {
		reports := make(chan report, 8)
		stop, err := media.StartConfiguredRevocation(ctx, f.db, liveRevocationRegistry{f.node}, f.bindingsPath,
			func(result media.RevocationTickResult, err error) {
				select {
				case reports <- report{result, err}:
				case <-ctx.Done():
				}
			})
		require.NoError(t, err)
		return stop, reports
	}
	stop, reports := start()
	defer stop()
	// Repairing the file does not hot-replace a running trust snapshot.
	require.NoError(t, os.WriteFile(f.bindingsPath, original, 0600))
	alarmed := false
	for !alarmed {
		before := []int64{players[0].bytes.Load(), players[1].bytes.Load(), players[2].bytes.Load()}
		select {
		case got := <-reports:
			require.NoError(t, got.err)
			require.Zero(t, got.result.Closed, "untrusted control must not turn absence into success")
			alarmed = got.result.Alarms > 0
		case <-ctx.Done():
			t.Fatal("untrusted runtime did not remain pending and raise its overdue alarm")
		}
		for i, player := range players {
			select {
			case <-player.done:
				t.Fatal("trust failure must not kick any player")
			default:
			}
			require.Greater(t, player.bytes.Load(), before[i])
		}
	}
	stop()
	require.NoError(t, f.db.First(&aViewer, aViewer.ID).Error)
	require.Equal(t, models.ViewerStateRevokePending, aViewer.State)
	// Resolver errors use the worker's existing sanitized network class;
	// runtime_unavailable is reserved for missing/untrusted dependencies.
	require.Equal(t, media.RevocationErrorNetworkUnavailable, aViewer.LastErrorClass)
	oldAttempts := aViewer.Attempts
	require.Positive(t, oldAttempts)
	progress, err := playauth.NewOpenAPIRevocationStore(f.db, nil).Progress(ctx, a.ID)
	require.NoError(t, err)
	require.Equal(t, playauth.OpenAPIRevocationStatusPending, progress.Status)
	require.Zero(t, progress.Closed)

	// Really close and reopen the file-backed database. No grant or viewer is
	// reseeded, and no manual retry_at/attempt/state rewrite hides recovery.
	dialect := f.db.Dialector
	raw, err := f.db.DB()
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	f.db, err = gorm.Open(dialect, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	reopened, err := f.db.DB()
	require.NoError(t, err)
	reopened.SetMaxOpenConns(1)
	defer reopened.Close()
	var client models.Client
	require.NoError(t, f.db.First(&client, a.ID).Error)
	require.Equal(t, models.StatusDisabled, client.Status)
	require.Equal(t, a.AuthEpoch+1, client.AuthEpoch)
	var recovered models.Viewer
	require.NoError(t, f.db.First(&recovered, aViewer.ID).Error)
	require.Equal(t, aViewer, recovered)
	stopRecovered, reports := start()
	defer stopRecovered()
	started := time.Now()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	closed := false
	for !closed {
		select {
		case got := <-reports:
			require.NoError(t, got.err)
			closed = got.result.Closed == 1
		case <-deadline.C:
			t.Fatal("recreated runner did not close the original durable target after trust recovery")
		}
	}
	select {
	case <-players[0].done:
	case <-deadline.C:
		t.Fatal("original A did not disconnect")
	}
	stopRecovered()
	require.NoError(t, f.db.First(&recovered, aViewer.ID).Error)
	require.Equal(t, models.ViewerStateClosed, recovered.State)
	require.Equal(t, media.RevocationErrorKicked, recovered.LastErrorClass)
	require.Greater(t, recovered.Attempts, oldAttempts)
	f.checkSurvivors(t, stream, events)
	for second := 0; second < 5; second++ {
		beforeB, beforeU := players[1].bytes.Load(), players[2].bytes.Load()
		time.Sleep(time.Second)
		for _, player := range players[1:] {
			select {
			case <-player.done:
				t.Fatal("original survivor disconnected during recovery")
			default:
			}
		}
		require.Greater(t, players[1].bytes.Load(), beforeB)
		require.Greater(t, players[2].bytes.Load(), beforeU)
	}
	var persistedB models.Viewer
	require.NoError(t, f.db.First(&persistedB, bViewer.ID).Error)
	require.Equal(t, bViewer, persistedB)
	require.Equal(t, publisher, f.publisher(t))
	require.Equal(t, generation, f.probe.generation(stream))
	t.Logf("bad pin: pending + overdue alarm, no kick; database/factory/runner recreated; original A closed after recovery, B/U + publisher preserved (observation %s)", time.Since(started))
}
