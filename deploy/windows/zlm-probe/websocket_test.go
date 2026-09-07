package main

import (
	"context"
	"golang.org/x/net/websocket"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWebSocketSampleRequiresContinuingMP4(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload []byte
		want    bool
	}{
		{"media", []byte("0000ftyp0000moof"), true},
		{"error page", []byte("<html>denied</html>"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
				defer ws.Close()
				for i := 0; i < 12; i++ {
					if websocket.Message.Send(ws, tc.payload) != nil {
						return
					}
					time.Sleep(10 * time.Millisecond)
				}
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			ws, err := dialMediaWebSocket(ctx, server.URL, "live", "sample", "fixture-token")
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			_, err = readWebSocketMP4(ws, 30*time.Millisecond, 200*time.Millisecond)
			if (err == nil) != tc.want {
				t.Fatalf("err=%v want success=%v", err, tc.want)
			}
		})
	}
}

func TestWebSocketDialHonorsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := dialMediaWebSocket(ctx, "http://127.0.0.1:1", "live", "sample", "fixture"); err == nil {
		t.Fatal("canceled dial succeeded")
	}
}
