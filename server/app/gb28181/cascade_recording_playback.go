package gb28181

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emiago/sipgo/sip"
	"go.uber.org/zap"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/catalog"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/media"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/mansrtsp"
	gbplayback "uvplatform.com/uvp-gb28181/app/gb28181/playback"
	"uvplatform.com/uvp-gb28181/app/gb28181/sdp"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

type cascadePlaybackService interface {
	Create(context.Context, gbplayback.CreateRequest) (gbplayback.CreateResult, error)
	Action(context.Context, string, string, gbplayback.ActionRequest) (*gbplayback.Session, error)
	StopForOwner(context.Context, string, string, string) error
	PeekForOwner(string, string) (*gbplayback.Session, bool)
	GetForOwner(string, string) (*gbplayback.Session, bool)
}

// A sender admission and cleanup guard serialize the source's independent
// expiry with starting the upstream sender, including ambiguous API timeouts.
type cascadePlaybackLease struct {
	mu            sync.Mutex
	runtime       *cascadeVideoRuntime
	session       *cascadeVideoSession
	service       cascadePlaybackService
	id, owner     string
	download      bool
	row           *model.GbCascadeMediaSession
	sender        *media.SenderLease
	closing       bool
	senderStopped bool
}

func (l *cascadePlaybackLease) beforeCleanup(ctx context.Context) error {
	if err := l.stopSender(ctx); err != nil {
		return err
	}
	// The INVITE owner sends BYE exactly once, after sender shutdown succeeds.
	if !l.download {
		l.session.cancel()
	}
	return nil
}

func (l *cascadePlaybackLease) stopSender(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closing = true
	if l.row == nil || l.senderStopped {
		return nil
	}
	if err := l.runtime.stopSender(ctx, *l.row); err != nil {
		return err
	}
	l.senderStopped = true
	return nil
}

func (l *cascadePlaybackLease) startSender(ctx context.Context, row model.GbCascadeMediaSession, request media.SenderRequest) (*media.SenderLease, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closing || ctx.Err() != nil {
		return nil, context.Canceled
	}
	l.row = &row
	sender, err := l.runtime.sender.Start(ctx, request)
	if err == nil {
		l.sender = sender
	}
	return sender, err
}

func (l *cascadePlaybackLease) release() {
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := l.service.StopForOwner(ctx, l.id, l.owner, "cascade session closed")
		cancel()
		if err == nil || errors.Is(err, gbplayback.ErrPlaybackNotFound) {
			return
		}
		time.Sleep(time.Second)
	}
}

func (h *cascadeVideoRuntime) acquirePlayback(ctx context.Context, session *cascadeVideoSession, target catalog.PublishedChannelTarget, offer sdp.CascadeVideoOffer) (media.StartResult, func(), error) {
	if h.playback == nil {
		return media.StartResult{}, nil, fmt.Errorf("playback service unavailable")
	}
	source, err := loadCascadeRecordingTarget(ctx, target)
	if err != nil {
		return media.StartResult{}, nil, err
	}
	owner := fmt.Sprintf("cascade:%d:%s", session.platform.ID, session.dialog.ID)
	lease := &cascadePlaybackLease{runtime: h, session: session, service: h.playback, owner: owner, download: offer.Download}
	mode := gbplayback.ModePlayback
	if offer.Download {
		mode = gbplayback.ModeDownload
	}
	recordingURI := ""
	if offer.RecordingURI != "" {
		recordingURI, err = h.resolveRecordingURI(ctx, source, target.SourceChannelID, offer)
		if err != nil {
			return media.StartResult{}, nil, err
		}
	}
	created, err := h.playback.Create(ctx, gbplayback.CreateRequest{OwnerID: owner, DeviceID: source.device.DeviceID,
		ChannelID: strconv.FormatUint(target.SourceChannelID, 10), SIPChannelID: source.channel.ChannelID, Authorization: source.authorization,
		RecordKey: owner, IdempotencyKey: session.dialog.ID, SegmentStart: offer.Start, SegmentEnd: offer.End, PlayFrom: offer.Start,
		Destination: net.JoinHostPort(source.device.IP, strconv.Itoa(source.device.Port)), Transport: source.device.Transport, PreferredNodeID: source.device.ZLMNodeID,
		Mode: mode, DownloadSpeed: offer.DownloadSpeed, RecordingURI: recordingURI, BeforeCleanup: lease.beforeCleanup})
	if err != nil {
		return media.StartResult{}, nil, err
	}
	if created.Session == nil {
		return media.StartResult{}, nil, fmt.Errorf("missing playback session")
	}
	lease.id = created.Session.ID
	session.playback = lease
	nodeID, err := strconv.ParseInt(created.Session.NodeID, 10, 64)
	if err != nil || nodeID <= 0 || created.Session.StreamID == "" {
		lease.release()
		return media.StartResult{}, nil, fmt.Errorf("invalid playback source")
	}
	return media.StartResult{NodeID: nodeID, App: "rtp", StreamID: created.Session.StreamID}, lease.release, nil
}

func (h *cascadeVideoRuntime) playbackInfo(req *sip.Request, tx sip.ServerTransaction, session *cascadeVideoSession) {
	session.controlMu.Lock()
	defer session.controlMu.Unlock()
	if !session.acknowledged.Load() || session.playback == nil || req.CSeq() == nil {
		_ = tx.Respond(sip.NewResponseFromRequest(req, 481, "Playback Dialog Does Not Exist", nil))
		return
	}
	if contentType := req.GetHeader("Content-Type"); contentType == nil || !strings.EqualFold(strings.TrimSpace(strings.SplitN(contentType.Value(), ";", 2)[0]), mansrtsp.ContentType) {
		_ = tx.Respond(sip.NewResponseFromRequest(req, 415, "Unsupported Media Type", nil))
		return
	}
	if err := session.dialog.ReadRequest(req, tx); err != nil {
		_ = tx.Respond(sip.NewResponseFromRequest(req, 500, "Invalid Dialog CSeq", nil))
		return
	}
	command, err := mansrtsp.ParseRequest(req.Body())
	if err != nil {
		_ = tx.Respond(sip.NewResponseFromRequest(req, 400, "Invalid Playback Control", nil))
		return
	}
	ctx, cancel := context.WithTimeout(session.dialog.Context(), 15*time.Second)
	defer cancel()
	latest, err := h.store.FindPlatform(ctx, session.platform.ID)
	if err != nil || !latest.Enabled || !latest.RecordingEnabled || latest.ConfigRevision != session.platform.ConfigRevision || latest.ProjectionRevision != session.platform.ProjectionRevision {
		session.cancel()
		_ = tx.Respond(sip.NewResponseFromRequest(req, 403, "Playback Permission Changed", nil))
		return
	}
	lease := session.playback
	if command.Action == "stop" {
		response := sip.NewResponseFromRequest(req, 200, "OK", mansrtsp.BuildResponse(command.CSeq, 200, "OK"))
		response.AppendHeader(sip.NewHeader("Content-Type", mansrtsp.ContentType))
		_ = tx.Respond(response)
		session.cancel()
		return
	}
	refreshed := true
	if statusOKForPlaybackAction(command.Action) {
		// Every PLAY may change the next emitted packet (including a Scale-only
		// request). Clear the old sample before forwarding control so RTP-Info
		// can never describe a packet from the previous PLAY.
		session.rtpInfo = nil
		if session.sender != nil {
			refreshed = session.sender.CanSampleRTPInfo() && session.sender.ResetRTPInfo(ctx) == nil
		}
	}
	_, err = lease.service.Action(ctx, lease.id, lease.owner, gbplayback.ActionRequest{Action: command.Action, PositionSeconds: command.PositionSeconds, Scale: command.Scale, RangeValue: command.RangeValue})
	if err == nil && statusOKForPlaybackAction(command.Action) && refreshed && session.sender != nil {
		// The downstream INFO transaction can complete before the sender emits
		// its first packet. Poll briefly for the sample tied to this PLAY; older
		// media nodes without the optional sampler still omit RTP-Info.
		sampleCtx, sampleCancel := context.WithTimeout(ctx, time.Second)
		defer sampleCancel()
		for {
			info, refreshErr := session.sender.RefreshRTPInfo(sampleCtx)
			if refreshErr != nil || info != nil && strings.TrimSpace(info.URL) != "" {
				if refreshErr == nil && info != nil && strings.TrimSpace(info.URL) != "" {
					session.rtpInfo = &mansrtsp.RTPInfo{URL: info.URL, Seq: info.Seq, RTPTime: info.RTPTime}
				}
				break
			}
			select {
			case <-sampleCtx.Done():
				break
			case <-time.After(20 * time.Millisecond):
				continue
			}
			break
		}
	}
	status, reason := 200, "OK"
	if err != nil {
		status, reason = 500, "Playback Control Failed"
		if errors.Is(err, gbplayback.ErrInvalidSession) {
			status, reason = 457, "Invalid Range"
		}
		if errors.Is(err, gbplayback.ErrPlaybackRangeUnsupported) {
			status, reason = 501, "Range Not Supported"
		}
		if errors.Is(err, gbplayback.ErrPlaybackNotFound) {
			status, reason = 454, "Session Not Found"
		}
	}
	body := mansrtsp.BuildResponse(command.CSeq, status, reason)
	if status == 200 && command.Action != "pause" && session.rtpInfo != nil {
		body = mansrtsp.BuildResponseWithHeaders(command.CSeq, status, reason, session.rtpInfo.Header())
	}
	response := sip.NewResponseFromRequest(req, 200, "OK", body)
	response.AppendHeader(sip.NewHeader("Content-Type", mansrtsp.ContentType))
	_ = tx.Respond(response)
}

func statusOKForPlaybackAction(action string) bool {
	return action != "pause" && action != "stop"
}

func cascadeRTPInfoURL(mediaIP, app, stream string) string {
	host := strings.TrimSpace(mediaIP)
	if host == "" || strings.TrimSpace(app) == "" || strings.TrimSpace(stream) == "" {
		return ""
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	return "rtsp://" + host + "/" + strings.Trim(app, "/") + "/" + strings.Trim(stream, "/")
}

func (h *cascadeVideoRuntime) waitCascadeVideo(ctx context.Context, session *cascadeVideoSession, sender *media.SenderLease) {
	interval := h.monitorInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	const senderCheckFailureThreshold = 3
	senderCheckFailures := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ended := false
			if lease := session.playback; lease != nil {
				current, ok := lease.service.PeekForOwner(lease.id, lease.owner)
				if ok && lease.download && current.State == gbplayback.StateStopping {
					// Wait for the source cleanup outcome before distinguishing a
					// complete download from an interrupted/expired source.
					continue
				}
				ended = !ok || current.State.IsTerminal() || current.State == gbplayback.StateStopping
				if ok && lease.download && current.State == gbplayback.StateEnded && current.EndReason == "file-to-end" {
					if err := h.notifyDownloadComplete(ctx, session); err != nil {
						app.Log(ctx).Warn("级联录像下载完成通知失败",
							zap.String("event", "cascade.download.completion_notify_failed"),
							zap.Uint64("platform_id", session.platform.ID), zap.String("call_id", session.dialog.InviteRequest.CallID().Value()), zap.Error(err))
					} else {
						// GB/T 28181-2022 9.9.2.1: after acknowledging the
						// completion MESSAGE the receiver ends the dialog with BYE.
						// Bound the wait so an absent receiver cannot retain it forever.
						timeout := h.downloadByeTimeout
						if timeout <= 0 {
							timeout = 32 * time.Second
						}
						timer := time.NewTimer(timeout)
						defer timer.Stop()
						select {
						case <-ctx.Done():
							return
						case <-timer.C:
						}
					}
				}
				if !ended && current.State == gbplayback.StatePlaying && session.acknowledged.Load() {
					_, _ = lease.service.GetForOwner(lease.id, lease.owner)
				}
			}
			if !ended && sender != nil {
				checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
				active, err := sender.IsActive(checkCtx)
				cancel()
				if err != nil {
					senderCheckFailures++
					ended = senderCheckFailures >= senderCheckFailureThreshold
				} else {
					senderCheckFailures = 0
					ended = !active
				}
			}
			if ended {
				session.cancel()
				return
			}
		}
	}
}

func (h *cascadeVideoRuntime) notifyDownloadComplete(ctx context.Context, session *cascadeVideoSession) error {
	session.controlMu.Lock()
	defer session.controlMu.Unlock()
	body, err := manscdp.BuildMediaStatusFileEnd(cascadeWireProfile(session.platform), session.source.SenderID, 1)
	if err != nil {
		return err
	}
	request := sip.NewRequest(sip.MESSAGE, session.dialog.InviteRequest.Contact().Address)
	request.AppendHeader(sip.NewHeader("Content-Type", "Application/MANSCDP+xml"))
	request.SetBody(body)
	sendCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	response, err := session.dialog.Do(sendCtx, request)
	if err != nil {
		return fmt.Errorf("notify download completion: %w", err)
	}
	if response == nil || response.StatusCode != 200 {
		return fmt.Errorf("download completion was not acknowledged")
	}
	return nil
}
