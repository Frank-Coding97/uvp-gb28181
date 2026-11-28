package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type originObserver struct{ calls int }

func (o *originObserver) Observe(context.Context, string, int64, string, string, string, string) error {
	o.calls++
	return nil
}
func TestBackendOriginHooksAlwaysDenyAndRequireAuthenticatedNode(t *testing.T) {
	for _, tc := range []struct {
		name, params string
		auth         bool
		calls        int
	}{
		{"authenticated", "uvp_origin_probe=proof", true, 1},
		{"unauthenticated", "uvp_origin_probe=proof", false, 0},
		{"duplicate", "uvp_origin_probe=one&uvp_origin_probe=two", true, 0},
		{"mixed-token", "uvp_origin_probe=proof&play_token=token", true, 0},
		{"empty", "uvp_origin_probe=", true, 0},
		{"malformed", "uvp_origin_probe=%zz", true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHookController(nil)
			observer := &originObserver{}
			h.SetBackendOriginObserver(observer)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			if tc.auth {
				c.Set(hookNodeContextKey, HookNodeIdentity{ID: 1, MediaServerUUID: "node"})
			}
			payload, _ := json.Marshal(onPlayBody{App: "rtp", Stream: "stream", MediaServerID: "node", IP: "203.0.113.42", Params: tc.params})
			c.Request = httptest.NewRequest(http.MethodPost, "/hook", strings.NewReader(string(payload)))
			c.Request.Header.Set("Content-Type", "application/json")
			h.OnPlay(c)
			var body map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			require.EqualValues(t, -1, body["code"])
			require.Equal(t, tc.calls, observer.calls)
		})
	}
}
