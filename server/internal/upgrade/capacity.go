package upgrade

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const capacityReserve uint64 = 256 << 20

// 64 KiB covers supported local Linux filesystem allocation units. Refuse
// larger units rather than silently underestimating many small files.
const capacityBlockSize uint64 = 64 << 10

type filesystemCapacity struct {
	Device    uint64
	Available uint64
	BlockSize uint64
}
type spaceRequirement struct {
	Path  string
	Bytes uint64
}

// RegisteredCapacityPreflight validates the complete archive and budgets space
// on each affected filesystem before the workflow closes the business gate.
// Existing releases are never counted as reclaimable space.
func RegisteredCapacityPreflight(layout RegisteredLayout, deployment string, runner CommandRunner, registeredBackup ...*ExternalBackupRegistration) func(context.Context, TaskRecord) error {
	return func(ctx context.Context, task TaskRecord) error {
		if err := layout.validate(); err != nil {
			return err
		}
		expanded, err := inspectPackage(ctx, task.StagingPath)
		if err != nil {
			return fmt.Errorf("package preflight: %w", err)
		}
		var backup uint64
		for _, name := range []string{"config", "data"} {
			size, err := treeStorageSize(ctx, filepath.Join(layout.Root, name))
			if err != nil {
				return err
			}
			backup += size
		}
		// A Compose installation may keep SQLite outside its registered root
		// (for example in a host volume). Its snapshot is written into updater
		// state, so budget that additional file there. When it is under root,
		// the config/data tree budget already covers the source bytes and the
		// snapshot replaces the excluded source in the same budget.
		if layout.SQLiteDatabase != "" && !isWithin(layout.Root, layout.SQLiteDatabase) {
			info, err := os.Stat(layout.SQLiteDatabase)
			if err != nil {
				return fmt.Errorf("stat registered sqlite database: %w", err)
			}
			if !info.Mode().IsRegular() {
				return errors.New("registered sqlite database is not a regular file")
			}
			backup += allocatedStorage(uint64(info.Size()))
		}
		if len(registeredBackup) > 1 {
			return errors.New("multiple external backup registrations")
		}
		if len(registeredBackup) == 1 && registeredBackup[0] != nil {
			registration := registeredBackup[0]
			if err := validateExternalBackupRegistration(registration); err != nil {
				return err
			}
			if registration.MaxBackupBytes == 0 || registration.MaxBackupBytes > ^uint64(0)-backup-capacityReserve {
				return errors.New("external database backup has no bounded capacity evidence")
			}
			backup += registration.MaxBackupBytes
		}
		// Migration extraction and activation extraction can coexist. Round each
		// archive member to filesystem blocks and reserve metadata/headroom too.
		required := []spaceRequirement{
			{layout.State, expanded + backup + capacityReserve},
			{layout.Releases, 2*expanded + capacityReserve},
		}
		if deployment == "compose" {
			output, ok := runner.(CommandOutputRunner)
			if !ok {
				return errors.New("Docker storage probe is unavailable")
			}
			data, err := output.Output(ctx, "docker", "info", "--format", "{{.DockerRootDir}}")
			if err != nil {
				return fmt.Errorf("probe Docker storage: %w", err)
			}
			root := strings.TrimSpace(string(data))
			if !filepath.IsAbs(root) {
				return errors.New("Docker storage root must be an absolute local path")
			}
			// Saved layers, imported layers and writable layer need separate room.
			required = append(required, spaceRequirement{root, 3*expanded + capacityReserve})
		}
		return checkCapacity(required, probeFilesystemCapacity)
	}
}

func checkCapacity(required []spaceRequirement, probe func(string) (filesystemCapacity, error)) error {
	type budget struct {
		available, required uint64
		path                string
	}
	budgets := map[uint64]budget{}
	for _, r := range required {
		capacity, err := probe(r.Path)
		if err != nil {
			return fmt.Errorf("probe free space for %s: %w", r.Path, err)
		}
		if capacity.BlockSize > capacityBlockSize {
			return fmt.Errorf("filesystem allocation unit at %s exceeds the supported capacity budget", r.Path)
		}
		b, exists := budgets[capacity.Device]
		if !exists {
			b = budget{available: capacity.Available, path: r.Path}
		} else if capacity.Available < b.available {
			b.available = capacity.Available
		}
		if ^uint64(0)-b.required < r.Bytes {
			return errors.New("capacity requirement overflow")
		}
		b.required += r.Bytes
		budgets[capacity.Device] = b
	}
	for _, b := range budgets {
		if b.required > b.available {
			return fmt.Errorf("insufficient disk space at %s: need %d bytes, available %d", b.path, b.required, b.available)
		}
	}
	return nil
}

func treeStorageSize(ctx context.Context, root string) (uint64, error) {
	var total uint64
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if errors.Is(err, os.ErrNotExist) && path == root {
			return nil
		}
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("backup source contains a symlink: %s", path)
		}
		if info.Mode().IsRegular() {
			total += allocatedStorage(uint64(info.Size())) + capacityBlockSize
		} else if info.IsDir() {
			total += capacityBlockSize
		} else {
			return fmt.Errorf("unsupported backup source: %s", path)
		}
		return nil
	})
	return total, err
}

func inspectPackage(ctx context.Context, path string) (uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	var reader io.Reader = file
	if strings.HasSuffix(strings.ToLower(path), ".gz") || strings.HasSuffix(strings.ToLower(path), ".tgz") {
		gz, err := gzip.NewReader(file)
		if err != nil {
			return 0, err
		}
		defer gz.Close()
		reader = gz
	}
	limited := &io.LimitedReader{R: reader, N: (8 << 30) + (32 << 20)}
	tr := tar.NewReader(limited)
	seen := map[string]bool{}
	directories := map[string]bool{}
	var expanded, storage uint64
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 0, err
		}
		name, err := safeArchiveNameWithDirs(h.Name)
		if err != nil {
			return 0, err
		}
		components := strings.Split(filepath.ToSlash(name), "/")
		if len(name) > 4096 || len(components) > 64 {
			return 0, errors.New("package path exceeds supported length or depth")
		}
		for _, component := range components {
			if len(component) > 255 {
				return 0, errors.New("package path component exceeds filesystem limit")
			}
		}
		if _, exists := seen[name]; exists || len(seen) >= 10000 {
			return 0, errors.New("duplicate path or too many package entries")
		}
		if directories[name] && h.Typeflag != tar.TypeDir {
			return 0, fmt.Errorf("package file %q replaces a parent directory", name)
		}
		for parent := filepath.Dir(name); parent != "."; parent = filepath.Dir(parent) {
			if isDir, exists := seen[parent]; exists && !isDir {
				return 0, fmt.Errorf("package parent %q is a file", parent)
			}
			directories[parent] = true
			if len(directories) > 10000 {
				return 0, errors.New("package contains too many implicit directories")
			}
		}
		seen[name] = h.Typeflag == tar.TypeDir
		if h.Size < 0 || h.Size > 8<<30 || expanded > 8<<30-uint64(h.Size) {
			return 0, errors.New("package expanded size exceeds limit")
		}
		expanded += uint64(h.Size)
		switch h.Typeflag {
		case tar.TypeDir:
			directories[name] = true
			if len(directories) > 10000 {
				return 0, errors.New("package contains too many directories")
			}
		case tar.TypeReg, tar.TypeRegA:
			storage += allocatedStorage(uint64(h.Size)) + capacityBlockSize
			n, err := copyContext(ctx, tr, io.Discard)
			if err != nil {
				return 0, err
			}
			if n != h.Size {
				return 0, io.ErrUnexpectedEOF
			}
		default:
			return 0, fmt.Errorf("unsupported package entry %q", name)
		}
	}
	if len(seen) == 0 {
		return 0, errors.New("empty package")
	}
	// Drain through gzip EOF to validate its checksum and reject expansion beyond
	// the bound even when extra bytes follow the tar end markers.
	if _, err := copyContext(ctx, limited, io.Discard); err != nil {
		return 0, err
	}
	if limited.N == 0 {
		return 0, errors.New("package stream exceeds expanded size limit")
	}
	return storage + uint64(len(directories))*capacityBlockSize, nil
}

func allocatedStorage(bytes uint64) uint64 {
	return (bytes + capacityBlockSize - 1) / capacityBlockSize * capacityBlockSize
}
