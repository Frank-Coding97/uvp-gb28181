package gormhelper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gorm.io/gorm"
	gormLog "gorm.io/gorm/logger"
	"strings"
	"time"
	"unicode"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

func createCustomGormLog(sqlType string) gormLog.Interface {
	level := gormLog.Warn
	switch strings.ToLower(app.ConfigYml.GetString("gormv2." + sqlType + ".loglevel")) {
	case "silent":
		level = gormLog.Silent
	case "error":
		level = gormLog.Error
	case "info":
		level = gormLog.Info
	}
	threshold := app.ConfigYml.GetDuration("gormv2."+sqlType+".slowthreshold") * time.Second
	return &logger{Config: gormLog.Config{LogLevel: level, SlowThreshold: threshold}, dialect: sqlType}
}

type logger struct {
	gormLog.Config
	dialect string
	now     func() time.Time
}

func (l *logger) LogMode(level gormLog.LogLevel) gormLog.Interface {
	clone := *l
	clone.LogLevel = level
	return &clone
}

// ParamsFilter runs before Dialector.Explain; binding values never reach it.
func (l *logger) ParamsFilter(_ context.Context, sql string, _ ...interface{}) (string, []interface{}) {
	return sql, nil
}

// db.diagnostic 这三支统一走 DEBUG（C03）：
// GORM 的 logger.Interface 只把"我发了条消息"告诉你，**文本本身被故意丢弃**
// （`text_omitted: true`，参数是 `_ string`）。它既不说发生了什么，也不带定位字段——
// 判据②「看到它要做什么」答不出，判据①「定位谁」也答不出。按 C08 的工作，SQL 层面的
// 真相另有其处：`db.query` / `db.query_failed` / `db.slow_query` 三个事件带
// dialect/operation/fingerprint/rows/duration_ms。这里再报一次只会制造噪声。
// ⚠️ 降级前它是全仓**唯一**跨等级的 event（Info/Warn/Error 各一处），降完正好归零。
func (l *logger) Info(ctx context.Context, _ string, _ ...interface{}) {
	if l.LogLevel >= gormLog.Info {
		app.Log(ctx).Named("db").Debug("GORM diagnostic", zap.String("event", "db.diagnostic"), zap.Bool("text_omitted", true))
	}
}
func (l *logger) Warn(ctx context.Context, _ string, _ ...interface{}) {
	if l.LogLevel >= gormLog.Warn {
		app.Log(ctx).Named("db").Debug("GORM diagnostic", zap.String("event", "db.diagnostic"), zap.Bool("text_omitted", true))
	}
}
func (l *logger) Error(ctx context.Context, _ string, _ ...interface{}) {
	if l.LogLevel >= gormLog.Error {
		app.Log(ctx).Named("db").Debug("GORM diagnostic", zap.String("event", "db.diagnostic"), zap.Bool("text_omitted", true))
	}
}
func (l *logger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	if l.LogLevel <= gormLog.Silent {
		return
	}
	now := time.Now
	if l.now != nil {
		now = l.now
	}
	elapsed := now().Sub(begin)
	level, event := zapcore.InfoLevel, "db.query"
	switch {
	case err != nil && l.LogLevel >= gormLog.Error && (!errors.Is(err, gormLog.ErrRecordNotFound) || !l.IgnoreRecordNotFoundError):
		level, event = zapcore.ErrorLevel, "db.query_failed"
	case elapsed > l.SlowThreshold && l.SlowThreshold != 0 && l.LogLevel >= gormLog.Warn:
		level, event = zapcore.WarnLevel, "db.slow_query"
	case l.LogLevel == gormLog.Info:
	default:
		return
	}
	entry := app.Log(ctx).Named("db").Check(level, "database statement completed")
	if entry == nil {
		return
	}
	sql, rows := fc()
	operation, fingerprint := statementSummary(sql)
	fields := []zap.Field{zap.String("event", event), zap.String("dialect", l.dialect), zap.String("operation", operation), zap.String("template_fingerprint", fingerprint), zap.Float64("duration_ms", float64(elapsed)/float64(time.Millisecond)), zap.Int64("rows", rows)}
	if table, ok := ctx.Value(schemaTableKey{}).(string); ok {
		fields = append(fields, zap.String("table", table))
	}
	if err != nil {
		fields = append(fields, logging.Error(err))
		if code := gormErrorCode(err); code != "" {
			fields = append(fields, zap.String("error_class", "database"), zap.String("error_code", code))
		}
	}
	entry.Write(fields...)
}

func gormErrorCode(err error) string {
	switch {
	case errors.Is(err, gorm.ErrInvalidTransaction):
		return "gorm_invalid_transaction"
	case errors.Is(err, gorm.ErrMissingWhereClause):
		return "gorm_missing_where_clause"
	case errors.Is(err, gorm.ErrPrimaryKeyRequired):
		return "gorm_primary_key_required"
	case errors.Is(err, gorm.ErrModelValueRequired):
		return "gorm_model_value_required"
	case errors.Is(err, gorm.ErrInvalidData):
		return "gorm_invalid_data"
	case errors.Is(err, gorm.ErrInvalidField):
		return "gorm_invalid_field"
	case errors.Is(err, gorm.ErrInvalidDB):
		return "gorm_invalid_db"
	case errors.Is(err, gorm.ErrInvalidValue):
		return "gorm_invalid_value"
	default:
		return ""
	}
}

// Fingerprints describe only SQL structure: identifiers and literals are
// opaque markers. This deliberately groups equivalent query shapes across
// tables; a statically parsed GORM schema supplies the separate table field.
func statementSummary(sql string) (string, string) {
	if len(sql) > 65536 {
		return "unknown", "oversized"
	}
	operation := "unknown"
	var shape strings.Builder
	shape.Grow(min(len(sql), 4096))
	for i := 0; i < len(sql); {
		c := sql[i]
		if c == '\'' || c == '"' || c == '`' || c == '[' {
			end := c
			if c == '[' {
				end = ']'
			}
			i++
			for i < len(sql) {
				if sql[i] == '\\' {
					i += min(2, len(sql)-i)
					continue
				}
				if sql[i] == end {
					i++
					if i < len(sql) && sql[i] == end {
						i++
						continue
					}
					break
				}
				i++
			}
			shape.WriteString("? ")
			continue
		}
		if i+1 < len(sql) && sql[i:i+2] == "--" {
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
			continue
		}
		if i+1 < len(sql) && sql[i:i+2] == "/*" {
			i += 2
			for i+1 < len(sql) && sql[i:i+2] != "*/" {
				i++
			}
			i = min(i+2, len(sql))
			continue
		}
		if unicode.IsSpace(rune(c)) {
			i++
			continue
		}
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' {
			start := i
			for i < len(sql) && (sql[i] >= 'a' && sql[i] <= 'z' || sql[i] >= 'A' && sql[i] <= 'Z' || sql[i] >= '0' && sql[i] <= '9' || sql[i] == '_') {
				i++
			}
			word := strings.ToUpper(sql[start:i])
			switch word {
			case "SELECT", "INSERT", "UPDATE", "DELETE", "CREATE", "ALTER", "DROP":
				if operation == "unknown" {
					operation = strings.ToLower(word)
				}
				shape.WriteString(word)
			case "FROM", "WHERE", "INTO", "SET", "VALUES", "JOIN", "ON", "AND", "OR", "IN", "LIMIT", "ORDER", "BY", "GROUP", "AS", "NULL", "IS", "NOT", "RETURNING":
				shape.WriteString(word)
			default:
				shape.WriteByte('?')
			}
			shape.WriteByte(' ')
			continue
		}
		if c >= '0' && c <= '9' || c == '$' || c == '?' || c == '@' {
			i++
			for i < len(sql) && (sql[i] >= '0' && sql[i] <= '9' || sql[i] == '.' || sql[i] >= 'a' && sql[i] <= 'z') {
				i++
			}
			shape.WriteString("? ")
			continue
		}
		switch c {
		case '(', ')', ',', '=', '<', '>', '+', '-', '*', '/', ';':
			shape.WriteByte(c)
		default:
			shape.WriteByte('?')
		}
		i++
	}
	sum := sha256.Sum256([]byte(shape.String()))
	return operation, hex.EncodeToString(sum[:8])
}

type schemaTableKey struct{}

func attachSchemaTable(db *gorm.DB) {
	if db.Statement.Schema != nil && db.Statement.Schema.Table != "" {
		db.Statement.Context = context.WithValue(db.Statement.Context, schemaTableKey{}, db.Statement.Schema.Table)
	}
}
func installLogContext(db *gorm.DB) {
	db.Callback().Query().Before("gorm:query").Register("logging:schema", attachSchemaTable)
	db.Callback().Create().Before("gorm:create").Register("logging:schema", attachSchemaTable)
	db.Callback().Update().Before("gorm:update").Register("logging:schema", attachSchemaTable)
	db.Callback().Delete().Before("gorm:delete").Register("logging:schema", attachSchemaTable)
}
