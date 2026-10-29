package upgrade

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// LoadRegisteredVerifier binds privileged execution to administrator-installed
// public keys and an inbox, independently of business-process task metadata.
func LoadRegisteredVerifier(registrationPath, state string) (func(context.Context, TaskRecord) (TaskRecord, error), error) {
	registration, err := ReadRegistration(registrationPath)
	if err != nil {
		return nil, err
	}
	return NewRegisteredVerifier(registration, state)
}

// NewRegisteredVerifier binds verification to an already-read registration.
// Callers use this when one privileged helper constructs its adapter,
// preflight, socket policy, and verifier from a single trusted snapshot.
func NewRegisteredVerifier(registration Registration, state string) (func(context.Context, TaskRecord) (TaskRecord, error), error) {
	keys, err := RegisteredReleasePublicKeys(registration)
	if err != nil {
		return nil, err
	}
	inbox, err := registration.StagingInbox(state)
	if err != nil || !filepath.IsAbs(state) {
		return nil, errors.New("registered staging and state roots must be absolute")
	}
	state = filepath.Clean(state)
	target := Target{Deployment: registration.Deployment, OS: runtime.GOOS, Arch: runtime.GOARCH, UpdaterProtocol: UpdaterProtocolVersion}
	verified := filepath.Join(state, "verified")
	return func(ctx context.Context, task TaskRecord) (TaskRecord, error) {
		if len(keys) == 0 {
			return TaskRecord{}, errors.New("updater has no administrator-installed release public keys")
		}
		if !validTaskID(task.TaskID) || !validDigest(task.TargetDigest) {
			return TaskRecord{}, errors.New("invalid task identity")
		}
		path, err := filepath.Abs(task.StagingPath)
		if err != nil || (!isWithin(inbox, path) && !isWithin(verified, path)) {
			return TaskRecord{}, errors.New("task artifact is outside registered staging roots")
		}
		base := inbox
		if isWithin(verified, path) {
			base = verified
		}
		relative, err := filepath.Rel(base, path)
		if err != nil {
			return TaskRecord{}, err
		}
		resolvedBase, err := filepath.EvalSymlinks(base)
		if err != nil {
			return TaskRecord{}, err
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || resolved != filepath.Join(resolvedBase, relative) {
			return TaskRecord{}, errors.New("task artifact path contains a symlink")
		}
		metadata, err := os.Open(path + ".json")
		if err != nil {
			return TaskRecord{}, fmt.Errorf("read signed staging metadata: %w", err)
		}
		bytes, readErr := io.ReadAll(io.LimitReader(metadata, 2*maxManifestEnvelopeSize+1))
		_ = metadata.Close()
		if readErr != nil {
			return TaskRecord{}, readErr
		}
		if len(bytes) > 2*maxManifestEnvelopeSize {
			return TaskRecord{}, errors.New("staging metadata exceeds size limit")
		}
		var staged StagedArtifact
		if err := decodeStrictJSON(bytes, &staged); err != nil {
			return TaskRecord{}, err
		}
		manifest, err := VerifySignedManifest(staged.ManifestEnvelope, keys)
		if err != nil {
			return TaskRecord{}, fmt.Errorf("helper manifest verification: %w", err)
		}
		artifact, err := manifest.ArtifactFor(target)
		if err != nil {
			return TaskRecord{}, err
		}
		if !strings.EqualFold(task.TargetDigest, artifact.SHA256) {
			return TaskRecord{}, errors.New("task digest differs from signed release")
		}
		if task.Preflight == nil || !isSupportedVersion(task.Preflight.CurrentVersion) {
			return TaskRecord{}, errors.New("source release preflight is required before applying a package")
		}
		current := task.Preflight.CurrentVersion
		if manifest.Version == current || !IsVersionAtLeast(manifest.Version, current) {
			return TaskRecord{}, errors.New("normal upgrade requires a newer signed release; rollback uses a separate workflow")
		}
		if manifest.MinVersion != "" && !IsVersionAtLeast(current, manifest.MinVersion) {
			return TaskRecord{}, errors.New("installed source is below the signed minimum version")
		}
		if err := os.MkdirAll(verified, 0700); err != nil {
			return TaskRecord{}, err
		}
		source, err := os.Open(path)
		if err != nil {
			return TaskRecord{}, err
		}
		defer source.Close()
		info, err := source.Stat()
		if err != nil {
			return TaskRecord{}, err
		}
		if !info.Mode().IsRegular() || info.Size() != artifact.Size {
			return TaskRecord{}, errors.New("staged package size or file type differs from signed release")
		}
		destination, err := os.CreateTemp(verified, ".verified-*")
		if err != nil {
			return TaskRecord{}, err
		}
		defer func() { _ = destination.Close(); _ = os.Remove(destination.Name()) }()
		hash := sha256.New()
		count, err := copyContext(ctx, io.LimitReader(source, artifact.Size+1), io.MultiWriter(destination, hash))
		if err != nil {
			return TaskRecord{}, err
		}
		if count != artifact.Size || hex.EncodeToString(hash.Sum(nil)) != artifact.SHA256 {
			return TaskRecord{}, errors.New("helper artifact SHA-256 verification failed")
		}
		if err := destination.Sync(); err != nil {
			return TaskRecord{}, err
		}
		if err := destination.Close(); err != nil {
			return TaskRecord{}, err
		}
		final := filepath.Join(verified, task.TaskID+"-"+artifact.SHA256+"-"+artifact.Filename)
		if err := os.Rename(destination.Name(), final); err != nil {
			return TaskRecord{}, err
		}
		staged.Path, staged.SHA256, staged.ManifestEnvelope = final, artifact.SHA256, append([]byte(nil), staged.ManifestEnvelope...)
		if err := SaveStagedMetadata(final+".json", staged); err != nil {
			return TaskRecord{}, err
		}
		task.StagingPath, task.Version, task.BuildID = final, manifest.Version, manifest.BuildID
		task.Images = append([]ImageArtifact(nil), artifact.Images...)
		task.Compatibility = CloneCompatibility(manifest.Compatibility)
		return task, nil
	}, nil
}

// RegisteredReleasePublicKeys decodes the administrator-installed trust root
// once so all helper components use the same key material.
func RegisteredReleasePublicKeys(registration Registration) (map[string]ed25519.PublicKey, error) {
	keys := make(map[string]ed25519.PublicKey)
	for id, encoded := range registration.TrustedKeys {
		raw, err := base64.StdEncoding.DecodeString(encoded)
		if len(encoded) == 64 {
			raw, err = hex.DecodeString(encoded)
		}
		if err != nil || len(raw) != ed25519.PublicKeySize || strings.TrimSpace(id) == "" {
			return nil, errors.New("registered release public key is invalid")
		}
		keys[id] = ed25519.PublicKey(raw)
	}
	return keys, nil
}
