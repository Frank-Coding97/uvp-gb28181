package upgrade

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func registrationPathComponents(path string) ([]string, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("registration path must be absolute")
	}
	parts := strings.Split(strings.TrimPrefix(filepath.Clean(path), string(filepath.Separator)), string(filepath.Separator))
	if len(parts) == 0 || len(parts) == 1 && parts[0] == "" {
		return nil, errors.New("registration path must name a file")
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return nil, errors.New("registration path contains an invalid component")
		}
	}
	return parts, nil
}

// registrationOwnerTrusted permits root-owned registrations everywhere. A
// non-root helper may additionally use a file owned by its own effective UID,
// which keeps standalone and macOS development installs usable. A root helper
// therefore trusts only root-owned path components.
func registrationOwnerTrusted(owner uint32) bool {
	effectiveUID := os.Geteuid()
	return owner == 0 || effectiveUID > 0 && owner == uint32(effectiveUID)
}
