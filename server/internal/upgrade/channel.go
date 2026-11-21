package upgrade

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type ChannelManifest struct {
	SchemaVersion  int    `json:"schema_version"`
	Channel        string `json:"channel"`
	Sequence       uint64 `json:"sequence"`
	ReleaseID      string `json:"release_id"`
	PublishedAt    string `json:"published_at"`
	ExpiresAt      string `json:"expires_at"`
	ManifestURL    string `json:"manifest_url"`
	ManifestSHA256 string `json:"manifest_sha256"`
}

func VerifySignedChannel(envelope []byte, keys map[string]ed25519.PublicKey, now time.Time) (ChannelManifest, error) {
	payload, err := verifySignedPayload(envelope, keys)
	if err != nil {
		return ChannelManifest{}, err
	}
	var channel ChannelManifest
	if err := decodeStrictJSON(payload, &channel); err != nil {
		return ChannelManifest{}, fmt.Errorf("decode channel pointer: %w", err)
	}
	if channel.SchemaVersion != 1 || (channel.Channel != "stable" && channel.Channel != "beta") || channel.Sequence == 0 || !safePolicyToken(channel.ReleaseID) || !validDigest(channel.ManifestSHA256) {
		return ChannelManifest{}, errors.New("channel pointer identity or manifest digest is invalid")
	}
	published, err := time.Parse(time.RFC3339, channel.PublishedAt)
	if err != nil {
		return ChannelManifest{}, errors.New("channel publication time is required")
	}
	expires, err := time.Parse(time.RFC3339, channel.ExpiresAt)
	if err != nil || !expires.After(published) {
		return ChannelManifest{}, errors.New("channel expiry must follow its publication time")
	}
	if published.After(now.Add(5 * time.Minute)) {
		return ChannelManifest{}, errors.New("channel publication time is in the future; check the system clock")
	}
	if !now.Before(expires) {
		return ChannelManifest{}, errors.New("channel pointer has expired; check the release source and system clock")
	}
	endpoint, err := url.Parse(channel.ManifestURL)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.Fragment != "" {
		return ChannelManifest{}, errors.New("channel release manifest URL must use HTTPS without credentials")
	}
	return channel, nil
}

// FetchChannelRelease binds a current signed channel pointer to an immutable
// independently signed release. Sequence persistence belongs to the caller
// and must happen only after this function has verified both documents.
func FetchChannelRelease(ctx context.Context, client *http.Client, source string, envelope []byte, keys map[string]ed25519.PublicKey, now time.Time) (Manifest, ChannelManifest, error) {
	channel, err := VerifySignedChannel(envelope, keys, now)
	if err != nil {
		return Manifest{}, ChannelManifest{}, err
	}
	origin, err := url.Parse(source)
	if err != nil || origin.Scheme != "https" || origin.User != nil || origin.Host == "" {
		return Manifest{}, ChannelManifest{}, errors.New("channel source must use HTTPS without credentials")
	}
	endpoint, _ := url.Parse(channel.ManifestURL)
	if endpoint.Host != origin.Host {
		return Manifest{}, ChannelManifest{}, errors.New("channel release manifest leaves registered HTTPS origin")
	}
	release, err := FetchSignedManifest(ctx, client, channel.ManifestURL)
	if err != nil {
		return Manifest{}, ChannelManifest{}, err
	}
	digest := sha256.Sum256(release)
	if hex.EncodeToString(digest[:]) != channel.ManifestSHA256 {
		return Manifest{}, ChannelManifest{}, errors.New("release manifest differs from signed channel digest")
	}
	manifest, err := VerifySignedManifest(release, keys)
	if err != nil {
		return Manifest{}, ChannelManifest{}, err
	}
	if manifest.Channel != channel.Channel || manifest.ReleaseID != channel.ReleaseID {
		return Manifest{}, ChannelManifest{}, errors.New("release manifest identity differs from signed channel pointer")
	}
	return manifest, channel, nil
}

// safePolicyToken restricts channel identity fields to a conservative token
// alphabet. It is shared by the signed channel pointer validation.
func safePolicyToken(value string) bool {
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '.' && r != '-' && r != '_' {
			return false
		}
	}
	return value != ""
}
