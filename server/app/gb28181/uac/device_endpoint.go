package uac

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// SetDeviceEndpointLookup is configured before the UAC starts serving requests.
func (u *UAC) SetDeviceEndpointLookup(lookup func(context.Context, string) (*gbmodels.GbDevice, error)) {
	u.deviceEndpoint = lookup
}

func (u *UAC) liveDialogDestination(deviceID, original, transport string) func(context.Context, *sip.Request) (string, error) {
	originalIP, _, _ := net.SplitHostPort(original)
	return func(ctx context.Context, req *sip.Request) (string, error) {
		// ACK retransmissions retain the response route. Only a new BYE transaction
		// may follow a later validated heartbeat endpoint.
		if req.Method != sip.BYE {
			return "", nil
		}
		d, err := u.deviceEndpoint(ctx, deviceID)
		if err != nil {
			return "", fmt.Errorf("读取停播信令端点失败: %w", err)
		}
		if d == nil || d.DeviceID != deviceID || !strings.EqualFold(d.Transport, transport) ||
			d.RegisterExpireAt == nil || !d.RegisterExpireAt.After(time.Now()) ||
			d.Port < 1 || d.Port > 65535 {
			return "", nil
		}
		ip := net.ParseIP(d.IP)
		peerIP, _, splitErr := net.SplitHostPort(req.Destination())
		if ip == nil || splitErr != nil || !ip.Equal(net.ParseIP(originalIP)) || !ip.Equal(net.ParseIP(peerIP)) {
			return "", nil
		}
		return net.JoinHostPort(d.IP, strconv.Itoa(d.Port)), nil
	}
}
