// Package sqlitedialect keeps the application's SQLite test and embedded
// database setup on one GORM dialector and one database/sql driver.
package sqlitedialect

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

const (
	timeFormatKey   = "_time_format"
	timeFormatValue = "sqlite"
)

// Open returns the SQLite GORM dialector used by the application and tests.
// The modernc driver defaults to time.Time.String formatting; the explicit
// format keeps stored timestamps compatible with the existing SQLite data.
func Open(dsn string) gorm.Dialector {
	normalizedDSN, err := normalizeDSN(dsn)
	if err != nil {
		return invalidDialector{Dialector: sqlite.Open(dsn), err: err}
	}
	return sqlite.Open(normalizedDSN)
}

func normalizeDSN(dsn string) (string, error) {
	queryStart := strings.IndexByte(dsn, '?')
	if dsn == "" {
		return "", fmt.Errorf("sqlite DSN must include a database name")
	}
	if queryStart == 0 {
		return "", fmt.Errorf("sqlite DSN must include a database name before the query")
	}
	if queryStart < 0 {
		return dsn + "?_time_format=sqlite", nil
	}

	query := dsn[queryStart+1:]
	values, err := url.ParseQuery(query)
	if err != nil {
		return "", fmt.Errorf("parse sqlite DSN query: %w", err)
	}

	if formats, ok := values[timeFormatKey]; ok {
		for _, format := range formats {
			if format != timeFormatValue {
				return "", fmt.Errorf("conflicting %s=%q: only %q is supported", timeFormatKey, format, timeFormatValue)
			}
		}
		return dsn, nil
	}

	if query == "" {
		return dsn + "_time_format=sqlite", nil
	}
	return dsn + "&_time_format=sqlite", nil
}

// invalidDialector delays DSN validation errors until gorm.Open, which is the
// error-returning API used by callers of Open.
type invalidDialector struct {
	gorm.Dialector
	err error
}

func (d invalidDialector) Initialize(*gorm.DB) error {
	return d.err
}
