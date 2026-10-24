package upgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxArtifactDownloadSize = int64(8 << 30)

// FetchSignedManifest downloads a signed manifest from the configured release
// source. It does not verify the signature; callers must pass the returned
// bytes to VerifySignedManifest before using any release metadata.
func FetchSignedManifest(ctx context.Context, client *http.Client, source string) ([]byte, error) {
	u, err := url.Parse(source)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return nil, errors.New("manifest source must be an HTTPS URL without credentials")
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	requestClient := *client
	originalHost := u.Host
	previousRedirects := client.CheckRedirect
	requestClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || req.URL.Host != originalHost || req.URL.User != nil {
			return errors.New("manifest redirect leaves the configured HTTPS host")
		}
		if previousRedirects != nil {
			return previousRedirects(req, via)
		}
		if len(via) >= 10 {
			return errors.New("too many manifest redirects")
		}
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create manifest request: %w", err)
	}
	resp, err := requestClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download manifest: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download manifest: unexpected HTTP status %s", resp.Status)
	}
	if resp.ContentLength > maxManifestEnvelopeSize {
		return nil, fmt.Errorf("manifest response exceeds %d bytes", maxManifestEnvelopeSize)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxManifestEnvelopeSize+1))
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	if len(data) == 0 || len(data) > maxManifestEnvelopeSize {
		return nil, fmt.Errorf("manifest response exceeds %d bytes", maxManifestEnvelopeSize)
	}
	return data, nil
}

// DownloadArtifact streams an HTTPS artifact into dst and verifies its exact
// declared length and SHA-256 digest. Callers should pass a newly-created
// staging file and discard it whenever this function returns an error.
func DownloadArtifact(ctx context.Context, client *http.Client, artifact Artifact, dst io.Writer) (int64, error) {
	if dst == nil {
		return 0, errors.New("artifact destination is required")
	}
	u, err := url.Parse(artifact.URL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return 0, errors.New("artifact URL must be an HTTPS URL without credentials")
	}
	if artifact.Size < 0 || artifact.Size > maxArtifactDownloadSize {
		return 0, fmt.Errorf("artifact size is outside the supported limit of %d bytes", maxArtifactDownloadSize)
	}
	if len(artifact.SHA256) != sha256.Size*2 {
		return 0, errors.New("artifact sha256 must be a 64-character hexadecimal digest")
	}
	if _, err := hex.DecodeString(artifact.SHA256); err != nil {
		return 0, errors.New("artifact sha256 is not hexadecimal")
	}
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Hour}
	}
	requestClient := *client
	originalHost := u.Host
	previousRedirects := client.CheckRedirect
	requestClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || req.URL.Host != originalHost || req.URL.User != nil {
			return errors.New("artifact redirect leaves the configured HTTPS host")
		}
		if previousRedirects != nil {
			return previousRedirects(req, via)
		}
		if len(via) >= 10 {
			return errors.New("too many artifact redirects")
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, fmt.Errorf("create artifact request: %w", err)
	}
	resp, err := requestClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("download artifact: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download artifact: unexpected HTTP status %s", resp.Status)
	}
	if resp.ContentLength >= 0 && resp.ContentLength != artifact.Size {
		return 0, fmt.Errorf("artifact content length %d does not match manifest size %d", resp.ContentLength, artifact.Size)
	}
	hasher := sha256.New()
	reader := io.LimitReader(resp.Body, artifact.Size+1)
	written, err := io.Copy(io.MultiWriter(dst, hasher), reader)
	if err != nil {
		return written, fmt.Errorf("read artifact: %w", err)
	}
	if written != artifact.Size {
		return written, fmt.Errorf("artifact bytes %d do not match manifest size %d", written, artifact.Size)
	}
	if !strings.EqualFold(hex.EncodeToString(hasher.Sum(nil)), artifact.SHA256) {
		return written, errors.New("artifact sha256 does not match manifest")
	}
	return written, nil
}
