package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/websocket"
)

func dialMediaWebSocket(ctx context.Context, base, app, stream, token string) (*websocket.Conn, error) {
	location := strings.Replace(base, "http://", "ws://", 1) + "/" + url.PathEscape(app) + "/" + url.PathEscape(stream) + ".live.mp4"
	if token != "" {
		location += "?" + url.Values{"play_token": {token}}.Encode()
	}
	config, err := websocket.NewConfig(location, base)
	if err != nil {
		return nil, errors.New("invalid WebSocket player configuration")
	}
	return config.DialContext(ctx)
}

func readWebSocketMP4(ws *websocket.Conn, duration, timeout time.Duration) (int, error) {
	if err := ws.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return 0, err
	}
	ws.MaxPayloadBytes = 2 << 20
	start := time.Now()
	total := 0
	ftyp := false
	moof := false
	for {
		var payload []byte
		if err := websocket.Message.Receive(ws, &payload); err != nil {
			return total, errors.New("WebSocket media ended before the required sample")
		}
		total += len(payload)
		ftyp = ftyp || bytes.Contains(payload, []byte("ftyp"))
		moof = moof || bytes.Contains(payload, []byte("moof"))
		if ftyp && moof && time.Since(start) >= duration {
			return total, nil
		}
	}
}

func mediaPlayerCount(client *apiClient, query url.Values) (int, error) {
	response, err := client.call(context.Background(), "/index/api/getMediaPlayerList", query)
	if err != nil || response.Code != 0 {
		return 0, errors.New("WebSocket reader query failed")
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(response.Data, &entries); err != nil {
		return 0, err
	}
	return len(entries), nil
}

func checkWebSocketMedia(client *apiClient, secret, app, stream string, receiver *hookReceiver) (map[string]any, error) {
	query := url.Values{"schema": {"fmp4"}, "vhost": {"__defaultVhost__"}, "app": {app}, "stream": {stream}, "secret": {secret}}
	for _, token := range []string{"", "invalid-fixture-token"} {
		before := receiver.countCode(hookOnPlay, -1)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		ws, err := dialMediaWebSocket(ctx, client.baseURL, app, stream, token)
		cancel()
		if err == nil {
			_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
			ws.MaxPayloadBytes = 2 << 20
			var payload []byte
			readErr := websocket.Message.Receive(ws, &payload)
			_ = ws.Close()
			if readErr == nil && len(payload) > 0 {
				return nil, errors.New("unauthorized WebSocket received payload")
			}
		}
		if _, err := receiver.waitForCode(hookOnPlay, -1, before, 3*time.Second); err != nil {
			return nil, errors.New("unauthorized WebSocket did not reach the rejecting Hook")
		}
	}
	before, err := mediaPlayerCount(client, query)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	ws, err := dialMediaWebSocket(ctx, client.baseURL, app, stream, receiver.playToken)
	cancel()
	if err != nil {
		return nil, errors.New("authorized WebSocket handshake failed")
	}
	defer ws.Close()
	location := ws.Config().Location
	if location.Query().Has("secret") || strings.Contains(location.String(), secret) {
		return nil, errors.New("management secret appeared in a media URL")
	}
	count, err := readWebSocketMP4(ws, 2*time.Second, 6*time.Second)
	if err != nil {
		return nil, err
	}
	active, err := mediaPlayerCount(client, query)
	if err != nil || active <= before {
		return nil, errors.New("WebSocket reader was not registered")
	}
	if err := ws.Close(); err != nil {
		return nil, errors.New("WebSocket close failed")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		current, err := mediaPlayerCount(client, query)
		if err != nil {
			return nil, err
		}
		if current == before {
			return map[string]any{"bytes": count, "sample_min_ms": 2000, "missing_and_wrong_token_rejected": true, "reader_released": true, "management_secret_absent": true}, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil, errors.New("WebSocket reader did not release within five seconds")
}
