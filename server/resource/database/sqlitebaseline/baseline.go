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
	Version = "sqlite-baseline-20260910-ren-r4"
	SHA256  = "e24e21e14bdd97a77c26de6271ee0c781ceff47ee2aef90b2eb14f537d96b712"
)
