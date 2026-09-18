package media

import (
	"context"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playback"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/openapi/config"
)

// ResolveRTP reuses the same pinned TLS probe and durable runtime confirmation
// as viewer revocation. The playback adapter must still match the persisted
// resource's original node revision and boot; this is not playback qualification.
func (f *TrustedRevocationFactory) ResolveRTP(ctx context.Context, uuid string) (playback.IntentRTPRuntime, error) {
	runtime, err := f.Resolve(ctx, uuid)
	if err != nil {
		return playback.IntentRTPRuntime{}, err
	}
	control, ok := runtime.Control.(*zlm.OpenAPIRuntimeControl)
	if !ok || control == nil || !runtime.Trusted || runtime.Release == nil || runtime.CurrentBootNonce == "" {
		if runtime.Release != nil {
			runtime.Release()
		} else if control != nil {
			control.Close()
		}
		return playback.IntentRTPRuntime{}, config.ErrNodeRuntimeUnavailable
	}
	return playback.IntentRTPRuntime{Control: control, BootNonce: runtime.CurrentBootNonce, Release: runtime.Release}, nil
}
