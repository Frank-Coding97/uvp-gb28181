package standalone

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Recovery seals both names and contents before directory publication. Empty
// directories participate; timestamps do not. No runtime file is excluded:
// the failed instance must remain a complete, identifiable forensic copy.
func recoveryTreeIdentity(ctx context.Context, root string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if _, err := ensureReleaseChild(root, root, true); err != nil {
		return "", err
	}
	hash := sha256.New()
	encoder := json.NewEncoder(hash)
	seen := make(map[string]bool)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := ensureReleaseChild(root, path, entry.IsDir()); err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		key := strings.ToLower(filepath.ToSlash(rel))
		if seen[key] {
			return errors.New("recovery directory contains case-aliased paths")
		}
		seen[key] = true
		record := struct {
			Path      string
			Directory bool
			Size      int64
			SHA256    string
		}{Path: filepath.ToSlash(rel), Directory: entry.IsDir()}
		if !entry.IsDir() {
			file, err := backupOpenSource(path, false)
			if err != nil {
				return err
			}
			digest := sha256.New()
			n, copyErr := io.Copy(digest, backupContextReader{ctx: ctx, reader: file})
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
			record.SHA256 = hex.EncodeToString(digest.Sum(nil))
			record.Size = n
		}
		return encoder.Encode(record)
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
