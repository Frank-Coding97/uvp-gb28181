package controllers

import (
	"strings"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

// profileForDevice resolves only the persisted effective version. Missing or
// malformed rows intentionally use the 2016 compatibility profile; capability
// hints never participate in this decision.
func profileForDevice(device *gbmodels.GbDevice) protocol.Profile {
	if device == nil || strings.TrimSpace(device.EffectiveVersion) != protocol.Version2022 {
		return protocol.ProfileFor(protocol.Version2016)
	}
	return protocol.ProfileFor(protocol.Version2022)
}
