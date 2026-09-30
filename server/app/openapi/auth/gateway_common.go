package auth

import (
	"net/http"
	"strconv"
)

type GatewayOption func(*Gateway)

func WithPTZDispatcher(dispatcher PTZDispatcher) GatewayOption {
	return func(gateway *Gateway) {
		if gateway != nil {
			gateway.ptz = dispatcher
		}
	}
}

func WithPlayDispatcher(dispatcher PlayDispatcher) GatewayOption {
	return func(gateway *Gateway) {
		if gateway != nil {
			gateway.play = dispatcher
		}
	}
}

func unwrapResponseWriter(writer http.ResponseWriter) http.ResponseWriter {
	for i := 0; i < 4; i++ {
		unwrapper, ok := writer.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			break
		}
		unwrapped := unwrapper.Unwrap()
		if unwrapped == nil || unwrapped == writer {
			break
		}
		writer = unwrapped
	}
	return writer
}

func captureGatewayHeaders(r *http.Request) HeaderValues {
	if r == nil {
		return HeaderValues{}
	}
	return HeaderValues{
		SignVersion:     append([]string(nil), r.Header.Values("X-UVP-Sign-Version")...),
		AccessKey:       append([]string(nil), r.Header.Values("X-UVP-Access-Key")...),
		Timestamp:       append([]string(nil), r.Header.Values("X-UVP-Timestamp")...),
		Nonce:           append([]string(nil), r.Header.Values("X-UVP-Nonce")...),
		Signature:       append([]string(nil), r.Header.Values("X-UVP-Signature")...),
		ContentType:     append([]string(nil), r.Header.Values("Content-Type")...),
		ContentEncoding: append([]string(nil), r.Header.Values("Content-Encoding")...),
		MethodOverride:  append(append(append([]string(nil), r.Header.Values("X-HTTP-Method-Override")...), r.Header.Values("X-HTTP-Method")...), r.Header.Values("X-Method-Override")...),
		IdempotencyKey:  append([]string(nil), r.Header.Values("Idempotency-Key")...),
	}
}

func parseUnixTimestamp(value string) (int64, error) { return strconv.ParseInt(value, 10, 64) }
