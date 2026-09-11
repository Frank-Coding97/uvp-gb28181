package routes

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/internal/standalone/readiness"
)

func TestStandaloneReadinessRequiresLocalProofAndSignsActualState(t *testing.T) {
	const secret = "test-instance-secret"
	challenge, err := readiness.NewChallenge()
	require.NoError(t, err)
	for _, tc := range []struct {
		name, remote, proof string
		ready               bool
		want                int
	}{
		{"remote spoofing forwarded header", "192.0.2.8:5000", readiness.RequestProof(secret, challenge), true, 403},
		{"missing proof", "127.0.0.1:5000", "", true, 403},
		{"wrong instance", "127.0.0.1:5000", readiness.RequestProof("other", challenge), true, 403},
		{"ready", "127.0.0.1:5000", readiness.RequestProof(secret, challenge), true, 200},
		{"not ready", "[::1]:5000", readiness.RequestProof(secret, challenge), false, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			engine := gin.New()
			registerStandaloneReadiness(engine, secret, func(context.Context) readiness.Status {
				called = true
				return readiness.Status{BackendReady: tc.ready, SIPState: "unconfigured", PID: 42}
			})
			req := httptest.NewRequest(http.MethodGet, "/api/standalone/ready", nil)
			req.RemoteAddr = tc.remote
			req.Header.Set("X-Forwarded-For", "127.0.0.1")
			req.Header.Set(readiness.ChallengeHeader, challenge)
			req.Header.Set(readiness.ProofHeader, tc.proof)
			out := httptest.NewRecorder()
			engine.ServeHTTP(out, req)
			require.Equal(t, tc.want, out.Code)
			require.Equal(t, tc.want != 403, called)
			if called {
				require.True(t, readiness.VerifyResponse(secret, challenge, out.Body.Bytes(), out.Header().Get(readiness.ProofHeader)))
				require.Contains(t, out.Body.String(), `"sip_state":"unconfigured"`)
			}
			require.NotContains(t, out.Body.String(), secret)
		})
	}
}

func TestStandaloneReadinessSignsInstallationStateAndFailsClosed(t *testing.T) {
	const secret = "standalone-readiness-secret"
	challenge, err := readiness.NewChallenge()
	require.NoError(t, err)

	for _, tc := range []struct {
		name, phase           string
		credentialAccepted    bool
		installationReady     bool
		wantStatus            int
		wantBackend, wantAuth bool
	}{
		{
			name:               "pending admin remains ready",
			phase:              "pending_admin",
			credentialAccepted: true,
			installationReady:  true,
			wantStatus:         http.StatusOK,
			wantBackend:        true,
			wantAuth:           true,
		},
		{
			name:               "pending sip remains ready",
			phase:              "pending_sip",
			credentialAccepted: true,
			installationReady:  true,
			wantStatus:         http.StatusOK,
			wantBackend:        true,
			wantAuth:           true,
		},
		{
			name:               "reload failure fails closed",
			phase:              "pending_sip",
			credentialAccepted: true,
			installationReady:  false,
			wantStatus:         http.StatusServiceUnavailable,
			wantBackend:        false,
			wantAuth:           false,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			engine := gin.New()
			engine.Use(func(c *gin.Context) {
				c.Set("standalone.installation_phase", tc.phase)
				c.Set("standalone.credential_accepted", tc.credentialAccepted)
				c.Set("standalone.installation_ready", tc.installationReady)
				c.Next()
			})
			registerStandaloneReadiness(engine, secret, func(context.Context) readiness.Status {
				return readiness.Status{
					BackendReady:       true,
					DatabaseReady:      true,
					RedisReady:         true,
					AuthorizationReady: true,
					SIPState:           "unconfigured",
					PID:                42,
				}
			})

			req := httptest.NewRequest(http.MethodGet, "/api/standalone/ready", nil)
			req.RemoteAddr = "127.0.0.1:5000"
			req.Header.Set(readiness.ChallengeHeader, challenge)
			req.Header.Set(readiness.ProofHeader, readiness.RequestProof(secret, challenge))
			out := httptest.NewRecorder()
			engine.ServeHTTP(out, req)

			require.Equal(t, tc.wantStatus, out.Code)
			body := append([]byte(nil), out.Body.Bytes()...)
			var state readiness.Status
			require.NoError(t, json.Unmarshal(body, &state))
			require.Equal(t, tc.phase, state.InstallationPhase)
			require.Equal(t, tc.credentialAccepted, state.CredentialAccepted)
			require.Equal(t, tc.wantBackend, state.BackendReady)
			require.Equal(t, tc.wantAuth, state.AuthorizationReady)
			require.True(t, state.DatabaseReady)
			require.True(t, state.RedisReady)

			signature := out.Header().Get(readiness.ProofHeader)
			require.True(t, readiness.VerifyResponse(secret, challenge, body, signature))
			state.InstallationPhase = "tampered"
			tampered, err := json.Marshal(state)
			require.NoError(t, err)
			require.False(t, readiness.VerifyResponse(secret, challenge, tampered, signature))
			require.NotContains(t, string(body), secret)
		})
	}
}

type readinessCache struct {
	app.CacheInterf
	failure error
}

func (c readinessCache) Exists(context.Context, ...string) (int64, error) { return 0, c.failure }

type readinessToken struct{ app.TokenServiceInterface }
type readinessSession struct{ app.SessionValidatorInterface }

func TestStandaloneBackendProbeChecksLiveDependencies(t *testing.T) {
	oldDB, oldCache, oldCasbin, oldToken, oldSession := app.GormDbSQLite, app.Cache, app.CasbinV2, app.TokenService, app.SessionValidator
	t.Cleanup(func() {
		app.GormDbSQLite, app.Cache, app.CasbinV2, app.TokenService, app.SessionValidator = oldDB, oldCache, oldCasbin, oldToken, oldSession
	})
	db, err := gormhelper.NewSQLiteClient(filepath.Join(t.TempDir(), "ready.db"))
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	app.GormDbSQLite = db
	app.Cache = readinessCache{}
	app.CasbinV2 = developerRouteCasbin{}
	app.TokenService = readinessToken{}
	app.SessionValidator = readinessSession{}
	oldProbe := standaloneBusinessProbe.Load()
	t.Cleanup(func() { standaloneBusinessProbe.Store(oldProbe) })
	called := false
	SetStandaloneBusinessProbe(func(context.Context) (bool, string) { called = true; return true, "ready" })
	require.True(t, probeStandaloneBackend(context.Background()).BackendReady)
	require.False(t, called, "business probe must not run before installation completes")
	completeContext := context.WithValue(context.Background(), standaloneInstallationPhaseKey{}, "complete")
	require.True(t, probeStandaloneBackend(completeContext).BusinessReady)
	require.True(t, called)
	called = false

	app.Cache = readinessCache{failure: errors.New("internal Redis failure")}
	state := probeStandaloneBackend(completeContext)
	require.False(t, called, "unhealthy dependencies must not publish business readiness")
	require.False(t, state.BusinessReady)
	require.False(t, state.BackendReady)
	require.True(t, state.DatabaseReady)
	require.False(t, state.RedisReady)
	app.Cache = readinessCache{}
	app.SessionValidator = nil
	state = probeStandaloneBackend(context.Background())
	require.False(t, state.BackendReady)
	require.False(t, state.AuthorizationReady)
	app.SessionValidator = readinessSession{}
	require.NoError(t, raw.Close())
	state = probeStandaloneBackend(context.Background())
	require.False(t, state.BackendReady)
	require.False(t, state.DatabaseReady)
}

func TestStandaloneReadinessNeverPublishesBusinessReadyBeforeInstallation(t *testing.T) {
	const secret = "business-readiness-test-secret"
	challenge, err := readiness.NewChallenge()
	require.NoError(t, err)
	for _, phase := range []string{"pending_admin", "pending_sip", "complete"} {
		t.Run(phase, func(t *testing.T) {
			engine := gin.New()
			engine.Use(func(c *gin.Context) {
				c.Set("standalone.installation_phase", phase)
				c.Set("standalone.installation_ready", true)
				c.Next()
			})
			registerStandaloneReadiness(engine, secret, func(context.Context) readiness.Status {
				return readiness.Status{BackendReady: true, BusinessReady: true, BusinessReason: "ready"}
			})
			req := httptest.NewRequest("GET", "/api/standalone/ready", nil)
			req.RemoteAddr = "127.0.0.1:12345"
			req.Header.Set(readiness.ChallengeHeader, challenge)
			req.Header.Set(readiness.ProofHeader, readiness.RequestProof(secret, challenge))
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, req)
			require.Equal(t, 200, response.Code)
			var state readiness.Status
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &state))
			require.Equal(t, phase == "complete", state.BusinessReady)
			if phase != "complete" {
				require.Equal(t, "installation_pending", state.BusinessReason)
			}
			signature := response.Header().Get(readiness.ProofHeader)
			require.True(t, readiness.VerifyResponse(secret, challenge, response.Body.Bytes(), signature))
			state.BusinessReady = !state.BusinessReady
			altered, err := json.Marshal(state)
			require.NoError(t, err)
			require.False(t, readiness.VerifyResponse(secret, challenge, altered, signature))
		})
	}
}
