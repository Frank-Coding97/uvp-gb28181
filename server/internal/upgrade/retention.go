package upgrade

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// PruneCompletedArtifacts removes only helper-generated files belonging to
// completed tasks. Callers serialize it with the workflow lock. Current,
// retained rollback, failed and recovery-required tasks are always protected.
// Unknown releases and Docker images are never removed.
func PruneCompletedArtifacts(ctx context.Context, store *TaskStore, layout RegisteredLayout, retain int) error {
	if retain < 0 || retain > 50 {
		return errors.New("invalid retained version count")
	}
	if err := layout.validate(); err != nil {
		return err
	}
	if store == nil || filepath.Clean(store.root) != filepath.Clean(layout.State) {
		return errors.New("cleanup store does not match registered state")
	}
	records, err := store.List()
	if err != nil {
		return err
	}
	completedAt := make(map[string]time.Time, len(records))
	for _, record := range records {
		at, err := time.Parse(time.RFC3339Nano, record.UpdatedAt)
		if err != nil {
			return errors.New("cleanup requires a valid task completion timestamp")
		}
		completedAt[record.TaskID] = at
	}
	sort.Slice(records, func(i, j int) bool { return completedAt[records[i].TaskID].After(completedAt[records[j].TaskID]) })
	protected := map[string]bool{}
	current, err := os.Readlink(layout.Current)
	if err == nil {
		if !filepath.IsAbs(current) {
			current = filepath.Join(filepath.Dir(layout.Current), current)
		}
		protected[protectedReleaseRoot(layout.Releases, current)] = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect retained current release: %w", err)
	}
	keep := map[string]bool{}
	completed := 0
	for _, r := range records {
		safeTerminal := r.Phase == PhaseSucceeded || r.Phase == PhaseRolledBack
		if !safeTerminal || completed < retain || protected[filepath.Join(layout.Releases, r.TaskID)] {
			keep[r.TaskID] = true
			if r.PreviousTarget != "" {
				protected[protectedReleaseRoot(layout.Releases, r.PreviousTarget)] = true
			}
		}
		if safeTerminal {
			completed++
		}
	}
	for _, r := range records {
		if protected[filepath.Join(layout.Releases, r.TaskID)] {
			keep[r.TaskID] = true
		}
	}
	protectedStaging := map[string]bool{}
	for _, r := range records {
		if keep[r.TaskID] {
			protectedStaging[r.StagingPath] = true
		}
	}
	for _, r := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		if keep[r.TaskID] {
			continue
		}
		if !validTaskID(r.TaskID) {
			return errors.New("invalid task identity in retained state")
		}
		for _, entry := range []struct{ root, name string }{
			{layout.Releases, r.TaskID},
			{filepath.Join(layout.State, "backups"), r.TaskID},
			{filepath.Join(layout.State, "prepared-backups"), r.TaskID},
			{filepath.Join(layout.State, "migration-staging"), r.TaskID},
			{filepath.Join(layout.State, "compose-staging"), r.TaskID},
		} {
			if err := removeOwnedArtifact(entry.root, entry.name); err != nil {
				return err
			}
		}
		verified := filepath.Join(layout.State, "verified")
		if r.StagingPath != "" && !protectedStaging[r.StagingPath] && filepath.Dir(r.StagingPath) == verified {
			for _, name := range []string{filepath.Base(r.StagingPath), filepath.Base(r.StagingPath) + ".json"} {
				if err := removeOwnedArtifact(verified, name); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func removeOwnedArtifact(root, name string) error {
	if filepath.Base(name) != name || name == "." || name == ".." {
		return errors.New("invalid cleanup artifact name")
	}
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("cleanup root is not a regular directory")
	}
	dir, err := openCleanupRoot(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := dir.RemoveAll(name); err != nil {
		return fmt.Errorf("remove completed upgrade artifact: %w", err)
	}
	return syncDirectory(root)
}

func protectedReleaseRoot(releases, target string) string {
	rel, err := filepath.Rel(releases, filepath.Clean(target))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.Clean(target)
	}
	return filepath.Join(releases, strings.Split(rel, string(filepath.Separator))[0])
}

// Walk directory handles from the filesystem root. Every child is verified
// against the inode inspected through its already-open parent, so replacing a
// pathname with a symlink cannot redirect deletion into an unrelated tree.
func openCleanupRoot(path string) (*os.Root, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("cleanup root must be absolute")
	}
	path = filepath.Clean(path)
	volume := filepath.VolumeName(path)
	dir, err := os.OpenRoot(volume + string(filepath.Separator))
	if err != nil {
		return nil, err
	}
	for _, component := range strings.Split(strings.TrimPrefix(path, volume+string(filepath.Separator)), string(filepath.Separator)) {
		if component == "" {
			continue
		}
		before, err := dir.Lstat(component)
		if err != nil {
			dir.Close()
			return nil, err
		}
		if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
			dir.Close()
			return nil, errors.New("cleanup ancestor is not a real directory")
		}
		next, err := dir.OpenRoot(component)
		if err != nil {
			dir.Close()
			return nil, err
		}
		after, err := next.Stat(".")
		dir.Close()
		if err != nil || !os.SameFile(before, after) {
			next.Close()
			return nil, errors.New("cleanup directory changed while opening")
		}
		dir = next
	}
	return dir, nil
}
