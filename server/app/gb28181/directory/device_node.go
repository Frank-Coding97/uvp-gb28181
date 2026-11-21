package directory

import (
	"fmt"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

func deviceDirectoryNode(device gbmodels.GbDevice, key string, depth int) DirectoryNodeVO {
	name := device.Alias
	if name == "" {
		name = device.Name
	}
	if name == "" {
		name = device.DeviceID
	}
	return DirectoryNodeVO{
		Key:      key,
		Name:     name,
		Type:     "device",
		Code:     device.DeviceID,
		ReadOnly: true,
		Depth:    depth,
		Device: &DirectoryDeviceVO{
			ID:       device.ID,
			DeviceID: device.DeviceID,
			Name:     device.Name,
			Alias:    device.Alias,
			Online:   device.Status == gbmodels.DeviceStatusOnline,
		},
	}
}

func directoryDeviceKey(view string, parentID uint, deviceID uint) string {
	return fmt.Sprintf("%s:device:%d:%d", view, parentID, deviceID)
}
