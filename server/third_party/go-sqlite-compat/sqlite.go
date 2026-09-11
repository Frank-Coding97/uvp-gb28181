// Package sqlite provides the narrow compatibility surface required by the
// pinned GORM SQLite dialect. The actual database/sql driver is registered by
// modernc.org/sqlite.
package sqlite

import modernc "modernc.org/sqlite"

var _ error = (*modernc.Error)(nil)

// Error is the only upstream go-sqlite symbol used by glebarez/sqlite v1.11.0.
// It is an alias so error translation continues to recognize modernc errors.
type Error = modernc.Error
