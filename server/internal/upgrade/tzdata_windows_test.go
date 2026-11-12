//go:build windows

package upgrade

// Windows ships no system zoneinfo database, and Go only finds time zones via
// the ZONEINFO environment variable, an embedded tzdata package, or a
// GOROOT-relative zip that does not exist in a test binary. Linking tzdata in
// here mirrors what server/main.go does for the real programs, so scheduler
// tests that configure "Asia/Shanghai" behave the same as production.
import _ "time/tzdata"
