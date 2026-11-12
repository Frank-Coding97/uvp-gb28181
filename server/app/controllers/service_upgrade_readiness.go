package controllers

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"uvplatform.com/uvp-gb28181/app/global/app"
	upgrade "uvplatform.com/uvp-gb28181/internal/upgrade"
)

// UpgradeReadiness exposes no configuration or database identifiers. Only a
// direct loopback peer can inspect the candidate; forwarded headers cannot
// turn an external caller into a local updater.
func (c *ServiceUpgradeController) UpgradeReadiness(ctx *gin.Context) {
	host, _, err := net.SplitHostPort(ctx.Request.RemoteAddr)
	if err != nil {
		ctx.AbortWithStatus(http.StatusForbidden)
		return
	}
	peer := net.ParseIP(strings.Trim(host, "[]"))
	if peer == nil || !peer.IsLoopback() {
		ctx.AbortWithStatus(http.StatusForbidden)
		return
	}
	state, err := upgrade.GateStateFromEnv()
	if err != nil || state.Mode != upgrade.GateCandidate {
		ctx.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	queryCtx, cancel := context.WithTimeout(ctx.Request.Context(), 3*time.Second)
	defer cancel()
	digest, err := c.migrationStateDigest(queryCtx)
	if err != nil {
		ctx.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	// Build identity must be embedded in the running executable. version.json
	// alone can describe a different binary and is insufficient for admission.
	version, buildID := app.RuntimeVersionIdentity()
	if version == "" || buildID == "" {
		ctx.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	ctx.Header("Cache-Control", "no-store")
	ctx.JSON(http.StatusOK, upgrade.RuntimeIdentity{Version: version, BuildID: buildID, TaskID: state.TaskID, DatabaseReady: true, MigrationDigest: digest, DatabaseDialect: strings.ToLower(strings.TrimSpace(app.ConfigYml.GetString("gormv2.usedbtype")))})
}
