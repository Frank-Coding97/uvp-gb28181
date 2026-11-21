package devicesessions

import (
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
	"uvplatform.com/uvp-gb28181/app/deviceaccess"
)

func TestRequestUploadURLUsesIngressAndValidatesToken(t *testing.T) {
	token := uuid.NewString()
	ctx := deviceaccess.WithBaseURL(context.Background(), "http://camera.example:5177")
	got, err := RequestUploadURL(ctx, token)
	require.NoError(t, err)
	require.Equal(t, "http://camera.example:5177/api/gb28181/device-snapshots/uploads/"+token+"/", got)
	_, err = RequestUploadURL(context.Background(), token)
	require.Error(t, err)
	_, err = RequestUploadURL(ctx, "../../other")
	require.ErrorIs(t, err, ErrInvalidRequest)
	for _, base := range []string{"http://localhost:8280", "http://[::1]:8280", "http://0.0.0.0:8280", "https://user:pass@example.com", "https://example.com?x=y"} {
		_, err = RequestUploadURL(deviceaccess.WithBaseURL(context.Background(), base), token)
		require.Error(t, err)
	}
}

func TestCreateCaptureBuildsCommandFromRequestIngress(t *testing.T) {
	service, commands, _, _, _ := newCaptureTestService(t)
	service.uploadURL = RequestUploadURL
	require.True(t, service.Ready())
	ctx := deviceaccess.WithBaseURL(context.Background(), "http://camera.example:15500")
	_, err := service.CreateCapture(ctx, CaptureRequest{ClientID: 91, Scope: captureScope(), DeviceID: captureTestDevice, ChannelID: captureTestChannel, IdempotencyKey: "ingress-capture", SnapNum: 1, Interval: 1})
	require.NoError(t, err)
	require.Len(t, commands.commands, 1)
	body, err := commands.commands[0].Build(1)
	require.NoError(t, err)
	require.Contains(t, string(body), "http://camera.example:15500/api/gb28181/device-snapshots/uploads/")
}
