package videoshare

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	basemodels "uvplatform.com/uvp-gb28181/app/models"
)

func renewalFixture(t *testing.T) (*Service, uint, *atomic.Int64, Link) {
	t.Helper()
	s, channel, now := newServiceFixture(t)
	t.Cleanup(s.DrainViewers)
	clock := new(atomic.Int64)
	clock.Store(now.UnixNano())
	s.now = func() time.Time { return time.Unix(0, clock.Load()).UTC() }
	s.SetPlaybackPreparer(playbackPreparerFunc(func(context.Context, *gbmodels.GbChannelVideoShare) (PlaybackSource, error) {
		return PlaybackSource{StreamID: "s", Generation: 1, HTTPFLVURL: "http://media.invalid/live.flv", HLSURL: "http://media.invalid/rtp/s/hls.m3u8"}, nil
	}))
	link, err := s.SetEnabled(context.Background(), channel, 9, true, 0)
	require.NoError(t, err)
	return s, channel, clock, link
}

func startedRenewalViewer(t *testing.T, s *Service, link Link) Viewer {
	t.Helper()
	viewer, err := s.CreateViewer(context.Background(), link.PublicID, link.Signature)
	require.NoError(t, err)
	finish, err := s.beginViewerRequest(viewer.Token)
	require.NoError(t, err)
	require.True(t, s.confirmViewerMedia(viewer.Token))
	t.Cleanup(func() { finish(false) })
	return viewer
}

func TestAcquireExpiredLinkRenewsCredentialAndPreservesAuthority(t *testing.T) {
	for _, scan := range []bool{false, true} {
		t.Run(fmt.Sprintf("scanned=%t", scan), func(t *testing.T) {
			s, channel, clock, old := renewalFixture(t)
			active := startedRenewalViewer(t, s, old)
			unused, err := s.CreateViewer(context.Background(), old.PublicID, old.Signature)
			require.NoError(t, err)
			clock.Store(old.ExpiresAt.UnixNano())
			if scan {
				count, err := s.ScanExpired(context.Background(), 100)
				require.NoError(t, err)
				require.Equal(t, 1, count)
			}
			state, err := s.Status(context.Background(), channel, 10)
			require.NoError(t, err)
			s.ttl = func() time.Duration { return 30 * time.Minute }
			renewed, err := s.AcquireLink(context.Background(), channel, 10, state.Revision)
			require.NoError(t, err)
			require.NotEqual(t, old.PublicID, renewed.PublicID)
			require.NotEqual(t, old.Signature, renewed.Signature)
			require.Equal(t, old.Generation, renewed.Generation, "renewing a link does not replace the share authorization")
			require.Equal(t, state.Revision+1, renewed.Revision)
			require.Equal(t, old.ExpiresAt.Add(30*time.Minute), *renewed.ExpiresAt)
			current, err := s.Resolve(context.Background(), renewed.PublicID, renewed.Signature)
			require.NoError(t, err)
			require.EqualValues(t, 9, *current.CreatedBy, "a new operator must not take ownership of old viewers")
			require.Equal(t, *old.ExpiresAt, *current.ActivatedAt)
			require.Empty(t, current.RevokeReason)
			require.Nil(t, current.RevokedAt)
			again, err := s.AcquireLink(context.Background(), channel, 10, renewed.Revision)
			require.NoError(t, err)
			require.Equal(t, renewed, again, "clicking during the renewed TTL must be idempotent")
			_, err = s.Resolve(context.Background(), old.PublicID, old.Signature)
			require.ErrorIs(t, err, ErrNotFound)
			_, err = s.CreateViewer(context.Background(), old.PublicID, old.Signature)
			require.ErrorIs(t, err, ErrNotFound)
			access, err := s.LookupViewer(context.Background(), active.Token)
			require.NoError(t, err)
			require.Equal(t, *old.ExpiresAt, access.ExpiresAt, "old viewer retains its admission cutoff")
			require.Equal(t, renewed.PublicID, access.Share.PublicID)
			_, err = s.LookupViewer(context.Background(), unused.Token)
			require.ErrorIs(t, err, ErrExpired, "renewal cannot admit an unused old viewer")
			var logs []basemodels.SysOperationLog
			require.NoError(t, s.db.Where("operation = ?", "video_share.renewed").Find(&logs).Error)
			require.Len(t, logs, 1)
			require.EqualValues(t, 10, logs[0].UserID)
			require.Contains(t, logs[0].RequestData, `"event":"video_share.renewed"`)
			for _, secret := range []string{old.PublicID, old.Signature, renewed.PublicID, renewed.Signature} {
				require.NotContains(t, logs[0].RequestData, secret)
			}
		})
	}
}

func TestConcurrentExpiredLinkRenewalCommitsOnceAndRetryReusesLink(t *testing.T) {
	s, channel, clock, old := renewalFixture(t)
	clock.Store(old.ExpiresAt.UnixNano())
	const count = 8
	var ready sync.WaitGroup
	ready.Add(count)
	start := make(chan struct{})
	links := make(chan Link, count)
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		go func() {
			ready.Done()
			<-start
			link, err := s.AcquireLink(context.Background(), channel, 10, old.Revision)
			links <- link
			errs <- err
		}()
	}
	ready.Wait()
	close(start)
	successes, conflicts := 0, 0
	for i := 0; i < count; i++ {
		err := <-errs
		if err == nil {
			successes++
		} else {
			require.ErrorIs(t, err, ErrConflict)
			conflicts++
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, count-1, conflicts)
	state, err := s.Status(context.Background(), channel, 10)
	require.NoError(t, err)
	require.Equal(t, old.Revision+1, state.Revision)
	current, err := s.AcquireLink(context.Background(), channel, 10, state.Revision)
	require.NoError(t, err)
	for i := 0; i < count; i++ {
		link := <-links
		if link.Enabled {
			require.Equal(t, current, link)
		}
	}
	for i := 0; i < count-1; i++ {
		retry, err := s.AcquireLink(context.Background(), channel, 10, state.Revision)
		require.NoError(t, err)
		require.Equal(t, current, retry)
	}
	var audits int64
	require.NoError(t, s.db.Model(&basemodels.SysOperationLog{}).Where("operation = ?", "video_share.renewed").Count(&audits).Error)
	require.EqualValues(t, 1, audits)
}

func TestRenewalDoesNotReviveDisabledRevokedOrUnauthorizedShares(t *testing.T) {
	for _, scenario := range []string{"disabled", "creator-revoked", "owner-lost-permission", "operator-lost-permission", "device-changed"} {
		t.Run(scenario, func(t *testing.T) {
			s, channel, clock, old := renewalFixture(t)
			clock.Store(old.ExpiresAt.UnixNano())
			switch scenario {
			case "disabled":
				_, err := s.SetEnabled(context.Background(), channel, 9, false, old.Revision)
				require.NoError(t, err)
			case "creator-revoked":
				require.NoError(t, s.db.Transaction(func(tx *gorm.DB) error {
					return RevokeCreatorTx(context.Background(), tx, 9, "creator_disabled", s.now())
				}))
			case "device-changed":
				require.NoError(t, s.db.Exec("UPDATE gb_device SET access_epoch = access_epoch + 1").Error)
			}
			var before gbmodels.GbChannelVideoShare
			require.NoError(t, s.db.Where("channel_pk = ?", channel).Take(&before).Error)
			if scenario == "owner-lost-permission" || scenario == "operator-lost-permission" {
				original := s.authorize
				s.authorize = func(ctx context.Context, tx *gorm.DB, actor, target uint) (AuthorizationSnapshot, error) {
					if (scenario == "owner-lost-permission" && actor == 9) || (scenario == "operator-lost-permission" && actor == 10) {
						return AuthorizationSnapshot{}, ErrExpired
					}
					return original(ctx, tx, actor, target)
				}
			}
			_, err := s.AcquireLink(context.Background(), channel, 10, before.Revision)
			require.ErrorIs(t, err, ErrExpired)
			var after gbmodels.GbChannelVideoShare
			require.NoError(t, s.db.Where("channel_pk = ?", channel).Take(&after).Error)
			require.Equal(t, before, after)
		})
	}
}

func TestRenewalFailuresRollBackAndKeepExistingViewers(t *testing.T) {
	for _, scenario := range []string{"random", "audit", "invalid-ttl"} {
		t.Run(scenario, func(t *testing.T) {
			s, channel, clock, old := renewalFixture(t)
			active := startedRenewalViewer(t, s, old)
			clock.Store(old.ExpiresAt.UnixNano())
			_, err := s.ScanExpired(context.Background(), 100)
			require.NoError(t, err)
			var before gbmodels.GbChannelVideoShare
			require.NoError(t, s.db.Where("channel_pk = ?", channel).Take(&before).Error)
			var beforeAudits int64
			require.NoError(t, s.db.Model(&basemodels.SysOperationLog{}).Count(&beforeAudits).Error)
			boom := errors.New("renewal dependency unavailable")
			switch scenario {
			case "random":
				s.random = func([]byte) error { return boom }
			case "invalid-ttl":
				s.ttl = func() time.Duration { return 30 * time.Second }
			default:
				require.NoError(t, s.db.Callback().Create().Before("gorm:create").Register("test:fail_renewal_audit", func(tx *gorm.DB) {
					if tx.Statement.Table == "sys_operation_logs" {
						tx.AddError(boom)
					}
				}))
			}
			_, err = s.AcquireLink(context.Background(), channel, 10, before.Revision)
			if scenario == "invalid-ttl" {
				require.Error(t, err)
			} else {
				require.ErrorIs(t, err, boom)
			}
			var after gbmodels.GbChannelVideoShare
			require.NoError(t, s.db.Where("channel_pk = ?", channel).Take(&after).Error)
			require.Equal(t, before, after)
			var afterAudits int64
			require.NoError(t, s.db.Model(&basemodels.SysOperationLog{}).Count(&afterAudits).Error)
			require.Equal(t, beforeAudits, afterAudits)
			_, err = s.LookupViewer(context.Background(), active.Token)
			require.NoError(t, err)
		})
	}
}

func TestExplicitRevocationStopsViewersFromEveryRenewedCredential(t *testing.T) {
	for _, scenario := range []string{"disable", "regenerate", "disable-reopen", "original-owner-revoked"} {
		t.Run(scenario, func(t *testing.T) {
			s, channel, clock, link := renewalFixture(t)
			var viewers []Viewer
			var historical []Link
			for i := 0; i < 3; i++ {
				historical = append(historical, link)
				viewers = append(viewers, startedRenewalViewer(t, s, link))
				if i < 2 {
					clock.Store(link.ExpiresAt.UnixNano())
					var err error
					link, err = s.AcquireLink(context.Background(), channel, uint(10+i), link.Revision)
					require.NoError(t, err)
				}
			}
			var err error
			switch scenario {
			case "regenerate":
				_, err = s.Regenerate(context.Background(), channel, 10, link.Revision)
			case "original-owner-revoked":
				err = s.db.Transaction(func(tx *gorm.DB) error {
					return RevokeCreatorTx(context.Background(), tx, 9, "creator_disabled", s.now())
				})
			default:
				var disabled Link
				disabled, err = s.SetEnabled(context.Background(), channel, 10, false, link.Revision)
				require.NoError(t, err)
				if scenario == "disable-reopen" {
					_, err = s.SetEnabled(context.Background(), channel, 10, true, disabled.Revision)
				}
			}
			require.NoError(t, err)
			for _, viewer := range viewers {
				_, err = s.LookupViewer(context.Background(), viewer.Token)
				require.ErrorIs(t, err, ErrExpired)
			}
			for _, old := range historical {
				_, err = s.Resolve(context.Background(), old.PublicID, old.Signature)
				require.Error(t, err)
			}
		})
	}
}

func TestRenewedLinkKeepsExistingFLVAndHLSMediaFlowing(t *testing.T) {
	var sequence atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ".flv"):
			w.Header().Set("Content-Type", "video/x-flv")
			_, _ = io.WriteString(w, "FLV")
			w.(http.Flusher).Flush()
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-r.Context().Done():
					return
				case <-ticker.C:
					if _, err := io.WriteString(w, "next-frame\n"); err != nil {
						return
					}
					w.(http.Flusher).Flush()
				}
			}
		case strings.HasSuffix(r.URL.Path, ".m3u8"):
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			fmt.Fprintf(w, "#EXTM3U\n#EXTINF:2,\nsegment-%d.ts\n", sequence.Add(1))
		default:
			w.Header().Set("Content-Type", "video/mp2t")
			fmt.Fprint(w, r.URL.Path)
		}
	}))
	defer upstream.Close()
	s, channel, clock, old := renewalFixture(t)
	defer s.DrainViewers()
	s.SetPlaybackPreparer(playbackPreparerFunc(func(context.Context, *gbmodels.GbChannelVideoShare) (PlaybackSource, error) {
		return PlaybackSource{StreamID: "s", Generation: 1, HTTPFLVURL: upstream.URL + "/live.flv", HLSURL: upstream.URL + "/rtp/s/hls.m3u8"}, nil
	}))
	flv, err := s.CreateViewer(context.Background(), old.PublicID, old.Signature)
	require.NoError(t, err)
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { s.RelayHTTPFLV(r.Context(), w, flv.Token) }))
	defer relay.Close()
	requestCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, relay.URL, nil)
	require.NoError(t, err)
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	prefix := make([]byte, 3)
	_, err = io.ReadFull(response.Body, prefix)
	require.NoError(t, err)
	require.Equal(t, "FLV", string(prefix))
	hls, err := s.CreateViewer(context.Background(), old.PublicID, old.Signature)
	require.NoError(t, err)
	readHLS := func(expectedSegment int) {
		manifest := httptest.NewRecorder()
		s.RelayHLS(context.Background(), manifest, hls.Token, "")
		require.Equal(t, http.StatusOK, manifest.Code, manifest.Body.String())
		entries := strings.Fields(manifest.Body.String())
		resource := entries[len(entries)-1]
		body := httptest.NewRecorder()
		s.RelayHLS(context.Background(), body, hls.Token, resource[strings.LastIndex(resource, "/")+1:])
		require.Equal(t, http.StatusOK, body.Code, body.Body.String())
		require.Contains(t, body.Body.String(), fmt.Sprintf("segment-%d.ts", expectedSegment))
	}
	readHLS(1)
	clock.Store(old.ExpiresAt.UnixNano())
	renewed, err := s.AcquireLink(context.Background(), channel, 10, old.Revision)
	require.NoError(t, err)
	require.NotEqual(t, old.PublicID, renewed.PublicID)
	_, err = s.LookupViewer(context.Background(), flv.Token)
	require.NoError(t, err, "FLV authorization monitor must accept the same continuing share")
	next := make([]byte, len("next-frame\n"))
	_, err = io.ReadFull(response.Body, next)
	require.NoError(t, err)
	require.Equal(t, "next-frame\n", string(next))
	readHLS(2)
	_, err = s.SetEnabled(context.Background(), channel, 10, false, renewed.Revision)
	require.NoError(t, err)
	_, err = s.LookupViewer(context.Background(), flv.Token)
	require.ErrorIs(t, err, ErrExpired)
	_, err = s.LookupViewer(context.Background(), hls.Token)
	require.ErrorIs(t, err, ErrExpired)
	_, err = io.ReadAll(response.Body)
	require.True(t, err == nil || errors.Is(err, io.ErrUnexpectedEOF), "revoked FLV must end rather than hit client timeout")
}
