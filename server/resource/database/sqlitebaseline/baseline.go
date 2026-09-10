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
	Version = "sqlite-baseline-20260907-ren-r3"
	SHA256  = "8a0fb8b4575ea7d9dffe749a7d7d0d764e64f79dd8a2018ee23737c848786a61"
)
