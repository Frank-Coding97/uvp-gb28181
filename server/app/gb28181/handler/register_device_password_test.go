package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/device"
)

// newDevicePasswordRegisterHandler 构造一个把设备独立密码固定成 credential 的注册处理器。
// resolverErr 非 nil 时模拟「查库失败」。
func newDevicePasswordRegisterHandler(credential device.RegisterCredential, resolverErr error) *RegisterHandler {
	h := NewRegisterHandler(securityTestCfg())
	// 鉴权失败分支会重新发 401 挑战并取用 nonce，多备几个避免挑战本身失败(500)。
	h.SetSecurity(&fakeRegisterSecurity{nonces: []string{"dp-nonce-1", "dp-nonce-2", "dp-nonce-3", "dp-nonce-4"}})
	h.handleRegister = func(context.Context, device.RegisterInfo, int) (bool, error) { return false, nil }
	h.SetDevicePasswordResolver(func(context.Context, string) (device.RegisterCredential, error) {
		if resolverErr != nil {
			return device.RegisterCredential{}, resolverErr
		}
		return credential, nil
	})
	return h
}

func registerStatus(t *testing.T, h *RegisterHandler, nonce, password string) int {
	t.Helper()
	tx := &captureServerTransaction{}
	h.Handle(authorizedRegisterRequest(t, nonce, password), tx)
	return tx.response.StatusCode
}

// 设备配了独立密码 → 只认它：设备端配独立密码能注册，统一接入密码当场失效。
//
// 这是「一设备一密码」的核心语义。若哪天有人把这里改成「设备密码失败再回落统一密码」，
// 本用例会红：那样设备密码就失去了防顶替的作用。
func TestRegisterDevicePasswordIsExclusive(t *testing.T) {
	const devicePassword = "device-pass-9"
	h := newDevicePasswordRegisterHandler(device.RegisterCredential{
		Password: devicePassword, DeviceScoped: true, DeviceID: securityTestDeviceID,
	}, nil)

	require.Equal(t, sip.StatusOK, registerStatus(t, h, "device-pwd-nonce", devicePassword))
	require.Equal(t, sip.StatusUnauthorized, registerStatus(t, h, "unified-pwd-nonce", securityTestPassword))
}

// 设备没配独立密码（DeviceScoped=false）→ 回落平台统一接入密码。
func TestRegisterFallsBackToUnifiedPasswordWhenDeviceHasNone(t *testing.T) {
	h := newDevicePasswordRegisterHandler(device.RegisterCredential{DeviceID: securityTestDeviceID}, nil)

	require.Equal(t, sip.StatusOK, registerStatus(t, h, "fallback-nonce", securityTestPassword))
	require.Equal(t, sip.StatusUnauthorized, registerStatus(t, h, "fallback-wrong-nonce", "not-the-password"))
}

// 查库失败只能降级，不能把设备挡在门外：回落统一密码继续放行。
func TestRegisterFallsBackToUnifiedPasswordWhenLookupFails(t *testing.T) {
	h := newDevicePasswordRegisterHandler(device.RegisterCredential{}, errors.New("db down"))

	require.Equal(t, sip.StatusOK, registerStatus(t, h, "lookup-failed-nonce", securityTestPassword))
}
