package app

import "fmt"

// ValidateDatabaseSelection runs before opening any connection. The existing
// server mode may initialize multiple server databases; SQLite is exclusive.
func ValidateDatabaseSelection(primary string, enabled map[string]bool, standalone bool) error {
	switch primary {
	case "mysql", "postgresql", "sqlserver", "sqlite":
	default:
		return fmt.Errorf("unknown database dialect %q", primary)
	}
	if standalone && primary != "sqlite" {
		return fmt.Errorf("standalone requires sqlite")
	}
	if !enabled[primary] {
		return fmt.Errorf("selected database %s is not enabled", primary)
	}
	if enabled["sqlite"] {
		if primary != "sqlite" {
			return fmt.Errorf("sqlite cannot be combined with another primary database")
		}
		for name, on := range enabled {
			if on && name != "sqlite" {
				return fmt.Errorf("sqlite cannot be combined with %s", name)
			}
		}
	}
	return nil
}
