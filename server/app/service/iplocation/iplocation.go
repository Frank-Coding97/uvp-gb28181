// Package iplocation resolves a client IP without making it a prerequisite for login.
package iplocation

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	ip2region "github.com/lionsoul2014/ip2region/binding/golang/service"
)

const (
	Unknown       = "未知"
	Private       = "内网"
	defaultRemote = "https://ipwho.is"
	positiveTTL   = 7 * 24 * time.Hour
	negativeTTL   = 30 * time.Minute
	maxCacheSize  = 4096
)

type Provider interface {
	Lookup(context.Context, string) (string, bool)
}

type ProviderFunc func(context.Context, string) (string, bool)

func (f ProviderFunc) Lookup(ctx context.Context, ip string) (string, bool) { return f(ctx, ip) }

type ResolverOptions struct {
	Local  Provider
	Remote Provider
	Now    func() time.Time
}

type Resolver struct {
	local, remote Provider
	now           func() time.Time
	mu            sync.Mutex
	cache         map[string]cacheEntry
}

type cacheEntry struct {
	value   string
	expires time.Time
}

func NewResolver(options ResolverOptions) *Resolver {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Resolver{local: options.Local, remote: options.Remote, now: now, cache: make(map[string]cacheEntry)}
}

func (r *Resolver) Resolve(ctx context.Context, rawIP string) string {
	ip := net.ParseIP(strings.TrimSpace(rawIP))
	if ip == nil {
		return Unknown
	}
	if isPrivate(ip) {
		return Private
	}
	key := ip.String()
	if value, ok := r.cached(key); ok {
		return value
	}
	if r.local != nil {
		if value, ok := r.local.Lookup(ctx, key); ok {
			value = normalize(value)
			if value != Unknown {
				r.store(key, value, positiveTTL)
				return value
			}
		}
	}
	if r.remote != nil {
		if value, ok := r.remote.Lookup(ctx, key); ok {
			value = normalize(value)
			if value != Unknown {
				r.store(key, value, positiveTTL)
				return value
			}
		}
	}
	r.store(key, Unknown, negativeTTL)
	return Unknown
}

func (r *Resolver) cached(key string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.cache[key]
	if !ok || !entry.expires.After(r.now()) {
		if ok {
			delete(r.cache, key)
		}
		return "", false
	}
	return entry.value, true
}

func (r *Resolver) store(key, value string, ttl time.Duration) {
	r.mu.Lock()
	if _, exists := r.cache[key]; !exists && len(r.cache) >= maxCacheSize {
		for oldestKey := range r.cache {
			delete(r.cache, oldestKey)
			break
		}
	}
	r.cache[key] = cacheEntry{value: value, expires: r.now().Add(ttl)}
	r.mu.Unlock()
}

func isPrivate(ip net.IP) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()
}

func normalize(value string) string {
	if strings.Contains(value, "|") {
		return NormalizeRegion(value)
	}
	value = strings.TrimSpace(value)
	if value == "" || value == "0" {
		return Unknown
	}
	return value
}

func NormalizeRegion(value string) string {
	parts := strings.Split(value, "|")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" && part != "0" {
			result = append(result, part)
		}
	}
	if len(result) == 0 {
		return Unknown
	}
	return strings.Join(result, " ")
}

type ipWhoIsResponse struct {
	Success    bool   `json:"success"`
	Country    string `json:"country"`
	Region     string `json:"region"`
	City       string `json:"city"`
	Connection struct {
		ISP string `json:"isp"`
	} `json:"connection"`
}

type IPWhoIsProvider struct {
	client  *http.Client
	baseURL string
}

func NewIPWhoIsProvider(client *http.Client, baseURL string, timeout time.Duration) Provider {
	if client == nil {
		client = &http.Client{Timeout: timeout}
	} else if timeout > 0 && client.Timeout == 0 {
		copyClient := *client
		copyClient.Timeout = timeout
		client = &copyClient
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultRemote
	}
	return &IPWhoIsProvider{client: client, baseURL: strings.TrimRight(baseURL, "/")}
}

func (p *IPWhoIsProvider) Lookup(ctx context.Context, ip string) (string, bool) {
	endpoint := p.baseURL + "/" + url.PathEscape(ip)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", false
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", false
	}
	var payload ipWhoIsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil || !payload.Success {
		return "", false
	}
	parts := []string{payload.Country, payload.Region, payload.City, payload.Connection.ISP}
	return strings.Join(parts, " "), true
}

type IP2RegionProvider struct{ region *ip2region.Ip2Region }

func NewIP2RegionProvider(v4Path, v6Path string) Provider {
	if v4Path == "" || !fileExists(v4Path) {
		v4Path = ""
	}
	if v6Path == "" || !fileExists(v6Path) {
		v6Path = ""
	}
	if v4Path == "" && v6Path == "" {
		return nil
	}
	region, err := ip2region.NewIp2RegionWithPath(v4Path, v6Path)
	if err != nil {
		return nil
	}
	return &IP2RegionProvider{region: region}
}

func (p *IP2RegionProvider) Lookup(_ context.Context, ip string) (string, bool) {
	if p == nil || p.region == nil {
		return "", false
	}
	value, err := p.region.Search(ip)
	return value, err == nil && normalize(value) != Unknown
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func DefaultResolver() *Resolver {
	v4, v6 := findDatabasePaths()
	return NewResolver(ResolverOptions{
		Local:  NewIP2RegionProvider(v4, v6),
		Remote: NewIPWhoIsProvider(nil, defaultRemote, 2*time.Second),
	})
}

func findDatabasePaths() (string, string) {
	candidates := []string{}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, cwd)
	}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates, exeDir, filepath.Dir(exeDir))
	}
	for _, root := range candidates {
		for _, dir := range []string{filepath.Join(root, "resource", "ipdb"), filepath.Join(root, "server", "resource", "ipdb")} {
			v4 := filepath.Join(dir, "ip2region_v4.xdb")
			v6 := filepath.Join(dir, "ip2region_v6.xdb")
			if fileExists(v4) || fileExists(v6) {
				return v4, v6
			}
		}
	}
	return "", ""
}
