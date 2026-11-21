package zlm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSinglePortReceiverUsesSharedListenerAndCanonicalSSRC(t *testing.T) {
	var closed []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index/api/getServerConfig":
			fmt.Fprint(w, `{"code":0,"data":[{"rtp_proxy.port":"10000"}]}`)
		case "/index/api/close_streams":
			require.Equal(t, "rtp", r.URL.Query().Get("app"))
			require.Equal(t, "__defaultVhost__", r.URL.Query().Get("vhost"))
			require.Equal(t, "1", r.URL.Query().Get("force"))
			closed = append(closed, r.URL.Query().Get("stream"))
			fmt.Fprint(w, `{"code":0,"count_hit":1,"count_closed":1}`)
		case "/index/api/getRtpInfo":
			fmt.Fprint(w, `{"code":0,"exist":false}`)
		default:
			t.Errorf("shared receiver must not allocate or close a listener: %s", r.URL.Path)
			http.Error(w, "unexpected", 500)
		}
	}))
	defer srv.Close()
	c := (&Client{baseURL: srv.URL + "/index/api", secret: "fixture", http: srv.Client()})
	first, err := c.OpenSinglePortReceiver(context.Background(), "1000000001", 10000)
	require.NoError(t, err)
	require.Equal(t, "3B9ACA01", first.StreamID)
	require.Equal(t, 10000, first.Port)
	second, err := c.OpenSinglePortReceiver(context.Background(), "1000000002", 10000)
	require.NoError(t, err)
	require.NotEqual(t, first.StreamID, second.StreamID)
	require.NoError(t, c.CloseSinglePortReceiver(context.Background(), first.StreamID))
	require.Equal(t, []string{first.StreamID}, closed)
}

func TestSinglePortReceiverRejectsInvalidSSRCAndListenerMismatch(t *testing.T) {
	for _, ssrc := range []string{"", "0", "4294967296", "abc", "-1"} {
		_, err := SinglePortStreamID(ssrc)
		require.Error(t, err)
	}
	for _, actual := range []string{"0", "20000", "invalid", ""} {
		t.Run(actual, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.True(t, strings.HasSuffix(r.URL.Path, "getServerConfig"))
				fmt.Fprintf(w, `{"code":0,"data":[{"rtp_proxy.port":%q}]}`, actual)
			}))
			defer srv.Close()
			_, err := (&Client{baseURL: srv.URL + "/index/api", secret: "fixture", http: srv.Client()}).OpenSinglePortReceiver(context.Background(), "1000000001", 10000)
			require.ErrorContains(t, err, "单端口")
		})
	}
}

func TestSinglePortCleanupRejectsUnconfirmedReadback(t *testing.T) {
	for _, response := range []string{`{"code":0}`, `{"code":0,"exist":null}`, `{"code":-1,"exist":false}`} {
		t.Run(response, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "close_streams") {
					fmt.Fprint(w, `{"code":0,"count_closed":1}`)
					return
				}
				fmt.Fprint(w, response)
			}))
			defer srv.Close()
			c := &Client{baseURL: srv.URL + "/index/api", secret: "fixture", http: srv.Client()}
			require.ErrorContains(t, c.CloseSinglePortReceiver(context.Background(), "3B9ACA01"), "回读失败")
		})
	}
}
