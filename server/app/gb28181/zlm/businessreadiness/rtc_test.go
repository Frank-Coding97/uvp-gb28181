package businessreadiness_test

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/businessreadiness"
)

func TestStandaloneProbeRequiresRTCAddressAndTransport(t *testing.T) {
	registry, added, media, _, client, now, clock := setupProbe(t)
	current, ok := registry.Get(added.ID)
	require.True(t, ok)
	current.ReceiveHost = "192.0.2.52"
	require.NoError(t, registry.Update(context.Background(), *current))
	registry.SetAutoOnDemandReady(added.ID, true)
	media.ManageRTCExternIP = true
	expected, err := zlm.ExpectedConfigForNode(current, media)
	require.NoError(t, err)
	client.config = expected
	client.config["rtc.port"] = "0"
	client.config["rtc.tcpPort"] = "0"
	input := probeInput(registry, media)
	probe := businessreadiness.NewProbe(businessreadiness.ProbeConfig{Client: client, Now: now})
	require.Equal(t, businessreadiness.ReasonConfigNotConverged, probe.Check(context.Background(), input).Reason)
	client.config["rtc.port"] = "18000"
	require.Equal(t, businessreadiness.ReasonHookUnconfirmed, probe.Check(context.Background(), input).Reason)
	*clock = now().Add(time.Second)
	registry.UpdateHeartbeatFields(added.MediaServerUUID, 0, 0, now())
	require.True(t, probe.Check(context.Background(), input).BusinessReady)
	client.config["rtc.externIP"] = "127.0.0.1"
	require.Equal(t, businessreadiness.ReasonConfigNotConverged, probe.Check(context.Background(), input).Reason)
}
