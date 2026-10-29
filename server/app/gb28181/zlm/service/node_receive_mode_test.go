package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/service"
)

func TestSinglePortMismatchReportsSafePortDetails(t *testing.T) {
	for _, tc := range []struct{ actual, reason, message string }{
		{"20000", "port_mismatch", "节点配置端口为 20000，填写端口为 10000"},
		{"0", "listener_disabled", "节点配置端口为 0，填写端口为 10000"},
		{"", "config_unavailable", "节点未返回有效的 rtp_proxy.port，填写端口为 10000"},
		{"secret=hidden", "config_unavailable", "节点未返回有效的 rtp_proxy.port，填写端口为 10000"},
	} {
		t.Run(tc.reason+tc.actual, func(t *testing.T) {
			svc := newSvc(newMemoryRepo(), &mockProbe{getServerConfig: map[string]string{"rtp_proxy.port": tc.actual}})
			_, err := svc.Create(context.Background(), service.CreateNodeReq{Host: "192.0.2.1", APIPort: 8080, APISecret: "fixture", RTPReceiveMode: "single", RTPProxyPort: 10000})
			var mismatch *service.RTPListenerMismatchError
			require.True(t, errors.As(err, &mismatch))
			require.ErrorIs(t, err, service.ErrRTPListenerMismatch)
			require.Equal(t, tc.reason, mismatch.Reason)
			require.Equal(t, 10000, mismatch.RequestedPort)
			require.ErrorContains(t, err, tc.message)
			require.NotContains(t, err.Error(), "secret=hidden")
		})
	}
}

func TestNodeReceiveModeDefaultValidationAndUpdate(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo()
	svc := newSvc(repo, &mockProbe{getServerConfig: map[string]string{"rtp_proxy.port": "10000"}})
	created, err := svc.Create(ctx, service.CreateNodeReq{Host: "192.0.2.1", APIPort: 8080, APISecret: "fixture"})
	require.NoError(t, err)
	require.Equal(t, "multi", created.RTPReceiveMode)
	mode, port := "single", 10000
	updated, err := svc.Update(ctx, created.ID, service.UpdateNodeReq{RTPReceiveMode: &mode, RTPProxyPort: &port})
	require.NoError(t, err)
	require.Equal(t, mode, updated.RTPReceiveMode)
	require.Equal(t, port, updated.RTPProxyPort)
	got, _ := repo.Get(ctx, created.ID)
	require.Equal(t, mode, got.RTPReceiveMode)
	for _, invalid := range []string{"", "both", "disabled"} {
		_, err = svc.Update(ctx, created.ID, service.UpdateNodeReq{RTPReceiveMode: &invalid})
		require.Error(t, err)
	}
	badPort := 65535
	_, err = svc.Update(ctx, created.ID, service.UpdateNodeReq{RTPProxyPort: &badPort})
	require.Error(t, err)
}

func TestSinglePortNodeDoesNotConsumeMultiPortCapacity(t *testing.T) {
	n := node.Node{RTPReceiveMode: "single", RTPProxyPort: 10000, RTPPortStart: 30000, RTPPortEnd: 30002,
		Stats: node.Stats{MediaSourceCount: 100}}
	require.False(t, n.IsNearCapacity())
	n.Stats.WorkThreadLoadAvg = 1
	n.Stats.NetThreadLoadAvg = 1
	require.True(t, n.IsNearCapacity())
}

func TestSinglePortModeRequiresMatchingZLMConfigBeforePersistence(t *testing.T) {
	ctx := context.Background()
	for _, actual := range []string{"", "0", "20000"} {
		t.Run(actual, func(t *testing.T) {
			repo := newMemoryRepo()
			probe := &mockProbe{getServerConfig: map[string]string{"rtp_proxy.port": actual}}
			svc := newSvc(repo, probe)
			_, err := svc.Create(ctx, service.CreateNodeReq{Host: "192.0.2.1", APIPort: 8080, APISecret: "fixture", RTPReceiveMode: "single", RTPProxyPort: 10000})
			require.ErrorContains(t, err, "单端口")
			require.Empty(t, repo.rows)
			old, err := svc.Create(ctx, service.CreateNodeReq{Host: "192.0.2.1", APIPort: 8080, APISecret: "fixture"})
			require.NoError(t, err)
			mode, port := "single", 10000
			_, err = svc.Update(ctx, old.ID, service.UpdateNodeReq{RTPReceiveMode: &mode, RTPProxyPort: &port})
			require.ErrorContains(t, err, "单端口")
			stored, _ := repo.Get(ctx, old.ID)
			require.Equal(t, "multi", stored.EffectiveRTPReceiveMode())
		})
	}
}
