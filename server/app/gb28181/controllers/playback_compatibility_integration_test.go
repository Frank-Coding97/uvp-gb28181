package controllers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gbconfig "uvplatform.com/uvp-gb28181/app/gb28181/config"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/service"
)

type compatibilityRepo struct {
	rows map[int64]node.Node
	fail bool
}

func (r *compatibilityRepo) List(context.Context) ([]node.Node, error) {
	var out []node.Node
	for _, n := range r.rows {
		out = append(out, n)
	}
	return out, nil
}
func (r *compatibilityRepo) Get(_ context.Context, id int64) (*node.Node, error) {
	n, ok := r.rows[id]
	if !ok {
		return nil, nil
	}
	return &n, nil
}
func (r *compatibilityRepo) Create(_ context.Context, n node.Node) (int64, error) {
	if r.fail {
		return 0, errors.New("create failed")
	}
	n.ID = int64(len(r.rows) + 1)
	r.rows[n.ID] = n
	return n.ID, nil
}
func (r *compatibilityRepo) Update(_ context.Context, n node.Node) error {
	if r.fail {
		return errors.New("update failed")
	}
	r.rows[n.ID] = n
	return nil
}
func (r *compatibilityRepo) Delete(_ context.Context, id int64) error { delete(r.rows, id); return nil }

type compatibilityProbe struct {
	port      string
	failApply bool
}

func (p *compatibilityProbe) GetServerConfig(context.Context, *node.Node) (map[string]string, error) {
	return map[string]string{"rtp_proxy.port": p.port}, nil
}
func (p *compatibilityProbe) ApplyConfigForNode(context.Context, *node.Node, service.MediaTuning) error {
	if p.failApply {
		return errors.New("apply failed")
	}
	return nil
}
func (p *compatibilityProbe) KickSessions(context.Context, *node.Node) (int, error) { return 0, nil }
func (p *compatibilityProbe) RestartServer(context.Context, *node.Node, int) error  { return nil }

func compatibilityServiceFixture(t *testing.T) (*service.NodeService, *PlaybackCompatibility, *serviceConfigTestYAML, *compatibilityRepo, *compatibilityProbe) {
	_, source, _ := playbackPolicyFixture(t)
	repo := &compatibilityRepo{rows: map[int64]node.Node{}}
	reg := node.NewRegistry(repo)
	policy := NewPlaybackCompatibility(reg)
	probe := &compatibilityProbe{port: "10000"}
	svc := service.NewNodeService(reg, probe, service.MediaTuning{})
	svc.SetPlaybackCompatibility(policy)
	return svc, policy, source, repo, probe
}

func TestSinglePortNodeEntrypointsCloseFixedPlayback(t *testing.T) {
	for _, action := range []string{"create", "update", "enable", "activate"} {
		t.Run(action, func(t *testing.T) {
			svc, policy, source, _, _ := compatibilityServiceFixture(t)
			ctx := context.Background()
			req := service.CreateNodeReq{Host: "192.0.2.1", APIPort: 80, APISecret: "fixture"}
			mode := "single"
			if action == "create" {
				req.RTPReceiveMode = mode
				req.RTPProxyPort = 10000
			}
			n, err := svc.Create(ctx, req)
			require.NoError(t, err)
			switch action {
			case "create":
				require.True(t, n.FixedAddressPlaybackDisabled)
			case "update":
				n, err = svc.Update(ctx, n.ID, service.UpdateNodeReq{RTPReceiveMode: &mode})
				require.NoError(t, err)
				require.True(t, n.FixedAddressPlaybackDisabled)
			default:
				require.NoError(t, svc.Disable(ctx, n.ID))
				_, err = svc.Update(ctx, n.ID, service.UpdateNodeReq{RTPReceiveMode: &mode})
				require.NoError(t, err)
				require.True(t, source.GetBool(gbconfig.FixedAddressEnabledConfigKey))
				if action == "activate" {
					require.NoError(t, svc.Activate(ctx, n.ID))
				} else {
					changed, err := svc.EnableWithPlaybackResult(ctx, n.ID)
					require.NoError(t, err)
					require.True(t, changed)
				}
			}
			require.False(t, source.GetBool(gbconfig.FixedAddressEnabledConfigKey))
			require.False(t, source.GetBool(gbconfig.AutoOnDemandEnabledConfigKey))
			require.Len(t, policy.Settings().SinglePortNodes, 1)
			require.NoError(t, svc.Disable(ctx, n.ID))
			require.Empty(t, policy.Settings().SinglePortNodes)
			require.False(t, source.GetBool(gbconfig.FixedAddressEnabledConfigKey))
		})
	}
}

func TestSinglePortNodeFailedSavesDoNotChangePlaybackPolicy(t *testing.T) {
	for _, failure := range []string{"listener", "disk", "database", "convergence"} {
		t.Run(failure, func(t *testing.T) {
			svc, _, source, repo, probe := compatibilityServiceFixture(t)
			ctx := context.Background()
			n, err := svc.Create(ctx, service.CreateNodeReq{Host: "192.0.2.1", APIPort: 80, APISecret: "fixture"})
			require.NoError(t, err)
			switch failure {
			case "listener":
				probe.port = "20000"
			case "disk":
				source.saveErr = errors.New("disk full")
			case "database":
				repo.fail = true
			case "convergence":
				// Updating a connection invalidates readiness and exercises external rollback.
				probe.failApply = true
			}
			mode := "single"
			req := service.UpdateNodeReq{RTPReceiveMode: &mode}
			if failure == "convergence" {
				host := "192.0.2.2"
				req.Host = &host
			}
			_, err = svc.Update(ctx, n.ID, req)
			require.Error(t, err)
			require.Equal(t, "multi", repo.rows[n.ID].EffectiveRTPReceiveMode())
			require.True(t, source.GetBool(gbconfig.FixedAddressEnabledConfigKey))
			require.True(t, source.GetBool(gbconfig.AutoOnDemandEnabledConfigKey))
		})
	}
}

func TestSinglePortSwitchSerializesWithStaleServiceConfigSave(t *testing.T) {
	svc, policy, source, repo, _ := compatibilityServiceFixture(t)
	controller := NewServiceConfigController()
	controller.SetPlaybackCompatibility(policy)
	router := newServiceConfigRouter(controller)
	stale := controller.currentServiceConfig()
	payload := jsonBody(t, stale)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	source.setHook = func(key string, value interface{}) {
		if key == gbconfig.FixedAddressEnabledConfigKey && value == false {
			once.Do(func() { close(entered); <-release })
		}
	}
	createDone := make(chan error, 1)
	go func() {
		_, err := svc.Create(context.Background(), service.CreateNodeReq{Host: "192.0.2.1", APIPort: 80, APISecret: "fixture", RTPReceiveMode: "single", RTPProxyPort: 10000})
		createDone <- err
	}()
	select {
	case <-entered:
	case err := <-createDone:
		t.Fatalf("node change failed before policy save: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("node change did not reach policy save")
	}
	recorder := httptest.NewRecorder()
	saveDone := make(chan struct{})
	go func() {
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/service-config", payload))
		close(saveDone)
	}()
	select {
	case <-saveDone:
		t.Fatal("configuration save bypassed node change gate")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	require.NoError(t, <-createDone)
	select {
	case <-saveDone:
	case <-time.After(3 * time.Second):
		t.Fatal("configuration save deadlocked")
	}
	require.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
	require.False(t, source.GetBool(gbconfig.FixedAddressEnabledConfigKey))
	require.Equal(t, "single", repo.rows[1].EffectiveRTPReceiveMode())
}
