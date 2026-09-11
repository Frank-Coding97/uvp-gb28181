package logging

import (
	"encoding/json"
	"go.uber.org/zap"
	"io"
	"os"
	"time"
)

// ReportStartupFailure never formats dependency errors or configuration values.
func ReportStartupFailure(out io.Writer, logger *zap.Logger, phase string, err error) {
	switch phase {
	case "config", "logging", "database", "migration", "casbin", "scheduler", "filesystem", "version", "upload", "cache":
	default:
		phase = "dependency"
	}
	if logger != nil {
		logger.Named("startup").Error("startup dependency failed", zap.String("event", "startup.failed"), zap.String("phase", phase), Error(err))
		return
	}
	if out == nil {
		out = os.Stderr
	}
	_ = json.NewEncoder(out).Encode(struct {
		Time  string `json:"created_at"`
		Event string `json:"event"`
		Phase string `json:"phase"`
		Class string `json:"error_class"`
	}{time.Now().Format("2006-01-02T15:04:05.000Z07:00"), "startup.failed", phase, ErrorClass(err)})
}
