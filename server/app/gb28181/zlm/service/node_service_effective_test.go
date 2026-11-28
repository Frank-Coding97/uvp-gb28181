package service_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/service"
)

// 节点级留空 = 跟随平台默认。DTO 必须把「实际生效值」一起带出去，
// 否则编辑弹窗三个输入框全空白 —— 用户会以为引导页配的地址没保存
//（2026-10-20 现场反馈：「有一个还是两个字段不会回显」）。
func TestNodeService_DTO_CarriesEffectiveMediaAddresses(t *testing.T) {
	repo := newMemoryRepo()
	reg := node.NewRegistry(repo)
	svc := service.NewNodeService(reg, &mockProbe{}, service.MediaTuning{
		HookIP: "192.0.2.10", SDPIP: "203.0.113.10", StreamIP: "play.example.com",
	})

	created, err := svc.Create(context.Background(), service.CreateNodeReq{
		Name: "n1", Host: "10.0.0.7", APIPort: 18080, APISecret: "s",
	})
	require.NoError(t, err)

	// 节点自己一个都没填 —— 三个原始字段必须如实为空，不能偷偷回填。
	require.Empty(t, created.HookIP)
	require.Empty(t, created.SDPIP)
	require.Empty(t, created.PlaybackHost)
	// 但「实际生效」要能读出来，界面才有话可说。
	require.Equal(t, "192.0.2.10", created.EffectiveHookIP)
	require.Equal(t, "203.0.113.10", created.EffectiveSDPIP)
	require.Equal(t, "play.example.com", created.EffectivePlaybackHost)
}

// 节点自己填了就以节点为准，平台默认不许盖掉覆盖值。
func TestNodeService_DTO_NodeOverrideWinsOverPlatformDefault(t *testing.T) {
	repo := newMemoryRepo()
	reg := node.NewRegistry(repo)
	reg.SetPlatformSDPIP("203.0.113.10")
	reg.SetPlatformPlaybackHost("play.example.com")
	svc := service.NewNodeService(reg, &mockProbe{}, service.MediaTuning{
		HookIP: "192.0.2.10", SDPIP: "203.0.113.10", StreamIP: "play.example.com",
	})

	created, err := svc.Create(context.Background(), service.CreateNodeReq{
		Name: "n2", Host: "10.0.0.8", APIPort: 18080, APISecret: "s",
		HookIP: "198.51.100.20", SDPIP: "198.51.100.21", PlaybackHost: "media.example.com",
	})
	require.NoError(t, err)

	require.Equal(t, "198.51.100.20", created.EffectiveHookIP)
	require.Equal(t, "198.51.100.21", created.EffectiveSDPIP)
	require.Equal(t, "media.example.com", created.EffectivePlaybackHost)
}

// 平台默认也为空时，SDP 的兜底链会继续退到平台播放地址 —— 这里锁住
// 「DTO 的生效值」与「真正下发用的 EffectiveSDPIP」是同一条链，不许漂。
func TestNodeService_DTO_EffectiveSDPIPFollowsDomainFallbackChain(t *testing.T) {
	repo := newMemoryRepo()
	reg := node.NewRegistry(repo)
	reg.SetPlatformSDPIP("")
	reg.SetPlatformPlaybackHost("203.0.113.55")
	svc := service.NewNodeService(reg, &mockProbe{}, service.MediaTuning{StreamIP: "203.0.113.55"})

	created, err := svc.Create(context.Background(), service.CreateNodeReq{
		Name: "n3", Host: "10.0.0.9", APIPort: 18080, APISecret: "s",
	})
	require.NoError(t, err)

	require.Equal(t, "203.0.113.55", created.EffectiveSDPIP)
}
