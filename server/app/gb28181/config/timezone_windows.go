//go:build windows

package config

// Windows does not provide the IANA zoneinfo database used by record queries.
// Embed Go's database so the offline package can load Asia/Shanghai without a
// Go installation or external zoneinfo files on the target machine.
import _ "time/tzdata"
