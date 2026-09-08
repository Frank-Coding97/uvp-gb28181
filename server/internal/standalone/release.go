package standalone

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	releaseManifestFormatVersion = 1
	releaseSchemaMinSupported    = 1
	releaseSchemaMaxSupported    = 3

	releaseBackendPath = "backend/uvp-server.exe"
	releaseRedisPath   = "redis/redis-server.exe"
	releaseMediaPath   = "media/MediaServer.exe"
)

var errInvalidRelease = errors.New("standalone: invalid release")

// Release describes the verified runtime files selected by installDir/current.json.
// The loader only returns paths after every manifest entry has been checked.
type Release struct {
	Version      string `json:"version"`
	SourceCommit string `json:"source_commit"`
	ReleaseDir   string `json:"release_dir"`
	BackendExe   string `json:"backend_exe"`
	RedisExe     string `json:"redis_exe"`
	MediaExe     string `json:"media_exe"`
	WebDir       string `json:"web_dir"`
	ResourceDir  string `json:"resource_dir"`
}

type releaseManifest struct {
	FormatVersion int                   `json:"format_version"`
	Version       string                `json:"version"`
	SourceCommit  string                `json:"source_commit"`
	Files         []releaseManifestFile `json:"files"`
	SchemaMin     int                   `json:"schema_min"`
	SchemaMax     int                   `json:"schema_max"`
}

type releaseManifestFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// LoadRelease reads and verifies the immutable release selected by current.json.
// It does not create files, execute binaries, or fetch anything from a network.
func LoadRelease(installDir string) (Release, error) {
	root, err := releaseRoot(installDir)
	if err != nil {
		return Release{}, err
	}

	currentPath := filepath.Join(root, "current.json")
	if _, err := ensureReleaseChild(root, currentPath, false); err != nil {
		return Release{}, fmt.Errorf("release current pointer: %w", err)
	}
	currentRaw, err := os.ReadFile(currentPath)
	if err != nil {
		return Release{}, fmt.Errorf("read release current pointer: %w", err)
	}
	version, err := parseReleaseCurrent(currentRaw)
	if err != nil {
		return Release{}, fmt.Errorf("release current pointer: %w", err)
	}
	return loadReleaseVersion(root, version)
}

// LoadReleaseVersion reads and verifies a specific immutable release directory.
// It does not read or modify current.json, execute binaries, or fetch anything
// from a network.
func LoadReleaseVersion(installDir, version string) (Release, error) {
	root, err := releaseRoot(installDir)
	if err != nil {
		return Release{}, err
	}
	return loadReleaseVersion(root, version)
}

func releaseRoot(installDir string) (string, error) {
	root, err := cleanAbsolute(installDir)
	if err != nil {
		return "", fmt.Errorf("release install directory: %w", err)
	}
	if _, err := ensureReleaseDirectory(root); err != nil {
		return "", fmt.Errorf("release install directory: %w", err)
	}
	return root, nil
}

func loadReleaseVersion(root, version string) (Release, error) {
	if !validReleaseVersion(version) {
		return Release{}, fmt.Errorf("release version: %w", errInvalidRelease)
	}
	releasesRoot := filepath.Join(root, "releases")
	if _, err := ensureReleaseChild(root, releasesRoot, true); err != nil {
		return Release{}, fmt.Errorf("release root: %w", err)
	}
	releaseDir := filepath.Join(releasesRoot, version)
	if _, err := ensureReleaseChild(root, releaseDir, true); err != nil {
		return Release{}, fmt.Errorf("release directory: %w", err)
	}
	manifestPath := filepath.Join(releaseDir, "manifest.json")
	if _, err := ensureReleaseChild(releaseDir, manifestPath, false); err != nil {
		return Release{}, fmt.Errorf("release manifest: %w", err)
	}
	manifestRaw, err := os.ReadFile(manifestPath)
	if err != nil {
		return Release{}, fmt.Errorf("read release manifest: %w", err)
	}
	manifest, err := decodeReleaseManifest(manifestRaw)
	if err != nil {
		return Release{}, fmt.Errorf("release manifest: %w", err)
	}
	if err := validateReleaseManifest(manifest, version); err != nil {
		return Release{}, err
	}

	listedExact := make(map[string]struct{}, len(manifest.Files))
	listedFolded := make(map[string]struct{}, len(manifest.Files))
	for _, item := range manifest.Files {
		manifestPath, err := validateReleaseManifestPath(item.Path)
		if err != nil {
			return Release{}, fmt.Errorf("release manifest file %q: %w", item.Path, err)
		}
		folded := strings.ToLower(manifestPath)
		if _, exists := listedFolded[folded]; exists {
			return Release{}, fmt.Errorf("release manifest file %q: %w", item.Path, errInvalidRelease)
		}
		listedExact[manifestPath] = struct{}{}
		listedFolded[folded] = struct{}{}
		filePath, err := releasePath(releaseDir, manifestPath)
		if err != nil {
			return Release{}, fmt.Errorf("release manifest file %q: %w", item.Path, err)
		}
		if _, err := ensureReleaseChild(releaseDir, filePath, false); err != nil {
			return Release{}, fmt.Errorf("release manifest file %q: %w", item.Path, err)
		}
		actual, err := releaseFileSHA256(filePath)
		if err != nil {
			return Release{}, fmt.Errorf("hash release file %q: %w", item.Path, err)
		}
		if !strings.EqualFold(actual, item.SHA256) {
			return Release{}, fmt.Errorf("release file %q: %w", item.Path, errInvalidRelease)
		}
	}
	for _, required := range []string{releaseBackendPath, releaseRedisPath, releaseMediaPath} {
		if _, exists := listedExact[required]; !exists {
			return Release{}, fmt.Errorf("required release file %q: %w", required, errInvalidRelease)
		}
	}

	webDir := filepath.Join(releaseDir, "web")
	if _, err := ensureReleaseChild(releaseDir, webDir, true); err != nil {
		return Release{}, fmt.Errorf("release web directory: %w", err)
	}
	resourceDir := filepath.Join(releaseDir, "resource")
	if _, err := ensureReleaseChild(releaseDir, resourceDir, true); err != nil {
		return Release{}, fmt.Errorf("release resource directory: %w", err)
	}
	if err := rejectUnlistedRuntimeFiles(releaseDir, listedFolded); err != nil {
		return Release{}, err
	}

	return Release{
		Version:      version,
		SourceCommit: manifest.SourceCommit,
		ReleaseDir:   releaseDir,
		BackendExe:   filepath.Join(releaseDir, filepath.FromSlash(releaseBackendPath)),
		RedisExe:     filepath.Join(releaseDir, filepath.FromSlash(releaseRedisPath)),
		MediaExe:     filepath.Join(releaseDir, filepath.FromSlash(releaseMediaPath)),
		WebDir:       webDir,
		ResourceDir:  resourceDir,
	}, nil
}

func parseReleaseCurrent(raw []byte) (string, error) {
	fields, err := decodeStrictJSONObject(raw)
	if err != nil {
		return "", err
	}
	versionRaw, ok := fields["version"]
	if !ok || len(fields) != 1 {
		return "", errInvalidRelease
	}
	var version string
	if err := decodeReleaseJSON(versionRaw, &version); err != nil {
		return "", errInvalidRelease
	}
	if !validReleaseVersion(version) {
		return "", errInvalidRelease
	}
	return version, nil
}

func validateReleaseManifest(manifest releaseManifest, version string) error {
	if manifest.FormatVersion != releaseManifestFormatVersion || manifest.Version != version {
		return fmt.Errorf("release manifest metadata: %w", errInvalidRelease)
	}
	if strings.TrimSpace(manifest.SourceCommit) == "" {
		return fmt.Errorf("release manifest source commit: %w", errInvalidRelease)
	}
	if manifest.SchemaMin < releaseSchemaMinSupported || manifest.SchemaMax < releaseSchemaMaxSupported || manifest.SchemaMax > releaseSchemaMaxSupported || manifest.SchemaMin > manifest.SchemaMax {
		return fmt.Errorf("release manifest schema range: %w", errInvalidRelease)
	}
	if len(manifest.Files) == 0 {
		return fmt.Errorf("release manifest files: %w", errInvalidRelease)
	}
	return nil
}

func validReleaseVersion(version string) bool {
	if version == "" || version == "." || version == ".." {
		return false
	}
	if !validWindowsPathComponent(version) {
		return false
	}
	for index, r := range version {
		if index == 0 {
			if !isASCIIAlphaNumeric(r) {
				return false
			}
			continue
		}
		if !isASCIIAlphaNumeric(r) && r != '.' && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

func isASCIIAlphaNumeric(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

func validateReleaseManifestPath(value string) (string, error) {
	if value == "" || strings.IndexByte(value, 0) >= 0 || strings.ContainsRune(value, '\\') || strings.HasPrefix(value, "/") || isWindowsDrivePath(value) {
		return "", errInvalidRelease
	}
	if path.Clean(value) != value {
		return "", errInvalidRelease
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." || !validWindowsPathComponent(part) {
			return "", errInvalidRelease
		}
	}
	return value, nil
}

func isWindowsDrivePath(value string) bool {
	return len(value) >= 2 && isASCIIAlphaNumeric(rune(value[0])) && value[1] == ':'
}

func validWindowsPathComponent(value string) bool {
	return value != "" && strings.TrimRight(value, " .") == value && !strings.ContainsRune(value, ':') && !isWindowsReservedDeviceName(value)
}

func isWindowsReservedDeviceName(value string) bool {
	value = strings.TrimRight(value, " .")
	if dot := strings.IndexByte(value, '.'); dot >= 0 {
		value = value[:dot]
	}
	value = strings.ToUpper(value)
	switch value {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9", "COM¹", "COM²", "COM³", "LPT¹", "LPT²", "LPT³":
		return true
	default:
		return false
	}
}

func releasePath(root, manifestPath string) (string, error) {
	candidate := filepath.Join(root, filepath.FromSlash(manifestPath))
	if !isWithin(root, candidate) {
		return "", errInvalidRelease
	}
	return candidate, nil
}

func decodeReleaseJSON(raw []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errInvalidRelease
		}
		return err
	}
	return nil
}

func decodeReleaseManifest(raw []byte) (releaseManifest, error) {
	fields, err := decodeStrictJSONObject(raw)
	if err != nil {
		return releaseManifest{}, err
	}
	if err := requireExactJSONKeys(fields, "format_version", "version", "source_commit", "files", "schema_min", "schema_max"); err != nil {
		return releaseManifest{}, err
	}
	var manifest releaseManifest
	if err := decodeReleaseJSON(raw, &manifest); err != nil {
		return releaseManifest{}, err
	}

	var rawFiles []json.RawMessage
	if err := decodeReleaseJSON(fields["files"], &rawFiles); err != nil {
		return releaseManifest{}, err
	}
	manifest.Files = make([]releaseManifestFile, len(rawFiles))
	for index, rawFile := range rawFiles {
		fileFields, err := decodeStrictJSONObject(rawFile)
		if err != nil {
			return releaseManifest{}, err
		}
		if err := requireExactJSONKeys(fileFields, "path", "sha256"); err != nil {
			return releaseManifest{}, err
		}
		if err := decodeReleaseJSON(rawFile, &manifest.Files[index]); err != nil {
			return releaseManifest{}, err
		}
	}
	return manifest, nil
}

func requireExactJSONKeys(fields map[string]json.RawMessage, allowed ...string) error {
	allowedKeys := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowedKeys[key] = struct{}{}
	}
	for key := range fields {
		if _, ok := allowedKeys[key]; !ok {
			return errInvalidRelease
		}
	}
	return nil
}

func decodeStrictJSONObject(raw []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	start, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if delimiter, ok := start.(json.Delim); !ok || delimiter != '{' {
		return nil, errInvalidRelease
	}
	fields := make(map[string]json.RawMessage)
	caseFoldedFields := make(map[string]struct{})
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, errInvalidRelease
		}
		if _, exists := fields[key]; exists {
			return nil, errInvalidRelease
		}
		caseFolded := strings.ToLower(key)
		if _, exists := caseFoldedFields[caseFolded]; exists {
			return nil, errInvalidRelease
		}
		caseFoldedFields[caseFolded] = struct{}{}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		fields[key] = value
	}
	end, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if delimiter, ok := end.(json.Delim); !ok || delimiter != '}' {
		return nil, errInvalidRelease
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, errInvalidRelease
		}
		return nil, err
	}
	return fields, nil
}

func ensureReleaseDirectory(target string) (os.FileInfo, error) {
	return ensureReleaseTarget(target, true)
}

func ensureReleaseFile(target string) (os.FileInfo, error) {
	return ensureReleaseTarget(target, false)
}

func ensureReleaseTarget(target string, directory bool) (os.FileInfo, error) {
	clean := filepath.Clean(target)
	if !filepath.IsAbs(clean) {
		return nil, errInvalidRelease
	}
	info, err := os.Lstat(clean)
	if err != nil {
		return nil, err
	}
	if err := validateReleaseInfo(clean, info, directory); err != nil {
		return nil, err
	}
	return info, nil
}

func ensureReleaseChild(root, target string, directory bool) (os.FileInfo, error) {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	if !isWithin(root, target) {
		return nil, errInvalidRelease
	}
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return nil, errInvalidRelease
	}
	current := root
	if relative == "." {
		return ensureReleaseTarget(target, directory)
	}
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return nil, err
		}
		if err := validateReleaseInfo(current, info, current != target || directory); err != nil {
			return nil, err
		}
	}
	return os.Lstat(target)
}

func validateReleaseInfo(name string, info os.FileInfo, directory bool) error {
	if info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return fmt.Errorf("release path %q: %w", name, errInvalidRelease)
	}
	if directory {
		if !info.IsDir() {
			return fmt.Errorf("release path %q is not a directory: %w", name, errInvalidRelease)
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("release path %q is not a regular file: %w", name, errInvalidRelease)
	}
	return nil
}

func releaseFileSHA256(name string) (string, error) {
	file, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func rejectUnlistedRuntimeFiles(root string, listed map[string]struct{}) error {
	return walkReleaseTree(root, root, listed)
}

func walkReleaseTree(root, directory string, listed map[string]struct{}) error {
	if _, err := ensureReleaseDirectory(directory); err != nil {
		return fmt.Errorf("release tree directory %q: %w", directory, err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("read release tree directory %q: %w", directory, err)
	}
	for _, entry := range entries {
		fullPath := filepath.Join(directory, entry.Name())
		info, err := os.Lstat(fullPath)
		if err != nil {
			return fmt.Errorf("inspect release tree path %q: %w", fullPath, err)
		}
		if info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return fmt.Errorf("release tree path %q: %w", fullPath, errInvalidRelease)
		}
		if info.IsDir() {
			if err := walkReleaseTree(root, fullPath, listed); err != nil {
				return err
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("release tree path %q: %w", fullPath, errInvalidRelease)
		}
		relative, err := filepath.Rel(root, fullPath)
		if err != nil {
			return fmt.Errorf("release tree path %q: %w", fullPath, errInvalidRelease)
		}
		relative = filepath.ToSlash(relative)
		extension := strings.ToLower(filepath.Ext(entry.Name()))
		if extension == ".exe" || extension == ".dll" {
			if _, exists := listed[strings.ToLower(relative)]; !exists {
				return fmt.Errorf("unlisted runtime file %q: %w", relative, errInvalidRelease)
			}
		}
	}
	return nil
}
