package logging

import (
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"reflect"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	mssql "github.com/microsoft/go-mssqldb"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	MaxEventBytes = 32 * 1024
	MaxStackBytes = 16 * 1024
	fieldBudget   = 24 * 1024
	maxFields     = 56
)

// safeError is the only object marshaler the core trusts. It never retains the
// original error, and its encoder cannot invoke third-party formatting code.
type safeError struct {
	class, typ, code string
	number           int64
}

func (e safeError) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddString("class", e.class)
	enc.AddString("type", e.typ)
	if e.number != 0 {
		enc.AddInt64("code", e.number)
	} else if e.code != "" {
		enc.AddString("code", e.code)
	}
	return nil
}
func Error(err error) zap.Field {
	if err == nil {
		return zap.Skip()
	}
	return zap.Object("error", summarizeError(err))
}
func ErrorClass(err error) string { return summarizeError(err).class }
func TypeName(v interface{}) string {
	if v == nil {
		return "nil"
	}
	s := reflect.TypeOf(v).String()
	s, _ = clipJSON(s, 128)
	return s
}
func summarizeError(err error) safeError {
	out := safeError{class: "unknown", typ: TypeName(err)}
	for depth := 0; err != nil && depth < 8; depth++ {
		switch e := err.(type) {
		case *mysql.MySQLError:
			if e != nil {
				out.class = "database"
				out.number = int64(e.Number)
			}
			return out
		case *pgconn.PgError:
			if e != nil {
				out.class = "database"
				if validSQLState(e.Code) {
					out.code = e.Code
				}
			}
			return out
		case mssql.Error:
			out.class = "database"
			out.number = int64(e.Number)
			return out
		case *mssql.Error:
			if e != nil {
				out.class = "database"
				out.number = int64(e.Number)
			}
			return out
		case syscall.Errno:
			out.number = int64(e)
			switch e {
			case syscall.ECONNREFUSED:
				out.class = "connection_refused"
			case syscall.ETIMEDOUT:
				out.class = "timeout"
			case syscall.ENOSPC:
				out.class = "storage_full"
			case syscall.EACCES, syscall.EPERM:
				out.class = "permission_denied"
			case syscall.EPIPE, syscall.ECONNRESET:
				out.class = "connection_closed"
			default:
				out.class = "system"
			}
			return out
		case *net.OpError:
			if e == nil {
				return out
			}
			err = e.Err
			continue
		case *url.Error:
			if e == nil {
				return out
			}
			err = e.Err
			continue
		case *os.PathError:
			if e == nil {
				return out
			}
			err = e.Err
			continue
		case *net.DNSError:
			out.class = "dns"
			if e != nil && e.IsTimeout {
				out.class = "timeout"
			}
			return out
		}
		// Is/As/Unwrap can themselves call user code. Only inspect standard wrappers
		// with known implementations; arbitrary user errors remain opaque.
		typ := reflect.TypeOf(err)
		if typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		if typ.PkgPath() == "fmt" && typ.Name() == "wrapError" {
			err = errors.Unwrap(err)
			continue
		}
		if typ.PkgPath() == "context" || typ.PkgPath() == "errors" || typ.PkgPath() == "io" {
			switch {
			case errors.Is(err, context.Canceled):
				out.class = "canceled"
			case errors.Is(err, context.DeadlineExceeded):
				out.class = "timeout"
			case errors.Is(err, io.EOF):
				out.class = "eof"
			case errors.Is(err, io.ErrShortWrite):
				out.class = "short_write"
			}
		}
		return out
	}
	return out
}
func validSQLState(s string) bool {
	if len(s) != 5 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}

func sensitiveKey(key string) bool {
	// Field keys come from controlled call sites. Oversized names are opaque.
	if len(key) > 128 {
		return true
	}
	var normalized [128]byte
	n := 0
	for i := 0; i < len(key); i++ {
		b := key[i]
		if b == '_' || b == '-' || b == '.' {
			continue
		}
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		normalized[n] = b
		n++
	}
	k := string(normalized[:n])
	for _, part := range []string{"password", "passwd", "secret", "token", "authorization", "cookie", "credential", "privatekey", "accesskey", "apikey"} {
		if strings.Contains(k, part) {
			return true
		}
	}
	switch k {
	case "pwd", "cap", "key", "headers", "body", "payload", "raw", "rawsql", "sql", "sip", "requestbody", "responsebody":
		return true
	}
	return false
}
func sanitizeField(f zap.Field) zap.Field {
	if sensitiveKey(f.Key) {
		return zap.String(f.Key, "[REDACTED]")
	}
	switch f.Type {
	case zapcore.ErrorType:
		err, _ := f.Interface.(error)
		return zap.Object(f.Key, summarizeError(err))
	case zapcore.ObjectMarshalerType:
		if e, ok := f.Interface.(safeError); ok {
			return zap.Object(f.Key, e)
		}
		return zap.String(f.Key, "[omitted:"+TypeName(f.Interface)+"]")
	case zapcore.ArrayMarshalerType, zapcore.ReflectType, zapcore.StringerType, zapcore.InlineMarshalerType, zapcore.BinaryType, zapcore.ByteStringType:
		return zap.String(f.Key, "[omitted:"+TypeName(f.Interface)+"]")
	case zapcore.StringType:
		k := strings.ToLower(f.Key)
		if k == "error" || k == "err" || k == "reason" || k == "panic" || k == "recover" || k == "detail" {
			return zap.String(f.Key, "[text omitted]")
		}
		if strings.Contains(k, "url") || k == "uri" || k == "endpoint" {
			u, e := url.Parse(f.String)
			if e != nil || u.Scheme == "" || u.Host == "" {
				f.String = "[url omitted]"
			} else {
				f.String = u.Scheme + "://" + u.Host
			}
		}
	case zapcore.NamespaceType, zapcore.UnknownType:
		return zap.Skip()
	}
	return f
}

// clipJSON bounds the encoded JSON string content without first allocating a
// potentially huge encoded string. Invalid UTF-8 is accounted as replacement
// runes, matching encoding/json and Zap's encoder.
func clipJSON(s string, limit int) (string, bool) {
	used := 0
	for i, r := range s {
		n := utf8.RuneLen(r)
		if r == utf8.RuneError {
			_, size := utf8.DecodeRuneInString(s[i:])
			if size == 1 {
				n = 6
			}
		}
		if r < 32 {
			n = 6
		} else if r == '"' || r == '\\' {
			n = 2
		} else if r == '\u2028' || r == '\u2029' {
			n = 6
		}
		if used+n > limit {
			return s[:i], true
		}
		used += n
	}
	return s, false
}
func encodedStringSize(s string) int {
	n := 0
	for i, r := range s {
		switch {
		case r < 32:
			n += 6
		case r == '"' || r == '\\':
			n += 2
		case r == '\u2028' || r == '\u2029':
			n += 6
		default:
			if r == utf8.RuneError {
				_, size := utf8.DecodeRuneInString(s[i:])
				if size == 1 {
					n += 6
					continue
				}
			}
			n += utf8.RuneLen(r)
		}
	}
	return n
}
func fieldSize(f zap.Field) int {
	n := encodedStringSize(f.Key) + 8
	switch f.Type {
	case zapcore.StringType:
		n += encodedStringSize(f.String)
	case zapcore.ObjectMarshalerType:
		n += 256
	default:
		n += 64
	}
	return n
}
func importantField(k string) bool {
	switch k {
	case "event", "error", "error_class", "error_code", "stack", "panic_type", "request_id", "client_request_id", "execution_id":
		return true
	}
	return false
}
func sanitizeFields(fields []zap.Field, budget, slots int) ([]zap.Field, int, bool) {
	out := make([]zap.Field, 0, min(len(fields), max(0, slots)))
	used := 0
	truncated := false
	for pass := 0; pass < 2; pass++ {
		for _, raw := range fields {
			if importantField(raw.Key) != (pass == 0) {
				continue
			}
			if len(out) >= slots || budget-used < 32 {
				truncated = true
				continue
			}
			f := sanitizeField(raw)
			if f.Type == zapcore.SkipType {
				continue
			}
			var cut bool
			f.Key, cut = clipJSON(f.Key, 128)
			truncated = truncated || cut
			if f.Type == zapcore.StringType {
				limit := 2048
				if f.Key == "stack" {
					limit = MaxStackBytes
				}
				limit = min(limit, budget-used-encodedStringSize(f.Key)-8)
				f.String, cut = clipJSON(f.String, max(0, limit))
				truncated = truncated || cut
			}
			size := fieldSize(f)
			if size > budget-used {
				truncated = true
				continue
			}
			out = append(out, f)
			used += size
		}
	}
	return out, used, truncated
}
