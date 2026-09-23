package zlm

import (
	"context"
	"fmt"
	"sync"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// ServerConfigCache ZLM 节点端口配置缓存(nodeID → ServerConfig)
//
// ZLM 的端口是 ZLM 自身配置,不在 meta_node 表里;运行时通过 /index/api/getServerConfig
// 一次性拉取并缓存,避免每次快照 / 抓帧调用都重复请求。
//
// 缓存策略:
//   - 首次访问某 nodeID 触发拉取
//   - 拉取成功后永久缓存(直到进程重启;ZLM 端口配置修改极少)
//   - 拉取失败不缓存,下次访问再试
//   - 支持手动失效(Invalidate),ZLM 节点重启或配置热更后可调
type ServerConfigCache struct {
	mu    sync.RWMutex
	data  map[int64]cachedEntry
	fetch fetchFunc
}

type cachedEntry struct {
	cfg       node.ServerConfig
	fetchedAt time.Time
}

// fetchFunc 拉取指定节点端口配置的委托(便于测试注入)
type fetchFunc func(ctx context.Context, nodeID int64) (node.ServerConfig, error)

// NewServerConfigCache 构造缓存;fetch 为拉取函数,通常传 NewClientForNode+GetServerConfig 的组合
func NewServerConfigCache(fetch fetchFunc) *ServerConfigCache {
	return &ServerConfigCache{
		data:  make(map[int64]cachedEntry),
		fetch: fetch,
	}
}

// Get 获取指定节点端口配置(首次访问触发拉取,后续命中缓存)
func (c *ServerConfigCache) Get(ctx context.Context, nodeID int64) (node.ServerConfig, error) {
	c.mu.RLock()
	if e, ok := c.data[nodeID]; ok {
		c.mu.RUnlock()
		return e.cfg, nil
	}
	c.mu.RUnlock()

	if c.fetch == nil {
		return node.ServerConfig{}, fmt.Errorf("ServerConfigCache: fetch 未配置")
	}
	cfg, err := c.fetch(ctx, nodeID)
	if err != nil {
		return node.ServerConfig{}, err
	}

	c.mu.Lock()
	c.data[nodeID] = cachedEntry{cfg: cfg, fetchedAt: time.Now()}
	c.mu.Unlock()
	return cfg, nil
}

// Refresh 绕过缓存重新拉取指定节点配置,成功后替换该节点的缓存值。
func (c *ServerConfigCache) Refresh(ctx context.Context, nodeID int64) (node.ServerConfig, error) {
	if c == nil || c.fetch == nil {
		return node.ServerConfig{}, fmt.Errorf("ServerConfigCache: fetch 未配置")
	}
	cfg, err := c.fetch(ctx, nodeID)
	if err != nil {
		return node.ServerConfig{}, err
	}

	c.mu.Lock()
	c.data[nodeID] = cachedEntry{cfg: cfg, fetchedAt: time.Now()}
	c.mu.Unlock()
	return cfg, nil
}

// Invalidate 手动失效指定节点缓存(节点重启 / 配置热更后调)
func (c *ServerConfigCache) Invalidate(nodeID int64) {
	c.mu.Lock()
	delete(c.data, nodeID)
	c.mu.Unlock()
}

// Clear 清空所有缓存
func (c *ServerConfigCache) Clear() {
	c.mu.Lock()
	c.data = make(map[int64]cachedEntry)
	c.mu.Unlock()
}

// FetchViaRegistry 构造一个默认的 fetchFunc:通过 node registry 拿节点 → 调 GetServerConfig
//
// 用法:
//
//	cache := NewServerConfigCache(FetchViaRegistry(zlmRegistry))
func FetchViaRegistry(registry interface {
	Get(id int64) (*node.Node, bool)
}) fetchFunc {
	return func(ctx context.Context, nodeID int64) (node.ServerConfig, error) {
		n, ok := registry.Get(nodeID)
		if !ok {
			return node.ServerConfig{}, fmt.Errorf("node %d 不存在", nodeID)
		}
		client := NewClientForNode(n)
		m, err := client.GetServerConfig(ctx)
		if err != nil {
			return node.ServerConfig{}, fmt.Errorf("拉 ZLM %d 配置失败: %w", nodeID, err)
		}
		return node.ParseServerConfig(m), nil
	}
}
