package devicesessions

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestNewUploadURLBuilderUsesConfiguredDeviceReachableBase(t *testing.T) {
	builder, err := NewUploadURLBuilder("https://capture.example.com/uvp/")
	require.NoError(t, err)

	token := uuid.NewString()
	got, err := builder(context.Background(), token)
	require.NoError(t, err)
	require.Equal(t, "https://capture.example.com/uvp/api/gb28181/device-snapshots/uploads/"+token+"/", got)
}

func TestNewUploadURLBuilderRejectsUnsafeOrUnreachableBases(t *testing.T) {
	for _, raw := range []string{
		"",
		"file:///tmp/captures",
		"http:///captures",
		"http://localhost:8080",
		"http://127.0.0.1:8080",
		"http://0.0.0.0:8080",
		"http://[::1]:8080",
		"https://capture.example.com?forwarded=attacker.example",
		"https://user:secret@capture.example.com",
	} {
		t.Run(raw, func(t *testing.T) {
			_, err := NewUploadURLBuilder(raw)
			require.ErrorIs(t, err, ErrInvalidUploadBaseURL)
		})
	}
}

func TestUploadURLBuilderRejectsNonUUIDUploadTokens(t *testing.T) {
	builder, err := NewUploadURLBuilder("https://capture.example.com")
	require.NoError(t, err)
	_, err = builder(context.Background(), "../../other/path?x=y")
	require.ErrorIs(t, err, ErrInvalidRequest)
}
