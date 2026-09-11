package standalone

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// This is an admission estimate, not a disk reservation: concurrent writers
// can still exhaust space, and every later write must continue to fail closed.
// Existing active data and backups already consume free space. Budget three
// copies of the staged bytes for the copy, SQLite WAL and fresh Redis AOF,
// plus room for small files and maintenance metadata.
func recoverySpaceBudget(manifest BackupManifest) (uint64, error) {
	const reserve = uint64(64 << 20)
	needed := reserve
	for _, file := range manifest.Files {
		if !strings.HasPrefix(file.Path, "config/") && !strings.HasPrefix(file.Path, "data/") {
			continue
		}
		if file.Size < 0 {
			return 0, errors.New("invalid recovery file size")
		}
		// Include a 4 KiB allocation allowance for every file, including empty files.
		size := uint64(file.Size)
		remaining := (math.MaxUint64 - needed) / 3
		if remaining < 4096 || size > remaining-4096 {
			return 0, errors.New("recovery space estimate overflow")
		}
		needed += (size + 4096) * 3
	}
	return needed, nil
}

func checkRecoverySpace(path string, manifest BackupManifest, available func(string) (uint64, error)) error {
	if available == nil {
		return errors.New("recovery space query unavailable")
	}
	needed, err := recoverySpaceBudget(manifest)
	if err != nil {
		return err
	}
	free, err := available(path)
	if err != nil {
		return fmt.Errorf("query recovery disk space: %w", err)
	}
	if free < needed {
		return fmt.Errorf("insufficient recovery disk space: available %d bytes, require at least %d bytes", free, needed)
	}
	return nil
}
