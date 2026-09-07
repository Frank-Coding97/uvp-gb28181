package main

import (
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestHookCapabilityIsBoundToEvent(t *testing.T) {
	play := hookCapability("fixture-secret", "node-1", hookOnPlay)
	publish := hookCapability("fixture-secret", "node-1", hookOnPublish)
	if play == "" || publish == "" || play == publish {
		t.Fatal("event-specific Hook capabilities were not derived independently")
	}
}

func TestHookReceiverAcceptsValidRequestAndRejectsCredentialVariants(t *testing.T) {
	receiver, err := newHookReceiver("node-1", "fixture-secret")
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.close(2 * time.Second)
	receiver.setTokens("play-fixture", "publish-fixture")
	body := []byte(`{"mediaServerId":"node-1","hook_index":0,"app":"rtp","stream":"stream-1","params":"play_token=play-fixture","ip":"127.0.0.1","port":1,"id":"session-1"}`)
	validQuery := url.Values{"node": {"node-1"}, "cap": {hookCapability("fixture-secret", "node-1", hookOnPlay)}}
	status, response, err := postHook(receiver.baseURL+"/index/hook/on_play", validQuery, body)
	if err != nil || status != http.StatusOK || response.Code != 0 {
		t.Fatalf("valid Hook request was not accepted: status=%d code=%d err=%v", status, response.Code, err)
	}
	for _, test := range []struct {
		name  string
		path  string
		query url.Values
	}{
		{name: "wrong_cap", path: "/index/hook/on_play", query: url.Values{"node": {"node-1"}, "cap": {"bad"}}},
		{name: "wrong_node", path: "/index/hook/on_play", query: url.Values{"node": {"node-2"}, "cap": {hookCapability("fixture-secret", "node-2", hookOnPlay)}}},
		{name: "wrong_event", path: "/index/hook/on_play", query: url.Values{"node": {"node-1"}, "cap": {hookCapability("fixture-secret", "node-1", hookOnPublish)}}},
	} {
		status, response, err := postHook(receiver.baseURL+test.path, test.query, body)
		if err != nil || status != http.StatusOK || response.Code != -1 {
			t.Fatalf("%s credential variant was not rejected: status=%d code=%d err=%v", test.name, status, response.Code, err)
		}
	}
}

func TestHookReceiverRejectsMissingMediaIdentityAndIndex(t *testing.T) {
	receiver, err := newHookReceiver("node-1", "fixture-secret")
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.close(2 * time.Second)
	query := url.Values{"node": {"node-1"}, "cap": {hookCapability("fixture-secret", "node-1", hookOnPlay)}}
	for _, body := range []string{`{"hook_index":0}`, `{"mediaServerId":"node-1"}`} {
		status, response, err := postHook(receiver.baseURL+"/index/hook/on_play", query, []byte(body))
		if err != nil || status != http.StatusOK || response.Code != -1 {
			t.Fatalf("incomplete Hook body was not rejected: status=%d code=%d err=%v", status, response.Code, err)
		}
	}
}

func TestHookReceiverRequiresExactlyOnePlaybackToken(t *testing.T) {
	receiver, err := newHookReceiver("node-1", "fixture-secret")
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.close(2 * time.Second)
	receiver.setTokens("play-fixture", "publish-fixture")
	query := url.Values{"node": {"node-1"}, "cap": {hookCapability("fixture-secret", "node-1", hookOnPlay)}}
	for _, body := range []string{
		`{"mediaServerId":"node-1","hook_index":1,"params":""}`,
		`{"mediaServerId":"node-1","hook_index":2,"params":"play_token=play-fixture&play_token=play-fixture"}`,
		`{"mediaServerId":"node-1","hook_index":3,"params":"play_token=wrong"}`,
	} {
		status, response, err := postHook(receiver.baseURL+"/index/hook/on_play", query, []byte(body))
		if err != nil || status != http.StatusOK || response.Code != -1 {
			t.Fatalf("invalid playback token shape was not rejected: status=%d code=%d err=%v", status, response.Code, err)
		}
	}
}
