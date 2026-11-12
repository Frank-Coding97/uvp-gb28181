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
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

const (
	defaultCascadeMobilePositionInterval = 30 * time.Second
	maxCascadeMobilePositionInterval     = 24 * time.Hour
)

type cascadeMobilePositionSubscription struct {
	key              string
	platform         model.GbCascadePlatform
	client           *sipclient.TransactionClient
	dialog           sipclient.NotifyDialog
	target           string
	sn               int
	interval         time.Duration
	expires          time.Time
	remoteSeq        uint32
	notifySeq        uint32
	generation       uint64
	reason           string
	force            bool
	lastDigest       string
	subscribedAt     time.Time
	renewedAt        time.Time
	lastNotifyAt     *time.Time
	lastNotifyStatus string
	lastNotifyCode   int
	failures         int
	sendCancel       context.CancelFunc
	wake             chan struct{}
	mu               sync.Mutex
}

type cascadeMobilePositionSubscriptions struct {
	store    *repository.GormRepository
	clients  *cascadePlatformClientFactory
	interval time.Duration
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	sessions map[string]*cascadeMobilePositionSubscription
	closed   bool
	wg       sync.WaitGroup
	done     chan struct{}
}

func newCascadeMobilePositionSubscriptions(store *repository.GormRepository, clients *cascadePlatformClientFactory, interval time.Duration) *cascadeMobilePositionSubscriptions {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &cascadeMobilePositionSubscriptions{store: store, clients: clients, interval: interval, ctx: ctx, cancel: cancel, sessions: map[string]*cascadeMobilePositionSubscription{}, done: make(chan struct{})}
}

func (m *cascadeMobilePositionSubscriptions) Subscriptions(p model.GbCascadePlatform) cascadeservice.PlatformSubscriptions {
	result := cascadeservice.NewPlatformSubscriptions("unavailable")
	if m == nil {
		return result
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return result
	}
	sessions := make([]*cascadeMobilePositionSubscription, 0)
	for _, s := range m.sessions {
		if s.platform.ID == p.ID {
			sessions = append(sessions, s)
		}
	}
	m.mu.Unlock()
	result.MobilePosition.State = "inactive"
	now := time.Now()
	for _, s := range sessions {
		s.mu.Lock()
		if p.Enabled && s.platform.ConfigRevision == p.ConfigRevision && cascadeWireProfile(s.platform) == cascadeWireProfile(p) && s.reason == "" && now.Before(s.expires) {
			view := cascadeservice.SubscriptionSession{Target: s.target, Event: s.dialog.Event, SubscribedAt: s.subscribedAt, RenewedAt: s.renewedAt, ExpiresAt: s.expires, LastNotifyStatus: s.lastNotifyStatus, LastNotifyCode: s.lastNotifyCode}
			if s.lastNotifyAt != nil {
				at := *s.lastNotifyAt
				view.LastNotifyAt = &at
			}
			result.MobilePosition.Sessions = append(result.MobilePosition.Sessions, view)
		}
		s.mu.Unlock()
	}
	if len(result.MobilePosition.Sessions) > 0 {
		result.MobilePosition.State = "active"
	}
	return result
}

func parseMobilePositionSubscription(req *sip.Request) (manscdp.MobilePositionSubscriptionQuery, error) {
	return manscdp.ParseMobilePositionSubscriptionQuery(req.Body())
}

// cascadePresenceQueryCmdType 只取 body 里的 CmdType 供分发判断用。
// 空 body / 解不出来一律返回 ""（调用方按原路径继续，最终由解析错误给出 400），
// 这样「presence 事件但没有 body」的老行为不受影响。
func cascadePresenceQueryCmdType(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	query, err := manscdp.ParseSubscriptionQuery(body)
	if err != nil {
		return ""
	}
	return query.CmdType
}

func cascadeMobilePositionInterval(query manscdp.MobilePositionSubscriptionQuery) (time.Duration, error) {
	if !query.IntervalPresent {
		return defaultCascadeMobilePositionInterval, nil
	}
	if query.Interval <= 0 || query.Interval > int(maxCascadeMobilePositionInterval/time.Second) {
		return 0, fmt.Errorf("MobilePosition Interval 超出允许范围")
	}
	return time.Duration(query.Interval) * time.Second, nil
}

func (m *cascadeMobilePositionSubscriptions) Handle(req *sip.Request, tx sip.ServerTransaction) bool {
	if req == nil || tx == nil || req.Method != sip.SUBSCRIBE {
		return false
	}
	reject := func(code int, reason string) bool {
		_ = tx.Respond(sip.NewResponseFromRequest(req, code, reason, nil))
		return true
	}
	eventHeaders := req.GetHeaders("Event")
	if len(eventHeaders) != 1 || !strings.EqualFold(strings.TrimSpace(strings.Split(eventHeaders[0].Value(), ";")[0]), "presence") {
		return false
	}
	// ⛔⛔ `Event: presence` 是**共用**事件名：报警（CmdType=Alarm）、移动位置
	// （MobilePosition）、云台位置（PTZPosition）都复用它。这里是「移动位置订阅」
	// 的入口，只该接 MobilePosition；以前不看 body 就一律接管，导致 PTZPosition
	// 订阅被送进 ParseMobilePositionSubscriptionQuery 判成
	// `非法 MobilePosition 订阅请求` → 400（LiveGBS 3.6.10 实测，2026-10-26）。
	// 其余 CmdType 原样放行，交给报警/云台/目录那三个入口。
	if cmdType := cascadePresenceQueryCmdType(req.Body()); cmdType != "" && cmdType != manscdp.CmdMobilePosition {
		return false
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
	key := fmt.Sprintf("%d|%s|%s|MobilePosition", p.ID, req.CallID().Value(), remoteTag)
	m.mu.Lock()
	existing := m.sessions[key]
	m.mu.Unlock()
	if localTag != "" && existing != nil {
		return m.refresh(ctx, req, tx, existing, *p, expires)
	}
	// ⛔ 带 To-tag 但没有对应会话（进程重启/上一轮被终止/对端自留 tag）时，
	// 按 RFC 应回 481；但上级实测会把 481 当致命错误放弃订阅。兼容为「当新建」，
	// 并沿用对端给的 To-tag 作为我方对话 tag。
	if expires == 0 {
		return reject(481, "Subscription Does Not Exist")
	}
	q, err := parseMobilePositionSubscription(req)
	if err != nil {
		return reject(400, err.Error())
	}
	interval, err := cascadeMobilePositionInterval(q)
	if err != nil {
		return reject(400, err.Error())
	}
	if !cascadeDeviceStatusRequestTargetsMatch(p.LocalDeviceID, q.DeviceID, req.To().Address.User, req.Recipient.User) {
		return reject(403, "Subscription target mismatch")
	}
	allowed, err := cascadePositionTargetAllowed(ctx, m.store, *p, q.DeviceID)
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
	state := &cascadeMobilePositionSubscription{key: key, platform: *p, client: client.(*cascadePlatformClient).transactions, dialog: sipclient.NotifyDialog{CallID: req.CallID().Value(), From: from, To: to, RemoteTarget: *cloned.Contact().Address.Clone(), Event: "presence"}, target: q.DeviceID, sn: q.SN, interval: interval, expires: time.Now().Add(time.Duration(expires) * time.Second), remoteSeq: req.CSeq().SeqNo, generation: 1, force: true, wake: make(chan struct{}, 1)}
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

func (m *cascadeMobilePositionSubscriptions) refresh(ctx context.Context, req *sip.Request, tx sip.ServerTransaction, s *cascadeMobilePositionSubscription, p model.GbCascadePlatform, expires int) bool {
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
	var nextInterval *time.Duration
	var nextSN *int
	if len(req.Body()) > 0 {
		q, err := parseMobilePositionSubscription(req)
		if err != nil || q.DeviceID != s.target {
			return reject(403, "Subscription scope changed")
		}
		if q.IntervalPresent {
			interval, err := cascadeMobilePositionInterval(q)
			if err != nil {
				return reject(400, err.Error())
			}
			nextInterval = &interval
		}
		nextSN = &q.SN
	}
	response := sip.NewResponseFromRequest(req, 200, "OK", nil)
	response.AppendHeader(sip.NewHeader("Expires", strconv.Itoa(expires)))
	response.AppendHeader(&sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: p.LocalDeviceID, Host: p.LocalSIPIP, Port: p.LocalSIPPort}})
	if err := tx.Respond(response); err != nil {
		return true
	}
	if nextInterval != nil {
		s.interval = *nextInterval
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
	if expires == 0 {
		s.reason = "timeout"
	}
	s.dialog.RemoteTarget = *req.Contact().Address.Clone()
	s.signal()
	return true
}

func (s *cascadeMobilePositionSubscription) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (m *cascadeMobilePositionSubscriptions) remove(s *cascadeMobilePositionSubscription) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[s.key] == s {
		delete(m.sessions, s.key)
	}
}
func (m *cascadeMobilePositionSubscriptions) run(s *cascadeMobilePositionSubscription) {
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
		wait := min(m.interval, time.Until(s.expires))
		closing := s.reason != ""
		if s.lastNotifyAt != nil {
			untilReport := s.interval - time.Since(*s.lastNotifyAt)
			if untilReport > 0 {
				wait = min(wait, untilReport)
			}
		}
		s.mu.Unlock()
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

type mobilePositionNotifyXML struct {
	XMLName   xml.Name `xml:"Notify"`
	CmdType   string   `xml:"CmdType"`
	SN        int      `xml:"SN"`
	DeviceID  string   `xml:"DeviceID"`
	Time      string   `xml:"Time"`
	Longitude *float64 `xml:"Longitude,omitempty"`
	Latitude  *float64 `xml:"Latitude,omitempty"`
	Speed     *float64 `xml:"Speed,omitempty"`
	Direction *float64 `xml:"Direction,omitempty"`
	Altitude  *float64 `xml:"Altitude,omitempty"`
}

type mobilePositionItemXML struct {
	DeviceID    string   `xml:"DeviceID"`
	CaptureTime string   `xml:"CaptureTime"`
	Longitude   float64  `xml:"Longitude"`
	Latitude    float64  `xml:"Latitude"`
	Speed       *float64 `xml:"Speed,omitempty"`
	Direction   *float64 `xml:"Direction,omitempty"`
	Altitude    *float64 `xml:"Altitude,omitempty"`
}

type mobilePositionListNotifyXML struct {
	XMLName    xml.Name `xml:"Notify"`
	CmdType    string   `xml:"CmdType"`
	SN         int      `xml:"SN"`
	DeviceID   string   `xml:"DeviceID"`
	Time       string   `xml:"Time"`
	SumNum     int      `xml:"SumNum"`
	DeviceList struct {
		Num   int                     `xml:"Num,attr"`
		Items []mobilePositionItemXML `xml:"Item"`
	} `xml:"DeviceList"`
}

type cascadePublishedPosition struct {
	publishedID string
	row         gbmodels.GbMobilePositionLatest
}

func cascadePositionTargetAllowed(ctx context.Context, store *repository.GormRepository, p model.GbCascadePlatform, target string) (bool, error) {
	if target == p.LocalDeviceID {
		return p.PublishPlatform, nil
	}
	projection, err := store.ProjectionSnapshot(ctx, p.ID)
	if err != nil {
		return false, err
	}
	for _, device := range projection.Devices {
		if device.Active && device.PublishedDeviceID == target {
			return true, nil
		}
	}
	for _, channel := range projection.Channels {
		if channel.Active && channel.PublishedChannelID == target {
			return true, nil
		}
	}
	return false, nil
}

func cascadePositionScope(projection *repository.ProjectionSnapshot, platform model.GbCascadePlatform, target string) map[uint64]string {
	channels := make(map[uint64]string)
	deviceProjectionIDs := make(map[uint64]bool)
	switch {
	case target == platform.LocalDeviceID:
		for _, device := range projection.Devices {
			if device.Active {
				deviceProjectionIDs[device.ID] = true
			}
		}
	case hasPublishedChannel(projection.Channels, target):
		for _, channel := range projection.Channels {
			if channel.Active && channel.PublishedChannelID == target {
				channels[channel.SourceChannelID] = channel.PublishedChannelID
			}
		}
	default:
		for _, device := range projection.Devices {
			if device.Active && device.PublishedDeviceID == target {
				deviceProjectionIDs[device.ID] = true
			}
		}
	}
	if target != platform.LocalDeviceID && len(channels) > 0 {
		return channels
	}
	for _, channel := range projection.Channels {
		if channel.Active && deviceProjectionIDs[channel.DeviceProjectionID] {
			channels[channel.SourceChannelID] = channel.PublishedChannelID
		}
	}
	return channels
}

func hasPublishedChannel(channels []model.GbCascadeChannelProjection, target string) bool {
	for _, channel := range channels {
		if channel.Active && channel.PublishedChannelID == target {
			return true
		}
	}
	return false
}

func buildCascadeMobilePositionBodies(profile protocol.Profile, target string, sn int, positions []cascadePublishedPosition) ([][]byte, error) {
	if profile.Version == protocol.Version2022 {
		body := mobilePositionListNotifyXML{CmdType: manscdp.CmdMobilePosition, SN: sn, DeviceID: target, Time: time.Now().Format(time.RFC3339), SumNum: len(positions)}
		body.DeviceList.Num = len(positions)
		body.DeviceList.Items = make([]mobilePositionItemXML, 0, len(positions))
		for _, position := range positions {
			row := position.row
			body.DeviceList.Items = append(body.DeviceList.Items, mobilePositionItemXML{
				DeviceID: position.publishedID, CaptureTime: row.EventTime.Format(time.RFC3339),
				Longitude: row.Longitude, Latitude: row.Latitude, Speed: row.Speed, Direction: row.Direction, Altitude: row.Altitude,
			})
		}
		encoded, err := manscdp.MarshalProfiledXML(profile, body)
		if err != nil {
			return nil, err
		}
		return [][]byte{encoded}, nil
	}
	bodies := make([][]byte, 0, len(positions))
	for _, position := range positions {
		row := position.row
		body, err := manscdp.MarshalProfiledXML(profile, mobilePositionNotifyXML{
			CmdType: manscdp.CmdMobilePosition, SN: sn, DeviceID: position.publishedID,
			Time: row.EventTime.Format(time.RFC3339), Longitude: &row.Longitude, Latitude: &row.Latitude,
			Speed: row.Speed, Direction: row.Direction, Altitude: row.Altitude,
		})
		if err != nil {
			return nil, err
		}
		bodies = append(bodies, body)
	}
	return bodies, nil
}

func (m *cascadeMobilePositionSubscriptions) publish(s *cascadeMobilePositionSubscription) bool {
	s.mu.Lock()
	if s.reason == "" && !time.Now().Before(s.expires) {
		s.reason = "timeout"
		s.generation++
	}
	generation, reason, target, sn, expires := s.generation, s.reason, s.target, s.sn, s.expires
	force := s.force
	dialog := s.dialog
	ctx, cancel := context.WithDeadline(m.ctx, func() time.Time {
		if reason != "" {
			return time.Now().Add(3 * time.Second)
		}
		return expires
	}())
	s.sendCancel = cancel
	s.mu.Unlock()
	defer cancel()
	p, err := m.store.FindPlatform(ctx, s.platform.ID)
	if err != nil || p == nil || !p.Enabled || p.ConfigRevision != s.platform.ConfigRevision || cascadeWireProfile(*p) != cascadeWireProfile(s.platform) {
		return false
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
	projection, projectionErr := m.store.ProjectionSnapshot(ctx, s.platform.ID)
	if projectionErr != nil {
		return m.failed(s, generation, 0, projectionErr)
	}
	allowed, err := cascadePositionTargetAllowed(ctx, m.store, *p, target)
	if err != nil {
		return m.failed(s, generation, 0, err)
	}
	if !allowed {
		s.mu.Lock()
		if s.generation == generation {
			s.reason = "noresource"
		}
		s.mu.Unlock()
		return true
	}
	scope := cascadePositionScope(projection, *p, target)
	if len(scope) == 0 && target != p.LocalDeviceID {
		return true
	}
	var rows []gbmodels.GbMobilePositionLatest
	if len(scope) > 0 {
		channelIDs := make([]uint64, 0, len(scope))
		for id := range scope {
			channelIDs = append(channelIDs, id)
		}
		if err := app.DB().WithContext(ctx).Where("channel_id IN ?", channelIDs).Find(&rows).Error; err != nil {
			return m.failed(s, generation, 0, err)
		}
	}
	positions := make([]cascadePublishedPosition, 0, len(rows))
	for _, item := range rows {
		if item.ChannelID == nil {
			continue
		}
		publishedID, ok := scope[uint64(*item.ChannelID)]
		if ok {
			positions = append(positions, cascadePublishedPosition{publishedID: publishedID, row: item})
		}
	}
	sort.Slice(positions, func(i, j int) bool { return positions[i].publishedID < positions[j].publishedID })
	var digestBuilder strings.Builder
	for _, item := range positions {
		fmt.Fprintf(&digestBuilder, "%s|%.8f|%.8f|%s|%v|%v|%v;", item.publishedID, item.row.Longitude, item.row.Latitude, item.row.EventTime.UTC().Format(time.RFC3339Nano), item.row.Speed, item.row.Direction, item.row.Altitude)
	}
	digest := digestBuilder.String()
	if !force {
		s.mu.Lock()
		due := s.lastNotifyAt == nil || time.Since(*s.lastNotifyAt) >= s.interval
		unchangedWithoutReport := s.lastNotifyAt == nil && digest == s.lastDigest
		s.mu.Unlock()
		if !due || unchangedWithoutReport {
			return true
		}
	}
	bodies, err := buildCascadeMobilePositionBodies(cascadeWireProfile(*p), target, sn, positions)
	if err != nil {
		return m.failed(s, generation, 0, err)
	}
	for _, body := range bodies {
		s.mu.Lock()
		if s.generation != generation || !time.Now().Before(s.expires) {
			s.mu.Unlock()
			return true
		}
		s.notifySeq++
		seq := s.notifySeq
		s.mu.Unlock()
		result := s.client.SendNotify(ctx, dialog, seq, "active;expires="+strconv.Itoa(max(1, int(time.Until(expires).Seconds()))), body)
		s.mu.Lock()
		s.notifySeq = max(s.notifySeq, result.LastCSeq)
		if s.generation == generation {
			at := time.Now()
			s.lastNotifyAt = &at
			s.lastNotifyCode = result.StatusCode
			s.lastNotifyStatus = "failed"
			if result.Success && result.StatusCode == 200 {
				s.lastNotifyStatus = "success"
				s.failures = 0
			}
		}
		s.mu.Unlock()
		if !result.Success || result.StatusCode != 200 {
			return m.failed(s, generation, result.StatusCode, errors.Join(result.BuildErr, result.TransportErr))
		}
	}
	s.mu.Lock()
	if s.generation == generation {
		s.lastDigest = digest
		s.force = false
	}
	s.mu.Unlock()
	return true
}
func (m *cascadeMobilePositionSubscriptions) failed(s *cascadeMobilePositionSubscription, generation uint64, status int, err error) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.generation != generation {
		return true
	}
	s.failures++
	s.lastNotifyStatus = "failed"
	s.lastNotifyCode = status
	app.Log(m.ctx).Warn("级联位置订阅通知失败", zap.String("event", "cascade.mobile_position.subscription_notify_failed"), zap.Uint64("platform_id", s.platform.ID), zap.Int("status", status), zap.Error(err))
	permanent := status >= 400 && status < 500 && status != 429
	return !permanent && s.failures < 3
}
func (m *cascadeMobilePositionSubscriptions) Reload(ctx context.Context) error {
	m.mu.Lock()
	sessions := make([]*cascadeMobilePositionSubscription, 0, len(m.sessions))
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
			valid, err = cascadePositionTargetAllowed(ctx, m.store, *p, s.target)
			if err != nil {
				return err
			}
		}
		if valid {
			continue
		}
		s.mu.Lock()
		s.reason = "deactivated"
		s.generation++
		if s.sendCancel != nil {
			s.sendCancel()
		}
		s.signal()
		s.mu.Unlock()
	}
	return nil
}
func (m *cascadeMobilePositionSubscriptions) Shutdown(ctx context.Context) error {
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

var _ cascadeservice.SubscriptionReader = (*cascadeMobilePositionSubscriptions)(nil)
