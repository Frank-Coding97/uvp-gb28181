package controllers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	gbconfig "uvplatform.com/uvp-gb28181/app/gb28181/config"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

type playbackNodes struct{ nodes []*node.Node }

func (n *playbackNodes) List() []*node.Node { return n.nodes }

func playbackPolicyFixture(t *testing.T) (*PlaybackCompatibility, *serviceConfigTestYAML, *playbackNodes) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	source := &serviceConfigTestYAML{values: map[string]interface{}{
		gbconfig.FixedAddressEnabledConfigKey: true, gbconfig.AutoOnDemandEnabledConfigKey: true,
	}}
	app.ConfigYml = source
	nodes := &playbackNodes{}
	return NewPlaybackCompatibility(nodes), source, nodes
}

func TestPlaybackCompatibilityNodeChange(t *testing.T) {
	for _, mode := range []string{"single", "multi"} {
		t.Run(mode, func(t *testing.T) {
			policy, source, nodes := playbackPolicyFixture(t)
			candidate := &node.Node{ID: 1, Name: "入口", State: node.StateOffline, RTPReceiveMode: mode}
			changed, err := policy.WithNodeChange(candidate, func() error {
				if mode == "single" {
					require.False(t, gbconfig.CurrentFixedAddressPlaybackSettings().FixedAddressEnabled)
				}
				nodes.nodes = []*node.Node{candidate}
				return nil
			})
			require.NoError(t, err)
			require.Equal(t, mode == "single", changed)
			require.Equal(t, mode == "multi", source.GetBool(gbconfig.FixedAddressEnabledConfigKey))
			require.Equal(t, mode == "multi", source.GetBool(gbconfig.AutoOnDemandEnabledConfigKey))
			if mode == "single" {
				require.Equal(t, []gbconfig.SinglePortNode{{ID: 1, Name: "入口"}}, policy.Settings().SinglePortNodes)
			}
		})
	}
}

func TestPlaybackCompatibilityFailureRestoresSettings(t *testing.T) {
	for _, failSave := range []bool{false, true} {
		t.Run(map[bool]string{false: "node", true: "config"}[failSave], func(t *testing.T) {
			policy, source, _ := playbackPolicyFixture(t)
			if failSave {
				source.saveErr = errors.New("disk full")
			}
			called := false
			changed, err := policy.WithNodeChange(&node.Node{RTPReceiveMode: "single"}, func() error { called = true; return errors.New("node failed") })
			require.Error(t, err)
			require.False(t, changed)
			require.Equal(t, !failSave, called)
			require.True(t, source.GetBool(gbconfig.FixedAddressEnabledConfigKey))
			require.True(t, source.GetBool(gbconfig.AutoOnDemandEnabledConfigKey))
		})
	}
}

func TestPlaybackCompatibilityDisabledNodesAndLegacyReconcile(t *testing.T) {
	policy, source, nodes := playbackPolicyFixture(t)
	nodes.nodes = []*node.Node{{ID: 2, Name: "停用", RTPReceiveMode: "single", AdminState: "disabled"}, {ID: 1, Name: "离线单端口", RTPReceiveMode: "single", State: node.StateOffline}}
	require.NoError(t, policy.Reconcile())
	require.False(t, source.GetBool(gbconfig.FixedAddressEnabledConfigKey))
	require.Len(t, policy.Settings().SinglePortNodes, 1)
	nodes.nodes[1].RTPReceiveMode = "multi"
	require.Empty(t, policy.Settings().SinglePortNodes)
	require.False(t, policy.Settings().FixedAddressEnabled)
}

func TestServiceConfigRejectsStaleFixedAddressEnable(t *testing.T) {
	policy, source, nodes := playbackPolicyFixture(t)
	nodes.nodes = []*node.Node{{ID: 7, Name: "门口媒体", RTPReceiveMode: "single"}}
	require.NoError(t, policy.Reconcile())
	controller := NewServiceConfigController()
	controller.SetPlaybackCompatibility(policy)
	router := newServiceConfigRouter(controller)
	config := controller.currentServiceConfig()
	require.Len(t, config.FixedAddressPlayback.SinglePortNodes, 1)
	config.FixedAddressPlayback.FixedAddressEnabled = true
	config.FixedAddressPlayback.SinglePortNodes = nil
	before := source.saveNum
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/service-config", jsonBody(t, config)))
	require.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), "门口媒体")
	require.Equal(t, before, source.saveNum)
	require.False(t, source.GetBool(gbconfig.FixedAddressEnabledConfigKey))
	config = controller.currentServiceConfig()
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/service-config", jsonBody(t, config)))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), "门口媒体")
	require.Equal(t, before+1, source.saveNum)
}

func TestPlaybackCompatibilityStartupPersistenceFailureStillUsesDynamicPlayback(t *testing.T) {
	policy, source, nodes := playbackPolicyFixture(t)
	nodes.nodes = []*node.Node{{ID: 1, Name: "单端口", RTPReceiveMode: "single"}}
	source.saveErr = errors.New("read-only filesystem")
	require.Error(t, policy.Reconcile())
	require.False(t, gbconfig.CurrentFixedAddressPlaybackSettings().FixedAddressEnabled)
	require.False(t, gbconfig.CurrentFixedAddressPlaybackSettings().AutoOnDemandEnabled)
}

func TestPlaybackCompatibilityRollbackFailureKeepsDynamicPlayback(t *testing.T) {
	for _, persistedSingle := range []bool{false, true} {
		t.Run(map[bool]string{false: "config rollback failed", true: "node rollback failed"}[persistedSingle], func(t *testing.T) {
			policy, source, nodes := playbackPolicyFixture(t)
			candidate := &node.Node{ID: 1, Name: "入口", RTPReceiveMode: "single"}
			_, err := policy.WithNodeChange(candidate, func() error {
				if persistedSingle {
					nodes.nodes = []*node.Node{candidate}
				} else {
					source.saveErr = errors.New("disk full")
				}
				return errors.New("node save failed")
			})
			require.ErrorContains(t, err, "保持关闭")
			require.False(t, gbconfig.CurrentFixedAddressPlaybackSettings().FixedAddressEnabled)
			require.False(t, gbconfig.CurrentFixedAddressPlaybackSettings().AutoOnDemandEnabled)
		})
	}
}
