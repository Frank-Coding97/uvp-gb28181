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

// 真机（大华，2026-10-24 抓包）发起广播音频 INVITE 时把收件人写成**通道编码**：
//   INVITE sip:37010301021320000006@<平台>:52064
// 平台编码反而没出现在收件人里。这种写法必须仍能匹配到本次广播，否则设备只能
// 收到 403，语音广播整条链路就是断的。
func TestCascadeAudioPullAcceptsInviteAddressedToChannelCode(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	db, store, row, req := cascadeAudioBindingFixture(t)
	req.To().Address.User = "34020000001320000002"
	req.Recipient.User = "34020000001320000002"
	match, err := resolveCascadeBroadcastPull(t.Context(), db, store, req, time.Now().UTC())
	require.NoError(t, err)
	require.NotNil(t, match)
	require.Equal(t, row.DialogKey, match.Row.DialogKey)
}

// 收件人既不是平台编码也不是通道编码时仍然必须拒绝（不能因为放宽就把任意
// 音频 INVITE 都收进来）。
func TestCascadeAudioPullRejectsInviteAddressedElsewhere(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	db, store, _, req := cascadeAudioBindingFixture(t)
	req.To().Address.User = "34020000001320000099"
	req.Recipient.User = "34020000001320000099"
	match, err := resolveCascadeBroadcastPull(t.Context(), db, store, req, time.Now().UTC())
	require.NoError(t, err)
	require.Nil(t, match)
}

// 2026-10-24 大华真机抓包原样（收件人写通道编码 + Subject 里 SSRC 是十六进制）：
//   INVITE sip:37010301021320000006@<平台>:52064
//   Subject: 34020000012000000001:03d7a8ef3276a8ef327,37010301021320000006:0552354c54f3354c54f
//   v=0 ... s=Play ... m=audio 9728 RTP/AVP 8 96 ... a=recvonly ... y=0003363721
// 这套信封必须能匹配；否则语音广播永远停在 488。
func TestCascadeAudioPullAcceptsDahuaBroadcastEnvelope(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	db, store, row, req := cascadeAudioBindingFixture(t)
	req.To().Address.User = "34020000001320000002"
	req.Recipient.User = "34020000001320000002"
	req.ReplaceHeader(sip.NewHeader("Subject", "34020000002000000002:03d7a8ef3276a8ef327,34020000001320000002:0552354c54f3354c54f"))
	match, err := resolveCascadeBroadcastPull(t.Context(), db, store, req, time.Now().UTC())
	require.NoError(t, err)
	require.NotNil(t, match)
	require.Equal(t, row.DialogKey, match.Row.DialogKey)
}

// Subject 里的 SSRC 段仍然不能乱写（夹带分隔符/空值/超长一律拒绝）。
func TestMediaSequenceTokenRejectsMalformedValues(t *testing.T) {
	for _, value := range []string{"", "12:34", "0x1f", "123456789012345678901", "zz"} {
		require.False(t, mediaSequenceToken(value), value)
	}
	for _, value := range []string{"0", "0003363721", "03d7a8ef3276a8ef327"} {
		require.True(t, mediaSequenceToken(value), value)
	}
}

// 设备发完 Broadcast 应答会立刻发音频 INVITE；应答落库（DeviceResult 写进同一行的
// sdp_summary）与音频认领是并发写同一行。认领必须在这种并发下仍然成功，且不能
// 把应答结果覆盖掉，否则设备只能收到 486，语音广播永远建不起来（真机复现）。
func TestCascadeAudioClaimSurvivesConcurrentBroadcastResultWrite(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	db, store, row, req := cascadeAudioBindingFixture(t)
	now := time.Now().UTC()
	match, err := resolveCascadeBroadcastPull(t.Context(), db, store, req, now)
	require.NoError(t, err)
	require.NotNil(t, match)
	// 模拟「应答处理」在 resolve 之后、Claim 之前改写 sdp_summary。
	var live model.GbCascadeMediaSession
	require.NoError(t, db.First(&live, row.ID).Error)
	var binding cascadeBroadcastBinding
	require.NoError(t, json.Unmarshal([]byte(live.SDPSummary), &binding))
	binding.DeviceResult = "OK"
	binding.NotifyStatus = 200
	updated, err := json.Marshal(binding)
	require.NoError(t, err)
	require.NoError(t, db.Model(&live).Update("sdp_summary", string(updated)).Error)

	require.NoError(t, match.Claim(t.Context(), db, req, now))
	var persisted model.GbCascadeMediaSession
	require.NoError(t, db.First(&persisted, row.ID).Error)
	var after cascadeBroadcastBinding
	require.NoError(t, json.Unmarshal([]byte(persisted.SDPSummary), &after))
	require.Equal(t, "OK", after.DeviceResult, "认领不能把应答结果覆盖掉")
	require.NotEmpty(t, after.AudioCallID)
	require.Equal(t, match.Binding.AudioLeaseID, after.AudioLeaseID)
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
				// ⛔ 这里必须改**授权相关**的字段。早先的写法是往 sdp_summary 末尾
				// 补一个空格（等于只改格式），用来代表"绑定被别的流程改过"；但真机上
				// 设备应答会合法地改写同一行的结果字段，纯粹格式变化/结果变化都不能
				// 再判失败（见 TestCascadeAudioClaimSurvivesConcurrentBroadcastResultWrite）。
				// 现在改成把通道编码换掉，语义上就是"授权绑定变了"，仍须拒绝。
				var live model.GbCascadeMediaSession
				require.NoError(t, db.First(&live, row.ID).Error)
				var binding cascadeBroadcastBinding
				require.NoError(t, json.Unmarshal([]byte(live.SDPSummary), &binding))
				binding.ChannelCode = "34020000001320000099"
				changed, err := json.Marshal(binding)
				require.NoError(t, err)
				require.NoError(t, db.Model(&live).Update("sdp_summary", string(changed)).Error)
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
