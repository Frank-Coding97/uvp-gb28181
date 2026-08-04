package controllers

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	gbsetup "uvplatform.cn/uvp-gb28181/app/gb28181/setup"
)

type platformInterfaceProvider struct {
	interfaces []net.Interface
	addresses  map[int][]net.Addr
}

func (p platformInterfaceProvider) Interfaces() ([]net.Interface, error) { return p.interfaces, nil }
func (p platformInterfaceProvider) Addrs(iface net.Interface) ([]net.Addr, error) {
	return p.addresses[iface.Index], nil
}

func TestPlatformController_UsesPersistedAdvertiseAddress(t *testing.T) {
	db := newSetupControllerDB(t)
	password := "Sec12345Aa!!"
	_, err := gbsetup.NewSIPConfigService(db).Save(t.Context(), gbsetup.SaveSIPConfigRequest{
		DeploymentMode: gbsetup.DeploymentLAN, ListenIP: "0.0.0.0", AdvertiseIP: "192.168.1.10",
		Port: 5061, Domain: "3402000000", ServerID: "34020000002000000001", Password: &password,
	})
	require.NoError(t, err)
	runtime := gbsetup.NewRuntimeStatus()
	runtime.MarkRunning()
	controller := NewConfiguredPlatformController(db, runtime, true, []string{"udp", "tcp"})
	controller.networkProvider = platformInterfaceProvider{
		interfaces: []net.Interface{
			{Index: 1, Name: "en0", Flags: net.FlagUp},
			{Index: 2, Name: "utun4", Flags: net.FlagUp},
		},
		addresses: map[int][]net.Addr{
			1: {&net.IPNet{IP: net.ParseIP("192.168.126.126"), Mask: net.CIDRMask(24, 32)}},
			2: {&net.IPNet{IP: net.ParseIP("10.8.0.3"), Mask: net.CIDRMask(32, 32)}},
		},
	}
	router := gin.New()
	router.GET("/platform", controller.Info)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/platform", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotContains(t, recorder.Body.String(), password)
	var response struct {
		Data PlatformInfo `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "0.0.0.0", response.Data.ListenIP)
	require.Equal(t, "192.168.1.10", response.Data.AdvertiseIP)
	require.Equal(t, "192.168.126.126", response.Data.SIPIP)
	require.Equal(t, []string{"192.168.126.126", "10.8.0.3"}, response.Data.SIPIPs)
	require.Equal(t, "sip:34020000002000000001@192.168.126.126:5061", response.Data.RegisterURI)
	require.Equal(t, "configured", response.Data.ConfigStatus)
}

func TestPlatformController_MissingConfigIsSuccessful(t *testing.T) {
	db := newSetupControllerDB(t)
	controller := NewConfiguredPlatformController(db, gbsetup.NewRuntimeStatus(), true, nil)
	router := gin.New()
	router.GET("/platform", controller.Info)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/platform", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"configStatus":"unconfigured"`)
}
