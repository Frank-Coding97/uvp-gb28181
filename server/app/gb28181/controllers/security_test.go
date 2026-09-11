package controllers

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	gbsecurity "uvplatform.cn/uvp-gb28181/app/gb28181/security"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
)

type securityControllerProvider struct {
	events        []gbsecurity.EventAggregate
	bans          []gbsecurity.FirewallBan
	rules         []gbsecurity.AccessRule
	policy        gbsecurity.SecurityPolicy
	updatedPolicy *gbsecurity.SecurityPolicy
	actor         string
}

func (p *securityControllerProvider) Snapshot() SecuritySnapshot          { return SecuritySnapshot{} }
func (p *securityControllerProvider) Events() []gbsecurity.EventAggregate { return p.events }
func (p *securityControllerProvider) Bans() []gbsecurity.FirewallBan      { return p.bans }
func (p *securityControllerProvider) Policy() gbsecurity.SecurityPolicy {
	if p.policy.BanScore != 0 {
		return p.policy
	}
	return gbsecurity.DefaultPolicy()
}
func (p *securityControllerProvider) UpdatePolicy(policy gbsecurity.SecurityPolicy, actor string) error {
	p.updatedPolicy = &policy
	p.actor = actor
	return nil
}
func (p *securityControllerProvider) Unban(_ string, actor string) error {
	p.actor = actor
	return nil
}
func (p *securityControllerProvider) AccessRules(listType gbsecurity.AccessListType) []gbsecurity.AccessRule {
	items := make([]gbsecurity.AccessRule, 0, len(p.rules))
	for _, item := range p.rules {
		if listType == "" || item.ListType == listType {
			items = append(items, item)
		}
	}
	return items
}
func (p *securityControllerProvider) CreateAccessRule(_ *gbsecurity.AccessRule, actor string) error {
	p.actor = actor
	return nil
}
func (p *securityControllerProvider) UpdateAccessRule(_ gbsecurity.AccessRule, actor string) error {
	p.actor = actor
	return nil
}
func (p *securityControllerProvider) DeleteAccessRule(_ uint64, actor string) error {
	p.actor = actor
	return nil
}
func (p *securityControllerProvider) AgentStatus() gbsecurity.AgentStatus {
	return gbsecurity.AgentStatus{}
}
func (p *securityControllerProvider) Stream() (<-chan SecuritySnapshot, func()) {
	return make(chan SecuritySnapshot), func() {}
}

type securityPageResponse[T any] struct {
	Code int `json:"code"`
	Data struct {
		Items    []T `json:"items"`
		Total    int `json:"total"`
		Page     int `json:"page"`
		PageSize int `json:"pageSize"`
	} `json:"data"`
}

func decodeSecurityPage[T any](t *testing.T, recorder *httptest.ResponseRecorder) securityPageResponse[T] {
	t.Helper()
	var response securityPageResponse[T]
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func TestSecurityControllerSnapshotReportsUnavailableRuntime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/snapshot", NewSecurityController(nil).Snapshot)
	req := httptest.NewRequest("GET", "/snapshot", nil)
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	require.Equal(t, 503, resp.Code)
	require.Contains(t, resp.Body.String(), `security runtime unavailable`)
}

func TestSecurityControllerEventsReturnsStableServerPage(t *testing.T) {
	base := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	provider := &securityControllerProvider{events: []gbsecurity.EventAggregate{
		{SourceIP: "203.0.113.1", LastSeenAt: base.Add(time.Minute)},
		{SourceIP: "203.0.113.3", LastSeenAt: base.Add(3 * time.Minute)},
		{SourceIP: "203.0.113.2", LastSeenAt: base.Add(2 * time.Minute)},
	}}
	recorder := serveSecurityList(t, "/events?page=2&pageSize=2", NewSecurityController(provider).Events)
	response := decodeSecurityPage[gbsecurity.EventAggregate](t, recorder)
	require.Equal(t, 0, response.Code)
	require.Equal(t, 3, response.Data.Total)
	require.Equal(t, 2, response.Data.Page)
	require.Equal(t, 2, response.Data.PageSize)
	require.Len(t, response.Data.Items, 1)
	require.Equal(t, "203.0.113.1", response.Data.Items[0].SourceIP)
}

func TestSecurityControllerBansReturnsNewestFirstAndDefaultsInvalidPaging(t *testing.T) {
	base := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	provider := &securityControllerProvider{bans: []gbsecurity.FirewallBan{
		{Decision: gbsecurity.BanDecision{DecisionID: "older", CreatedAt: base}},
		{Decision: gbsecurity.BanDecision{DecisionID: "newer", CreatedAt: base.Add(time.Minute)}},
	}}
	recorder := serveSecurityList(t, "/bans?page=bad&pageSize=1000", NewSecurityController(provider).Bans)
	response := decodeSecurityPage[gbsecurity.FirewallBan](t, recorder)
	require.Equal(t, 2, response.Data.Total)
	require.Equal(t, 1, response.Data.Page)
	require.Equal(t, 20, response.Data.PageSize)
	require.Equal(t, "newer", response.Data.Items[0].Decision.DecisionID)
}

func TestSecurityControllerAccessRulesPaginatesFilteredListNewestFirst(t *testing.T) {
	base := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	provider := &securityControllerProvider{rules: []gbsecurity.AccessRule{
		{ID: 1, ListType: gbsecurity.ListBlacklist, CreatedAt: base},
		{ID: 2, ListType: gbsecurity.ListAllowlist, CreatedAt: base.Add(time.Minute)},
		{ID: 3, ListType: gbsecurity.ListBlacklist, CreatedAt: base.Add(2 * time.Minute)},
	}}
	recorder := serveSecurityList(t, "/access-rules?listType=blacklist&page=1&pageSize=1", NewSecurityController(provider).AccessRules)
	response := decodeSecurityPage[gbsecurity.AccessRule](t, recorder)
	require.Equal(t, 2, response.Data.Total)
	require.Equal(t, 1, response.Data.Page)
	require.Equal(t, 1, response.Data.PageSize)
	require.Len(t, response.Data.Items, 1)
	require.Equal(t, uint64(3), response.Data.Items[0].ID)
}

func TestSecurityPolicyViewExposesPermanentAutomaticBan(t *testing.T) {
	view := securityPolicyView(gbsecurity.DefaultPolicy())
	require.True(t, view.PermanentAutoBan)
	require.Len(t, view.BanTTLs, 1)
	require.Equal(t, view.BanScore, view.BanTTLs[0].Score)
	require.Zero(t, view.BanTTLs[0].TTL)
}

func TestSecurityControllerPolicyReturnsPermanentAutomaticBan(t *testing.T) {
	router := gin.New()
	router.GET("/policy", NewSecurityController(&securityControllerProvider{policy: gbsecurity.DefaultPolicy()}).Policy)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/policy", nil))

	var response struct {
		Code int                `json:"code"`
		Data SecurityPolicyView `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, 200, recorder.Code)
	require.Equal(t, 0, response.Code)
	require.True(t, response.Data.PermanentAutoBan)
	require.Equal(t, []struct {
		Score int `json:"score"`
		TTL   int `json:"ttl"`
	}{{Score: response.Data.BanScore, TTL: 0}}, response.Data.BanTTLs)
}

func TestPolicyFromViewNormalizesLegacyFiniteAutomaticBan(t *testing.T) {
	view := securityPolicyView(gbsecurity.DefaultPolicy())
	view.PermanentAutoBan = false
	view.BanTTLs[0].TTL = 3600
	policy := policyFromView(view)
	require.NoError(t, policy.Validate())
	require.Equal(t, []gbsecurity.TTLStep{{Score: view.BanScore, TTL: 0}}, policy.BanTTLs)
}

func TestSecurityControllerUpdatePolicyNormalizesLegacyFiniteAutomaticBan(t *testing.T) {
	provider := &securityControllerProvider{policy: gbsecurity.DefaultPolicy()}
	router := gin.New()
	router.PUT("/policy", NewSecurityController(provider).UpdatePolicy)

	view := securityPolicyView(gbsecurity.DefaultPolicy())
	view.PermanentAutoBan = false
	view.BanTTLs[0].TTL = 3600
	payload, err := json.Marshal(view)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("PUT", "/policy", bytes.NewReader(payload)))

	require.Equal(t, 200, recorder.Code)
	require.NotNil(t, provider.updatedPolicy)
	require.Equal(t, []gbsecurity.TTLStep{{Score: view.BanScore, TTL: 0}}, provider.updatedPolicy.BanTTLs)
}

func serveSecurityList(t *testing.T, target string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/events", handler)
	router.GET("/bans", handler)
	router.GET("/access-rules", handler)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", target, nil))
	require.Equal(t, 200, recorder.Code)
	return recorder
}

func TestSecurityControllerMutationsUseAuthenticatedActor(t *testing.T) {
	policy, err := json.Marshal(securityPolicyView(gbsecurity.DefaultPolicy()))
	require.NoError(t, err)
	for _, tc := range []struct{ method, path, body string }{
		{"PUT", "/policy", string(policy)},
		{"POST", "/bans/test-ban/unban", ""},
		{"POST", "/access-rules", `{ "listType":"blacklist", "matchType":"ip", "matchValue":"198.51.100.7", "scope":"all_sip", "status":"enabled" }`},
		{"PUT", "/access-rules/7", `{ "listType":"blacklist", "matchType":"ip", "matchValue":"198.51.100.7", "scope":"all_sip", "status":"enabled" }`},
		{"DELETE", "/access-rules/7", ""},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			provider := &securityControllerProvider{}
			controller := NewSecurityController(provider)
			router := gin.New()
			router.Use(func(ctx *gin.Context) {
				ctx.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 42}})
				ctx.Next()
			})
			router.PUT("/policy", controller.UpdatePolicy)
			router.POST("/bans/:id/unban", controller.Unban)
			router.POST("/access-rules", controller.CreateAccessRule)
			router.PUT("/access-rules/:id", controller.UpdateAccessRule)
			router.DELETE("/access-rules/:id", controller.DeleteAccessRule)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body)))
			require.Equal(t, 200, response.Code, response.Body.String())
			require.Equal(t, "42", provider.actor)
		})
	}
}
