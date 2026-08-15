package playauth

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// T12 RED-1: 分配/取消共享 BumpRevocation 后,旧 token 验证失败
func TestBumpRevocation_InvalidatesOldTokens(t *testing.T) {
	// 测试结束时恢复全局失效分界,避免污染其他测试
	prev := RevokedBefore()
	defer revokedBefore.Store(prev)

	fixedNow := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	signer, err := NewSigner([]byte("test-root-secret-32-bytes-long!!"), WithNow(func() time.Time { return fixedNow }))
	require.NoError(t, err)

	binding := Binding{
		DeviceID:        "34020000002000000001",
		ChannelID:       "37011200001310000001",
		App:             "rtp",
		Stream:          "live",
		MediaServerID:   "node-1",
		MediaGeneration: 1,
	}
	grant, err := signer.IssueDirect(binding)
	require.NoError(t, err)

	// 撤销前可验证
	_, err = signer.Verify(grant.Token, binding)
	require.NoError(t, err)

	// Bump 后(签发时刻早于分界)应拒绝
	BumpRevocation(fixedNow.Add(time.Minute))
	_, err = signer.Verify(grant.Token, binding)
	assert.True(t, errors.Is(err, ErrTokenRevoked), "旧 token 应被撤销,实际: %v", err)
}

// T12 RED-2: Bump 之后签发的新 token 正常
func TestBumpRevocation_NewTokensStillValid(t *testing.T) {
	prev := RevokedBefore()
	defer revokedBefore.Store(prev)

	fixedNow := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	signer, err := NewSigner([]byte("test-root-secret-32-bytes-long!!"), WithNow(func() time.Time { return fixedNow }))
	require.NoError(t, err)

	BumpRevocation(fixedNow.Add(-time.Minute))

	binding := Binding{
		DeviceID:        "34020000002000000001",
		ChannelID:       "37011200001310000001",
		App:             "rtp",
		Stream:          "live",
		MediaServerID:   "node-1",
		MediaGeneration: 1,
	}
	grant, err := signer.IssueDirect(binding)
	require.NoError(t, err)

	_, err = signer.Verify(grant.Token, binding)
	require.NoError(t, err, "新签发的 token 不应被撤销")
}
