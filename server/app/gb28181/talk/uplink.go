package talk

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

var (
	ErrTalkSessionExpired = errors.New("对讲会话已过期")
	ErrUplinkNotReserved  = errors.New("对讲会话已开始发布,不能重复初始化上行")
)

// UplinkTarget 是后端转发一次 WHIP 请求所需的全部事实。
//
// 只在本进程内流转,不序列化进任何响应:URL 里带着一次性发布令牌。
type UplinkTarget struct {
	URL         string
	ContentType string
}

// PrepareUplink 为一次上行转发做准备:校验会话可用 → 轮换一次性发布令牌 →
// 拼出指向媒体节点的 WHIP 地址。
//
// 令牌在这里签发,而不是在 Create 时就下发浏览器:
//   - 明文令牌只存在于后端内存,不进浏览器历史 / Referer / 前端访问日志;
//   - 浏览器侧的上行入口是平台自己的路径,媒体节点的地址与自签证书不外露;
//   - 轮换使每次转发都对应一把新令牌,旧的立即失效。
//
// 上游用媒体节点的 API 地址(mediaNode.Host)而非播放地址:后者是给浏览器看的,
// 前者才是平台调 ZLM API 用的、保证可达的地址。
func (s *Service) PrepareUplink(ctx context.Context, sessionID string) (*UplinkTarget, error) {
	session, err := s.repo.FindBySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, ErrTalkSessionNotFound
	}
	now := s.now().UTC()
	if !session.ExpiresAt.After(now) || session.State.IsTerminal() {
		return nil, ErrTalkSessionExpired
	}
	// 只有预留态能开上行。已进入 publishing 的会话再换令牌也无法通过 on_publish
	// 回调(AuthorizePublish 对非 reserved 状态直接拒绝),这里提前挡住。
	if session.State != models.TalkSessionReserved {
		return nil, ErrUplinkNotReserved
	}
	mediaNode, ok := s.nodes.Get(session.NodeID)
	if !ok || mediaNode == nil {
		return nil, ErrTalkNodeUnavailable
	}
	config, err := s.configs.Get(ctx, session.NodeID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTalkNodeUnavailable, err)
	}
	if config.HTTPSPort <= 0 {
		return nil, ErrSecurePublishUnavailable
	}
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	issued, err := s.repo.ReissuePublishToken(ctx, sessionID, token, now)
	if err != nil {
		return nil, err
	}
	if !issued {
		// 并发转发或状态刚被清理:按冲突处理,不静默放行。
		return nil, ErrUplinkNotReserved
	}
	query := url.Values{"app": {session.App}, "stream": {session.SourceStream}, "token": {token}}
	upstream := fmt.Sprintf("https://%s:%d/index/api/whip?%s",
		mediaNode.Host, config.HTTPSPort, query.Encode())
	return &UplinkTarget{URL: upstream, ContentType: "application/sdp"}, nil
}
