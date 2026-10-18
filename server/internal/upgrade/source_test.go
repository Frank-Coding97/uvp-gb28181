package upgrade

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchSignedManifestRequiresHTTPSAndBoundsResponse(t *testing.T) {
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer httpServer.Close()
	if _, err := FetchSignedManifest(context.Background(), nil, httpServer.URL); err == nil {
		t.Fatal("HTTP manifest source was accepted")
	}

	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", maxManifestEnvelopeSize+1)))
	}))
	defer tlsServer.Close()
	client := tlsServer.Client()
	client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // test certificate only
	if _, err := FetchSignedManifest(context.Background(), client, tlsServer.URL); err == nil {
		t.Fatal("oversized manifest was accepted")
	}
}

func TestFetchSignedManifestRejectsCrossHostRedirect(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer target.Close()
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirect.Close()
	client := redirect.Client()
	client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // test certificates only
	if _, err := FetchSignedManifest(context.Background(), client, redirect.URL); err == nil {
		t.Fatal("cross-host redirect was accepted")
	}
}

func TestDownloadArtifactVerifiesSizeAndDigest(t *testing.T) {
	body := []byte("signed release archive")
	digest := sha256.Sum256(body)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer server.Close()
	client := server.Client()
	client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // test certificate only
	artifact := Artifact{URL: server.URL + "/release.tar", Size: int64(len(body)), SHA256: hex.EncodeToString(digest[:])}
	var destination bytes.Buffer
	written, err := DownloadArtifact(context.Background(), client, artifact, &destination)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if written != int64(len(body)) || !bytes.Equal(destination.Bytes(), body) {
		t.Fatalf("unexpected artifact: %d bytes %q", written, destination.Bytes())
	}

	artifact.SHA256 = strings.Repeat("0", 64)
	destination.Reset()
	if _, err := DownloadArtifact(context.Background(), client, artifact, &destination); err == nil {
		t.Fatal("digest mismatch was accepted")
	}
}

func TestDownloadArtifactRejectsTruncatedAndCrossHostResponses(t *testing.T) {
	body := []byte("short")
	truncated := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer truncated.Close()
	client := truncated.Client()
	client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // test certificate only
	digest := sha256.Sum256(body)
	artifact := Artifact{URL: truncated.URL + "/release.tar", Size: int64(len(body) + 1), SHA256: hex.EncodeToString(digest[:])}
	if _, err := DownloadArtifact(context.Background(), client, artifact, &bytes.Buffer{}); err == nil {
		t.Fatal("truncated artifact was accepted")
	}

	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer other.Close()
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusFound)
	}))
	defer redirect.Close()
	redirectClient := redirect.Client()
	redirectClient.Transport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // test certificates only
	artifact.URL = redirect.URL + "/release.tar"
	artifact.Size = int64(len(body))
	if _, err := DownloadArtifact(context.Background(), redirectClient, artifact, &bytes.Buffer{}); err == nil {
		t.Fatal("cross-host artifact redirect was accepted")
	}
}
