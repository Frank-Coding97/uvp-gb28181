package gb28181

import (
	"net"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
)

const (
	// cascadePeerTrustTTL 是「已启用级联平台地址表」的缓存时长。INVITE 是低频
	// 报文,但被洪水打时读路径不能每条都查库。
	cascadePeerTrustTTL = 30 * time.Second
	// cascadePeerTrustRetry 是查询失败后的短重试窗口。失败时保留旧快照,
	// 否则一次数据库抖动就会让所有级联点播被准入层丢弃,现象是「上级点播
	// 突然全部超时」而本机日志一片安静。
	cascadePeerTrustRetry = 5 * time.Second
)

// newCascadePlatformPeerTrust 返回「源地址是否属于某个已启用级联平台」的判定器,
// 交给 security.Runtime.SetPeerTrustResolver 使用。
//
// 作用域只到**准入层**:它回答的是「这个源 IP 要不要按陌生扫描丢掉」。真正的
// 身份校验在级联 INVITE 处理器里(From 编号 + 源端口 + 传输层 + Host/已鉴权地址 +
// Subject 收发双方),所以这里放宽不会让陌生 IP 拿到媒体。
func newCascadePlatformPeerTrust(db *gorm.DB) func(sourceIP, transport string) bool {
	if db == nil {
		return func(string, string) bool { return false }
	}
	cache := &cascadePeerTrustCache{db: db, ttl: cascadePeerTrustTTL}
	return cache.match
}

type cascadePeerTrustCache struct {
	db  *gorm.DB
	ttl time.Duration

	mu         sync.Mutex
	peers      map[string]map[string]struct{}
	expires    time.Time
	refreshing bool
}

// match 兼容 sip.TransportReadFilter 的入参口径:源 IP + 归一化传输层(小写)。
func (c *cascadePeerTrustCache) match(sourceIP, transport string) bool {
	ip := net.ParseIP(strings.TrimSpace(sourceIP))
	if ip == nil {
		return false
	}
	transports, ok := c.snapshot()[ip.String()]
	if !ok {
		return false
	}
	if len(transports) == 0 {
		// 平台没登记传输层(历史数据),只按 IP 认。
		return true
	}
	_, ok = transports[strings.ToLower(strings.TrimSpace(transport))]
	return ok
}

func (c *cascadePeerTrustCache) snapshot() map[string]map[string]struct{} {
	c.mu.Lock()
	cached := c.peers
	if cached != nil && time.Now().Before(c.expires) {
		c.mu.Unlock()
		return cached
	}
	if c.refreshing {
		// 已有协程在刷:读路径不排队等数据库,先按上一次快照判定。
		c.mu.Unlock()
		return cached
	}
	c.refreshing = true
	c.mu.Unlock()

	loaded, ok := c.load()

	c.mu.Lock()
	switch {
	case ok:
		c.peers = loaded
		c.expires = time.Now().Add(c.ttl)
	case cached != nil:
		c.expires = time.Now().Add(cascadePeerTrustRetry)
	}
	c.refreshing = false
	result := c.peers
	c.mu.Unlock()
	return result
}

func (c *cascadePeerTrustCache) load() (map[string]map[string]struct{}, bool) {
	var rows []struct {
		Host      string
		Transport string
	}
	if err := c.db.Model(&model.GbCascadePlatform{}).
		Select("host", "transport").
		Where("enabled = ?", true).
		Scan(&rows).Error; err != nil {
		return nil, false
	}
	peers := make(map[string]map[string]struct{}, len(rows))
	for _, row := range rows {
		transport := strings.ToLower(strings.TrimSpace(row.Transport))
		for _, address := range resolveCascadePeerHosts(row.Host) {
			set := peers[address]
			if set == nil {
				set = make(map[string]struct{})
				peers[address] = set
			}
			if transport != "" {
				set[transport] = struct{}{}
			}
		}
	}
	return peers, true
}

// resolveCascadePeerHosts 把平台配置的 Host 摊平成源 IP 集合。Host 允许填 IP,
// 也允许填域名(线上就有填域名的部署),后者走一次系统解析。
func resolveCascadePeerHosts(host string) []string {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil {
		return []string{ip.String()}
	}
	addresses, err := net.LookupIP(host)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(addresses))
	for _, ip := range addresses {
		out = append(out, ip.String())
	}
	return out
}
