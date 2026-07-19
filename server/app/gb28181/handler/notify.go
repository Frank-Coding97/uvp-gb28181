package handler

import (
	"context"
	"strconv"
	"strings"
	"sync"

	"github.com/emiago/sipgo/sip"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.cn/uvp-gb28181/app/gb28181/subscribe"
	"uvplatform.cn/uvp-gb28181/app/global/app"

	"go.uber.org/zap"
)

type SubscriptionNotifier interface {
	OnNotify(context.Context, subscribe.Notification) error
}

type NotifyHandler struct {
	mu       sync.RWMutex
	notifier SubscriptionNotifier
}

func NewNotifyHandler(notifier SubscriptionNotifier) *NotifyHandler {
	return &NotifyHandler{notifier: notifier}
}

// SetNotifier installs the subscription service before the SIP server starts.
func (h *NotifyHandler) SetNotifier(notifier SubscriptionNotifier) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.notifier = notifier
	h.mu.Unlock()
}

func headerValue(req *sip.Request, name string) string {
	if headers := req.GetHeaders(name); len(headers) > 0 {
		return headers[0].Value()
	}
	return ""
}

func parseSubscriptionState(value string) (state string, expires int) {
	parts := strings.Split(value, ";")
	state = strings.TrimSpace(parts[0])
	for _, part := range parts[1:] {
		key, val, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && strings.EqualFold(key, "expires") {
			expires, _ = strconv.Atoi(val)
		}
	}
	return state, expires
}

// Handle acknowledges malformed NOTIFY requests to prevent device retransmit storms.
func (h *NotifyHandler) Handle(req *sip.Request, tx sip.ServerTransaction) {
	defer func() { _ = tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil)) }()
	if h == nil {
		return
	}
	h.mu.RLock()
	notifier := h.notifier
	h.mu.RUnlock()
	if notifier == nil {
		return
	}
	head, err := manscdp.ParseHead(req.Body())
	if err != nil {
		app.ZapLog.Warn("GB28181 NOTIFY 解析失败", zap.Error(err))
		return
	}
	kind, err := manscdp.ResolveSubscriptionKind(headerValue(req, "Event"), req.Body())
	if err != nil {
		app.ZapLog.Warn("GB28181 NOTIFY 订阅类型不支持", zap.Error(err))
		return
	}
	state, expires := parseSubscriptionState(headerValue(req, "Subscription-State"))
	callID, cseq := sipPairKey(req)
	if err := notifier.OnNotify(context.Background(), subscribe.Notification{
		Kind: kind, DeviceCode: head.DeviceID, CallID: callID, CSeq: cseq,
		Source: req.Source(), SubscriptionState: state, Expires: expires, Body: req.Body(),
	}); err != nil {
		app.ZapLog.Warn("GB28181 NOTIFY 业务处理失败", zap.String("deviceId", head.DeviceID), zap.Error(err))
	}
}
