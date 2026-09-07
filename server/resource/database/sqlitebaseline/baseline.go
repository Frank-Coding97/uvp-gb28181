// Package sqlitebaseline contains the checked-in schema and system metadata
// used to initialize a fresh standalone SQLite database.
package sqlitebaseline

import _ "embed"

// SQL is the immutable SQLite DDL and deterministic system seed batch.  The
// caller owns the surrounding transaction and migration marker.
//
//go:embed baseline.sql
var SQL string

const (
	Version = "sqlite-baseline-20260907-e07857cc"
	SHA256  = "785a700f94513851de4b2c4f4ee6854275b6cfec6840d3bdb6ec3f7252db569a"
)
