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
	SHA256  = "cf94b8273725744a3a85ef78a39d9360b69477f85f508dfe61ea8d58e19576fd"
)
