package gb28181

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/gb28181/talk"
	"uvplatform.com/uvp-gb28181/app/gb28181/uac"
	"uvplatform.com/uvp-gb28181/internal/authoritytest"
)

const cascadeDeviceAudioPull = "v=0\r\no=34020000001320000001 0 0 IN IP4 192.0.2.30\r\ns=Play\r\nc=IN IP4 192.0.2.30\r\nt=0 0\r\nm=audio 30000 RTP/AVP 8\r\na=recvonly\r\na=rtpmap:8 PCMA/8000\r\n"

func cascadeAudioBindingFixture(t *testing.T) (*gorm.DB, *repository.GormRepository, model.GbCascadeMediaSession, *sip.Request) {
	t.Helper()
	db, platform, store := prepareCascadeBroadcastFixture(t)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbTalkSession{}))
	sender := cascadeRecordSenderFunc(func(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error) {
		return uac.TrackedMessageResult{StatusCode: 200, Attempted: true}, nil
	})
	runtime := newCascadeBroadcastRuntime(db, store, newCascadePlatformClientFactory(&cascadeStatusTransport{requests: make(chan *sip.Request, 1)}, nil, time.Second), sender)
	body, err := manscdp.BuildBroadcastNotifyWithProfile(protocol.ProfileFor(protocol.Version2022), manscdp.BroadcastNotify{SN: 97, SourceID: "34020000001360000001", TargetID: "34020000001320000012"})
	require.NoError(t, err)
	notify := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, body)
	tx := siptest.NewServerTxRecorder(notify)
	defer tx.Terminate()
	require.True(t, runtime.Handle(notify, tx))
	require.Equal(t, 200, tx.Result()[0].StatusCode)
	rows, err := store.ListNonterminalBroadcastSessions(t.Context())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, []byte(cascadeDeviceAudioPull))
	req.Method = sip.INVITE
	req.CSeq().MethodName = sip.INVITE
	req.From().Address.User = "34020000001320000001"
	req.From().Params = sip.NewParams()
	req.From().Params.Add("tag", "device-audio-tag")
	req.AppendHeader(&sip.ContactHeader{Address: sip.Uri{User: "34020000001320000001", Host: "192.0.2.30", Port: 5060}})
	callID := sip.CallIDHeader("device-audio-pull")
	req.ReplaceHeader(&callID)
	req.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
	req.AppendHeader(sip.NewHeader("Subject", platform.LocalDeviceID+":0,34020000001320000002:0"))
	req.SetSource("192.0.2.30:5060")
	req.SetTransport("UDP")
	return db, store, rows[0], req
}

func TestCascadeAudioPullBindsOriginalBroadcastAndSharesChannelLease(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	db, store, row, req := cascadeAudioBindingFixture(t)
	now := time.Now().UTC()
	match, err := resolveCascadeBroadcastPull(t.Context(), db, store, req, now)
	require.NoError(t, err)
	require.NotNil(t, match)
	require.Equal(t, row.DialogKey, match.Row.DialogKey)
	require.Equal(t, "34020000001360000001", match.Binding.UpstreamSourceID)
	require.Equal(t, "34020000001320000012", match.Authorization.PublishedID)
	require.Equal(t, "Play", match.Offer.SessionName)
	require.Empty(t, match.Offer.SSRC)
	require.NoError(t, match.Claim(t.Context(), db, req, now))
	require.NotEmpty(t, match.Binding.AudioLeaseID)
	leases := talk.NewGormRepo(db)
	lease, err := leases.FindCascadeLease(t.Context(), match.Binding.AudioLeaseID)
	require.NoError(t, err)
	require.EqualValues(t, 20, lease.ChannelID)
	_, err = leases.ReserveCascadeLease(t.Context(), "cascade-audio-other", 20, "34020000001320000001", "34020000001320000002", now.Add(time.Minute))
	require.ErrorIs(t, err, talk.ErrLeaseConflict)
	again, err := resolveCascadeBroadcastPull(t.Context(), db, store, req, now)
	require.NoError(t, err)
	require.NoError(t, again.Claim(t.Context(), db, req, now))
	require.Equal(t, match.Binding.AudioLeaseID, again.Binding.AudioLeaseID)
	rows, err := leases.ListCascadeLeases(t.Context())
	require.NoError(t, err)
	require.Len(t, rows, 1, "same transaction must not reserve twice")
	var persisted model.GbCascadeMediaSession
	require.NoError(t, db.First(&persisted, row.ID).Error)
	var binding cascadeBroadcastBinding
	require.NoError(t, json.Unmarshal([]byte(persisted.SDPSummary), &binding))
	require.Equal(t, match.Binding.AudioLeaseID, binding.AudioLeaseID)
	changed := req.Clone()
	changed.SetBody([]byte(cascadeDeviceAudioPull + "y=0100000001\r\n"))
	retry, err := resolveCascadeBroadcastPull(t.Context(), db, store, changed, now)
	require.NoError(t, err)
	require.Error(t, retry.Claim(t.Context(), db, changed, now), "same transaction may not switch media content")
}

func TestCascadeAudioPullRejectsInvalidPeerOrBinding(t *testing.T) {
	for name, mutate := range map[string]func(*testing.T, *gorm.DB, *model.GbCascadeMediaSession, *sip.Request){
		"source IP": func(_ *testing.T, _ *gorm.DB, _ *model.GbCascadeMediaSession, r *sip.Request) {
			r.SetSource("192.0.2.99:5060")
		},
		"source port": func(_ *testing.T, _ *gorm.DB, _ *model.GbCascadeMediaSession, r *sip.Request) {
			r.SetSource("192.0.2.30:5061")
		},
		"transport": func(_ *testing.T, _ *gorm.DB, _ *model.GbCascadeMediaSession, r *sip.Request) { r.SetTransport("TCP") },
		"To mismatch": func(_ *testing.T, _ *gorm.DB, _ *model.GbCascadeMediaSession, r *sip.Request) {
			r.To().Address.User = "34020000002000000099"
		},
		"Subject source": func(_ *testing.T, _ *gorm.DB, _ *model.GbCascadeMediaSession, r *sip.Request) {
			r.ReplaceHeader(sip.NewHeader("Subject", "34020000001360000001:0,34020000001320000002:0"))
		},
		"Subject receiver": func(_ *testing.T, _ *gorm.DB, _ *model.GbCascadeMediaSession, r *sip.Request) {
			r.ReplaceHeader(sip.NewHeader("Subject", "34020000002000000002:0,34020000001320000099:0"))
		},
		"Subject injection": func(_ *testing.T, _ *gorm.DB, _ *model.GbCascadeMediaSession, r *sip.Request) {
			r.ReplaceHeader(sip.NewHeader("Subject", "34020000002000000002:0,34020000001320000002:bad\r\n"))
		},
		"duplicate Subject": func(_ *testing.T, _ *gorm.DB, _ *model.GbCascadeMediaSession, r *sip.Request) {
			r.AppendHeader(sip.NewHeader("Subject", r.GetHeader("Subject").Value()))
		},
		"wrong codec": func(_ *testing.T, _ *gorm.DB, _ *model.GbCascadeMediaSession, r *sip.Request) {
			r.SetBody([]byte(strings.ReplaceAll(cascadeDeviceAudioPull, "PCMA/8000", "PCMU/8000")))
		},
		"Talk compatibility is not standard pull": func(_ *testing.T, _ *gorm.DB, _ *model.GbCascadeMediaSession, r *sip.Request) {
			r.SetBody([]byte(strings.ReplaceAll(cascadeDeviceAudioPull, "s=Play", "s=Talk")))
		},
		"platform revoked": func(t *testing.T, d *gorm.DB, _ *model.GbCascadeMediaSession, _ *sip.Request) {
			require.NoError(t, d.Exec("UPDATE gb_cascade_platform SET enabled=0").Error)
		},
		"epoch changed": func(t *testing.T, d *gorm.DB, _ *model.GbCascadeMediaSession, _ *sip.Request) {
			require.NoError(t, d.Exec("UPDATE gb_device SET access_epoch=2,cleanup_completed_epoch=2").Error)
		},
		"source mismatched row": func(t *testing.T, d *gorm.DB, row *model.GbCascadeMediaSession, _ *sip.Request) {
			require.NoError(t, d.Model(row).Update("source_channel_id", 99).Error)
		},
		"expired request": func(t *testing.T, d *gorm.DB, row *model.GbCascadeMediaSession, _ *sip.Request) {
			require.NoError(t, d.Model(row).Update("received_at", time.Now().Add(-time.Minute)).Error)
		},
	} {
		t.Run(name, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			db, store, row, req := cascadeAudioBindingFixture(t)
			mutate(t, db, &row, req)
			_, err := resolveCascadeBroadcastPull(t.Context(), db, store, req, time.Now())
			require.Error(t, err)
			leases, err := talk.NewGormRepo(db).ListCascadeLeases(t.Context())
			require.NoError(t, err)
			require.Len(t, leases, 1, "invalid INVITE must leave the original Notify reservation unchanged")
		})
	}
}

func TestCascadeAudioPullClaimFailsWithoutLeakingLease(t *testing.T) {
	for _, webBusy := range []bool{false, true} {
		t.Run(map[bool]string{false: "binding changes", true: "web occupies channel"}[webBusy], func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			db, store, row, req := cascadeAudioBindingFixture(t)
			now := time.Now()
			match, err := resolveCascadeBroadcastPull(t.Context(), db, store, req, now)
			require.NoError(t, err)
			if webBusy {
				_, err := talk.NewGormRepo(db).ReleaseCascadeLease(t.Context(), match.Binding.AudioLeaseID, gbmodels.TalkSessionFailed, "original reservation stopped", now)
				require.NoError(t, err)
				lease := gbmodels.GbTalkSession{SessionID: "web", ChannelID: 20, DeviceID: "34020000001320000001", State: gbmodels.TalkSessionReserved, ExpiresAt: now.Add(time.Minute)}
				require.NoError(t, talk.NewGormRepo(db).Create(t.Context(), &lease, "token"))
			} else {
				require.NoError(t, db.Model(&row).Update("sdp_summary", row.SDPSummary+" ").Error)
			}
			require.Error(t, match.Claim(t.Context(), db, req, now))
			leases, err := talk.NewGormRepo(db).ListCascadeLeases(t.Context())
			require.NoError(t, err)
			if webBusy {
				require.Empty(t, leases)
			} else {
				require.Len(t, leases, 1, "failed claim must not create or replace a Notify reservation")
			}
		})
	}
}

func TestCascadeAudioPullRejectsAmbiguityAndLeavesOrdinaryBroadcast(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	db, store, row, req := cascadeAudioBindingFixture(t)
	unrelated := req.Clone()
	unrelated.From().Address.User = "34020000001320000099"
	match, err := resolveCascadeBroadcastPull(t.Context(), db, store, unrelated, time.Now())
	require.NoError(t, err)
	require.Nil(t, match)
	duplicate := row
	duplicate.ID = 0
	duplicate.DialogKey += "/other"
	require.NoError(t, store.CreateMediaSession(t.Context(), &duplicate))
	_, err = resolveCascadeBroadcastPull(t.Context(), db, store, req, time.Now())
	require.ErrorContains(t, err, "ambiguous")
}

func TestCascadeAudioPullRevalidatesAuthorizationBeforeClaim(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	db, store, _, req := cascadeAudioBindingFixture(t)
	match, err := resolveCascadeBroadcastPull(t.Context(), db, store, req, time.Now())
	require.NoError(t, err)
	require.NoError(t, db.Exec("UPDATE gb_device SET access_epoch=2,cleanup_completed_epoch=2").Error)
	require.Error(t, match.Claim(t.Context(), db, req, time.Now()))
	rows, err := talk.NewGormRepo(db).ListCascadeLeases(t.Context())
	require.NoError(t, err)
	require.Len(t, rows, 1, "failed claim leaves the original reservation for its owner to stop")
}
