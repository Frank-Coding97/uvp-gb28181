package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/service"
)

type targetChangingConfigClient struct {
	setStarted chan struct{}
	releaseSet chan struct{}
	once       sync.Once
}

type revisionOnlyConfigRepo struct {
	mu   sync.Mutex
	row  node.Node
	next int64
}

func (r *revisionOnlyConfigRepo) List(context.Context) ([]node.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return []node.Node{r.row}, nil
}

func (r *revisionOnlyConfigRepo) Get(_ context.Context, id int64) (*node.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.row.ID != id {
		return nil, nil
	}
	copy := r.row
	return &copy, nil
}

func (r *revisionOnlyConfigRepo) Create(_ context.Context, n node.Node) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next++
	n.ID = r.next
	r.row = n
	return n.ID, nil
}

func (r *revisionOnlyConfigRepo) Update(_ context.Context, n node.Node) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.row = n
	return nil
}

func (r *revisionOnlyConfigRepo) UpdateCAS(_ context.Context, n node.Node, expectedRevision uint64) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.row.Revision != expectedRevision {
		return false, nil
	}
	n.Revision = expectedRevision + 1
	r.row = n
	return true, nil
}

func (r *revisionOnlyConfigRepo) Delete(context.Context, int64) error { return nil }

func (r *revisionOnlyConfigRepo) advanceRevisionWithoutChangingTarget() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.row.Revision++
}

func (c *targetChangingConfigClient) SetServerConfig(ctx context.Context, _ *node.Node, _ map[string]string) error {
	c.once.Do(func() { close(c.setStarted) })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.releaseSet:
		return nil
	}
}

func (c *targetChangingConfigClient) GetServerConfig(context.Context, *node.Node) (map[string]string, error) {
	return map[string]string{"hook.timeoutSec": "12"}, nil
}

type t13ConfigClient struct {
	reads  []map[string]string
	getErr error
	sets   []map[string]string
}

func (c *t13ConfigClient) GetServerConfig(context.Context, *node.Node) (map[string]string, error) {
	if c.getErr != nil {
		return nil, c.getErr
	}
	if len(c.reads) == 0 {
		return map[string]string{}, nil
	}
	read := c.reads[0]
	c.reads = c.reads[1:]
	return read, nil
}

func (c *t13ConfigClient) SetServerConfig(_ context.Context, _ *node.Node, params map[string]string) error {
	copied := make(map[string]string, len(params))
	for key, value := range params {
		copied[key] = value
	}
	c.sets = append(c.sets, copied)
	return nil
}

func TestConfigServiceT13_ExposesAuthoritativeModes(t *testing.T) {
	client := &t13ConfigClient{}
	reg := fakeRegistry(t, node.Node{Name: "n1", Host: "zlm", APIPort: 18080, APISecret: "s", State: node.StateActive})
	svc := service.NewConfigService(reg, client)
	id := reg.List()[0].ID
	groups, err := svc.GetGrouped(context.Background(), id)
	require.NoError(t, err)
	modes := map[string]service.ConfigMode{}
	for _, group := range groups {
		for _, item := range group.Items {
			modes[item.Key] = item.Mode
		}
	}
	require.Equal(t, service.ConfigModePlatformManaged, modes["hook.enable"])
	require.Equal(t, service.ConfigModeHotReload, modes["hook.timeoutSec"])
	require.Equal(t, service.ConfigModeRestartRequiredUnsupported, modes["http.port"])
	require.Equal(t, service.ConfigModeReadOnly, modes["api.version"])
}

func TestConfigServiceT13_ValidationOrderAndNoPartialSet(t *testing.T) {
	client := &t13ConfigClient{}
	reg := fakeRegistry(t, node.Node{Name: "n1", Host: "zlm", APIPort: 18080, APISecret: "s", State: node.StateActive})
	svc := service.NewConfigService(reg, client)
	id := reg.List()[0].ID

	_, err := svc.Update(context.Background(), id, service.UpdateConfigReq{Changes: map[string]string{
		"hook.timeoutSec": "12", "http.port": "8080",
	}})
	require.ErrorIs(t, err, service.ErrRestartRequiredUnsupported)
	require.Empty(t, client.sets, "mixed hot/restart batch must not partially Set")

	_, err = svc.Update(context.Background(), id, service.UpdateConfigReq{Changes: map[string]string{"api.version": "tamper"}})
	require.ErrorIs(t, err, service.ErrReadOnlyConfig)

	_, err = svc.Update(context.Background(), id, service.UpdateConfigReq{Changes: map[string]string{"hook.enable": "1"}})
	require.ErrorIs(t, err, service.ErrManagedConfigKey)
}

func TestConfigServiceT13_HotReloadAppliedOnlyAfterExactReadback(t *testing.T) {
	reg := fakeRegistry(t, node.Node{Name: "n1", Host: "zlm", APIPort: 18080, APISecret: "s", State: node.StateActive})
	id := reg.List()[0].ID

	mismatch := &t13ConfigClient{reads: []map[string]string{{"hook.timeoutSec": "11"}}}
	_, err := service.NewConfigService(reg, mismatch).Update(context.Background(), id, service.UpdateConfigReq{Changes: map[string]string{"hook.timeoutSec": "12"}})
	require.ErrorIs(t, err, service.ErrConfigReadbackMismatch)
	require.NotEmpty(t, mismatch.sets)

	fail := &t13ConfigClient{getErr: errors.New("readback failed with s")}
	_, err = service.NewConfigService(reg, fail).Update(context.Background(), id, service.UpdateConfigReq{Changes: map[string]string{"hook.timeoutSec": "12"}})
	require.ErrorIs(t, err, service.ErrConfigReadbackFailed)
	require.NotContains(t, err.Error(), "readback failed with s")

	okClient := &t13ConfigClient{reads: []map[string]string{{"hook.timeoutSec": "12"}}}
	resp, err := service.NewConfigService(reg, okClient).Update(context.Background(), id, service.UpdateConfigReq{Changes: map[string]string{"hook.timeoutSec": "12"}})
	require.NoError(t, err)
	require.Equal(t, []string{"hook.timeoutSec"}, resp.Applied)
}

func TestConfigServiceT13_TargetChangeCannotReturnApplied(t *testing.T) {
	reg := fakeRegistry(t, node.Node{
		Name: "n1", Host: "old-zlm", APIPort: 18080, APISecret: "old-secret",
		MediaServerUUID: "uuid-a", State: node.StateActive,
	})
	id := reg.List()[0].ID
	client := &targetChangingConfigClient{setStarted: make(chan struct{}), releaseSet: make(chan struct{})}
	svc := service.NewConfigService(reg, client)

	result := make(chan error, 1)
	go func() {
		_, err := svc.Update(context.Background(), id, service.UpdateConfigReq{
			Changes: map[string]string{"hook.timeoutSec": "12"},
		})
		result <- err
	}()

	<-client.setStarted
	current, ok := reg.Get(id)
	require.True(t, ok)
	current.Host = "new-zlm"
	current.APIPort = 28080
	current.APISecret = "new-secret"
	require.NoError(t, reg.Update(context.Background(), *current))
	close(client.releaseSet)

	err := <-result
	require.ErrorIs(t, err, service.ErrConfigTargetChanged)
	require.NotContains(t, err.Error(), "old-secret")
	require.NotContains(t, err.Error(), "new-secret")
}

func TestConfigServiceT13_RevisionOnlyTargetChangeCannotReturnApplied(t *testing.T) {
	repo := &revisionOnlyConfigRepo{}
	reg := node.NewRegistry(repo)
	added, err := reg.Add(context.Background(), node.Node{
		Name: "n1", Host: "zlm", APIPort: 18080, APISecret: "secret",
		MediaServerUUID: "uuid-a", State: node.StateActive,
	})
	require.NoError(t, err)

	client := &targetChangingConfigClient{setStarted: make(chan struct{}), releaseSet: make(chan struct{})}
	svc := service.NewConfigService(reg, client)
	result := make(chan error, 1)
	go func() {
		_, updateErr := svc.Update(context.Background(), added.ID, service.UpdateConfigReq{
			Changes: map[string]string{"hook.timeoutSec": "12"},
		})
		result <- updateErr
	}()

	<-client.setStarted
	repo.advanceRevisionWithoutChangingTarget()
	require.NoError(t, reg.LoadAll(context.Background()))
	close(client.releaseSet)

	err = <-result
	require.ErrorIs(t, err, service.ErrConfigTargetChanged)
	require.NotContains(t, err.Error(), "secret")
}
