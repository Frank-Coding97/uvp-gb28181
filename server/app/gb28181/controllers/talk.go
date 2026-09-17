package controllers

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/talk"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

const (
	// 前端上行 SDP 通常几 KB,64KB 足够,同时挡住把转发端当上传口用。
	maxTalkUplinkOfferBytes = 64 << 10
	// 上游回答 SDP 不应比 offer 大一个量级,给同一个上限即可。
	maxTalkUplinkAnswerBytes = 64 << 10
	// 单次转发到媒体节点的超时。WHIP 的 POST 要等 ICE 协商完成,给足 10s。
	talkUplinkTimeout = 10 * time.Second

	// 语音对讲端点的路由模式串(相对 device-mgmt 分组),routes 与这里共用同一常量。
	// 公开地址必须从平台自己的路由推导:t.FullPath() 裁掉这两段就是 API 分组前缀,
	// 于是「注册在哪」与「对外怎么拼地址」不可能错配。
	TalkCreateRoute = "/channel/:id/talk-sessions"
	TalkUplinkRoute = "/talk-sessions/:sessionId/uplink"
)

type TalkSessionService interface {
	Create(context.Context, talk.CreateRequest) (*talk.CreateResult, error)
	Get(context.Context, string) (*gbmodels.GbTalkSession, error)
	Renew(context.Context, string) (*gbmodels.GbTalkSession, error)
	Cleanup(context.Context, string, gbmodels.TalkSessionState, string) error
	PrepareUplink(context.Context, string) (*talk.UplinkTarget, error)
}

type TalkController struct {
	controllers.Common
	service      TalkSessionService
	dbFunc       func() *gorm.DB
	uplinkClient *http.Client
}

type TalkSessionView struct {
	SessionID string                    `json:"sessionId"`
	Mode      gbmodels.TalkSessionMode  `json:"mode"`
	State     gbmodels.TalkSessionState `json:"state"`
	Phase     gbmodels.TalkSignalPhase  `json:"phase,omitempty"`
	ExpiresAt time.Time                 `json:"expiresAt"`
	StartedAt *time.Time                `json:"startedAt,omitempty"`
	EndedAt   *time.Time                `json:"endedAt,omitempty"`
	Error     string                    `json:"error,omitempty"`
}

func NewTalkController(service TalkSessionService) *TalkController {
	return &TalkController{
		service: service, dbFunc: func() *gorm.DB { return app.DB() },
		uplinkClient: newTalkUplinkClient(),
	}
}

// newTalkUplinkClient 是转发 WHIP 发布请求到媒体节点的专用客户端。
//
// InsecureSkipVerify:媒体节点在内网用自签证书,平台到节点是可信链路;
// 而浏览器侧看到的是平台自己的证书,不再直连节点,这正是反代要解决的问题。
// 连接池刻意开小:上行转发只在会话建立时发生一次,不承载持续流量。
func newTalkUplinkClient() *http.Client {
	return &http.Client{
		Timeout: talkUplinkTimeout,
		Transport: &http.Transport{
			// #nosec G402 -- 内网媒体节点自签证书,链路由部署环境保证。
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
			ForceAttemptHTTP2: false,
			MaxIdleConns:      8,
			IdleConnTimeout:   90 * time.Second,
		},
	}
}

// talkPublicOrigin 返回平台自己的对外基址(scheme + host + 分组前缀)。
//
// 平台不得硬编码对外端口:同一份部署要能跑在任意域名 / 端口下,所以 host 与
// scheme 一律按请求实际到达的地址推导,并让 X-Forwarded-* 优先(前面可能有反向代理)。
func talkPublicOrigin(ctx *gin.Context) string {
	prefix := ctx.FullPath()
	for _, suffix := range []string{TalkCreateRoute, TalkUplinkRoute} {
		if strings.HasSuffix(prefix, suffix) {
			prefix = strings.TrimSuffix(prefix, suffix)
			break
		}
	}
	scheme := "http"
	if ctx.Request.TLS != nil {
		scheme = "https"
	}
	if proto := strings.TrimSpace(ctx.GetHeader("X-Forwarded-Proto")); proto != "" {
		scheme = proto
	}
	host := ctx.Request.Host
	if forwarded := strings.TrimSpace(ctx.GetHeader("X-Forwarded-Host")); forwarded != "" {
		host = forwarded
	}
	return scheme + "://" + host + prefix
}

func (c *TalkController) SetDB(dbFunc func() *gorm.DB) { c.dbFunc = dbFunc }

func (c *TalkController) Create(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	var request struct {
		Mode gbmodels.TalkSessionMode `json:"mode"`
	}
	if err := ctx.ShouldBindJSON(&request); err != nil || !request.Mode.Valid() {
		response.Fail(ctx, "mode 仅支持 broadcast 或 talk", http.StatusBadRequest)
		return
	}
	channel, device, ok := c.loadTarget(ctx)
	if !ok {
		return
	}
	actorID := c.GetCurrentUserID(ctx)
	result, err := c.service.Create(ctx.Request.Context(), talk.CreateRequest{
		Channel: channel, Device: device, ActorID: actorID, ActorDeptID: c.actorDeptID(ctx, actorID),
		Mode: request.Mode,
	})
	if err != nil {
		c.writeServiceError(ctx, err)
		return
	}
	// 上行入口回填成平台自己的地址:service 只给相对路径,
	// host / scheme 必须由请求现场推导,不能从配置或媒体节点拼。
	result.Uplink.URL = talkPublicOrigin(ctx) + result.Uplink.Path
	c.Success(ctx, result)
}

func (c *TalkController) Get(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	channel, _, ok := c.loadTarget(ctx)
	if !ok {
		return
	}
	session, ok := c.loadOwnedSession(ctx, channel.ID)
	if !ok {
		return
	}
	if session.State == gbmodels.TalkSessionActive {
		var err error
		session, err = c.service.Renew(ctx.Request.Context(), session.SessionID)
		if err != nil {
			c.writeServiceError(ctx, err)
			return
		}
	}
	c.Success(ctx, talkSessionView(session))
}

func (c *TalkController) Delete(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	channel, _, ok := c.loadTarget(ctx)
	if !ok {
		return
	}
	session, ok := c.loadOwnedSession(ctx, channel.ID)
	if !ok {
		return
	}
	if !session.State.IsTerminal() {
		if err := c.service.Cleanup(ctx.Request.Context(), session.SessionID, gbmodels.TalkSessionEnded, "user stopped"); err != nil {
			// 停止对调用方是幂等操作：只要会话确实落到了终态，就算某些拆解步骤降级
			// 也算停止成功。实测交叉 BYE 时 SIP BYE 拿不到 200 → 清理带上超时错误，
			// 可用户要的「停掉」其实已经达成，抛 504 只会让人以为没停掉而去重试。
			// 失败细节已写进会话 error 列并打了告警日志。
			latest, getErr := c.service.Get(ctx.Request.Context(), session.SessionID)
			if getErr != nil || latest == nil || !latest.State.IsTerminal() {
				c.writeServiceError(ctx, err)
				return
			}
			session = latest
		} else {
			session.State = gbmodels.TalkSessionEnded
		}
	}
	c.Success(ctx, gin.H{"sessionId": session.SessionID, "state": session.State})
}

// Uplink 把浏览器的 WHIP 发布请求转发到媒体节点。
//
// 对外只暴露平台自己的地址:媒体节点地址、自签证书、一次性令牌都不出平台。
// 令牌在这一步才签发(见 talk.Service.PrepareUplink),于是不会进浏览器历史、
// Referer,也不进前端日志 —— 直连时这三处全都会带上它。
func (c *TalkController) Uplink(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	sessionID := strings.TrimSpace(ctx.Param("sessionId"))
	if sessionID == "" {
		response.Fail(ctx, "对讲会话 ID 不合法", http.StatusBadRequest)
		return
	}
	target, err := c.service.PrepareUplink(ctx.Request.Context(), sessionID)
	if err != nil {
		c.writeUplinkError(ctx, err)
		return
	}
	offer, err := io.ReadAll(io.LimitReader(ctx.Request.Body, maxTalkUplinkOfferBytes+1))
	if err != nil {
		response.Fail(ctx, "读取发布请求失败", http.StatusBadRequest)
		return
	}
	if len(offer) > maxTalkUplinkOfferBytes {
		response.Fail(ctx, "发布请求过大", http.StatusRequestEntityTooLarge)
		return
	}
	upstream, err := http.NewRequestWithContext(ctx.Request.Context(), http.MethodPost, target.URL, bytes.NewReader(offer))
	if err != nil {
		response.Fail(ctx, "语音对讲媒体服务不可用", http.StatusServiceUnavailable)
		return
	}
	upstream.Header.Set("Content-Type", target.ContentType)
	upstream.Header.Set("Accept", target.ContentType)

	answer, err := c.uplinkClient.Do(upstream)
	if err != nil {
		response.Fail(ctx, "语音对讲媒体服务不可用", http.StatusServiceUnavailable)
		return
	}
	defer answer.Body.Close()
	body, err := io.ReadAll(io.LimitReader(answer.Body, maxTalkUplinkAnswerBytes+1))
	if err != nil || len(body) > maxTalkUplinkAnswerBytes {
		response.Fail(ctx, "语音对讲媒体服务响应异常", http.StatusBadGateway)
		return
	}
	// 只回传状态码与 SDP。上游其余响应头一律不透传,尤其不能让 Location 把
	// 媒体节点地址带回浏览器 —— 换成平台自己的会话地址。
	if answer.Header.Get("Location") != "" {
		ctx.Header("Location", talkPublicOrigin(ctx)+"/talk-sessions/"+url.PathEscape(sessionID))
	}
	ctx.Data(answer.StatusCode, target.ContentType, body)
}

func (c *TalkController) ready(ctx *gin.Context) bool {
	if c == nil || c.service == nil || c.dbFunc == nil || c.dbFunc() == nil {
		response.SetBusinessResult(ctx, 503, false)
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "message": "语音对讲服务未装配"})
		return false
	}
	return true
}

func (c *TalkController) loadTarget(ctx *gin.Context) (*gbmodels.GbChannel, *gbmodels.GbDevice, bool) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Fail(ctx, "通道 ID 不合法", http.StatusBadRequest)
		return nil, nil, false
	}
	db := c.dbFunc().WithContext(ctx.Request.Context())
	var channel gbmodels.GbChannel
	result := db.Scopes(ownerDeptScope(ctx)).Where("id = ?", uint(id)).Limit(1).Find(&channel)
	if result.Error != nil {
		response.Fail(ctx, "查询通道失败", http.StatusInternalServerError)
		return nil, nil, false
	}
	if result.RowsAffected == 0 {
		response.Fail(ctx, "通道不存在", http.StatusNotFound)
		return nil, nil, false
	}
	var device gbmodels.GbDevice
	result = db.Scopes(ownerDeptScope(ctx)).Where("device_id = ?", channel.DeviceID).Limit(1).Find(&device)
	if result.Error != nil {
		response.Fail(ctx, "查询设备失败", http.StatusInternalServerError)
		return nil, nil, false
	}
	if result.RowsAffected == 0 {
		response.Fail(ctx, "设备不存在", http.StatusNotFound)
		return nil, nil, false
	}
	return &channel, &device, true
}

func (c *TalkController) loadOwnedSession(ctx *gin.Context, channelID uint) (*gbmodels.GbTalkSession, bool) {
	session, err := c.service.Get(ctx.Request.Context(), ctx.Param("sessionId"))
	if err != nil {
		c.writeServiceError(ctx, err)
		return nil, false
	}
	if session == nil || session.ChannelID != channelID {
		response.Fail(ctx, "对讲会话不存在", http.StatusNotFound)
		return nil, false
	}
	return session, true
}

func (c *TalkController) actorDeptID(ctx *gin.Context, actorID uint) uint {
	if actorID == 0 {
		return 0
	}
	var user basemodels.User
	if err := c.dbFunc().WithContext(ctx.Request.Context()).Select("dept_id").Where("id = ?", actorID).Limit(1).Find(&user).Error; err != nil {
		return 0
	}
	return user.DeptID
}

func (c *TalkController) writeServiceError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, talk.ErrLeaseConflict):
		response.Fail(ctx, "通道正在对讲中", http.StatusConflict)
	case errors.Is(err, talk.ErrInvalidTalkSessionMode):
		response.Fail(ctx, "mode 仅支持 broadcast 或 talk", http.StatusBadRequest)
	case errors.Is(err, talk.ErrTalkTargetOffline):
		response.Fail(ctx, "设备或通道离线", http.StatusConflict)
	case errors.Is(err, talk.ErrTalkNodeUnavailable), errors.Is(err, talk.ErrSecurePublishUnavailable), errors.Is(err, talk.ErrTalkActivationUnavailable):
		response.Fail(ctx, "语音对讲媒体服务不可用", http.StatusServiceUnavailable)
	case errors.Is(err, context.DeadlineExceeded):
		response.Fail(ctx, "语音对讲操作超时", http.StatusGatewayTimeout)
	default:
		response.Fail(ctx, "语音对讲操作失败", http.StatusInternalServerError)
	}
}

// writeUplinkError 映射上行准备的失败原因。
// 会话不存在 / 过期 / 已被占用这三种要能被调用方区分出来:它们对应的处置动作不同。
func (c *TalkController) writeUplinkError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, talk.ErrTalkSessionNotFound):
		c.logUplinkFailure(ctx, err, "session_not_found")
		response.Fail(ctx, "对讲会话不存在", http.StatusNotFound)
	case errors.Is(err, talk.ErrTalkSessionExpired):
		c.logUplinkFailure(ctx, err, "session_expired")
		response.Fail(ctx, "对讲会话已过期", http.StatusGone)
	case errors.Is(err, talk.ErrUplinkNotReserved):
		c.logUplinkFailure(ctx, err, "session_not_reserved")
		response.Fail(ctx, "对讲会话已开始发布", http.StatusConflict)
	default:
		c.logUplinkFailure(ctx, err, "unavailable")
		c.writeServiceError(ctx, err)
	}
}

// logUplinkFailure 记上行准备失败。
//
// error_code 是稳定码:现场排查第一刀要分清「会话没了」还是「媒体节点不可用」,
// 文案会改、码不会;session_id 是唯一能把这条日志和 gb_talk_session 那行对上的键。
func (c *TalkController) logUplinkFailure(ctx *gin.Context, err error, code string) {
	app.Log(ctx.Request.Context()).Warn("Talk uplink prepare failed",
		zap.String("event", "gb28181.talk.uplink_prepare_failed"),
		zap.String("session_id", strings.TrimSpace(ctx.Param("sessionId"))),
		zap.String("error_code", code),
		logging.Error(err))
}

// logUplinkForwardFailure 记转发到媒体节点失败。
// 与 prepare 失败分开:那一步的问题在平台自己,这一步的问题在平台到节点的链路。
func (c *TalkController) logUplinkForwardFailure(ctx *gin.Context, sessionID string, err error) {
	app.Log(ctx.Request.Context()).Warn("Talk uplink forward failed",
		zap.String("event", "gb28181.talk.uplink_forward_failed"),
		zap.String("session_id", sessionID),
		logging.Error(err))
}

func talkSessionView(session *gbmodels.GbTalkSession) TalkSessionView {
	return TalkSessionView{
		SessionID: session.SessionID, Mode: session.Mode, State: session.State, Phase: session.SignalPhase, ExpiresAt: session.ExpiresAt,
		StartedAt: session.StartedAt, EndedAt: session.EndedAt, Error: session.Error,
	}
}
