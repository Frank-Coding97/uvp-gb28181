package zlm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAPIRuntimeSnapshotsAreFreshAndBound(t *testing.T) {
	boot := strings.Repeat("a", 32)
	c, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "GET", r.Method)
		require.Equal(t, "test-secret", r.Header.Get("secret"))
		require.Empty(t, r.URL.Query().Get("secret"))
		require.Equal(t, "no-store", r.Header.Get("Cache-Control"))
		w.Header().Set("Cache-Control", "no-store")
		if strings.HasSuffix(r.URL.Path, "getMediaPlayerList") {
			require.Equal(t, "a&b", r.URL.Query().Get("stream"))
			require.Len(t, r.URL.Query(), 4)
			fmt.Fprintf(w, `{"code":0,"bootNonce":%q,"data":[{"identifier":"12-9","typeid":"HttpSession"}]}`, boot)
		} else {
			require.Equal(t, "/index/api/getAllSession", r.URL.Path)
			require.Empty(t, r.URL.RawQuery)
			fmt.Fprintf(w, `{"code":0,"bootNonce":%q,"data":[{"id":"12-9","identifier":"12-9","type":"tcp"}]}`, boot)
		}
	})
	defer server.Close()
	players, err := c.GetRuntimeMediaPlayers(context.Background(), StreamTarget{"rtmp", "__defaultVhost__", "live", "a&b"})
	require.NoError(t, err)
	require.Equal(t, boot, players.BootNonce)
	require.Equal(t, "12-9", players.Players[0].Identifier)
	sessions, err := c.GetRuntimeSessions(context.Background())
	require.NoError(t, err)
	require.Equal(t, boot, sessions.BootNonce)
	require.Equal(t, "12-9", sessions.Sessions[0].ID)
}

func TestOpenAPIRuntimeSnapshotsRejectAmbiguousIdentity(t *testing.T) {
	boot := `"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`
	for _, body := range []string{
		`{"code":0,"data":[]}`,
		`{"code":0,"bootNonce":null,"data":[]}`,
		`{"code":0,"bootNonce":` + boot + `,"data":null}`,
		`{"code":0,"bootNonce":` + boot + `,"data":[{"identifier":""}]}`,
		`{"code":0,"bootNonce":` + boot + `,"data":[{"identifier":"12-9","identifier":"13-9"}]}`,
		`{"code":0,"bootNonce":` + boot + `,"data":[{"identifier":"12-9"},{"identifier":"12-9"}]}`,
		`{"code":0,"bootNonce":` + boot + `,"bootNonce":` + boot + `,"data":[]}`,
		`{"code":0,"bootNonce":` + boot + `,"data":[{"identifier":"12-9","id":"13-9","type":"tcp"}]}`,
		strings.Repeat("x", 1024*1024+1),
	} {
		c, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			_, _ = io.WriteString(w, body)
		})
		// The id mismatch case is only meaningful for the session endpoint.
		if !strings.Contains(body, `"id":"13-9"`) {
			_, err := c.GetRuntimeMediaPlayers(context.Background(), StreamTarget{"rtmp", "v", "a", "s"})
			require.ErrorIs(t, err, ErrRuntimeControlUnavailable)
		}
		_, err := c.GetRuntimeSessions(context.Background())
		require.ErrorIs(t, err, ErrRuntimeControlUnavailable)
		server.Close()
	}
}

func TestOpenAPIRuntimeSnapshotsEmptyAndCacheFailure(t *testing.T) {
	for _, cache := range []string{"no-store", "public", ""} {
		c, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", cache)
			_, _ = io.WriteString(w, `{"code":0,"bootNonce":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","data":[]}`)
		})
		snapshot, err := c.GetRuntimeMediaPlayers(context.Background(), StreamTarget{"rtmp", "v", "a", "s"})
		if cache == "no-store" {
			require.NoError(t, err)
			require.Empty(t, snapshot.Players)
		} else {
			require.ErrorIs(t, err, ErrRuntimeControlUnavailable)
		}
		server.Close()
	}
}
