package controllers

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbsetup "uvplatform.cn/uvp-gb28181/app/gb28181/setup"
)

func TestSetupController_SaveConfigUsesInjectedSaverBeforeReload(t *testing.T) {
	db := newSetupControllerDB(t)
	runtime := gbsetup.NewRuntimeStatus()
	var received gbsetup.SaveSIPConfigRequest
	var events []string
	password := "Sec12345Aa!!"
	saver := func(ctx context.Context, request gbsetup.SaveSIPConfigRequest) (gbsetup.SIPConfigView, error) {
		require.NotNil(t, ctx)
		received = request
		events = append(events, "save")
		return gbsetup.SIPConfigView{
			DeploymentMode:      request.DeploymentMode,
			ListenIP:            request.ListenIP,
			AdvertiseIP:         request.AdvertiseIP,
			AdvertiseIPInferred: request.AdvertiseIPInferred,
			Port:                request.Port,
			Domain:              request.Domain,
			ServerID:            request.ServerID,
			Password:            password,
			HasPassword:         true,
		}, nil
	}
	reloadCalls := 0
	reload := func() error {
		reloadCalls++
		events = append(events, "reload")
		runtime.MarkRunning()
		return nil
	}
	controller := NewSetupController(db, runtime, nil, reload)
	controller.SetConfigSaver(saver)
	router := newSetupControllerRouter(controller)

	body := `{"deploymentMode":"lan","listenIp":"0.0.0.0","advertiseIp":"192.168.1.10","advertiseIpInferred":true,"port":5061,"domain":"3402000000","serverId":"34020000002000000001","password":"Sec12345Aa!!"}`
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/config", bytes.NewBufferString(body)))

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, 1, reloadCalls)
	require.Equal(t, []string{"save", "reload"}, events)
	require.Equal(t, gbsetup.DeploymentLAN, received.DeploymentMode)
	require.Equal(t, "0.0.0.0", received.ListenIP)
	require.Equal(t, "192.168.1.10", received.AdvertiseIP)
	require.True(t, received.AdvertiseIPInferred)
	require.Equal(t, 5061, received.Port)
	require.Equal(t, "3402000000", received.Domain)
	require.Equal(t, "34020000002000000001", received.ServerID)
	require.NotNil(t, received.Password)
	require.Equal(t, password, *received.Password)
	var row gbsetup.SIPConfig
	require.ErrorIs(t, db.First(&row, gbsetup.SingletonID).Error, gorm.ErrRecordNotFound)
	require.Contains(t, recorder.Body.String(), `"reloadedOk":true`)
}

func TestSetupController_SaveConfigInjectedSaverValidationFailureSkipsReload(t *testing.T) {
	db := newSetupControllerDB(t)
	runtime := gbsetup.NewRuntimeStatus()
	saverCalls := 0
	reloadCalls := 0
	validationErr := &gbsetup.ValidationError{Fields: map[string]string{"port": "must be available"}}
	controller := NewSetupController(db, runtime, nil, func() error {
		reloadCalls++
		return errors.New("reload must not run")
	})
	controller.SetConfigSaver(func(context.Context, gbsetup.SaveSIPConfigRequest) (gbsetup.SIPConfigView, error) {
		saverCalls++
		return gbsetup.SIPConfigView{}, validationErr
	})
	router := newSetupControllerRouter(controller)

	body := `{"deploymentMode":"lan","listenIp":"0.0.0.0","advertiseIp":"192.168.1.10","port":5061,"domain":"3402000000","serverId":"34020000002000000001","password":"Sec12345Aa!!"}`
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/config", bytes.NewBufferString(body)))

	require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	require.Equal(t, 1, saverCalls)
	require.Equal(t, 0, reloadCalls)
	require.Contains(t, recorder.Body.String(), "SIP 配置校验失败")
	require.Contains(t, recorder.Body.String(), "port")
	var row gbsetup.SIPConfig
	require.ErrorIs(t, db.First(&row, gbsetup.SingletonID).Error, gorm.ErrRecordNotFound)
}
