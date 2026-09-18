package handler_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/gb28181/handler"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/management"
)

func TestOnPlayManagementPreviewClassificationAndTokenBoundaries(t *testing.T) {
	setHookPlayAuth(t, false, false)
	signer, err := management.NewPreviewSigner([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	classifier := management.PreviewClassifierFunc(func(_ context.Context, resource management.PreviewResource) (management.PreviewResourceClass, error) {
		switch resource.App {
		case "rtp":
			return management.PreviewResourceGB, nil
		case "camera":
			return management.PreviewResourceNonGBPreviewable, nil
		case "legacy":
			return management.PreviewResourceLegacyPublic, nil
		case "unknown":
			return management.PreviewResourceUnknownConflicted, nil
		case "conflict":
			return management.PreviewResourceUnknownConflicted, nil
		default:
			return management.PreviewResourceUnknownConflicted, nil
		}
	})
	h := handler.NewHookController(stream.NewNotifier())
	h.SetPreviewRuntime(classifier, signer)
	e := gin.New()
	e.POST("/index/hook/on_play", h.OnPlay)

	grant, err := signer.IssueForUser(17, management.PreviewBinding{
		NodeUUID: "node-a", VHost: "vhost-a", Schema: "rtsp", App: "camera", Stream: "stream-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	base := gin.H{
		"app": "camera", "stream": "stream-1", "schema": "rtsp", "vhost": "vhost-a",
		"mediaServerId": "node-a",
	}
	valid := cloneHookBody(base)
	valid["params"] = "?" + url.Values{management.PreviewQueryParameter: {grant.Token}}.Encode()
	assertHookCode(t, http.StatusOK, postJSON(t, e, "/index/hook/on_play", valid).Body.Bytes(), 0)

	for name, body := range map[string]gin.H{
		"dual token": func() gin.H {
			value := cloneHookBody(valid)
			value["params"] = url.Values{
				management.PreviewQueryParameter: {grant.Token},
				"play_token":                     {"legacy-token"},
			}.Encode()
			return value
		}(),
		"duplicate token": func() gin.H {
			value := cloneHookBody(valid)
			value["params"] = management.PreviewQueryParameter + "=" + url.QueryEscape(grant.Token) + "&" + management.PreviewQueryParameter + "=" + url.QueryEscape(grant.Token)
			return value
		}(),
		"duplicate unrelated parameter": func() gin.H {
			value := cloneHookBody(valid)
			value["params"] = "type=play&type=play&" + management.PreviewQueryParameter + "=" + url.QueryEscape(grant.Token)
			return value
		}(),
		"malformed parameter": func() gin.H {
			value := cloneHookBody(valid)
			value["params"] = "%zz"
			return value
		}(),
		"missing management token": func() gin.H {
			value := cloneHookBody(base)
			value["params"] = "type=play"
			return value
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			assertHookCode(t, http.StatusOK, postJSON(t, e, "/index/hook/on_play", body).Body.Bytes(), -1)
		})
	}

	for _, appName := range []string{"unknown", "conflict"} {
		t.Run(appName+" without token", func(t *testing.T) {
			body := gin.H{"app": appName, "stream": "stream-1", "schema": "rtsp", "vhost": "vhost-a", "mediaServerId": "node-a"}
			assertHookCode(t, http.StatusOK, postJSON(t, e, "/index/hook/on_play", body).Body.Bytes(), -1)
		})
	}

	assertHookCode(t, http.StatusOK, postJSON(t, e, "/index/hook/on_play", gin.H{
		"app": "rtp", "stream": "gb-stream", "schema": "rtsp", "vhost": "vhost-a", "mediaServerId": "node-a",
		"params": "?" + url.Values{management.PreviewQueryParameter: {grant.Token}}.Encode(),
	}).Body.Bytes(), -1)
	assertHookCode(t, http.StatusOK, postJSON(t, e, "/index/hook/on_play", gin.H{
		"app": "legacy", "stream": "legacy-stream", "schema": "rtsp", "vhost": "vhost-a", "mediaServerId": "node-a",
	}).Body.Bytes(), 0)
	assertHookCode(t, http.StatusOK, postJSON(t, e, "/index/hook/on_play", gin.H{
		"app": "legacy", "stream": "legacy-stream", "schema": "rtsp", "vhost": "vhost-a", "mediaServerId": "node-a",
		"params": "?" + url.Values{management.PreviewQueryParameter: {grant.Token}}.Encode(),
	}).Body.Bytes(), -1)
}

func TestOnPlayManagementPreviewUnassembledKeepsLegacyBehavior(t *testing.T) {
	setHookPlayAuth(t, false, false)
	h := handler.NewHookController(stream.NewNotifier())
	e := gin.New()
	e.POST("/index/hook/on_play", h.OnPlay)
	assertHookCode(t, http.StatusOK, postJSON(t, e, "/index/hook/on_play", gin.H{
		"app": "camera", "stream": "stream-1", "schema": "rtsp", "vhost": "vhost-a", "mediaServerId": "node-a",
		"params": "?media_access_token=not-yet-enforced",
	}).Body.Bytes(), 0)
}

func TestOnPlayManagementPreviewRuntimeIsAtomicAndGBClassificationCannotAnonAllow(t *testing.T) {
	setHookPlayAuth(t, false, false)
	h := handler.NewHookController(stream.NewNotifier())
	classifier := management.PreviewClassifierFunc(func(_ context.Context, _ management.PreviewResource) (management.PreviewResourceClass, error) {
		return management.PreviewResourceGB, nil
	})
	if err := h.SetPreviewRuntime(classifier, nil); !errors.Is(err, handler.ErrPreviewRuntimeIncomplete) {
		t.Fatalf("partial runtime error=%v", err)
	}
	e := gin.New()
	e.POST("/index/hook/on_play", h.OnPlay)
	// A rejected partial setter leaves the explicitly unassembled compatibility
	// mode intact, so the old non-rtp behavior is still observable here.
	assertHookCode(t, http.StatusOK, postJSON(t, e, "/index/hook/on_play", gin.H{
		"app": "camera", "stream": "stream-1", "schema": "rtsp", "vhost": "vhost-a", "mediaServerId": "node-a",
	}).Body.Bytes(), 0)

	signer, err := management.NewPreviewSigner([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.SetPreviewRuntime(classifier, signer); err != nil {
		t.Fatal(err)
	}
	assertHookCode(t, http.StatusOK, postJSON(t, e, "/index/hook/on_play", gin.H{
		"app": "camera", "stream": "stream-1", "schema": "rtsp", "vhost": "vhost-a", "mediaServerId": "node-a",
	}).Body.Bytes(), -1)
}

func cloneHookBody(source gin.H) gin.H {
	copy := make(gin.H, len(source)+1)
	for key, value := range source {
		copy[key] = value
	}
	return copy
}
