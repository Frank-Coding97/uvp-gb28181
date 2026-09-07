package routes

import (
	"context"
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
	require.True(t, probeStandaloneBackend(context.Background()).BackendReady)
	app.Cache = readinessCache{failure: errors.New("internal Redis failure")}
	state := probeStandaloneBackend(context.Background())
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
