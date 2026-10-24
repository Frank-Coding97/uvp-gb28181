package gb28181

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emiago/sipgo/sip"
	"go.uber.org/zap"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	cascadeservice "uvplatform.com/uvp-gb28181/app/gb28181/cascade/service"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/sipclient"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

type cascadeEventSubscriptionKind string

const (
	cascadeEventPTZ   cascadeEventSubscriptionKind = "ptz"
	cascadeEventAlarm cascadeEventSubscriptionKind = "alarm"
)

type cascadeEventSubscription struct {
	key              string
	deliveryID       string
	kind             cascadeEventSubscriptionKind
	platform         model.GbCascadePlatform
	client           *sipclient.TransactionClient
	dialog           sipclient.NotifyDialog
	target           string
	sn               int
	expires          time.Time
	remoteSeq        uint32
	notifySeq        uint32
	subscribedAt     time.Time
	renewedAt        time.Time
	lastNotifyAt     *time.Time
	lastNotifyStatus string
	lastNotifyCode   int
	reason           string
	generation       uint64
	force            bool
	failures         int
	sendCancel       context.CancelFunc
	wake             chan struct{}
	mu               sync.Mutex
	sendMu           sync.Mutex
}

type cascadeEventSubscriptions struct {
	store    *repository.GormRepository
	clients  *cascadePlatformClientFactory
	interval time.Duration
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	sessions map[string]*cascadeEventSubscription
	closed   bool
	wg       sync.WaitGroup
	done     chan struct{}
}

func newCascadeEventSubscriptions(store *repository.GormRepository, clients *cascadePlatformClientFactory, interval time.Duration) *cascadeEventSubscriptions {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ctx, cancel := context.WithCancel(context.Background())
	manager := &cascadeEventSubscriptions{store: store, clients: clients, interval: interval, ctx: ctx, cancel: cancel, sessions: make(map[string]*cascadeEventSubscription), done: make(chan struct{})}
	manager.wg.Add(1)
	go manager.runAlarmDeliveryWorker()
	return manager
}

func (m *cascadeEventSubscriptions) Subscriptions(p model.GbCascadePlatform) cascadeservice.PlatformSubscriptions {
	result := cascadeservice.NewPlatformSubscriptions("unavailable")
	if m == nil {
		return result
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return result
	}
	sessions := make([]*cascadeEventSubscription, 0, len(m.sessions))
	for _, s := range m.sessions {
		if s.platform.ID == p.ID {
			sessions = append(sessions, s)
		}
	}
	m.mu.Unlock()
	result.PTZPosition.State = "inactive"
	result.Alarm.State = "inactive"
	now := time.Now()
	for _, s := range sessions {
		s.mu.Lock()
		if p.Enabled && s.platform.ConfigRevision == p.ConfigRevision && cascadeWireProfile(s.platform) == cascadeWireProfile(p) && s.reason == "" && now.Before(s.expires) {
			view := cascadeservice.SubscriptionSession{Target: s.target, Event: s.dialog.Event, SubscribedAt: s.subscribedAt, RenewedAt: s.renewedAt, ExpiresAt: s.expires, LastNotifyStatus: s.lastNotifyStatus, LastNotifyCode: s.lastNotifyCode}
			if s.lastNotifyAt != nil {
				at := *s.lastNotifyAt
				view.LastNotifyAt = &at
			}
			if s.kind == cascadeEventPTZ {
				result.PTZPosition.Sessions = append(result.PTZPosition.Sessions, view)
			} else {
				result.Alarm.Sessions = append(result.Alarm.Sessions, view)
			}
		}
		s.mu.Unlock()
	}
	if len(result.PTZPosition.Sessions) > 0 {
		result.PTZPosition.State = "active"
	}
	if len(result.Alarm.Sessions) > 0 {
		result.Alarm.State = "active"
	}
	sort.Slice(result.PTZPosition.Sessions, func(i, j int) bool {
		return result.PTZPosition.Sessions[i].Target < result.PTZPosition.Sessions[j].Target
	})
	sort.Slice(result.Alarm.Sessions, func(i, j int) bool { return result.Alarm.Sessions[i].Target < result.Alarm.Sessions[j].Target })
	return result
}

func (m *cascadeEventSubscriptions) Handle(req *sip.Request, tx sip.ServerTransaction) bool {
	if req == nil || tx == nil || req.Method != sip.SUBSCRIBE {
		return false
	}
	kind, event, ok := m.subscriptionEventKind(req)
	if !ok {
		return false
	}
	reject := func(code int, reason string) bool {
		_ = tx.Respond(sip.NewResponseFromRequest(req, code, reason, nil))
		return true
	}
	expires, err := catalogSubscriptionExpires(req)
	if err != nil {
		return reject(400, "Invalid Expires")
	}
	if req.From() == nil || req.To() == nil || req.CallID() == nil || req.CallID().Value() == "" || req.CSeq() == nil || req.CSeq().MethodName != sip.SUBSCRIBE || req.CSeq().SeqNo == 0 || req.Contact() == nil || req.Contact().Address.Scheme != "sip" || req.Contact().Address.Host == "" {
		return reject(400, "Invalid subscription dialog")
	}
	remoteTag, _ := req.From().Params.Get("tag")
	localTag, _ := req.To().Params.Get("tag")
	if remoteTag == "" {
		return reject(400, "Missing From tag")
	}
	ctx, cancel := context.WithTimeout(m.ctx, 5*time.Second)
	defer cancel()
	platforms, err := m.store.ListPlatforms(ctx)
	if err != nil {
		return reject(503, "Subscription store unavailable")
	}
	p, ambiguous := findCascadeControlPeer(req, platforms)
	if p == nil || ambiguous || !p.Enabled {
		return reject(403, "Subscription upstream rejected")
	}
	key := fmt.Sprintf("%d|%s|%s|%s", p.ID, req.CallID().Value(), remoteTag, kind)
	m.mu.Lock()
	existing := m.sessions[key]
	m.mu.Unlock()
	if localTag != "" {
		if existing == nil {
			return reject(481, "Subscription Does Not Exist")
		}
		return m.refresh(ctx, req, tx, existing, *p, expires, event)
	}
	if existing != nil || expires == 0 {
		return reject(481, "Subscription Does Not Exist")
	}
	query, err := manscdp.ParseSubscriptionQuery(req.Body())
	if err != nil || !cascadeSubscriptionQueryMatches(kind, query.CmdType, event) {
		return reject(400, "Invalid subscription query")
	}
	if !cascadeDeviceStatusRequestTargetsMatch(p.LocalDeviceID, query.DeviceID, req.To().Address.User, req.Recipient.User) {
		return reject(403, "Subscription target mismatch")
	}
	allowed, err := m.targetAllowed(ctx, *p, kind, query.DeviceID)
	if err != nil {
		return reject(503, "Subscription scope unavailable")
	}
	if !allowed {
		return reject(403, "Subscription target not published")
	}
	client, err := m.clients.NewClient(*p)
	if err != nil {
		return reject(503, "Subscription sender unavailable")
	}
	response := sip.NewResponseFromRequest(req, 200, "OK", nil)
	response.AppendHeader(sip.NewHeader("Expires", strconv.Itoa(expires)))
	response.AppendHeader(&sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: p.LocalDeviceID, Host: p.LocalSIPIP, Port: p.LocalSIPPort}})
	cloned := req.Clone()
	from := response.To().AsFrom()
	to := cloned.From().AsTo()
	state := &cascadeEventSubscription{key: key, kind: kind, platform: *p, client: client.(*cascadePlatformClient).transactions, dialog: sipclient.NotifyDialog{CallID: req.CallID().Value(), From: from, To: to, RemoteTarget: *cloned.Contact().Address.Clone(), Event: event}, target: query.DeviceID, sn: query.SN, expires: time.Now().Add(time.Duration(expires) * time.Second), remoteSeq: req.CSeq().SeqNo, generation: 1, force: true, wake: make(chan struct{}, 1)}
	for _, header := range cloned.GetHeaders("Record-Route") {
		route, ok := header.(*sip.RecordRouteHeader)
		if !ok {
			return reject(400, "Invalid subscription route")
		}
		state.dialog.Routes = append(state.dialog.Routes, *route.Address.Clone())
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return reject(503, "Subscription service stopped")
	}
	if m.sessions[key] != nil {
		m.mu.Unlock()
		return reject(481, "Subscription already exists")
	}
	if err := tx.Respond(response); err != nil {
		m.mu.Unlock()
		return true
	}
	state.subscribedAt = time.Now()
	state.renewedAt = state.subscribedAt
	state.lastNotifyStatus = "pending"
	m.sessions[key] = state
	m.wg.Add(1)
	m.mu.Unlock()
	go m.run(state)
	return true
}

func (m *cascadeEventSubscriptions) subscriptionEventKind(req *sip.Request) (cascadeEventSubscriptionKind, string, bool) {
	if kind, event, ok := cascadeSubscriptionEventKind(req); ok {
		return kind, event, true
	}
	if m == nil || req == nil || len(req.Body()) != 0 || req.CallID() == nil || req.From() == nil {
		return "", "", false
	}
	eventHeaders := req.GetHeaders("Event")
	if len(eventHeaders) != 1 || !strings.EqualFold(strings.TrimSpace(strings.Split(eventHeaders[0].Value(), ";")[0]), "presence") {
		return "", "", false
	}
	remoteTag, _ := req.From().Params.Get("tag")
	if remoteTag == "" {
		return "", "", false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		if s.kind != cascadeEventAlarm || !strings.EqualFold(s.dialog.Event, "presence") || s.dialog.CallID != req.CallID().Value() {
			continue
		}
		tag, _ := s.dialog.To.Params.Get("tag")
		if tag == remoteTag {
			return cascadeEventAlarm, "presence", true
		}
	}
	return "", "", false
}

func cascadeSubscriptionEventKind(req *sip.Request) (cascadeEventSubscriptionKind, string, bool) {
	headers := req.GetHeaders("Event")
	if len(headers) != 1 {
		return "", "", false
	}
	event := strings.TrimSpace(strings.Split(headers[0].Value(), ";")[0])
	if strings.EqualFold(event, "PTZPosition") || strings.EqualFold(event, "PTZPrecisePosition") {
		return cascadeEventPTZ, event, true
	}
	if strings.EqualFold(event, "Alarm") {
		return cascadeEventAlarm, event, true
	}
	if strings.EqualFold(event, "presence") {
		query, err := manscdp.ParseSubscriptionQuery(req.Body())
		if err != nil {
			return "", "", false
		}
		if query.CmdType == manscdp.CmdAlarm {
			return cascadeEventAlarm, event, true
		}
	}
	return "", "", false
}

func cascadeSubscriptionQueryMatches(kind cascadeEventSubscriptionKind, cmdType, event string) bool {
	switch kind {
	case cascadeEventPTZ:
		return (cmdType == manscdp.CmdPTZPosition || cmdType == manscdp.CmdPTZPrecisePosition) && strings.EqualFold(event, cmdType)
	case cascadeEventAlarm:
		return cmdType == manscdp.CmdAlarm && (strings.EqualFold(event, "Alarm") || strings.EqualFold(event, "presence"))
	default:
		return false
	}
}

func (m *cascadeEventSubscriptions) targetAllowed(ctx context.Context, p model.GbCascadePlatform, kind cascadeEventSubscriptionKind, target string) (bool, error) {
	if target == p.LocalDeviceID {
		return p.PublishPlatform, nil
	}
	projection, err := m.store.ProjectionSnapshot(ctx, p.ID)
	if err != nil {
		return false, err
	}
	for _, device := range projection.Devices {
		if device.Active && device.PublishedDeviceID == target {
			if kind != cascadeEventPTZ {
				return true, nil
			}
			for _, channel := range projection.Channels {
				if channel.Active && channel.DeviceProjectionID == device.ID && channel.PTZAllowed {
					return true, nil
				}
			}
			return false, nil
		}
	}
	for _, channel := range projection.Channels {
		if channel.Active && channel.PublishedChannelID == target {
			return kind != cascadeEventPTZ || channel.PTZAllowed, nil
		}
	}
	return false, nil
}

func (m *cascadeEventSubscriptions) refresh(ctx context.Context, req *sip.Request, tx sip.ServerTransaction, s *cascadeEventSubscription, p model.GbCascadePlatform, expires int, event string) bool {
	reject := func(code int, reason string) bool {
		_ = tx.Respond(sip.NewResponseFromRequest(req, code, reason, nil))
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tag, _ := req.To().Params.Get("tag")
	expected, _ := s.dialog.From.Params.Get("tag")
	if !strings.EqualFold(event, s.dialog.Event) || tag != expected || req.From().Address.String() != s.dialog.To.Address.String() || req.To().Address.String() != s.dialog.From.Address.String() || s.reason != "" || !time.Now().Before(s.expires) || p.ConfigRevision != s.platform.ConfigRevision || cascadeWireProfile(p) != cascadeWireProfile(s.platform) {
		return reject(481, "Subscription Does Not Exist")
	}
	if req.CSeq().SeqNo <= s.remoteSeq {
		return reject(500, "Subscription CSeq out of order")
	}
	var nextSN *int
	if len(req.Body()) > 0 {
		query, err := manscdp.ParseSubscriptionQuery(req.Body())
		if err != nil || query.DeviceID != s.target || !cascadeSubscriptionQueryMatches(s.kind, query.CmdType, event) {
			return reject(403, "Subscription scope changed")
		}
		nextSN = &query.SN
	}
	response := sip.NewResponseFromRequest(req, 200, "OK", nil)
	response.AppendHeader(sip.NewHeader("Expires", strconv.Itoa(expires)))
	response.AppendHeader(&sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: p.LocalDeviceID, Host: p.LocalSIPIP, Port: p.LocalSIPPort}})
	if err := tx.Respond(response); err != nil {
		return true
	}
	if nextSN != nil {
		s.sn = *nextSN
	}
	if s.sendCancel != nil {
		s.sendCancel()
	}
	s.generation++
	s.remoteSeq = req.CSeq().SeqNo
	s.renewedAt = time.Now()
	s.expires = s.renewedAt.Add(time.Duration(expires) * time.Second)
	s.force = true
	s.dialog.RemoteTarget = *req.Contact().Address.Clone()
	if expires == 0 {
		s.reason = "timeout"
	}
	s.signal()
	return true
}

func (s *cascadeEventSubscription) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (m *cascadeEventSubscriptions) remove(s *cascadeEventSubscription) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[s.key] == s {
		delete(m.sessions, s.key)
	}
}

func (m *cascadeEventSubscriptions) run(s *cascadeEventSubscription) {
	defer m.wg.Done()
	defer m.remove(s)
	for {
		s.mu.Lock()
		reason := s.reason
		force := s.force
		wait := time.Until(s.expires)
		if reason != "" {
			wait = 0
		}
		s.mu.Unlock()
		if reason != "" || wait <= 0 {
			m.terminate(s, reason)
			return
		}
		if force {
			m.notifyActive(s)
			timer := time.NewTimer(min(wait, m.interval))
			select {
			case <-m.ctx.Done():
				timer.Stop()
				return
			case <-s.wake:
				timer.Stop()
			case <-timer.C:
			}
			continue
		}
		timer := time.NewTimer(wait)
		select {
		case <-m.ctx.Done():
			timer.Stop()
			return
		case <-s.wake:
			timer.Stop()
		case <-timer.C:
			s.mu.Lock()
			if s.reason == "" {
				s.reason = "timeout"
			}
			s.mu.Unlock()
		}
	}
}

func (m *cascadeEventSubscriptions) notifyActive(s *cascadeEventSubscription) {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	s.mu.Lock()
	if s.reason != "" || !s.force || !time.Now().Before(s.expires) {
		s.mu.Unlock()
		return
	}
	generation := s.generation
	dialog := s.dialog
	s.notifySeq++
	seq := s.notifySeq
	expires := s.expires
	ctx, cancel := context.WithTimeout(m.ctx, min(time.Until(expires), 3*time.Second))
	s.sendCancel = cancel
	s.mu.Unlock()
	defer cancel()
	result := s.client.SendNotify(ctx, dialog, seq, "active;expires="+strconv.Itoa(max(1, int(time.Until(expires).Seconds()))), nil)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifySeq = max(s.notifySeq, result.LastCSeq)
	if s.generation != generation {
		return
	}
	s.force = s.recordNotifyResult(result) != nil && s.reason == ""
}

// Caller holds s.mu. A received SIP rejection can have no transport error;
// preserve its status as an error rather than joining two nil errors.
func (s *cascadeEventSubscription) recordNotifyResult(result sipclient.NotifyResult) error {
	at := time.Now()
	s.lastNotifyAt, s.lastNotifyCode = &at, result.StatusCode
	if result.Success && result.StatusCode == 200 {
		s.lastNotifyStatus, s.failures = "success", 0
		return nil
	}
	s.lastNotifyStatus = "failed"
	s.failures++
	if (result.StatusCode >= 400 && result.StatusCode < 500 && result.StatusCode != 429) ||
		errors.Is(result.TransportErr, context.DeadlineExceeded) || errors.Is(result.TransportErr, sip.ErrTransactionTimeout) || s.failures >= 3 {
		s.reason = "noresource"
		s.signal()
	}
	return errors.Join(fmt.Errorf("cascade NOTIFY failed with SIP status %d", result.StatusCode), result.BuildErr, result.TransportErr)
}

func (m *cascadeEventSubscriptions) terminate(s *cascadeEventSubscription, reason string) {
	if reason == "" {
		reason = "timeout"
	}
	ctx, cancel := context.WithTimeout(m.ctx, 3*time.Second)
	defer cancel()
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	s.mu.Lock()
	s.notifySeq++
	seq := s.notifySeq
	dialog := s.dialog
	s.mu.Unlock()
	result := s.client.SendNotify(ctx, dialog, seq, "terminated;reason="+reason, nil)
	s.mu.Lock()
	s.notifySeq = max(s.notifySeq, result.LastCSeq)
	s.mu.Unlock()
}

func (m *cascadeEventSubscriptions) BroadcastPTZ(ctx context.Context, deviceCode, callID, cseq string, body []byte) error {
	notify, err := manscdp.ParsePTZPrecisePositionNotify(body)
	if err != nil {
		return err
	}
	return m.broadcast(ctx, cascadeEventPTZ, deviceCode, notify.DeviceID, notify.SN, callID, cseq, notify, true)
}

func (m *cascadeEventSubscriptions) BroadcastAlarm(ctx context.Context, deviceCode, callID, cseq string, body []byte) error {
	return m.acceptAlarm(ctx, deviceCode, callID, cseq, body, true)
}

// EnqueueAlarm acknowledges durable acceptance independently of upstream delivery.
func (m *cascadeEventSubscriptions) EnqueueAlarm(ctx context.Context, deviceCode, callID, cseq string, body []byte) error {
	return m.acceptAlarm(ctx, deviceCode, callID, cseq, body, false)
}

func (m *cascadeEventSubscriptions) acceptAlarm(ctx context.Context, deviceCode, callID, cseq string, body []byte, dispatch bool) error {
	notify, err := manscdp.ParseAlarmNotify(body)
	if err != nil {
		return err
	}
	sn, err := strconv.Atoi(strings.TrimSpace(notify.SN))
	if err != nil || sn <= 0 {
		return fmt.Errorf("非法告警通知 SN")
	}
	return m.broadcast(ctx, cascadeEventAlarm, deviceCode, notify.DeviceID, sn, callID, cseq, notify, dispatch)
}

func (m *cascadeEventSubscriptions) broadcast(ctx context.Context, kind cascadeEventSubscriptionKind, deviceCode, sourceCode string, eventSN int, callID, cseq string, notify any, dispatch bool) error {
	if m == nil || m.store == nil {
		if kind == cascadeEventAlarm && !dispatch {
			return fmt.Errorf("cascade Alarm acceptance runtime is unavailable")
		}
		return nil
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		if kind == cascadeEventAlarm && !dispatch {
			return fmt.Errorf("cascade Alarm acceptance runtime is stopped")
		}
		return nil
	}
	m.wg.Add(1)
	m.mu.Unlock()
	defer m.wg.Done()
	sendCtx, stopSend := context.WithCancel(ctx)
	stopManager := context.AfterFunc(m.ctx, stopSend)
	defer func() { stopManager(); stopSend() }()
	platforms, err := m.store.ListEnabledPlatforms(sendCtx)
	if err != nil {
		return err
	}
	var firstErr error
	for _, p := range platforms {
		if err := sendCtx.Err(); err != nil {
			return errors.Join(firstErr, err)
		}
		published, parent, ok, resolveErr := m.resolveSource(sendCtx, p, kind, deviceCode, sourceCode)
		if resolveErr != nil {
			if firstErr == nil {
				firstErr = resolveErr
			}
			continue
		}
		if !ok {
			continue
		}
		if kind == cascadeEventAlarm {
			key, sendErr := m.queueAlarm(sendCtx, p, deviceCode, sourceCode, published, parent, eventSN, callID, cseq, notify)
			if sendErr == nil && dispatch {
				sendErr = m.dispatchAlarmDelivery(sendCtx, key)
			}
			firstErr = errors.Join(firstErr, sendErr)
			continue
		}
		m.mu.Lock()
		sessions := make([]*cascadeEventSubscription, 0)
		for _, s := range m.sessions {
			if s.platform.ID == p.ID && s.kind == kind {
				sessions = append(sessions, s)
			}
		}
		m.mu.Unlock()
		for _, s := range sessions {
			s.sendMu.Lock()
			s.mu.Lock()
			if s.reason != "" || !time.Now().Before(s.expires) || s.platform.ConfigRevision != p.ConfigRevision || cascadeWireProfile(s.platform) != cascadeWireProfile(p) || (s.target != published && s.target != parent && (s.target != p.LocalDeviceID || !p.PublishPlatform)) {
				s.mu.Unlock()
				s.sendMu.Unlock()
				continue
			}
			current, currentParent, allowed, checkErr := m.resolveSource(sendCtx, p, kind, deviceCode, sourceCode)
			if checkErr != nil || !allowed || current != published || currentParent != parent || sendCtx.Err() != nil {
				s.mu.Unlock()
				s.sendMu.Unlock()
				if firstErr == nil {
					firstErr = errors.Join(checkErr, sendCtx.Err())
				}
				continue
			}
			generation := s.generation
			profile := cascadeWireProfile(p)
			outBody, buildErr := buildCascadeEventBody(profile, kind, published, s.sn, notify)
			if buildErr != nil {
				s.mu.Unlock()
				s.sendMu.Unlock()
				if firstErr == nil {
					firstErr = buildErr
				}
				continue
			}
			s.notifySeq++
			seq := s.notifySeq
			dialog := s.dialog
			expires := s.expires
			notifyCtx, cancelNotify := context.WithTimeout(sendCtx, min(time.Until(expires), 3*time.Second))
			s.sendCancel = cancelNotify
			s.mu.Unlock()
			result := s.client.SendNotify(notifyCtx, dialog, seq, "active;expires="+strconv.Itoa(max(1, int(time.Until(expires).Seconds()))), outBody)
			cancelNotify()
			s.mu.Lock()
			s.notifySeq = max(s.notifySeq, result.LastCSeq)
			if s.generation == generation {
				if notifyErr := s.recordNotifyResult(result); notifyErr != nil && firstErr == nil {
					firstErr = notifyErr
				}
			}
			s.mu.Unlock()
			s.sendMu.Unlock()
		}
	}
	if firstErr != nil {
		app.Log(ctx).Warn("级联事件分发失败", zap.String("event", "cascade.event.dispatch_failed"), zap.String("kind", string(kind)), zap.String("call_id", callID), zap.String("cseq", cseq), zap.Int("event_sn", eventSN), zap.Error(firstErr))
	}
	return firstErr
}

func (m *cascadeEventSubscriptions) resolveSource(ctx context.Context, p model.GbCascadePlatform, kind cascadeEventSubscriptionKind, deviceCode, sourceCode string) (string, string, bool, error) {
	projection, err := m.store.ProjectionSnapshot(ctx, p.ID)
	if errors.Is(err, repository.ErrPlatformNotFound) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	if !projection.Platform.Enabled || projection.Platform.ConfigRevision != p.ConfigRevision || projection.Revision != p.ProjectionRevision || cascadeWireProfile(projection.Platform) != cascadeWireProfile(p) {
		return "", "", false, nil
	}
	for _, channel := range projection.Channels {
		if !channel.Active || (kind == cascadeEventPTZ && !channel.PTZAllowed) {
			continue
		}
		var device model.GbCascadeDeviceProjection
		for _, candidate := range projection.Devices {
			if candidate.ID == channel.DeviceProjectionID && candidate.Active {
				device = candidate
				break
			}
		}
		if device.ID == 0 {
			continue
		}
		var row struct{ ID uint64 }
		result := app.DB().WithContext(ctx).Table("gb_channel AS c").Select("c.id").
			Joins("JOIN gb_device AS d ON d.device_id = c.device_id AND d.owner_dept_id = c.owner_dept_id").
			Where("c.id = ? AND d.id = ? AND c.channel_id = ? AND d.device_id = ? AND c.deleted_at IS NULL AND d.deleted_at IS NULL", channel.SourceChannelID, device.SourceDeviceID, sourceCode, deviceCode).
			Limit(1).Find(&row)
		if result.Error != nil {
			return "", "", false, result.Error
		}
		if row.ID != 0 {
			return channel.PublishedChannelID, device.PublishedDeviceID, true, nil
		}
	}
	for _, device := range projection.Devices {
		if !device.Active {
			continue
		}
		var row struct{ DeviceID string }
		if err := app.DB().WithContext(ctx).Table("gb_device").Select("device_id").Where("id = ? AND deleted_at IS NULL", device.SourceDeviceID).Limit(1).Find(&row).Error; err != nil {
			return "", "", false, err
		}
		if row.DeviceID == deviceCode && row.DeviceID == sourceCode {
			return device.PublishedDeviceID, device.PublishedDeviceID, true, nil
		}
	}
	return "", "", false, nil
}

type cascadePTZNotifyXML struct {
	XMLName  xml.Name `xml:"Notify"`
	CmdType  string   `xml:"CmdType"`
	SN       int      `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
	Time     string   `xml:"Time,omitempty"`
	Pan      *float64 `xml:"Pan,omitempty"`
	Tilt     *float64 `xml:"Tilt,omitempty"`
	Zoom     *float64 `xml:"Zoom,omitempty"`
	Focus    *float64 `xml:"Focus,omitempty"`
	Iris     *float64 `xml:"Iris,omitempty"`
}

type cascadeAlarmNotifyXML struct {
	XMLName        xml.Name             `xml:"Notify"`
	CmdType        string               `xml:"CmdType"`
	SN             int                  `xml:"SN"`
	DeviceID       string               `xml:"DeviceID"`
	Priority       int                  `xml:"AlarmPriority"`
	Method         int                  `xml:"AlarmMethod"`
	AlarmType      *int                 `xml:"AlarmType,omitempty"`
	AlarmTypeParam string               `xml:"AlarmTypeParam,omitempty"`
	AlarmTime      string               `xml:"AlarmTime,omitempty"`
	Description    string               `xml:"AlarmDescription,omitempty"`
	Longitude      *float64             `xml:"Longitude,omitempty"`
	Latitude       *float64             `xml:"Latitude,omitempty"`
	Info           *cascadeAlarmInfoXML `xml:"Info,omitempty"`
}

type cascadeAlarmInfoXML struct {
	AlarmType      *int                   `xml:"AlarmType,omitempty"`
	AlarmTypeParam *cascadeAlarmTypeParam `xml:"AlarmTypeParam,omitempty"`
}

type cascadeAlarmTypeParam struct {
	EventType *int `xml:"EventType,omitempty"`
}

func buildCascadeEventBody(profile protocol.Profile, kind cascadeEventSubscriptionKind, target string, sn int, notify any) ([]byte, error) {
	switch kind {
	case cascadeEventPTZ:
		position, ok := notify.(*manscdp.PTZPrecisePositionNotify)
		if !ok {
			return nil, fmt.Errorf("invalid PTZ notification")
		}
		cmdType := position.CmdType
		if profile.SupportsPrecisePTZ() {
			cmdType = manscdp.CmdPTZPosition
		}
		return manscdp.MarshalProfiledXML(profile, cascadePTZNotifyXML{CmdType: cmdType, SN: sn, DeviceID: target, Time: position.Time, Pan: position.Pan, Tilt: position.Tilt, Zoom: position.Zoom, Focus: position.Focus, Iris: position.Iris})
	case cascadeEventAlarm:
		alarm, ok := notify.(*manscdp.AlarmNotify)
		if !ok {
			return nil, fmt.Errorf("invalid Alarm notification")
		}
		var longitude, latitude *float64
		if alarm.Longitude != 0 || alarm.Latitude != 0 {
			longitude, latitude = &alarm.Longitude, &alarm.Latitude
		}
		output := cascadeAlarmNotifyXML{CmdType: manscdp.CmdAlarm, SN: sn, DeviceID: target, Priority: alarm.Priority, Method: alarm.Method, AlarmTime: alarm.AlarmTime, Description: alarm.Description, Longitude: longitude, Latitude: latitude}
		if profile.Version == protocol.Version2022 {
			eventType := alarm.Info.AlarmTypeParam.EventType
			if eventType == nil && strings.HasPrefix(alarm.AlarmTypeParam, "EventType=") {
				if parsed, err := strconv.Atoi(strings.TrimPrefix(alarm.AlarmTypeParam, "EventType=")); err == nil {
					eventType = &parsed
				}
			}
			if alarm.Info.AlarmType != nil || eventType != nil {
				output.Info = &cascadeAlarmInfoXML{AlarmType: alarm.Info.AlarmType}
				if eventType != nil {
					output.Info.AlarmTypeParam = &cascadeAlarmTypeParam{EventType: eventType}
				}
			} else if alarm.AlarmType != nil || alarm.AlarmTypeParam != "" {
				output.Info = &cascadeAlarmInfoXML{AlarmType: alarm.AlarmType}
			}
		} else {
			output.AlarmType = alarm.AlarmType
			output.AlarmTypeParam = alarm.AlarmTypeParam
		}
		return manscdp.MarshalProfiledXML(profile, output)
	default:
		return nil, fmt.Errorf("unsupported cascade event kind %q", kind)
	}
}

func (m *cascadeEventSubscriptions) Reload(ctx context.Context) error {
	m.mu.Lock()
	sessions := make([]*cascadeEventSubscription, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.mu.Unlock()
	for _, s := range sessions {
		p, err := m.store.FindPlatform(ctx, s.platform.ID)
		if err != nil && !errors.Is(err, repository.ErrPlatformNotFound) {
			return err
		}
		valid := err == nil && p.Enabled && p.ConfigRevision == s.platform.ConfigRevision && cascadeWireProfile(*p) == cascadeWireProfile(s.platform)
		if valid {
			valid, err = m.targetAllowed(ctx, *p, s.kind, s.target)
			if err != nil {
				return err
			}
		}
		if valid {
			continue
		}
		s.mu.Lock()
		s.reason = "deactivated"
		if s.sendCancel != nil {
			s.sendCancel()
		}
		s.signal()
		s.mu.Unlock()
	}
	return nil
}

func (m *cascadeEventSubscriptions) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	if !m.closed {
		m.closed = true
		m.cancel()
		go func() { m.wg.Wait(); close(m.done) }()
	}
	m.mu.Unlock()
	select {
	case <-m.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

var _ cascadeservice.SubscriptionReader = (*cascadeEventSubscriptions)(nil)
