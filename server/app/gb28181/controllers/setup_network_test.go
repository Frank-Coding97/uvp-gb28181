package controllers

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	gbsetup "uvplatform.cn/uvp-gb28181/app/gb28181/setup"
)

type controllerInterfaceProvider struct {
	interfaces []net.Interface
	addresses  map[int][]net.Addr
	err        error
	calls      int
}

func (p *controllerInterfaceProvider) Interfaces() ([]net.Interface, error) {
	p.calls++
	return p.interfaces, p.err
}

func (p *controllerInterfaceProvider) Addrs(iface net.Interface) ([]net.Addr, error) {
	return p.addresses[iface.Index], nil
}

func TestSetupController_NetworkInterfacesStableResponse(t *testing.T) {
	en0 := &net.IPNet{IP: net.ParseIP("192.168.1.20"), Mask: net.CIDRMask(24, 32)}
	docker := &net.IPNet{IP: net.ParseIP("172.17.0.1"), Mask: net.CIDRMask(16, 32)}
	provider := &controllerInterfaceProvider{
		interfaces: []net.Interface{
			{Index: 2, Name: "docker0", Flags: net.FlagUp},
			{Index: 1, Name: "en0", Flags: net.FlagUp},
		},
		addresses: map[int][]net.Addr{1: {en0}, 2: {docker}},
	}
	controller := NewSetupController(newSetupControllerDB(t), gbsetup.NewRuntimeStatus(), provider)
	router := newSetupControllerRouter(controller)
	router.GET("/network", controller.NetworkInterfaces)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/network", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	data := decodeSetupResponse(t, recorder)["data"].(map[string]any)
	require.Equal(t, "ok", data["scanStatus"])
	items := data["items"].([]any)
	require.Equal(t, "0.0.0.0", items[0].(map[string]any)["ip"])
	require.Equal(t, "192.168.1.20", items[1].(map[string]any)["ip"])
	require.Equal(t, float64(1), float64(provider.calls))
}

func TestSetupController_NetworkInterfacesFailureStillReturnsWildcard(t *testing.T) {
	provider := &controllerInterfaceProvider{err: errors.New("secret operating system detail")}
	controller := NewSetupController(newSetupControllerDB(t), gbsetup.NewRuntimeStatus(), provider)
	router := newSetupControllerRouter(controller)
	router.GET("/network", controller.NetworkInterfaces)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/network", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "secret operating system detail")
	data := decodeSetupResponse(t, recorder)["data"].(map[string]any)
	require.Equal(t, "failed", data["scanStatus"])
	items := data["items"].([]any)
	require.Len(t, items, 1)
	require.Equal(t, "0.0.0.0", items[0].(map[string]any)["ip"])
}
