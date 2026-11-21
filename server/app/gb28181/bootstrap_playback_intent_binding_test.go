package gb28181

import (
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/playauth"
	gbplayback "uvplatform.com/uvp-gb28181/app/gb28181/playback"
)

// 回放装配契约:ServiceConfig.Intents 非 nil 时,回放 Create 会进入持久意图分支并做
// s.rtp.(IntentRTPFactory) 工厂断言。legacy opener 不具备该能力,一旦搭配非 nil Intents,
// 每次创建回放会话都会以 ErrRTPUnavailable 结束(对外 502「回放控制失败」,
// 不建会话、不调度节点、不发 SIP INVITE)。本测试锚定该契约,防止装配再次失配。
func TestAlignPlaybackIntentBinding_LegacyOpenerDropsIntents(t *testing.T) {
	legacy := gbplayback.NewZLMRTPOpener(nil, nil, nil)
	_, implementsIntent := legacy.(gbplayback.IntentRTPFactory)
	require.False(t, implementsIntent, "legacy ZLM opener 不应实现 IntentRTPFactory")

	intents := new(playauth.DeviceOperationIntentStore)
	aligned, dropped := alignPlaybackIntentBinding(legacy, intents)
	require.Nil(t, aligned, "legacy opener 搭配 Intents 时必须降级为 nil")
	require.True(t, dropped, "不匹配组合必须被标记为降级")
}

func TestAlignPlaybackIntentBinding_IntentOpenerKeepsIntents(t *testing.T) {
	intentOpener := gbplayback.NewZLMIntentRTPOpener(nil, nil, nil)
	_, implementsIntent := intentOpener.(gbplayback.IntentRTPFactory)
	require.True(t, implementsIntent, "Intent opener 必须实现 IntentRTPFactory")

	intents := new(playauth.DeviceOperationIntentStore)
	aligned, dropped := alignPlaybackIntentBinding(intentOpener, intents)
	require.Same(t, intents, aligned, "具备 Intent 能力时不得丢弃 Intents")
	require.False(t, dropped)
}

func TestAlignPlaybackIntentBinding_NilIntentsStaysNil(t *testing.T) {
	legacy := gbplayback.NewZLMRTPOpener(nil, nil, nil)
	aligned, dropped := alignPlaybackIntentBinding(legacy, nil)
	require.Nil(t, aligned)
	require.False(t, dropped)
}
