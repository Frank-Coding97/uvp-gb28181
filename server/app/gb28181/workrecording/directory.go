package workrecording

import (
	"path"
	"strings"

	"github.com/google/uuid"
)

// Node paths are strings for the remote ZLM filesystem, never paths to open on
// the UVP host. Normalize Windows separators while preserving UNC roots.
func cleanNodePath(value string) string {
	value = strings.ReplaceAll(value, `\`, "/")
	unc := strings.HasPrefix(value, "//")
	value = path.Clean(value)
	if unc && !strings.HasPrefix(value, "//") {
		value = "/" + value
	}
	return value
}
func absoluteNodePath(value string) bool {
	return strings.HasPrefix(value, "/") || (len(value) > 3 && value[1] == ':' && value[2] == '/' && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')))
}
func WorkDirectory(configuredRoot, jobID string) (string, error) {
	parsed, err := uuid.Parse(jobID)
	if err != nil || parsed.String() != jobID || strings.TrimSpace(configuredRoot) == "" || strings.ContainsRune(configuredRoot, '\x00') {
		return "", ErrInvalidRequest
	}
	root := cleanNodePath(strings.TrimRight(configuredRoot, "/\\") + "/work-recordings/" + jobID)
	if len(root) > 1024 || strings.Count(root, "/work-recordings/") > 1 {
		return "", ErrInvalidRequest
	}
	return root, nil
}

// ResolvedWorkDirectory consumes getMP4RecordFile.rootPath from the target
// node. The response must resolve our exact job namespace to an absolute path;
// a node that ignores customized_path must fail before recording begins.
func ResolvedWorkDirectory(listingRoot, jobID string) (string, error) {
	parsed, err := uuid.Parse(jobID)
	if err != nil || parsed.String() != jobID {
		return "", ErrInvalidRequest
	}
	root := cleanNodePath(listingRoot)
	marker := "/work-recordings/" + jobID + "/"
	if !absoluteNodePath(root) || strings.Count(root, marker) != 1 || strings.Count(root, "/work-recordings/") != 1 || strings.ContainsRune(root, '\x00') {
		return "", ErrAttributionUnknown
	}
	end := strings.Index(root, marker) + len(marker) - 1
	result := root[:end]
	if len(result) > 1024 {
		return "", ErrAttributionUnknown
	}
	return result, nil
}
func ownsWorkDirectory(root, ownerID string) bool {
	if ownerID == "" || strings.ContainsAny(ownerID, "/\\") || ownerID == "." || ownerID == ".." {
		return false
	}
	normalized := cleanNodePath(root)
	return absoluteNodePath(normalized) && strings.HasSuffix(normalized, "/work-recordings/"+ownerID) && !strings.ContainsRune(normalized, '\x00')
}

// AbsoluteNodePath reports whether value is an absolute path on a recording
// node. Relative values are unusable for work recording: the node echoes them
// back relative, and ResolvedWorkDirectory refuses anything that is not
// absolute, so a relative root can never own a job directory.
func AbsoluteNodePath(value string) bool {
	return absoluteNodePath(cleanNodePath(value))
}

// DefaultRecordAppName is ZLM's built-in record app name. A node that never had
// record.appName written down still uses this value, so it is the correct
// assumption when the key is absent.
const DefaultRecordAppName = "record"

// RecordRootFromProbe recovers a node's record root from an uncustomized
// getMP4RecordFile answer.
//
// A node resolves such a probe to <root>/<record.appName>/<app>/<stream>/, so the
// root is that answer with the stream-specific tail removed. The tail is matched
// exactly instead of trimmed by position: a response that does not end in the app
// and stream we asked about tells us nothing about the root, and guessing here
// would place every work directory somewhere the node never agreed to.
func RecordRootFromProbe(probedRoot, appName, app, stream string) (string, error) {
	app = strings.TrimSpace(app)
	stream = strings.TrimSpace(stream)
	if app == "" || stream == "" || strings.ContainsAny(app+stream, "/\\") {
		return "", ErrInvalidRequest
	}
	appName = strings.TrimSpace(appName)
	if appName == "" {
		appName = DefaultRecordAppName
	}
	root := cleanNodePath(probedRoot)
	suffix := "/" + appName + "/" + app + "/" + stream
	if !strings.HasSuffix(root, suffix) {
		return "", ErrAttributionUnknown
	}
	resolved := strings.TrimSuffix(root, suffix)
	if resolved == "" || resolved == root || !absoluteNodePath(resolved) {
		return "", ErrAttributionUnknown
	}
	return resolved, nil
}
