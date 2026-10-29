package upgrade

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxArchiveBytes     int64 = 8 << 30
	maxArchiveEntries         = 8
	maxArchiveNameBytes       = 512
	maxArchiveExpanded  int64 = 8 << 30
)

// StagedArtifact is a verified artifact owned by the updater staging root.
// The path is never accepted from a caller; it is derived from the digest and
// sanitized filename after the archive has been fully verified.
type StagedArtifact struct {
	ReleaseNotes  string                    `json:"release_notes,omitempty"`
	PublishedAt   string                    `json:"published_at,omitempty"`
	Path          string                    `json:"path"`
	Filename      string                    `json:"filename"`
	Size          int64                     `json:"size"`
	SHA256        string                    `json:"sha256"`
	ReleaseID     string                    `json:"release_id"`
	Version       string                    `json:"version"`
	BuildID       string                    `json:"build_id"`
	Deployment    string                    `json:"deployment"`
	OS            string                    `json:"os"`
	Arch          string                    `json:"arch"`
	Images        []ImageArtifact           `json:"images,omitempty"`
	Compatibility *CompatibilityDeclaration `json:"compatibility,omitempty"`
	// ManifestEnvelope is the exact release envelope that was verified before
	// staging. It lets the privileged updater repeat verification without
	// trusting business-process metadata.
	ManifestEnvelope []byte `json:"manifest_envelope,omitempty"`
}

// ImportArchive verifies and stages one signed offline upgrade archive. The
// accepted container is tar or gzip-compressed tar with exactly one
// manifest.json and one regular-file artifact matching target.
func ImportArchive(ctx context.Context, input io.Reader, root string, trustedKeys map[string]ed25519.PublicKey, target Target) (StagedArtifact, error) {
	if input == nil {
		return StagedArtifact{}, errors.New("upgrade archive is required")
	}
	if root == "" {
		return StagedArtifact{}, errors.New("staging root is required")
	}
	if len(trustedKeys) == 0 {
		return StagedArtifact{}, errors.New("trusted release keys are required")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return StagedArtifact{}, fmt.Errorf("create staging root: %w", err)
	}
	limited := &countReader{r: io.LimitReader(input, maxArchiveBytes+1)}
	buffered := bufio.NewReaderSize(limited, 32*1024)
	var expanded io.Reader = buffered
	var gzipReader *gzip.Reader
	if header, err := buffered.Peek(2); err == nil && header[0] == 0x1f && header[1] == 0x8b {
		var gzipErr error
		gzipReader, gzipErr = gzip.NewReader(buffered)
		if gzipErr != nil {
			return StagedArtifact{}, fmt.Errorf("open upgrade gzip: %w", gzipErr)
		}
		defer gzipReader.Close()
		expanded = gzipReader
	}
	expandedCounter := &countReader{r: io.LimitReader(expanded, maxArchiveExpanded+1)}
	expanded = expandedCounter

	tmp, err := os.CreateTemp(root, ".artifact-*")
	if err != nil {
		return StagedArtifact{}, fmt.Errorf("create staging file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()

	reader := tar.NewReader(expanded)
	var manifestEnvelope []byte
	var artifactName string
	var artifactSize int64
	artifactHash := newHashWriter()
	seen := make(map[string]struct{})
	entries := 0
	for {
		if err := ctx.Err(); err != nil {
			return StagedArtifact{}, err
		}
		header, nextErr := reader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return StagedArtifact{}, fmt.Errorf("read upgrade archive: %w", nextErr)
		}
		entries++
		if entries > maxArchiveEntries {
			return StagedArtifact{}, errors.New("upgrade archive contains too many entries")
		}
		name, err := safeArchiveName(header.Name)
		if err != nil {
			return StagedArtifact{}, err
		}
		if _, exists := seen[name]; exists {
			return StagedArtifact{}, fmt.Errorf("upgrade archive contains duplicate entry %q", name)
		}
		seen[name] = struct{}{}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return StagedArtifact{}, fmt.Errorf("upgrade archive entry %q is not a regular file", name)
		}
		if header.Size < 0 || header.Size > maxArchiveExpanded {
			return StagedArtifact{}, fmt.Errorf("upgrade archive entry %q exceeds size limit", name)
		}
		switch name {
		case "manifest.json":
			if header.Size > maxManifestEnvelopeSize {
				return StagedArtifact{}, errors.New("upgrade manifest exceeds size limit")
			}
			manifestEnvelope, err = readEntry(ctx, reader, header.Size, maxManifestEnvelopeSize)
			if err != nil {
				return StagedArtifact{}, err
			}
		default:
			if artifactName != "" {
				return StagedArtifact{}, errors.New("upgrade archive contains more than one artifact")
			}
			artifactName = name
			artifactSize, err = copyEntry(ctx, reader, header.Size, tmp, artifactHash)
			if err != nil {
				return StagedArtifact{}, err
			}
		}
	}
	if _, err := copyContext(ctx, expanded, io.Discard); err != nil {
		return StagedArtifact{}, fmt.Errorf("finish upgrade archive: %w", err)
	}
	if limited.n > maxArchiveBytes || expandedCounter.n > maxArchiveExpanded {
		return StagedArtifact{}, errors.New("upgrade archive exceeds size limit")
	}
	if len(manifestEnvelope) == 0 || artifactName == "" {
		return StagedArtifact{}, errors.New("upgrade archive must contain manifest.json and one artifact")
	}
	manifest, err := VerifySignedManifest(manifestEnvelope, trustedKeys)
	if err != nil {
		return StagedArtifact{}, fmt.Errorf("verify upgrade manifest: %w", err)
	}
	artifact, err := manifest.ArtifactFor(target)
	if err != nil {
		return StagedArtifact{}, err
	}
	if artifact.Filename != artifactName || artifact.Size != artifactSize || !strings.EqualFold(artifact.SHA256, artifactHash.Sum()) {
		return StagedArtifact{}, errors.New("staged artifact does not match signed manifest")
	}
	if err := tmp.Sync(); err != nil {
		return StagedArtifact{}, fmt.Errorf("sync staged artifact: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return StagedArtifact{}, fmt.Errorf("close staged artifact: %w", err)
	}
	destination := filepath.Join(root, artifact.SHA256+"-"+artifactName)
	if err := os.Rename(tmpPath, destination); err != nil {
		return StagedArtifact{}, fmt.Errorf("commit staged artifact: %w", err)
	}
	if err := syncDirectory(root); err != nil {
		return StagedArtifact{}, fmt.Errorf("sync staging directory: %w", err)
	}
	return StagedArtifact{ReleaseNotes: manifest.ReleaseNotes, PublishedAt: manifest.PublishedAt, Path: destination, Filename: artifactName, Size: artifactSize, SHA256: artifact.SHA256, ReleaseID: manifest.ReleaseID, Version: manifest.Version, BuildID: manifest.BuildID, Deployment: artifact.Deployment, OS: artifact.OS, Arch: artifact.Arch, Images: append([]ImageArtifact(nil), artifact.Images...), Compatibility: CloneCompatibility(manifest.Compatibility), ManifestEnvelope: bytes.Clone(manifest.Envelope)}, nil
}

func safeArchiveName(name string) (string, error) {
	if name == "" || len(name) > maxArchiveNameBytes || filepath.IsAbs(name) || strings.ContainsAny(name, `/\\`) || name == "." || name == ".." {
		return "", fmt.Errorf("unsafe upgrade archive path %q", name)
	}
	return name, nil
}

func readEntry(ctx context.Context, r io.Reader, size, max int64) ([]byte, error) {
	if size < 0 || size > max {
		return nil, errors.New("upgrade archive entry exceeds limit")
	}
	var data bytes.Buffer
	if _, err := copyContext(ctx, io.LimitReader(r, size), &data); err != nil {
		return nil, fmt.Errorf("read upgrade manifest: %w", err)
	}
	if int64(data.Len()) != size {
		return nil, errors.New("upgrade manifest is truncated")
	}
	return data.Bytes(), nil
}

func copyEntry(ctx context.Context, r io.Reader, size int64, dst io.Writer, hasher io.Writer) (int64, error) {
	if size < 0 || size > maxArchiveExpanded {
		return 0, errors.New("upgrade archive entry exceeds limit")
	}
	written, err := copyContext(ctx, io.LimitReader(r, size), io.MultiWriter(dst, hasher))
	if err != nil {
		return written, fmt.Errorf("stage upgrade artifact: %w", err)
	}
	if written != size {
		return written, errors.New("upgrade archive artifact is truncated")
	}
	return written, nil
}

func copyContext(ctx context.Context, r io.Reader, dst io.Writer) (int64, error) {
	return io.Copy(dst, &contextReader{ctx: ctx, r: r})
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

type countReader struct {
	r io.Reader
	n int64
}

func (r *countReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.n += int64(n)
	return n, err
}

type hashWriter struct{ hash hash.Hash }

func newHashWriter() *hashWriter                  { return &hashWriter{hash: sha256.New()} }
func (h *hashWriter) Write(p []byte) (int, error) { return h.hash.Write(p) }
func (h *hashWriter) Sum() string                 { return hex.EncodeToString(h.hash.Sum(nil)) }

// SaveStagedMetadata persists metadata beside a staged artifact for later task
// lookup. It intentionally stores no caller-provided path or command.
func SaveStagedMetadata(path string, staged StagedArtifact) error {
	if path == "" || staged.Path == "" || staged.SHA256 == "" {
		return errors.New("staged metadata is incomplete")
	}
	data, err := json.Marshal(staged)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create staged metadata directory: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".metadata-*")
	if err != nil {
		return fmt.Errorf("create staged metadata: %w", err)
	}
	tmp := file.Name()
	defer os.Remove(tmp)
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("write staged metadata: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("sync staged metadata: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close staged metadata: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace staged metadata: %w", err)
	}
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		return fmt.Errorf("sync staged metadata directory: %w", err)
	}
	return nil
}
