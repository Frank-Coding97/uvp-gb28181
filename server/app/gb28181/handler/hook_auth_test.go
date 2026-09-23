package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"uvplatform.cn/uvp-gb28181/app/gb28181/handler"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

type hookAuthResolver struct {
	nodes map[string]*node.Node
}

func (r hookAuthResolver) GetByUUID(id string) (*node.Node, bool) {
	n, ok := r.nodes[id]
	return n, ok
}

func TestHookAuthenticatorAcceptsValidCapabilityAcrossNAT(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mediaNode := &node.Node{MediaServerUUID: "node-a", Host: "192.168.10.220", APISecret: "secret-a"}
	auth := handler.NewHookAuthenticator()
	auth.SetResolver(hookAuthResolver{nodes: map[string]*node.Node{"node-a": mediaNode}})
	capability, err := playauth.HookCapability(mediaNode.APISecret, mediaNode.MediaServerUUID, playauth.HookOnFlowReport)
	require.NoError(t, err)

	called := 0
	engine := gin.New()
	engine.POST("/hook", auth.Middleware(playauth.HookOnFlowReport, handler.HookRejectNotification), func(c *gin.Context) {
		called++
		resolved, ok := handler.AuthenticatedHookNode(c)
		require.True(t, ok)
		require.Equal(t, "node-a", resolved.MediaServerUUID)
		c.JSON(http.StatusOK, gin.H{"code": 0})
	})
	req := httptest.NewRequest(http.MethodPost, "/hook?node=node-a&cap="+capability, nil)
	req.RemoteAddr = "10.8.0.2:45678"
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, req)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, 1, called)
}

func TestHookAuthenticatorRejectsInvalidRequestsWithoutCallingHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mediaNode := &node.Node{MediaServerUUID: "node-a", Host: "192.168.10.220", APISecret: "secret-a"}
	auth := handler.NewHookAuthenticator()
	auth.SetResolver(hookAuthResolver{nodes: map[string]*node.Node{"node-a": mediaNode}})
	valid, err := playauth.HookCapability(mediaNode.APISecret, mediaNode.MediaServerUUID, playauth.HookOnFlowReport)
	require.NoError(t, err)
	wrongEvent, err := playauth.HookCapability(mediaNode.APISecret, mediaNode.MediaServerUUID, playauth.HookOnPlay)
	require.NoError(t, err)

	tests := []struct {
		name, query string
	}{
		{name: "missing node", query: "cap=" + valid},
		{name: "missing cap", query: "node=node-a"},
		{name: "duplicate node", query: "node=node-a&node=node-b&cap=" + valid},
		{name: "duplicate cap", query: "node=node-a&cap=" + valid + "&cap=other"},
		{name: "unknown node", query: "node=node-b&cap=" + valid},
		{name: "invalid cap", query: "node=node-a&cap=invalid"},
		{name: "cross event", query: "node=node-a&cap=" + wrongEvent},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			called := 0
			engine := gin.New()
			engine.POST("/hook", auth.Middleware(playauth.HookOnFlowReport, handler.HookRejectNotification), func(c *gin.Context) { called++ })
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/hook?"+tc.query, nil))
			require.Equal(t, http.StatusOK, response.Code)
			require.Equal(t, 0, called)
		})
	}
}

func TestHookAuthenticatorFailsClosedWhenResolverIsUnavailable(t *testing.T) {
	auth := handler.NewHookAuthenticator()
	called := 0
	engine := gin.New()
	engine.POST("/hook", auth.Middleware(playauth.HookOnFlowReport, handler.HookRejectNotification), func(c *gin.Context) { called++ })
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/hook?node=node-a&cap=anything", nil))
	require.Equal(t, 0, called)
}

func TestHookAuthenticatorUsesSafeRejectResponses(t *testing.T) {
	tests := []struct {
		name  string
		mode  handler.HookRejectMode
		code  float64
		close *bool
	}{
		{name: "notification", mode: handler.HookRejectNotification, code: 0},
		{name: "admission", mode: handler.HookRejectAdmission, code: -1},
		{name: "none reader", mode: handler.HookRejectNoneReader, code: 0, close: boolPtr(false)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			auth := handler.NewHookAuthenticator()
			engine := gin.New()
			engine.POST("/hook", auth.Middleware(playauth.HookOnPlay, tc.mode), func(c *gin.Context) { t.Fatal("handler must not run") })
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/hook", nil))
			var body map[string]interface{}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			require.Equal(t, tc.code, body["code"])
			if tc.close != nil {
				require.Equal(t, *tc.close, body["close"])
			}
		})
	}
}

func TestHookAuthenticatorDoesNotLogCapability(t *testing.T) {
	core, observed := observer.New(zap.WarnLevel)
	previous := app.ZapLog
	app.ZapLog = zap.New(core)
	t.Cleanup(func() { app.ZapLog = previous })
	auth := handler.NewHookAuthenticator()
	engine := gin.New()
	engine.POST("/hook", auth.Middleware(playauth.HookOnFlowReport, handler.HookRejectNotification), func(c *gin.Context) {})
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/hook?node=node-a&cap=must-not-appear", nil))

	encoded, err := json.Marshal(observed.All())
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "must-not-appear")

	// 被拒绝时**对方还没通过认证**，所以"是谁"只能由 `source_ip`（谁连过来的）+
	// `node_id`（它自称是谁）一起回答 —— 只有 reason_code 的拒绝日志等于
	// "有人被拒了，但不知道是谁"。
	var rejected map[string]interface{}
	for _, entry := range observed.All() {
		if entry.ContextMap()["event"] == "gb28181.hook.auth.rejected" {
			rejected = entry.ContextMap()
		}
	}
	require.NotNil(t, rejected, "拒绝事件未记录")
	require.Equal(t, "192.0.2.1", rejected["source_ip"], "拒绝事件必须留下对端地址")
	require.Equal(t, "node-a", rejected["node_id"], "拒绝事件必须留下载荷自称的节点")

	// 受控短码统一 snake_case（register / playauth 两侧都是），kebab 会让人以为
	// 它们来自不同的枚举、进而写出互不兼容的过滤规则。
	reasonCode, _ := rejected["reason_code"].(string)
	require.NotEmpty(t, reasonCode, "拒绝事件必须给出 reason_code")
	require.NotContains(t, reasonCode, "-", "受控短码不要用 kebab-case")
}

func boolPtr(value bool) *bool { return &value }

// hookRetiredObserver 是 HookRetiredObserver 的测试替身：只认识"退休过"的那一个 uuid。
type hookRetiredObserver struct {
	retired map[string]bool
	seen    []string
}

func (o *hookRetiredObserver) ObserveRetiredHook(uuid, sourceIP string) bool {
	o.seen = append(o.seen, uuid+"@"+sourceIP)
	return o.retired[uuid]
}

// 认证 miss 的两种事实必须分开记：
//   - node_retired：平台**自己删过**它，凭据还在 ⇒ 对端残留本该被撤掉；
//   - node_unknown：平台根本不认识它 ⇒ 只可能是伪造 / 串台 / 别人家配错了地址。
//
// 混成一种，"我们删过还没撤干净"与"陌生人一直在敲门"在日志里就长得一模一样。
func TestHookAuthenticatorDistinguishesRetiredNodeFromUnknownSender(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, observed := observer.New(zap.WarnLevel)
	previous := app.ZapLog
	app.ZapLog = zap.New(core)
	t.Cleanup(func() { app.ZapLog = previous })

	auth := handler.NewHookAuthenticator()
	auth.SetResolver(hookAuthResolver{nodes: map[string]*node.Node{}})
	retired := &hookRetiredObserver{retired: map[string]bool{"retired-node": true}}
	auth.SetRetiredObserver(retired)

	engine := gin.New()
	engine.POST("/hook", auth.Middleware(playauth.HookOnServerKeepalive, handler.HookRejectNotification), func(c *gin.Context) {
		t.Fatal("handler must not run")
	})
	for _, uuid := range []string{"retired-node", "stranger-node"} {
		request := httptest.NewRequest(http.MethodPost, "/hook?node="+uuid+"&cap=stale", nil)
		request.RemoteAddr = "192.168.10.220:45678"
		engine.ServeHTTP(httptest.NewRecorder(), request)
	}

	reasons := map[string]string{}
	for _, entry := range observed.All() {
		if entry.ContextMap()["event"] != "gb28181.hook.auth.rejected" {
			continue
		}
		reasons[entry.ContextMap()["node_id"].(string)] = entry.ContextMap()["reason_code"].(string)
	}
	require.Equal(t, "node_retired", reasons["retired-node"])
	require.Equal(t, "node_unknown", reasons["stranger-node"])
	// 观察器两类都要问（未知节点也可能是"退休表还没装载"的那一个），
	// 并且必须带上真实的 source_ip —— 认证失败时那是唯一的可信来源标识。
	require.Equal(t, []string{"retired-node@192.168.10.220", "stranger-node@192.168.10.220"}, retired.seen)
}

// 未装配观察器时退回老行为：只记 node_unknown，不 panic。
func TestHookAuthenticatorFallsBackWhenRetiredObserverAbsent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, observed := observer.New(zap.WarnLevel)
	previous := app.ZapLog
	app.ZapLog = zap.New(core)
	t.Cleanup(func() { app.ZapLog = previous })

	auth := handler.NewHookAuthenticator()
	auth.SetResolver(hookAuthResolver{nodes: map[string]*node.Node{}})
	engine := gin.New()
	engine.POST("/hook", auth.Middleware(playauth.HookOnServerKeepalive, handler.HookRejectNotification), func(c *gin.Context) {})
	request := httptest.NewRequest(http.MethodPost, "/hook?node=some-node&cap=stale", nil)
	request.RemoteAddr = "192.168.10.220:45678"
	engine.ServeHTTP(httptest.NewRecorder(), request)

	for _, entry := range observed.All() {
		if entry.ContextMap()["event"] != "gb28181.hook.auth.rejected" {
			continue
		}
		require.Equal(t, "node_unknown", entry.ContextMap()["reason_code"])
	}
}

// 现场回归锚点（2026-09-21）：一个已从平台移除、却仍在回调的 ZLM 实例会让同一条
// 拒绝以心跳节奏（实测 10s）反复出现。180 次心跳只应留下 **1 条明细**，
// 而不是 180 条 —— 但那条明细的定位字段一个都不能少（谁自称是谁、从哪来、为什么被拒）。
func TestHookAuthenticatorFoldsRepeatedRejectionsFromOneSource(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, observed := observer.New(zap.WarnLevel)
	previous := app.ZapLog
	app.ZapLog = zap.New(core)
	t.Cleanup(func() { app.ZapLog = previous })

	auth := handler.NewHookAuthenticator()
	auth.SetResolver(hookAuthResolver{nodes: map[string]*node.Node{}})
	engine := gin.New()
	engine.POST("/hook", auth.Middleware(playauth.HookOnServerKeepalive, handler.HookRejectNotification), func(c *gin.Context) {
		t.Fatal("handler must not run")
	})

	for beat := 0; beat < 180; beat++ {
		request := httptest.NewRequest(http.MethodPost, "/hook?node=ghost-node&cap=stale", nil)
		request.RemoteAddr = "192.168.10.220:45678"
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		require.Equal(t, http.StatusOK, response.Code)
	}

	rejected, summaries := 0, 0
	for _, entry := range observed.All() {
		switch entry.ContextMap()["event"] {
		case "gb28181.hook.auth.rejected":
			rejected++
			require.Equal(t, "ghost-node", entry.ContextMap()["node_id"])
			require.Equal(t, "192.168.10.220", entry.ContextMap()["source_ip"])
			require.Equal(t, "node_unknown", entry.ContextMap()["reason_code"])
		case "gb28181.hook.auth.rejected_summary":
			summaries++
		}
	}
	require.Equal(t, 1, rejected, "同源重复拒绝必须折叠成一条明细")
	require.Equal(t, 0, summaries, "窗口未到期不应出现汇总")
}

// 折叠的反面：`auth_runtime_unavailable` 是**平台自己坏了**，不能跟着变安静。
func TestHookAuthenticatorKeepsLoggingItsOwnRuntimeFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, observed := observer.New(zap.WarnLevel)
	previous := app.ZapLog
	app.ZapLog = zap.New(core)
	t.Cleanup(func() { app.ZapLog = previous })

	auth := handler.NewHookAuthenticator() // 未配置 resolver
	engine := gin.New()
	engine.POST("/hook", auth.Middleware(playauth.HookOnServerKeepalive, handler.HookRejectNotification), func(c *gin.Context) {})

	for attempt := 0; attempt < 5; attempt++ {
		request := httptest.NewRequest(http.MethodPost, "/hook?node=node-a&cap=anything", nil)
		request.RemoteAddr = "192.168.10.220:45678"
		engine.ServeHTTP(httptest.NewRecorder(), request)
	}

	rejected := 0
	for _, entry := range observed.All() {
		if entry.ContextMap()["event"] != "gb28181.hook.auth.rejected" {
			continue
		}
		rejected++
		require.Equal(t, "auth_runtime_unavailable", entry.ContextMap()["reason_code"])
	}
	require.Equal(t, 5, rejected, "平台自身故障必须每一次都留痕")
}
