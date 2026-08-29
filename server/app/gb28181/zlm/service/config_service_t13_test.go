package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/service"
)

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
