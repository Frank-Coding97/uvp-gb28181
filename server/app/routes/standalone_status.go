package routes

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"uvplatform.cn/uvp-gb28181/app/gb28181"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/internal/standalone/readiness"
)

type standaloneInstallationPhaseKey struct{}
type standaloneBusinessProbeFunc struct {
	probe func(context.Context) (bool, string)
}

var standaloneBusinessProbe atomic.Pointer[standaloneBusinessProbeFunc]

// SetStandaloneBusinessProbe publishes the fully assembled standalone runtime.
// Pending-install requests never invoke it while GB dependencies are starting.
func SetStandaloneBusinessProbe(probe func(context.Context) (bool, string)) {
	if probe == nil {
		standaloneBusinessProbe.Store(nil)
		return
	}
	standaloneBusinessProbe.Store(&standaloneBusinessProbeFunc{probe: probe})
}

// This route is registered only for explicit standalone instances. It is not
// a public shutdown or diagnostics API and never returns dependency errors.
func registerStandaloneReadiness(engine *gin.Engine, secret string, probe func(context.Context) readiness.Status) {
	engine.GET("/api/standalone/ready", func(c *gin.Context) {
		host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
		ip := net.ParseIP(host)
		challenge := c.GetHeader(readiness.ChallengeHeader)
		if err != nil || ip == nil || !ip.IsLoopback() || !readiness.VerifyRequest(secret, challenge, c.GetHeader(readiness.ProofHeader)) {
			c.Status(http.StatusForbidden)
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), time.Second)
		defer cancel()
		if phase, exists := c.Get("standalone.installation_phase"); exists {
			ctx = context.WithValue(ctx, standaloneInstallationPhaseKey{}, phase)
		}
		state := probe(ctx)
		if phase, exists := c.Get("standalone.installation_phase"); exists {
			state.InstallationPhase, _ = phase.(string)
			state.CredentialAccepted = c.GetBool("standalone.credential_accepted")
			if state.InstallationPhase != "complete" {
				state.BusinessReady = false
				state.BusinessReason = "installation_pending"
			}
			if !c.GetBool("standalone.installation_ready") {
				state.AuthorizationReady = false
				state.BackendReady = false
				state.BusinessReady = false
				state.BusinessReason = "status_unavailable"
			}
		}
		body, err := json.Marshal(state)
		if err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.Header(readiness.ProofHeader, readiness.ResponseProof(secret, challenge, body))
		status := http.StatusServiceUnavailable
		if state.BackendReady {
			status = http.StatusOK
		}
		c.Data(status, "application/json", body)
	})
}

func probeStandaloneBackend(ctx context.Context) readiness.Status {
	state := readiness.Status{PID: os.Getpid(), SIPState: string(gb28181.SIPRuntimeStatus().Snapshot().State)}
	if app.GormDbSQLite != nil {
		if db, err := app.GormDbSQLite.DB(); err == nil {
			state.DatabaseReady = db.PingContext(ctx) == nil
		}
	}
	if app.Cache != nil {
		_, err := app.Cache.Exists(ctx, "uvp:standalone:readiness")
		state.RedisReady = err == nil
	}
	state.AuthorizationReady = app.CasbinV2 != nil && app.TokenService != nil && app.SessionValidator != nil
	state.BackendReady = state.DatabaseReady && state.RedisReady && state.AuthorizationReady
	state.BusinessReason = "installation_pending"
	if phase, _ := ctx.Value(standaloneInstallationPhaseKey{}).(string); phase == "complete" {
		state.BusinessReason = "status_unavailable"
		if state.BackendReady {
			state.BusinessReason = "config_not_converged"
			if published := standaloneBusinessProbe.Load(); published != nil {
				state.BusinessReady, state.BusinessReason = published.probe(ctx)
			}
		}
	}
	return state
}
