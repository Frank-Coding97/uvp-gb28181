package gb28181

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emiago/sipgo/sip"
	"go.uber.org/zap"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/catalog"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	cascadeservice "uvplatform.com/uvp-gb28181/app/gb28181/cascade/service"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/sipclient"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

// These dialogs describe upstream subscriptions, not our device subscriptions.
// They deliberately expire on restart; a stale SIP dialog cannot be recovered
// from the device-side subscription table.
type cascadeCatalogSubscriptions struct {
	store    *repository.GormRepository
	clients  *cascadePlatformClientFactory
	interval time.Duration
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	sessions map[string]*cascadeCatalogSubscription
	closed   bool
	wg       sync.WaitGroup
	done     chan struct{}
}

type cascadeCatalogSubscription struct {
	key              string
	platform         model.GbCascadePlatform
	client           *sipclient.TransactionClient
	mu               sync.Mutex
	dialog           sipclient.NotifyDialog
	target           string
	sn               int
	expires          time.Time
	remoteSeq        uint32
	notifySeq        uint32
	generation       uint64
	reason           string
	force            bool
	confirmed        []catalog.CatalogItem
	failures         int
	subscribedAt     time.Time
	renewedAt        time.Time
	lastNotifyAt     *time.Time
	lastNotifyStatus string
	lastNotifyCode   int
	sendCancel       context.CancelFunc
	wake             chan struct{}
}

func newCascadeCatalogSubscriptions(store *repository.GormRepository, clients *cascadePlatformClientFactory, interval time.Duration) *cascadeCatalogSubscriptions {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &cascadeCatalogSubscriptions{store: store, clients: clients, interval: interval, ctx: ctx, cancel: cancel, sessions: make(map[string]*cascadeCatalogSubscription), done: make(chan struct{})}
}

func (m *cascadeCatalogSubscriptions) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

// Subscriptions exposes only current upstream dialogs, never device-side subscriptions.
func (m *cascadeCatalogSubscriptions) Subscriptions(p model.GbCascadePlatform) cascadeservice.PlatformSubscriptions {
	result := cascadeservice.NewPlatformSubscriptions("unavailable")
	if m == nil {
		return result
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return result
	}
	sessions := make([]*cascadeCatalogSubscription, 0, len(m.sessions))
	for _, s := range m.sessions {
		if s.platform.ID == p.ID {
			sessions = append(sessions, s)
		}
	}
	m.mu.Unlock()
	result.Catalog.State = "inactive"
	// Keys are immutable. Stable ordering also keeps the expanded ledger steady.
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].key < sessions[j].key })
	now := time.Now()
	for _, s := range sessions {
		s.mu.Lock()
		if p.Enabled && s.platform.ConfigRevision == p.ConfigRevision && cascadeWireProfile(s.platform) == cascadeWireProfile(p) && s.reason == "" && now.Before(s.expires) {
			view := cascadeservice.SubscriptionSession{Target: s.target, Event: s.dialog.Event, SubscribedAt: s.subscribedAt, RenewedAt: s.renewedAt, ExpiresAt: s.expires, LastNotifyStatus: s.lastNotifyStatus, LastNotifyCode: s.lastNotifyCode}
			if s.lastNotifyAt != nil {
				at := *s.lastNotifyAt
				view.LastNotifyAt = &at
			}
			result.Catalog.Sessions = append(result.Catalog.Sessions, view)
		}
		s.mu.Unlock()
	}
	if len(result.Catalog.Sessions) > 0 {
		result.Catalog.State = "active"
	}
	return result
}

func catalogSubscriptionEvent(req *sip.Request) (string, error) {
	headers := req.GetHeaders("Event")
	if len(headers) != 1 {
		return "", fmt.Errorf("missing or duplicate Event")
	}
	parts := strings.Split(headers[0].Value(), ";")
	if !strings.EqualFold(strings.TrimSpace(parts[0]), "Catalog") {
		return "", fmt.Errorf("unsupported event")
	}
	event := "Catalog"
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid Catalog Event")
	}
	if len(parts) == 2 {
		name, value, ok := strings.Cut(strings.TrimSpace(parts[1]), "=")
		if !ok || !strings.EqualFold(name, "id") || value == "" {
			return "", fmt.Errorf("invalid Catalog event id")
		}
		if _, err := strconv.ParseUint(value, 10, 64); err != nil {
			return "", fmt.Errorf("invalid Catalog event id")
		}
		event += ";id=" + value
	}
	return event, nil
}

func catalogSubscriptionExpires(req *sip.Request) (int, error) {
	headers := append(req.GetHeaders("Expires"), req.GetHeaders("Expire")...)
	if len(headers) == 0 {
		return 600, nil
	}
	if len(headers) != 1 {
		return 0, fmt.Errorf("ambiguous subscription expiry")
	}
	expires, err := strconv.ParseUint(strings.TrimSpace(headers[0].Value()), 10, 32)
	if err != nil || expires > 86400 {
		return 0, fmt.Errorf("invalid subscription expiry")
	}
	return int(expires), nil
}

func (m *cascadeCatalogSubscriptions) Handle(req *sip.Request, tx sip.ServerTransaction) bool {
	if req == nil || tx == nil || req.Method != sip.SUBSCRIBE {
		return false
	}
	reject := func(code int, reason string) bool {
		_ = tx.Respond(sip.NewResponseFromRequest(req, code, reason, nil))
		return true
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return reject(503, "Subscription service stopped")
	}
	m.wg.Add(1)
	m.mu.Unlock()
	defer m.wg.Done()
	event, err := catalogSubscriptionEvent(req)
	if err != nil {
		return reject(489, "Bad Event")
	}
	expires, err := catalogSubscriptionExpires(req)
	if err != nil {
		return reject(400, "Invalid Expires")
	}
	if req.From() == nil || req.To() == nil || req.CallID() == nil || req.CallID().Value() == "" || req.CSeq() == nil || req.CSeq().MethodName != sip.SUBSCRIBE || req.CSeq().SeqNo == 0 || req.CSeq().SeqNo >= 1<<31 || req.Contact() == nil || req.Contact().Address.Scheme != "sip" || req.Contact().Address.Host == "" {
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
	key := fmt.Sprintf("%d|%s|%s|%s", p.ID, req.CallID().Value(), remoteTag, event)
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
	query, err := catalog.ParseCatalogQuery(cascadeWireProfile(*p), req.Body())
	if err != nil {
		return reject(400, "Invalid Catalog subscription")
	}
	if !cascadeDeviceStatusRequestTargetsMatch(p.LocalDeviceID, query.DeviceID, req.To().Address.User, req.Recipient.User) {
		return reject(403, "Subscription target mismatch")
	}
	snapshot, err := buildCascadeCatalogSnapshot(ctx, m.store, p)
	if err != nil || snapshot.Revision != p.ProjectionRevision {
		return reject(503, "Catalog publication changed")
	}
	items, err := catalog.SubscriptionItems(snapshot, p.LocalDeviceID, query.DeviceID)
	if err != nil {
		return reject(403, "Subscription target not published")
	}
	// Bind the initial assumption to the snapshot actually authorized at
	// acceptance. Changes before the worker starts must still produce events.
	baseline := append([]catalog.CatalogItem(nil), items...)
	for i := range baseline {
		if baseline[i].Status == catalog.ResourceStatusOffline {
			baseline[i].Status = catalog.ResourceStatusOnline
		}
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
	state := &cascadeCatalogSubscription{key: key, platform: *p, client: client.(*cascadePlatformClient).transactions,
		dialog: sipclient.NotifyDialog{CallID: req.CallID().Value(), From: from, To: to, RemoteTarget: *cloned.Contact().Address.Clone(), Event: event},
		target: query.DeviceID, sn: query.SN, expires: time.Now().Add(time.Duration(expires) * time.Second), remoteSeq: req.CSeq().SeqNo, generation: 1, confirmed: baseline, force: true, wake: make(chan struct{}, 1)}
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
	// Publish the dialog only after its response is accepted. No NOTIFY can race
	// ahead of the To tag assigned in the SUBSCRIBE 200 response.
	if err = tx.Respond(response); err != nil {
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

func (m *cascadeCatalogSubscriptions) refresh(ctx context.Context, req *sip.Request, tx sip.ServerTransaction, s *cascadeCatalogSubscription, p model.GbCascadePlatform, expires int, event string) bool {
	reject := func(code int, reason string) bool {
		_ = tx.Respond(sip.NewResponseFromRequest(req, code, reason, nil))
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tag, _ := req.To().Params.Get("tag")
	expected, _ := s.dialog.From.Params.Get("tag")
	if tag != expected || req.From().Address.String() != s.dialog.To.Address.String() || req.To().Address.String() != s.dialog.From.Address.String() || s.reason != "" || !time.Now().Before(s.expires) || p.ConfigRevision != s.platform.ConfigRevision || cascadeWireProfile(p) != cascadeWireProfile(s.platform) {
		return reject(481, "Subscription Does Not Exist")
	}
	if req.CSeq().SeqNo <= s.remoteSeq {
		return reject(500, "Subscription CSeq out of order")
	}
	sn, target := s.sn, s.target
	if len(req.Body()) > 0 {
		query, err := catalog.ParseCatalogQuery(cascadeWireProfile(p), req.Body())
		if err != nil {
			return reject(400, "Invalid Catalog subscription")
		}
		sn, target = query.SN, query.DeviceID
	}
	if target != s.target || !cascadeDeviceStatusRequestTargetsMatch(p.LocalDeviceID, target, req.To().Address.User, req.Recipient.User) {
		return reject(403, "Subscription scope changed")
	}
	if expires > 0 {
		snapshot, err := buildCascadeCatalogSnapshot(ctx, m.store, &p)
		if err != nil || snapshot.Revision != p.ProjectionRevision {
			return reject(503, "Catalog publication changed")
		}
		if _, err = catalog.SubscriptionItems(snapshot, p.LocalDeviceID, target); err != nil {
			return reject(403, "Subscription target not published")
		}
	}
	response := sip.NewResponseFromRequest(req, 200, "OK", nil)
	response.AppendHeader(sip.NewHeader("Expires", strconv.Itoa(expires)))
	response.AppendHeader(&sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: p.LocalDeviceID, Host: p.LocalSIPIP, Port: p.LocalSIPPort}})
	if err := tx.Respond(response); err != nil {
		return true
	}
	if s.sendCancel != nil {
		s.sendCancel()
	}
	s.generation++
	s.remoteSeq = req.CSeq().SeqNo
	s.sn = sn
	s.force = true
	s.dialog.Event = event
	s.dialog.RemoteTarget = *req.Contact().Address.Clone()
	s.renewedAt = time.Now()
	s.expires = s.renewedAt.Add(time.Duration(expires) * time.Second)
	if expires == 0 {
		s.reason = "timeout"
	}
	s.signal()
	return true
}

func (s *cascadeCatalogSubscription) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (m *cascadeCatalogSubscriptions) remove(s *cascadeCatalogSubscription) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[s.key] == s {
		delete(m.sessions, s.key)
	}
}

func (m *cascadeCatalogSubscriptions) run(s *cascadeCatalogSubscription) {
	defer m.wg.Done()
	defer m.remove(s)
	for {
		if m.ctx.Err() != nil {
			return
		}
		if !m.publish(s) {
			return
		}
		s.mu.Lock()
		remaining := time.Until(s.expires)
		closing := s.reason != ""
		s.mu.Unlock()
		wait := min(m.interval, remaining)
		if closing || wait <= 0 {
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
		}
	}
}

func (m *cascadeCatalogSubscriptions) currentPlatform(ctx context.Context, s *cascadeCatalogSubscription) (*model.GbCascadePlatform, error) {
	p, err := m.store.FindPlatform(ctx, s.platform.ID)
	if err != nil {
		return nil, err
	}
	if !p.Enabled || p.ConfigRevision != s.platform.ConfigRevision || cascadeWireProfile(*p) != cascadeWireProfile(s.platform) {
		return nil, errCascadeSubscriptionRevoked
	}
	return p, nil
}

var errCascadeSubscriptionRevoked = errors.New("cascade subscription authorization changed")

func (m *cascadeCatalogSubscriptions) publish(s *cascadeCatalogSubscription) bool {
	s.mu.Lock()
	if s.reason == "" && !time.Now().Before(s.expires) {
		s.reason = "timeout"
		s.generation++
	}
	generation, reason, sn, target, expires := s.generation, s.reason, s.sn, s.target, s.expires
	force := s.force
	previous := append([]catalog.CatalogItem(nil), s.confirmed...)
	dialog := s.dialog
	deadline := expires
	if reason != "" {
		deadline = time.Now().Add(3 * time.Second)
	}
	ctx, cancel := context.WithDeadline(m.ctx, deadline)
	s.sendCancel = cancel
	s.mu.Unlock()
	defer cancel()
	p, err := m.currentPlatform(ctx, s)
	if errors.Is(err, repository.ErrPlatformNotFound) || errors.Is(err, errCascadeSubscriptionRevoked) {
		return false
	}
	if err != nil {
		if reason != "" {
			return false
		}
		return m.failed(s, generation, 0, err)
	}
	if reason != "" {
		s.mu.Lock()
		s.notifySeq++
		seq := s.notifySeq
		s.mu.Unlock()
		result := s.client.SendNotify(ctx, dialog, seq, "terminated;reason="+reason, nil)
		s.mu.Lock()
		s.notifySeq = max(s.notifySeq, result.LastCSeq)
		s.mu.Unlock()
		return false
	}
	snapshot, err := buildCascadeCatalogSnapshot(ctx, m.store, p)
	if err != nil {
		return m.failed(s, generation, 0, err)
	}
	if snapshot.Revision != p.ProjectionRevision {
		return true
	}
	current, scopeErr := catalog.SubscriptionItems(snapshot, p.LocalDeviceID, target)
	targetRemoved := errors.Is(scopeErr, catalog.ErrUnknownCatalogTarget)
	if scopeErr != nil && !targetRemoved {
		return m.failed(s, generation, 0, scopeErr)
	}

	changes := catalog.DiffCatalog(previous, current)
	if len(changes) == 0 && !force {
		if targetRemoved {
			s.mu.Lock()
			if s.generation == generation {
				s.reason = "noresource"
				s.generation++
			}
			s.mu.Unlock()
		}
		return true
	}
	remaining := max(1, int(time.Until(expires).Seconds()))
	state := "active;expires=" + strconv.Itoa(remaining)
	var fits func([]byte) bool
	if strings.EqualFold(p.Transport, "UDP") {
		fits = func(body []byte) bool { return s.client.NotifyFitsUDP(dialog, 2147483646, state, body) }
	}
	bodies, err := catalog.PlanCatalogNotifications(cascadeWireProfile(*p), target, sn, changes, max(1, min(p.CatalogBatchSize, 10000)), fits)
	if err != nil {
		return m.failed(s, generation, 0, err)
	}
	for _, batch := range bodies {
		// Do not send an obsolete batch after a projection mutation. The next round
		// recomputes events from the last acknowledged snapshot, including DEL.
		latest, checkErr := m.currentPlatform(ctx, s)
		if errors.Is(checkErr, repository.ErrPlatformNotFound) || errors.Is(checkErr, errCascadeSubscriptionRevoked) {
			return false
		}
		if checkErr != nil {
			return m.failed(s, generation, 0, checkErr)
		}
		if latest.ProjectionRevision != snapshot.Revision {
			return true
		}
		s.mu.Lock()
		if s.generation != generation || !time.Now().Before(s.expires) {
			s.mu.Unlock()
			return true
		}
		s.notifySeq++
		seq := s.notifySeq
		s.mu.Unlock()
		state = "active;expires=" + strconv.Itoa(max(1, int(time.Until(expires).Seconds())))
		result := s.client.SendNotify(ctx, dialog, seq, state, batch.Body)
		s.mu.Lock()
		s.notifySeq = max(s.notifySeq, result.LastCSeq)
		if result.Success && result.StatusCode == 200 {
			s.confirmed = catalog.ApplyCatalogChanges(s.confirmed, batch.Changes)
		}
		stale := s.generation != generation
		if !stale {
			at := time.Now()
			s.lastNotifyAt = &at
			s.lastNotifyCode = result.StatusCode
			s.lastNotifyStatus = "failed"
			if result.Success && result.StatusCode == 200 {
				s.lastNotifyStatus = "success"
			}
		}
		s.mu.Unlock()
		if stale {
			return true
		}
		if !result.Success || result.StatusCode != 200 {
			if errors.Is(result.TransportErr, context.DeadlineExceeded) || errors.Is(result.TransportErr, sip.ErrTransactionTimeout) {
				return false
			}
			return m.failed(s, generation, result.StatusCode, errors.Join(result.BuildErr, result.TransportErr))
		}
	}
	s.mu.Lock()
	if s.generation == generation {
		s.confirmed = append([]catalog.CatalogItem(nil), current...)
		s.force = false
		s.failures = 0
		if targetRemoved {
			s.reason = "noresource"
			s.generation++
		}
	}
	s.mu.Unlock()
	return true
}

func (m *cascadeCatalogSubscriptions) failed(s *cascadeCatalogSubscription, generation uint64, status int, err error) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.generation != generation {
		return true
	}
	s.failures++
	permanent := status >= 400 && status < 500 && status != 429
	app.Log(m.ctx).Warn("级联目录订阅通知失败", zap.String("event", "cascade.catalog.subscription_notify_failed"), zap.Uint64("platform_id", s.platform.ID), zap.String("call_id", s.dialog.CallID), zap.Int("status", status), zap.Int("attempt", s.failures), zap.Error(err))
	return !permanent && s.failures < 3
}

func (m *cascadeCatalogSubscriptions) Reload(ctx context.Context) error {
	m.mu.Lock()
	sessions := make([]*cascadeCatalogSubscription, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.mu.Unlock()
	for _, s := range sessions {
		_, err := m.currentPlatform(ctx, s)
		if err != nil && !errors.Is(err, repository.ErrPlatformNotFound) && !errors.Is(err, errCascadeSubscriptionRevoked) {
			return err
		}
		s.mu.Lock()
		if err != nil {
			// Cancel any in-flight delivery immediately. The worker rechecks persisted
			// authorization and removes the dialog without sending to the old peer.
			s.generation++
			if s.sendCancel != nil {
				s.sendCancel()
			}
		}
		s.signal()
		s.mu.Unlock()
	}
	return nil
}

func (m *cascadeCatalogSubscriptions) Shutdown(ctx context.Context) error {
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

// Own subscription work in the same shutdown/reload chain as cascade actors.
type cascadeSubscriptionRuntime struct {
	cascadeRuntimeLifecycle
	subscriptions *cascadeCatalogSubscriptions
	positions     *cascadeMobilePositionSubscriptions
	events        *cascadeEventSubscriptions
	upgrades      *cascadeUpgradeRuntime
	broadcast     *cascadeBroadcastRuntime
}

func (r *cascadeSubscriptionRuntime) Subscriptions(p model.GbCascadePlatform) cascadeservice.PlatformSubscriptions {
	result := r.subscriptions.Subscriptions(p)
	if r.positions != nil {
		position := r.positions.Subscriptions(p)
		result.MobilePosition = position.MobilePosition
	}
	if r.events != nil {
		events := r.events.Subscriptions(p)
		result.PTZPosition = events.PTZPosition
		result.Alarm = events.Alarm
	}
	return result
}

func (r *cascadeSubscriptionRuntime) Reload(ctx context.Context) error {
	if r.positions == nil && r.events == nil {
		return errors.Join(r.cascadeRuntimeLifecycle.Reload(ctx), r.subscriptions.Reload(ctx))
	}
	var positionErr, eventErr error
	if r.positions != nil {
		positionErr = r.positions.Reload(ctx)
	}
	if r.events != nil {
		eventErr = r.events.Reload(ctx)
	}
	return errors.Join(r.cascadeRuntimeLifecycle.Reload(ctx), r.subscriptions.Reload(ctx), positionErr, eventErr)
}
func (r *cascadeSubscriptionRuntime) Shutdown(ctx context.Context) error {
	var broadcastErr error
	if r.broadcast != nil {
		broadcastErr = r.broadcast.Shutdown(ctx)
	}
	var upgradeErr error
	if r.upgrades != nil {
		upgradeErr = r.upgrades.Shutdown(ctx)
	}
	if r.positions == nil && r.events == nil {
		return errors.Join(broadcastErr, upgradeErr, r.subscriptions.Shutdown(ctx), r.cascadeRuntimeLifecycle.Shutdown(ctx))
	}
	var positionErr, eventErr error
	if r.positions != nil {
		positionErr = r.positions.Shutdown(ctx)
	}
	if r.events != nil {
		eventErr = r.events.Shutdown(ctx)
	}
	return errors.Join(broadcastErr, upgradeErr, positionErr, eventErr, r.subscriptions.Shutdown(ctx), r.cascadeRuntimeLifecycle.Shutdown(ctx))
}

func (r *cascadeSubscriptionRuntime) BroadcastPTZ(ctx context.Context, deviceCode, callID, cseq string, body []byte) error {
	if r.events == nil {
		return nil
	}
	return r.events.BroadcastPTZ(ctx, deviceCode, callID, cseq, body)
}

func (r *cascadeSubscriptionRuntime) BroadcastAlarm(ctx context.Context, deviceCode, callID, cseq string, body []byte) error {
	if r.events == nil {
		return nil
	}
	return r.events.BroadcastAlarm(ctx, deviceCode, callID, cseq, body)
}

func (r *cascadeSubscriptionRuntime) EnqueueAlarm(ctx context.Context, deviceCode, callID, cseq string, body []byte) error {
	if r.events == nil {
		return fmt.Errorf("cascade Alarm acceptance runtime is unavailable")
	}
	return r.events.EnqueueAlarm(ctx, deviceCode, callID, cseq, body)
}
