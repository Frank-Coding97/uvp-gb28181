package firmware

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"uvplatform.com/uvp-gb28181/app/global/app"
)

var (
	// ErrDownloadTokenInvalid token 不存在 / 已消费 / 已过期（三种情况不区分）
	ErrDownloadTokenInvalid = errors.New("firmware: download token invalid or already used")

	// ErrDownloadTokenMalformed token 格式不合法（长度或字符集）
	ErrDownloadTokenMalformed = errors.New("firmware: download token malformed")

	// ErrDownloadTokenExpired token 已过期
	ErrDownloadTokenExpired = errors.New("firmware: download token expired")
)

const (
	// downloadTokenBytes token 随机字节数，128 bit
	downloadTokenBytes = 16
	// downloadTokenLen base64url 无填充编码后的字符数
	downloadTokenLen = 22
	// downloadTokenTTL 下载 token 有效期 1 小时
	downloadTokenTTL = 1 * time.Hour
	// downloadCacheKeyPrefix 缓存键前缀
	downloadCacheKeyPrefix = "firmware:download:token:"
	// DownloadRoutePath 固件文件下载路由(免 JWT，凭一次性 token 兑换)。
	// ⛔ 必须与 routes.go 里实际 engine.GET 注册的路径**逐字一致**，否则前端
	//   拿到链接后直接 404 —— 这个错不会在服务端暴露（Generate 照样成功返回 200），
	//   只会表现为"点了下载 Page Not Found"，极难归因。
	//   历史坑：这里曾写成 `/api/v1/gb28181/firmware/download/%s`，而真实注册的是
	//   `/api/gb28181/device-mgmt/firmware-repository/download/:token`。
	//   测试 download_token_service_test.go 会断言两者一致，改这里必须同步改路由。
	DownloadRoutePath = "/api/gb28181/device-mgmt/firmware-repository/download/"
)

// DownloadTokenSnapshot 下载 token 快照，存储在 cache 中
type DownloadTokenSnapshot struct {
	FirmwareID string    `json:"firmwareId"`
	UserID     uint64    `json:"userId"`
	DeptID     uint64    `json:"deptId"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

// DownloadTokenService 负责固件下载临时 token 的生成与消费
type DownloadTokenService struct {
	cache app.CacheInterf
	ttl   time.Duration
}

// NewDownloadTokenService 构造 DownloadTokenService
func NewDownloadTokenService(cache app.CacheInterf) *DownloadTokenService {
	return &DownloadTokenService{
		cache: cache,
		ttl:   downloadTokenTTL,
	}
}

// Generate 生成临时下载 token 并返回 token、下载 URL、过期时间
func (s *DownloadTokenService) Generate(ctx context.Context, firmwareID string, userID, deptID uint64) (token, downloadURL string, expiresAt time.Time, err error) {
	expiresAt = time.Now().Add(s.ttl)

	snapshot := DownloadTokenSnapshot{
		FirmwareID: firmwareID,
		UserID:     userID,
		DeptID:     deptID,
		ExpiresAt:  expiresAt,
	}

	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		return "", "", time.Time{}, err
	}

	token, err = newDownloadToken()
	if err != nil {
		return "", "", time.Time{}, err
	}

	cacheKey := downloadCacheKey(token)
	if err := s.cache.Set(ctx, cacheKey, string(snapshotJSON), s.ttl); err != nil {
		return "", "", time.Time{}, err
	}

	downloadURL = DownloadRoutePath + token
	return token, downloadURL, expiresAt, nil
}

// Consume 原子消费 token 并返回快照（GetDel 保证只能消费一次）
func (s *DownloadTokenService) Consume(ctx context.Context, token string) (*DownloadTokenSnapshot, error) {
	if !isWellFormedDownloadToken(token) {
		return nil, ErrDownloadTokenMalformed
	}

	cacheKey := downloadCacheKey(token)
	raw, err := s.cache.GetDel(ctx, cacheKey)
	if err != nil {
		if errors.Is(err, app.ErrKeyNotFound) {
			return nil, ErrDownloadTokenInvalid
		}
		return nil, err
	}

	var snapshot DownloadTokenSnapshot
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		// 快照损坏（理论上不该发生），token 已被 GetDel 消费
		return nil, ErrDownloadTokenInvalid
	}

	// 检查过期
	if time.Now().After(snapshot.ExpiresAt) {
		return nil, ErrDownloadTokenExpired
	}

	return &snapshot, nil
}

// newDownloadToken 生成 128 bit 的 base64url 无填充 token
func newDownloadToken() (string, error) {
	buf := make([]byte, downloadTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// isWellFormedDownloadToken 校验 token 长度与字符集
func isWellFormedDownloadToken(token string) bool {
	if len(token) != downloadTokenLen {
		return false
	}
	_, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil
}

// downloadCacheKey 拼接缓存键
func downloadCacheKey(token string) string {
	return downloadCacheKeyPrefix + token
}
