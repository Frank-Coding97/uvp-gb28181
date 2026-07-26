package setup

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// 扫码回填 SIP 接入信息 —— 平台侧 token 生成与兑换.
// spec: wiki/projects/uvp-gb28181/specs/qr-sip-provisioning.md

var (
	// ErrTokenInvalid token 不存在 / 已消费 / 已过期.
	// 三种情况刻意不区分:对用户而言处置动作相同(回平台重新生成),
	// 区分反而泄露 token 是否曾有效.
	ErrTokenInvalid = errors.New("qr: token invalid or already used")

	// ErrTokenMalformed token 格式不合(长度或字符集),不查 cache 直接拒
	ErrTokenMalformed = errors.New("qr: token malformed")

	// ErrSIPNotConfigured 平台 SIP 尚未配置,不该生成接入码
	ErrSIPNotConfigured = errors.New("qr: sip not configured")
)

const (
	// qrTokenBytes token 随机字节数,128 bit
	qrTokenBytes = 16
	// qrTokenLen base64url 无填充编码后的字符数
	qrTokenLen = 22
	// qrTokenTTL 二维码有效期.二维码是"生成→立刻扫"的场景,
	// 长 TTL 只增加暴露窗口
	qrTokenTTL = 5 * time.Minute
	// qrCacheKeyPrefix 缓存键前缀,与项目 uvp-gb28181: 约定一致
	qrCacheKeyPrefix = "uvp-gb28181:qr:token:"
	// defaultTransport SIP 传输协议缺省值
	defaultTransport = "udp"
)

// QRPayload 二维码兑换后下发给设备的 SIP 接入六元组.
// 注意 Password 是平台全局统一密码的明文 —— 只经免鉴权但需一次性 token 的
// 兑换端点下发,响应须带 Cache-Control: no-store.
type QRPayload struct {
	ServerID  string `json:"serverId"`
	Domain    string `json:"domain"`
	IP        string `json:"ip"`
	Port      int    `json:"port"`
	Transport string `json:"transport"`
	Password  string `json:"password"`
}

// QRService 负责一次性接入 token 的生成与兑换.
//
// 设计要点:
//   - token 的 cache value 存的是**生成时刻的六元组快照**,不是标记位.
//     否则管理员在二维码生成后轮换密码,已泄露的旧 token 能换到新密码.
//   - 兑换走 CacheInterf.GetDel 原子取出并删除.用 Get + Del 两步会让
//     持有二维码截图的人与合法扫码者并发抢兑成功.
//   - 兑换路径不碰数据库,快照自足.
type QRService struct {
	cache       app.CacheInterf
	config      *SIPConfigService
	transportFn func() []string
	ttl         time.Duration
}

// NewQRService 构造 QRService.
// transportFn 提供 SIP 传输协议列表 —— 该值来自 YAML 而非 gb_sip_config 表
// (表里没有 transport 列),故用函数注入而不是查库.
func NewQRService(cache app.CacheInterf, config *SIPConfigService, transportFn func() []string) *QRService {
	return &QRService{
		cache:       cache,
		config:      config,
		transportFn: transportFn,
		ttl:         qrTokenTTL,
	}
}

// GenerateToken 生成一次性接入 token,并把当前 SIP 六元组快照写入缓存.
// 返回 token 与有效期秒数.
//
// 返回相对秒数而非绝对时间戳:前端以响应到达时刻起算倒计时,免受客户端
// 时钟偏移影响.
func (s *QRService) GenerateToken(ctx context.Context) (string, int, error) {
	view, err := s.config.Get(ctx)
	if err != nil {
		return "", 0, err
	}
	if view == nil {
		return "", 0, ErrSIPNotConfigured
	}

	payload := QRPayload{
		ServerID:  view.ServerID,
		Domain:    view.Domain,
		IP:        view.AdvertiseIP,
		Port:      view.Port,
		Transport: s.firstTransport(),
		Password:  view.Password,
	}

	snapshot, err := json.Marshal(payload)
	if err != nil {
		return "", 0, err
	}

	token, err := newQRToken()
	if err != nil {
		return "", 0, err
	}

	if err := s.cache.Set(ctx, qrCacheKey(token), string(snapshot), s.ttl); err != nil {
		return "", 0, err
	}

	return token, int(s.ttl.Seconds()), nil
}

// Exchange 原子消费 token 并返回其承载的六元组快照.
// token 不存在 / 已被消费 / 已过期都返回 ErrTokenInvalid;格式不合返回 ErrTokenMalformed.
func (s *QRService) Exchange(ctx context.Context, token string) (*QRPayload, error) {
	if !isWellFormedQRToken(token) {
		// 格式就不对,不必查缓存 —— 也顺带挡掉大部分乱扫的二维码
		return nil, ErrTokenMalformed
	}

	raw, err := s.cache.GetDel(ctx, qrCacheKey(token))
	if err != nil {
		if errors.Is(err, app.ErrKeyNotFound) {
			return nil, ErrTokenInvalid
		}
		return nil, err
	}

	var payload QRPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		// 缓存里的快照坏了(理论上不该发生);token 已被 GetDel 消费,不可重试
		return nil, ErrTokenInvalid
	}
	return &payload, nil
}

// firstTransport 取传输协议列表首项,空则回落 udp.
// 设备端只能单选一种传输,而平台配置是列表.
func (s *QRService) firstTransport() string {
	if s.transportFn == nil {
		return defaultTransport
	}
	list := s.transportFn()
	if len(list) == 0 || list[0] == "" {
		return defaultTransport
	}
	return list[0]
}

// newQRToken 生成 128 bit 的 base64url 无填充 token.
// 选 RawURLEncoding 而非 hex:同熵下更短(22 vs 32 字符),二维码码点更少更易扫.
func newQRToken() (string, error) {
	buf := make([]byte, qrTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// isWellFormedQRToken 校验 token 长度与字符集
func isWellFormedQRToken(token string) bool {
	if len(token) != qrTokenLen {
		return false
	}
	_, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil
}

// qrCacheKey 拼接缓存键
func qrCacheKey(token string) string {
	return qrCacheKeyPrefix + token
}
