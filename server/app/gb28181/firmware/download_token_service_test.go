package firmware

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// mockCache 实现 app.CacheInterf 用于测试
type mockCache struct {
	store map[string]string
}

func newMockCache() *mockCache {
	return &mockCache{store: make(map[string]string)}
}

func (m *mockCache) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	m.store[key] = value
	return nil
}

func (m *mockCache) Get(ctx context.Context, key string) (string, error) {
	val, ok := m.store[key]
	if !ok {
		return "", app.ErrKeyNotFound
	}
	return val, nil
}

func (m *mockCache) GetDel(ctx context.Context, key string) (string, error) {
	val, ok := m.store[key]
	if !ok {
		return "", app.ErrKeyNotFound
	}
	delete(m.store, key)
	return val, nil
}

func (m *mockCache) Del(ctx context.Context, keys ...string) error {
	for _, key := range keys {
		delete(m.store, key)
	}
	return nil
}

func (m *mockCache) Exists(ctx context.Context, keys ...string) (int64, error) {
	count := int64(0)
	for _, key := range keys {
		if _, ok := m.store[key]; ok {
			count++
		}
	}
	return count, nil
}

func (m *mockCache) Expire(ctx context.Context, key string, expiration time.Duration) error {
	// mockCache 不实现真实的 TTL，只记录 Set 时传入的 TTL
	return nil
}

func (m *mockCache) GetAll(ctx context.Context) ([]app.CacheItem, error) {
	items := make([]app.CacheItem, 0, len(m.store))
	for k, v := range m.store {
		items = append(items, app.CacheItem{Key: k, Value: v})
	}
	return items, nil
}

func (m *mockCache) GetInt(ctx context.Context, key string) (int64, error) {
	val, ok := m.store[key]
	if !ok {
		return 0, app.ErrKeyNotFound
	}
	var i int64
	err := json.Unmarshal([]byte(val), &i)
	return i, err
}

func (m *mockCache) SetInt(ctx context.Context, key string, value int64, expiration time.Duration) error {
	data, _ := json.Marshal(value)
	m.store[key] = string(data)
	return nil
}

func (m *mockCache) Incr(ctx context.Context, key string) (int64, error) {
	return 0, nil
}

func (m *mockCache) Decr(ctx context.Context, key string) (int64, error) {
	return 0, nil
}

func (m *mockCache) Close() error {
	return nil
}

// TC3.1: 生成 token 并存入 cache
func TestDownloadTokenService_Generate(t *testing.T) {
	cache := newMockCache()
	service := NewDownloadTokenService(cache)

	firmwareID := "fw-test-123"
	userID := uint64(100)
	deptID := uint64(200)

	token, downloadURL, expiresAt, err := service.Generate(context.Background(), firmwareID, userID, deptID)

	require.NoError(t, err)
	assert.Len(t, token, 22, "token should be 22 characters (base64url of 16 bytes)")
	assert.Contains(t, downloadURL, "/api/v1/gb28181/firmware/download/")
	assert.Contains(t, downloadURL, token)
	assert.True(t, expiresAt.After(time.Now()))
	assert.True(t, expiresAt.Before(time.Now().Add(61*time.Minute)), "TTL should be ~1 hour")

	// 验证 cache 中存在
	cacheKey := "firmware:download:token:" + token
	raw, err := cache.Get(context.Background(), cacheKey)
	require.NoError(t, err)

	var snapshot DownloadTokenSnapshot
	err = json.Unmarshal([]byte(raw), &snapshot)
	require.NoError(t, err)
	assert.Equal(t, firmwareID, snapshot.FirmwareID)
	assert.Equal(t, userID, snapshot.UserID)
	assert.Equal(t, deptID, snapshot.DeptID)
	assert.True(t, snapshot.ExpiresAt.After(time.Now()))
}

// TC3.2: 消费 token（原子 GetDel）
func TestDownloadTokenService_Consume_Atomic(t *testing.T) {
	cache := newMockCache()
	service := NewDownloadTokenService(cache)

	firmwareID := "fw-atomic-test"
	userID := uint64(300)
	deptID := uint64(400)

	token, _, _, err := service.Generate(context.Background(), firmwareID, userID, deptID)
	require.NoError(t, err)

	// 第一次消费 - 应该成功
	snapshot, err := service.Consume(context.Background(), token)
	require.NoError(t, err)
	assert.Equal(t, firmwareID, snapshot.FirmwareID)
	assert.Equal(t, userID, snapshot.UserID)
	assert.Equal(t, deptID, snapshot.DeptID)

	// 第二次消费 - 应该失败（token 已被删除）
	_, err = service.Consume(context.Background(), token)
	assert.ErrorIs(t, err, ErrDownloadTokenInvalid)

	// 验证 cache 中确实不存在了
	cacheKey := "firmware:download:token:" + token
	_, err = cache.Get(context.Background(), cacheKey)
	assert.ErrorIs(t, err, app.ErrKeyNotFound)
}

// TC3.3: 过期 token 校验
func TestDownloadTokenService_Consume_Expired(t *testing.T) {
	cache := newMockCache()
	service := NewDownloadTokenService(cache)

	// 手动构造一个已过期的 snapshot
	expiredSnapshot := DownloadTokenSnapshot{
		FirmwareID: "fw-expired",
		UserID:     500,
		DeptID:     600,
		ExpiresAt:  time.Now().Add(-1 * time.Hour), // 1 小时前过期
	}

	snapshotJSON, err := json.Marshal(expiredSnapshot)
	require.NoError(t, err)

	token := "M-AjiUjCfiKk41XPB4Erwg" // 22 chars valid base64url
	cacheKey := "firmware:download:token:" + token
	err = cache.Set(context.Background(), cacheKey, string(snapshotJSON), time.Hour)
	require.NoError(t, err)

	// 消费过期 token - 应该返回过期错误
	_, err = service.Consume(context.Background(), token)
	assert.ErrorIs(t, err, ErrDownloadTokenExpired)

	// 验证过期 token 仍被 GetDel 删除了（不可重试）
	_, err = cache.Get(context.Background(), cacheKey)
	assert.ErrorIs(t, err, app.ErrKeyNotFound)
}

// TC3.4: 格式不合法的 token
func TestDownloadTokenService_Consume_Malformed(t *testing.T) {
	cache := newMockCache()
	service := NewDownloadTokenService(cache)

	testCases := []struct {
		name  string
		token string
	}{
		{"too short", "short"},
		{"too long", "this-is-way-too-long-to-be-a-valid-token"},
		{"wrong length", "exactly-21-chars-long"},
		{"invalid base64url chars", "invalid/token+with=pad"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.Consume(context.Background(), tc.token)
			assert.ErrorIs(t, err, ErrDownloadTokenMalformed)
		})
	}
}

// TC3.5: token 不存在
func TestDownloadTokenService_Consume_NotFound(t *testing.T) {
	cache := newMockCache()
	service := NewDownloadTokenService(cache)

	// 格式正确但不存在的 token (22 个有效 base64url 字符)
	nonExistentToken := "FghRB5CcmqaN7c93JbE_Gw"

	_, err := service.Consume(context.Background(), nonExistentToken)
	assert.ErrorIs(t, err, ErrDownloadTokenInvalid)
}
