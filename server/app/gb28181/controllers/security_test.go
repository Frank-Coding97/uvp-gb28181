package controllers

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	gbsecurity "uvplatform.cn/uvp-gb28181/app/gb28181/security"
)

type securityControllerProvider struct {
	events []gbsecurity.EventAggregate
	bans   []gbsecurity.FirewallBan
	rules  []gbsecurity.AccessRule
}

func (p *securityControllerProvider) Snapshot() SecuritySnapshot          { return SecuritySnapshot{} }
func (p *securityControllerProvider) Events() []gbsecurity.EventAggregate { return p.events }
func (p *securityControllerProvider) Bans() []gbsecurity.FirewallBan      { return p.bans }
func (p *securityControllerProvider) Policy() gbsecurity.SecurityPolicy {
	return gbsecurity.DefaultPolicy()
}
func (p *securityControllerProvider) UpdatePolicy(gbsecurity.SecurityPolicy, string) error {
	return nil
}
func (p *securityControllerProvider) Unban(string, string) error { return nil }
func (p *securityControllerProvider) AccessRules(listType gbsecurity.AccessListType) []gbsecurity.AccessRule {
	items := make([]gbsecurity.AccessRule, 0, len(p.rules))
	for _, item := range p.rules {
		if listType == "" || item.ListType == listType {
			items = append(items, item)
		}
	}
	return items
}
func (p *securityControllerProvider) CreateAccessRule(*gbsecurity.AccessRule, string) error {
	return nil
}
func (p *securityControllerProvider) UpdateAccessRule(gbsecurity.AccessRule, string) error {
	return nil
}
func (p *securityControllerProvider) DeleteAccessRule(uint64, string) error { return nil }
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
