package gormhelper

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/internal/sqlitedialect"
	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

const sqliteRuntimeVersion = "3.53.4"

type SQLiteRuntime struct {
	Version            string `json:"version"`
	JournalMode        string `json:"journal_mode"`
	Synchronous        int    `json:"synchronous"`
	ForeignKeys        int    `json:"foreign_keys"`
	BusyTimeout        int    `json:"busy_timeout_ms"`
	MaxOpenConnections int    `json:"max_open_connections"`
}

func NewSQLiteClient(path string) (*gorm.DB, error) {
	if err := standalone.ValidateDatabaseFile(path); err != nil {
		return nil, err
	}
	uriPath := filepath.ToSlash(path)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	query := url.Values{}
	for _, pragma := range []string{"busy_timeout(5000)", "foreign_keys(1)", "journal_mode(WAL)", "synchronous(FULL)"} {
		query.Add("_pragma", pragma)
	}
	dsn := (&url.URL{Scheme: "file", Path: uriPath, RawQuery: query.Encode()}).String()
	logger := gormlog.Discard
	if app.ConfigYml != nil && app.ZapLog != nil {
		logger = redefineLog("sqlite")
	}
	db, err := gorm.Open(sqlitedialect.Open(dsn), &gorm.Config{SkipDefaultTransaction: true, PrepareStmt: true, TranslateError: true, Logger: logger})
	if err != nil {
		return nil, err
	}
	raw, err := db.DB()
	if err != nil {
		return nil, err
	}
	raw.SetMaxOpenConns(1)
	raw.SetMaxIdleConns(1)
	info, err := InspectSQLite(db)
	if err != nil {
		_ = raw.Close()
		return nil, err
	}
	if info.Version != sqliteRuntimeVersion || info.JournalMode != "wal" || info.Synchronous != 2 || info.ForeignKeys != 1 || info.BusyTimeout != 5000 || info.MaxOpenConnections != 1 {
		_ = raw.Close()
		return nil, fmt.Errorf("sqlite runtime configuration mismatch: %+v", info)
	}
	if err = db.Callback().Query().Before("gorm:query").Register("disable_raise_record_not_found", MaskNotDataError); err == nil {
		err = db.Callback().Create().Before("gorm:before_create").Register("CreateBeforeHook", CreateBeforeHook)
	}
	if err == nil {
		err = db.Callback().Update().Before("gorm:before_update").Register("UpdateBeforeHook", UpdateBeforeHook)
	}
	if err == nil {
		err = db.Callback().Delete().Before("gorm:before_delete").Register("DeleteBeforeHook", DeleteBeforeHook)
	}
	if err != nil {
		_ = raw.Close()
		return nil, err
	}
	return db, nil
}

func InspectSQLite(db *gorm.DB) (*SQLiteRuntime, error) {
	raw, err := db.DB()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	info := &SQLiteRuntime{MaxOpenConnections: raw.Stats().MaxOpenConnections}
	for _, q := range []struct {
		sql  string
		dest any
	}{
		{"SELECT sqlite_version()", &info.Version}, {"PRAGMA journal_mode", &info.JournalMode},
		{"PRAGMA synchronous", &info.Synchronous}, {"PRAGMA foreign_keys", &info.ForeignKeys}, {"PRAGMA busy_timeout", &info.BusyTimeout},
	} {
		if err = raw.QueryRowContext(ctx, q.sql).Scan(q.dest); err != nil {
			return nil, err
		}
	}
	return info, nil
}
