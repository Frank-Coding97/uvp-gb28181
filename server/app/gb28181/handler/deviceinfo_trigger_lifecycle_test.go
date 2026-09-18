package handler

import (
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
)

type deviceInfoLifecycleOwner struct {
	accepted bool
	work     func()
}

func (o *deviceInfoLifecycleOwner) Go(fn func()) bool {
	if !o.accepted {
		return false
	}
	o.work = fn
	return true
}

func TestLoggingDeviceInfoTriggerUsesShutdownOwnerAdmission(t *testing.T) {
	owner := &deviceInfoLifecycleOwner{accepted: true}
	trigger := NewUACDeviceInfoTrigger(&uac.UAC{}, owner)

	trigger.Trigger(nil, "device-1", "127.0.0.1:5060", "UDP")

	require.NotNil(t, owner.work)
}

func TestLoggingDeviceInfoTriggerDropsAfterOwnerCloses(t *testing.T) {
	owner := &deviceInfoLifecycleOwner{}
	trigger := NewUACDeviceInfoTrigger(&uac.UAC{}, owner)

	trigger.Trigger(nil, "device-1", "127.0.0.1:5060", "UDP")

	require.Nil(t, owner.work)
}
