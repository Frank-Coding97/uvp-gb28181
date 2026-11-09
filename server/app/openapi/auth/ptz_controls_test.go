package auth

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPTZControlRoutesHaveIndependentScopesAndExactPaths(t *testing.T) {
	const deviceID = "34020000002000000010"
	const channelID = "37011200001310000010"
	tests := []struct {
		scope string
		path  string
		body  string
	}{
		{PTZMoveScope, "/openapi/v1/ptz/control", `{"action":"stop","speed":0}`},
		{PTZPreciseScope, "/openapi/v1/ptz/position/set", `{"pan":1}`},
		{PTZExtendedScope, "/openapi/v1/ptz/extended/control", `{"action":"scan_stop","id":0}`},
		{PTZWiperScope, "/openapi/v1/ptz/wiper/set", `{"action":"off"}`},
		{PTZHomePositionSetScope, "/openapi/v1/ptz/home/set", `{"enabled":false}`},
		{PTZCruiseControlScope, "/openapi/v1/ptz/cruise/control", `{"action":"stop","trackId":0}`},
		{"ptz:cruise:track:create", "/openapi/v1/ptz/cruise/save", `{"trackId":0,"stops":[{"presetId":1}]}`},
		{PTZTargetTrackScope, "/openapi/v1/ptz/track/set", `{"mode":"auto"}`},
	}
	for _, test := range tests {
		t.Run(test.scope, func(t *testing.T) {
			request := gatewayRequest{
				method: http.MethodPost, path: test.path, rawURI: test.path, pattern: ptzPattern(test.scope),
				scope: test.scope, deviceID: deviceID, channelID: channelID,
				idempotencyKey: "request-1", body: []byte(fmt.Sprintf(`{"deviceId":%q,"channelId":%q,`, deviceID, channelID) + test.body[1:]),
			}
			require.NoError(t, bindActionRequest(&request))
			require.True(t, isPTZRequestScope(test.scope))
			require.Equal(t, http.MethodPost, ptzMethod(test.scope))
			require.True(t, ptzRouteMatches(request))
			require.NoError(t, validatePTZRequest(request))

			request.method = http.MethodPut
			require.False(t, ptzRouteMatches(request))
			request.method = http.MethodPost
			request.rawQuery = "debug=1"
			request.rawURI += "?debug=1"
			request.bound = false
			require.Error(t, bindActionRequest(&request))
		})
	}
}

func TestPTZCruiseTrackReadRouteValidatesTrackIDAndHasNoWriteRequirements(t *testing.T) {
	const deviceID = "34020000002000000010"
	const channelID = "37011200001310000010"
	path := "/openapi/v1/ptz/cruise/info"
	query := "deviceId=" + deviceID + "&channelId=" + channelID + "&trackId=0"
	request := gatewayRequest{
		method: http.MethodGet, path: path, rawURI: path + "?" + query, rawQuery: query,
		pattern: ptzPattern("ptz:cruise:track:read"), scope: "ptz:cruise:track:read",
		deviceID: deviceID, channelID: channelID, trackID: "0",
	}
	require.True(t, isPTZRequestScope(request.scope))
	require.Equal(t, http.MethodGet, ptzMethod(request.scope))
	require.True(t, ptzRouteMatches(request))
	require.NoError(t, validatePTZRequest(request), "reading a cached track does not require a body or idempotency key")

	request.trackID = "256"
	request.rawQuery = "deviceId=" + deviceID + "&channelId=" + channelID + "&trackId=256"
	request.rawURI = request.path + "?" + request.rawQuery
	require.ErrorIs(t, validatePTZRequest(request), ErrPTZInvalid)
	request.trackID = "0"
	request.method = http.MethodPost
	require.False(t, ptzRouteMatches(request))
}

func TestPTZControlRoutesRejectDuplicateJSONAndMissingIdempotencyKey(t *testing.T) {
	request := gatewayRequest{
		method: http.MethodPost, path: "/openapi/v1/ptz/wiper/set",
		rawURI:  "/openapi/v1/ptz/wiper/set",
		pattern: ptzPattern(PTZWiperScope), scope: PTZWiperScope,
		deviceID: "34020000002000000010", channelID: "37011200001310000010",
		idempotencyKey: "wiper-1", body: []byte(`{"action":"on","action":"off"}`),
	}
	require.ErrorIs(t, validatePTZRequest(request), ErrPTZInvalid)
	request.body = []byte(`{"action":"on"}`)
	request.idempotencyKey = ""
	require.ErrorIs(t, validatePTZRequest(request), ErrPTZInvalid)
}

func TestPTZUnsupportedCapabilityHasStablePublicError(t *testing.T) {
	response := ptzMetadataFailure("request-1", ErrPTZUnsupported)
	require.Equal(t, http.StatusUnprocessableEntity, response.status)
	require.Equal(t, "CAPABILITY_NOT_SUPPORTED", response.code)
}
