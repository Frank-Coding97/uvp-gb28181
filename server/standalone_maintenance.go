package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/service"
	"uvplatform.cn/uvp-gb28181/internal/standalone"
	"uvplatform.cn/uvp-gb28181/internal/standalone/candidatehealth"
)

// This entry is reachable only after bootstrap consumes an inherited one-use
// permit. No public flag selects it, and no business routes or jobs are built.
func runStandaloneMaintenance(operation standalone.MaintenancePermitClaims) error {
	if app.DataPath == "" || app.GormDbSQLite == nil {
		return errors.New("maintenance requires standalone SQLite")
	}
	raw, err := app.GormDbSQLite.DB()
	if err != nil {
		return err
	}
	defer raw.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	switch operation.Purpose {
	case "bootstrap_db":
		err = runBootstrapDB()
	case "migrate_up":
		err = runMigrateUp()
	case "db_check":
		err = runDBCheck()
	case "revoke_sessions":
		sessions := service.NewAuthSessionService(app.GormDbSQLite)
		err = app.GormDbSQLite.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return sessions.RevokeAllForRestoreTx(tx) })
	case "candidate_health":
		cfg := app.ConfigYml
		client := redis.NewClient(&redis.Options{
			Addr:     net.JoinHostPort(cfg.GetString("redis.host"), strconv.Itoa(cfg.GetInt("redis.port"))),
			Password: cfg.GetString("redis.password"), DB: cfg.GetInt("redis.indexdb"),
			DialTimeout: 3 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second,
		})
		defer client.Close()
		err = candidatehealth.Check(ctx, app.GormDbSQLite, client, cfg)
	default:
		return errors.New("unsupported maintenance operation")
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(struct {
		Status  string `json:"status"`
		Purpose string `json:"purpose"`
		Version string `json:"version"`
	}{"maintenance_complete", operation.Purpose, operation.Version})
}
