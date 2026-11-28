package upgrade

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SyncStandaloneControl advances the standard stable entry after an older
// helper activated a release. That helper only switched current, leaving the
// root control script behind. Custom administrator commands remain untouched.
func SyncStandaloneControl(layout RegisteredLayout) (bool, error) {
	if err := layout.validate(); err != nil {
		return false, err
	}
	const name = "uvp-gb28181-ctl.sh"
	if layout.Control != filepath.Join(layout.Root, name) {
		return false, nil
	}
	info, err := os.Lstat(layout.Current)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return false, nil
	}
	release, err := filepath.EvalSymlinks(layout.Current)
	if err != nil {
		return false, err
	}
	releases, err := filepath.EvalSymlinks(layout.Releases)
	if err != nil {
		return false, err
	}
	root, err := filepath.EvalSymlinks(layout.Root)
	if err != nil {
		return false, err
	}
	if !isWithin(root, releases) || !isWithin(releases, release) {
		return false, errors.New("active control script is outside registered releases")
	}
	source := filepath.Join(release, name)
	info, err = os.Lstat(source)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&0111 == 0 || info.Size() > 1<<20 {
		return false, errors.New("active control script is not a bounded executable file")
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return false, err
	}
	if !strings.Contains(string(data), "stop-for-upgrade") {
		return false, nil
	}
	if info, err = os.Lstat(layout.Control); err == nil && !info.Mode().IsRegular() {
		return false, errors.New("stable control script is not a regular file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	same, err := filesHaveSameContent(source, layout.Control)
	if err != nil {
		return false, err
	}
	if same {
		return false, nil
	}
	if err := copyFileAtomically(source, layout.Control, 0755); err != nil {
		return false, fmt.Errorf("update stable control script: %w", err)
	}
	return true, nil
}
