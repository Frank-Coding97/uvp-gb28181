package play

import (
	"errors"
	"fmt"
	"strings"
)

var ErrInvalidFixedStreamID = errors.New("非法固定流标识")

// FixedStreamID returns the stable media identity for one GB28181 channel.
func FixedStreamID(deviceID, channelID string) (string, error) {
	if !validGBID(deviceID) || !validGBID(channelID) {
		return "", fmt.Errorf("%w: deviceId/channelId 必须是20位十进制编码", ErrInvalidFixedStreamID)
	}
	return deviceID + "_" + channelID, nil
}

func ParseFixedStreamID(streamID string) (deviceID, channelID string, err error) {
	if strings.Count(streamID, "_") != 1 {
		return "", "", ErrInvalidFixedStreamID
	}
	parts := strings.SplitN(streamID, "_", 2)
	if !validGBID(parts[0]) || !validGBID(parts[1]) {
		return "", "", ErrInvalidFixedStreamID
	}
	return parts[0], parts[1], nil
}

func validGBID(value string) bool {
	if len(value) != 20 {
		return false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}
