package devicesessions

import (
	"context"
	"github.com/google/uuid"
	"uvplatform.com/uvp-gb28181/app/deviceaccess"
)

// RequestUploadURL uses the same trusted request ingress as console captures.
func RequestUploadURL(ctx context.Context, token string) (string, error) {
	base, err := deviceaccess.FromContext(ctx)
	if err != nil {
		return "", err
	}
	parsed, err := uuid.Parse(token)
	if err != nil || parsed.String() != token {
		return "", ErrInvalidRequest
	}
	return base + "/api/gb28181/device-snapshots/uploads/" + token + "/", nil
}
