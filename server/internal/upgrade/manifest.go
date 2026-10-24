package upgrade

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const maxManifestEnvelopeSize = 1 << 20

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var sha256ReferencePattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-rc\.(0|[1-9][0-9]*))?$`)

type SignedManifest struct {
	Payload    string              `json:"payload"`
	Signatures []ManifestSignature `json:"signatures"`
}

type ManifestSignature struct {
	KeyID     string `json:"key_id"`
	Signature string `json:"signature"`
}

// CompatibilityDeclaration is signed release metadata used exclusively by
// unattended upgrades. A missing declaration keeps a release eligible for
// administrator-confirmed upgrades but never authorizes automatic service
// interruption.
type CompatibilityDeclaration struct {
	SchemaVersion    int                    `json:"schema_version"`
	AutomaticAllowed bool                   `json:"automatic_allowed"`
	SourceBuilds     []ReleaseIdentity      `json:"source_builds,omitempty"`
	Database         DatabaseCompatibility  `json:"database,omitempty"`
	Rollback         *RollbackCompatibility `json:"rollback,omitempty"`
}

type ReleaseIdentity struct {
	Version string `json:"version"`
	BuildID string `json:"build_id"`
}

type DatabaseCompatibility struct {
	Dialect                       string   `json:"dialect,omitempty"`
	AllowedSourceMigrationDigests []string `json:"allowed_source_migration_digests,omitempty"`
	TargetMigrationDigest         string   `json:"target_migration_digest,omitempty"`
}

type RollbackCompatibility struct {
	AutomaticAllowed       bool `json:"automatic_allowed"`
	DatabaseCompatible     bool `json:"database_compatible"`
	DependenciesCompatible bool `json:"dependencies_compatible"`
}

type Manifest struct {
	SchemaVersion      int                       `json:"schema_version"`
	Channel            string                    `json:"channel"`
	Sequence           uint64                    `json:"sequence"`
	ReleaseID          string                    `json:"release_id"`
	Version            string                    `json:"version"`
	BuildID            string                    `json:"build_id"`
	PublishedAt        string                    `json:"published_at,omitempty"`
	ReleaseNotes       string                    `json:"release_notes,omitempty"`
	MinVersion         string                    `json:"min_version,omitempty"`
	MinUpdaterProtocol int                       `json:"min_updater_protocol"`
	Artifacts          []Artifact                `json:"artifacts"`
	Compatibility      *CompatibilityDeclaration `json:"compatibility,omitempty"`
	// Envelope is the exact signed JSON envelope that produced this manifest.
	// It is deliberately excluded from the payload JSON: callers persist it
	// beside a staged artifact so the privileged updater can verify the same
	// signature independently before it trusts any task metadata.
	Envelope []byte `json:"-"`
}

type Artifact struct {
	Deployment string          `json:"deployment"`
	OS         string          `json:"os"`
	Arch       string          `json:"arch"`
	Filename   string          `json:"filename"`
	URL        string          `json:"url"`
	Size       int64           `json:"size"`
	SHA256     string          `json:"sha256"`
	Images     []ImageArtifact `json:"images,omitempty"`
}

// ImageArtifact describes an image embedded in a Compose release artifact.
// The archive path is relative to the extracted artifact root; digest and
// config_id are independently checked after docker load when present.
type ImageArtifact struct {
	Reference string `json:"reference"`
	Archive   string `json:"archive"`
	Digest    string `json:"digest,omitempty"`
	ConfigID  string `json:"config_id,omitempty"`
}

// Target describes the installed environment that a release must support.
// The updater protocol is deliberately part of the target check so an older
// helper cannot start an upgrade it does not understand.
type Target struct {
	Deployment      string
	OS              string
	Arch            string
	UpdaterProtocol int
}

// ArtifactFor returns the single artifact that can be applied to target.
func (m Manifest) ArtifactFor(target Target) (Artifact, error) {
	if target.UpdaterProtocol < m.MinUpdaterProtocol {
		return Artifact{}, fmt.Errorf("updater protocol %d is below manifest minimum %d", target.UpdaterProtocol, m.MinUpdaterProtocol)
	}
	var match *Artifact
	for i := range m.Artifacts {
		artifact := &m.Artifacts[i]
		if artifact.Deployment != target.Deployment || artifact.OS != target.OS || artifact.Arch != target.Arch {
			continue
		}
		if match != nil {
			return Artifact{}, fmt.Errorf("manifest contains duplicate artifact for %s/%s/%s", target.Deployment, target.OS, target.Arch)
		}
		match = artifact
	}
	if match == nil {
		return Artifact{}, fmt.Errorf("manifest has no artifact for %s/%s/%s", target.Deployment, target.OS, target.Arch)
	}
	return *match, nil
}

// VerifySignedManifest verifies the exact UTF-8 payload bytes covered by an
// Ed25519 signature, then strictly decodes and validates the release manifest.
func VerifySignedManifest(envelope []byte, trustedKeys map[string]ed25519.PublicKey) (Manifest, error) {
	payload, err := verifySignedPayload(envelope, trustedKeys)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := decodeStrictJSON(payload, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("invalid manifest payload: %w", err)
	}
	if err := validateManifest(manifest); err != nil {
		return Manifest{}, err
	}
	manifest.Envelope = bytes.Clone(envelope)
	return manifest, nil
}

func verifySignedPayload(envelope []byte, trustedKeys map[string]ed25519.PublicKey) ([]byte, error) {
	var signed SignedManifest
	if len(envelope) == 0 || len(envelope) > maxManifestEnvelopeSize {
		return nil, fmt.Errorf("manifest envelope exceeds %d bytes", maxManifestEnvelopeSize)
	}
	if err := decodeStrictJSON(envelope, &signed); err != nil {
		return nil, fmt.Errorf("invalid manifest envelope: %w", err)
	}
	if signed.Payload == "" || len(signed.Signatures) == 0 {
		return nil, errors.New("manifest payload and signatures are required")
	}
	payload, err := base64.StdEncoding.DecodeString(signed.Payload)
	if err != nil || len(payload) == 0 || len(payload) > maxManifestEnvelopeSize {
		return nil, errors.New("manifest payload is not valid base64")
	}
	var verified bool
	for _, signature := range signed.Signatures {
		key := trustedKeys[signature.KeyID]
		if len(key) != ed25519.PublicKeySize {
			continue
		}
		sig, decodeErr := base64.StdEncoding.DecodeString(signature.Signature)
		if decodeErr == nil && ed25519.Verify(key, payload, sig) {
			verified = true
			break
		}
	}
	if !verified {
		return nil, errors.New("manifest signature is not trusted")
	}
	return payload, nil
}

func validateManifest(manifest Manifest) error {
	if manifest.SchemaVersion != 1 {
		return fmt.Errorf("unsupported manifest schema version %d", manifest.SchemaVersion)
	}
	if manifest.Channel == "" || manifest.ReleaseID == "" || manifest.Version == "" || manifest.BuildID == "" {
		return errors.New("manifest identity fields are required")
	}
	if !isSupportedVersion(manifest.Version) {
		return fmt.Errorf("manifest version %q is invalid", manifest.Version)
	}
	if manifest.Sequence == 0 {
		return errors.New("manifest sequence must be positive")
	}
	if manifest.MinUpdaterProtocol < 1 {
		return errors.New("manifest min_updater_protocol must be positive")
	}
	if manifest.PublishedAt != "" {
		if _, err := time.Parse(time.RFC3339, manifest.PublishedAt); err != nil {
			return fmt.Errorf("manifest published_at is invalid: %w", err)
		}
	}
	if manifest.ReleaseNotes != "" && len(manifest.ReleaseNotes) > 256<<10 {
		return errors.New("manifest release_notes exceed 256 KiB")
	}
	if manifest.MinVersion != "" && !isSupportedVersion(manifest.MinVersion) {
		return fmt.Errorf("manifest min_version %q is invalid", manifest.MinVersion)
	}
	if err := validateCompatibilityDeclaration(manifest.Compatibility); err != nil {
		return err
	}
	if len(manifest.Artifacts) == 0 {
		return errors.New("manifest must contain at least one artifact")
	}
	selectors := make(map[string]struct{}, len(manifest.Artifacts))
	for i, artifact := range manifest.Artifacts {
		if artifact.Deployment == "" || artifact.OS == "" || artifact.Arch == "" || artifact.Filename == "" || artifact.Size < 0 {
			return fmt.Errorf("artifact %d has incomplete identity or size", i)
		}
		if filepath.Base(artifact.Filename) != artifact.Filename || artifact.Filename == "." || artifact.Filename == ".." || strings.ContainsAny(artifact.Filename, `/\\`) {
			return fmt.Errorf("artifact %d filename must be a single safe path component", i)
		}
		if !sha256Pattern.MatchString(artifact.SHA256) {
			return fmt.Errorf("artifact %d has invalid sha256", i)
		}
		if _, err := hex.DecodeString(artifact.SHA256); err != nil {
			return fmt.Errorf("artifact %d has invalid sha256: %w", i, err)
		}
		selector := artifact.Deployment + "\x00" + artifact.OS + "\x00" + artifact.Arch
		if _, exists := selectors[selector]; exists {
			return fmt.Errorf("artifact %d duplicates deployment, OS and architecture", i)
		}
		selectors[selector] = struct{}{}
		u, err := url.Parse(artifact.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return fmt.Errorf("artifact %d URL must be an HTTPS URL without credentials", i)
		}
		imageReferences := make(map[string]struct{}, len(artifact.Images))
		imageArchives := make(map[string]struct{}, len(artifact.Images))
		for imageIndex, image := range artifact.Images {
			if artifact.Deployment != "compose" {
				return fmt.Errorf("artifact %d image metadata is only valid for compose deployments", i)
			}
			if strings.TrimSpace(image.Reference) == "" || strings.TrimSpace(image.Archive) == "" {
				return fmt.Errorf("artifact %d image %d identity is incomplete", i, imageIndex)
			}
			if _, exists := imageReferences[image.Reference]; exists {
				return fmt.Errorf("artifact %d image %d duplicates an image reference", i, imageIndex)
			}
			imageReferences[image.Reference] = struct{}{}
			if _, err := safeArchiveNameWithDirs(image.Archive); err != nil {
				return fmt.Errorf("artifact %d image %d archive is unsafe: %w", i, imageIndex, err)
			}
			if _, exists := imageArchives[image.Archive]; exists {
				return fmt.Errorf("artifact %d image %d duplicates an image archive", i, imageIndex)
			}
			imageArchives[image.Archive] = struct{}{}
			if image.Digest != "" && !sha256ReferencePattern.MatchString(image.Digest) {
				return fmt.Errorf("artifact %d image %d digest must be sha256", i, imageIndex)
			}
			if image.ConfigID != "" && !sha256ReferencePattern.MatchString(image.ConfigID) {
				return fmt.Errorf("artifact %d image %d config_id must be sha256", i, imageIndex)
			}
		}
	}
	return nil
}

func validateCompatibilityDeclaration(declaration *CompatibilityDeclaration) error {
	if declaration == nil {
		return nil
	}
	if declaration.SchemaVersion != 1 {
		return fmt.Errorf("unsupported compatibility schema version %d", declaration.SchemaVersion)
	}
	identities := make(map[string]struct{}, len(declaration.SourceBuilds))
	for index, source := range declaration.SourceBuilds {
		if !isSupportedVersion(source.Version) || strings.TrimSpace(source.BuildID) == "" {
			return fmt.Errorf("compatibility source build %d is invalid", index)
		}
		key := source.Version + "\x00" + source.BuildID
		if _, exists := identities[key]; exists {
			return fmt.Errorf("compatibility source build %d is duplicated", index)
		}
		identities[key] = struct{}{}
	}
	if databaseCompatibilitySpecified(declaration.Database) {
		if err := validateDatabaseCompatibility(declaration.Database); err != nil {
			return err
		}
	}
	if !declaration.AutomaticAllowed {
		return nil
	}
	if len(declaration.SourceBuilds) == 0 {
		return errors.New("automatic compatibility requires at least one source build")
	}
	if !databaseCompatibilitySpecified(declaration.Database) {
		return errors.New("automatic compatibility requires database compatibility")
	}
	return nil
}

func databaseCompatibilitySpecified(database DatabaseCompatibility) bool {
	return database.Dialect != "" || len(database.AllowedSourceMigrationDigests) != 0 || database.TargetMigrationDigest != ""
}

func validateDatabaseCompatibility(database DatabaseCompatibility) error {
	if normalized := NormalizeDatabaseDialect(database.Dialect); normalized == "" || database.Dialect != normalized {
		return fmt.Errorf("compatibility database dialect %q is unsupported", database.Dialect)
	}
	if len(database.AllowedSourceMigrationDigests) == 0 {
		return errors.New("compatibility database requires allowed source migration digests")
	}
	digests := make(map[string]struct{}, len(database.AllowedSourceMigrationDigests))
	for index, digest := range database.AllowedSourceMigrationDigests {
		if !sha256Pattern.MatchString(digest) {
			return fmt.Errorf("compatibility source migration digest %d is invalid", index)
		}
		if _, exists := digests[digest]; exists {
			return fmt.Errorf("compatibility source migration digest %d is duplicated", index)
		}
		digests[digest] = struct{}{}
	}
	if !sha256Pattern.MatchString(database.TargetMigrationDigest) {
		return errors.New("compatibility target migration digest is invalid")
	}
	return nil
}

// NormalizeDatabaseDialect produces the identity stored in preflight and
// signed compatibility declarations. The database configuration accepts the
// common postgres alias, while release metadata has one canonical spelling.
func NormalizeDatabaseDialect(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "mysql", "postgresql", "sqlite", "sqlserver":
		return strings.ToLower(strings.TrimSpace(value))
	case "postgres":
		return "postgresql"
	default:
		return ""
	}
}

// AllowsAutomaticUpgrade proves that an unattended upgrade matches the exact
// source build and database state declared by its signer.
func (m Manifest) AllowsAutomaticUpgrade(preflight PreflightSnapshot) error {
	return m.Compatibility.AllowsAutomatic(preflight)
}

func (declaration *CompatibilityDeclaration) AllowsAutomatic(preflight PreflightSnapshot) error {
	if declaration == nil {
		return errors.New("release has no automatic compatibility declaration")
	}
	if err := validateCompatibilityDeclaration(declaration); err != nil {
		return err
	}
	if !declaration.AutomaticAllowed {
		return errors.New("release does not allow automatic upgrade")
	}
	matchedSource := false
	for _, source := range declaration.SourceBuilds {
		if source.Version == preflight.CurrentVersion && source.BuildID == preflight.CurrentBuildID {
			matchedSource = true
			break
		}
	}
	if !matchedSource {
		return errors.New("current version and build are not declared for automatic upgrade")
	}
	if actual := NormalizeDatabaseDialect(preflight.DatabaseDialect); actual == "" || actual != declaration.Database.Dialect {
		return errors.New("current database dialect is not declared for automatic upgrade")
	}
	for _, digest := range declaration.Database.AllowedSourceMigrationDigests {
		if digest == preflight.MigrationDigest {
			return nil
		}
	}
	return errors.New("current migration state is not declared for automatic upgrade")
}

// AllowsAutomaticRollback requires an explicit signer statement covering both
// database and package-managed dependency compatibility.
func (declaration *CompatibilityDeclaration) AllowsAutomaticRollback() bool {
	return declaration != nil && declaration.AutomaticAllowed && declaration.Rollback != nil && declaration.Rollback.AutomaticAllowed && declaration.Rollback.DatabaseCompatible && declaration.Rollback.DependenciesCompatible
}

// TargetMigrationMatches reports whether a post-migration database state is
// exactly the state declared by the release signer.
func (declaration *CompatibilityDeclaration) TargetMigrationMatches(dialect, digest string) bool {
	return declaration != nil && NormalizeDatabaseDialect(dialect) != "" && NormalizeDatabaseDialect(dialect) == declaration.Database.Dialect && digest != "" && digest == declaration.Database.TargetMigrationDigest
}

// CloneCompatibility returns a deep copy suitable for metadata persisted
// across the business process and privileged updater boundary.
func CloneCompatibility(declaration *CompatibilityDeclaration) *CompatibilityDeclaration {
	if declaration == nil {
		return nil
	}
	cloned := *declaration
	cloned.SourceBuilds = append([]ReleaseIdentity(nil), declaration.SourceBuilds...)
	cloned.Database.AllowedSourceMigrationDigests = append([]string(nil), declaration.Database.AllowedSourceMigrationDigests...)
	if declaration.Rollback != nil {
		rollback := *declaration.Rollback
		cloned.Rollback = &rollback
	}
	return &cloned
}

func CompatibilityEqual(left, right *CompatibilityDeclaration) bool {
	if left == nil || right == nil {
		return left == right
	}
	if left.SchemaVersion != right.SchemaVersion || left.AutomaticAllowed != right.AutomaticAllowed || len(left.SourceBuilds) != len(right.SourceBuilds) || left.Database.Dialect != right.Database.Dialect || left.Database.TargetMigrationDigest != right.Database.TargetMigrationDigest || len(left.Database.AllowedSourceMigrationDigests) != len(right.Database.AllowedSourceMigrationDigests) {
		return false
	}
	for index := range left.SourceBuilds {
		if left.SourceBuilds[index] != right.SourceBuilds[index] {
			return false
		}
	}
	for index := range left.Database.AllowedSourceMigrationDigests {
		if left.Database.AllowedSourceMigrationDigests[index] != right.Database.AllowedSourceMigrationDigests[index] {
			return false
		}
	}
	if left.Rollback == nil || right.Rollback == nil {
		return left.Rollback == right.Rollback
	}
	return *left.Rollback == *right.Rollback
}

func decodeStrictJSON(data []byte, target any) error {
	if err := rejectDuplicateJSON(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func rejectDuplicateJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := scanJSONValue(decoder); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func scanJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	switch delimiter := token.(type) {
	case json.Delim:
		switch delimiter {
		case '{':
			seen := make(map[string]struct{})
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return errors.New("object key is not a string")
				}
				if _, exists := seen[key]; exists {
					return fmt.Errorf("duplicate JSON field %q", key)
				}
				seen[key] = struct{}{}
				if err := scanJSONValue(decoder); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err := scanJSONValue(decoder); err != nil {
					return err
				}
			}
		default:
			return errors.New("unexpected JSON delimiter")
		}
		end, err := decoder.Token()
		if err != nil {
			return err
		}
		if end != delimiter+2 { // '{'->'}', '['->']'
			return errors.New("mismatched JSON delimiter")
		}
	}
	return nil
}

// IsVersionAtLeast reports whether current is at least minimum for the
// supported x.y.z and x.y.z-rc.N product version forms.
func IsVersionAtLeast(current, minimum string) bool {
	// Invalid versions are never considered compatible.  Treating an invalid
	// value as an equal comparison result would allow malformed release
	// metadata to pass a minimum-version gate.
	if !isSupportedVersion(current) || !isSupportedVersion(minimum) {
		return false
	}
	return compareVersions(current, minimum) >= 0
}

func isSupportedVersion(value string) bool {
	return versionPattern.MatchString(value)
}

func compareVersions(left, right string) int {
	parse := func(value string) ([]int, bool, int) {
		matches := versionPattern.FindStringSubmatch(value)
		if matches == nil {
			return nil, false, 0
		}
		values := make([]int, 3)
		for i := range values {
			var parsed int
			if _, err := fmt.Sscanf(matches[i+1], "%d", &parsed); err != nil {
				return nil, false, 0
			}
			values[i] = parsed
		}
		if matches[4] == "" {
			return values, true, -1
		}
		var rc int
		if _, err := fmt.Sscanf(matches[4], "%d", &rc); err != nil {
			return nil, false, 0
		}
		return values, true, rc
	}
	lv, lok, lrc := parse(left)
	rv, rok, rrc := parse(right)
	if !lok || !rok {
		return 0
	}
	for i := range lv {
		if lv[i] != rv[i] {
			if lv[i] < rv[i] {
				return -1
			}
			return 1
		}
	}
	if lrc == rrc {
		return 0
	}
	if lrc == -1 {
		return 1
	}
	if rrc == -1 {
		return -1
	}
	if lrc < rrc {
		return -1
	}
	return 1
}
