package httpsconfig

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func material(t *testing.T, expiry time.Time) ([]byte, []byte) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	cert := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: expiry, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kd, _ := x509.MarshalPKCS8PrivateKey(key)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kd})
}
func TestValidateCertificate(t *testing.T) {
	crt, key := material(t, time.Now().Add(time.Hour))
	_, other := material(t, time.Now().Add(time.Hour))
	info, err := ValidateCertificate(crt, key)
	if err != nil || info.Fingerprint == "" {
		t.Fatalf("valid pair: %+v %v", info, err)
	}
	expired, _ := material(t, time.Now().Add(-time.Minute))
	for _, tc := range []struct{ crt, key []byte }{{crt, other}, {[]byte("bad"), key}, {expired, key}, {crt, nil}} {
		if _, err := ValidateCertificate(tc.crt, tc.key); err == nil {
			t.Fatal("invalid certificate accepted")
		}
	}
}
func TestRenderBothTemplates(t *testing.T) {
	for _, platform := range []string{"standalone", "standalone-windows"} {
		t.Run(platform, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("../../../deploy", platform, "nginx.conf.template"))
			if err != nil {
				t.Fatal(err)
			}
			base := strings.NewReplacer("@HTTPS_PORT@", "15500", "@PLAIN_HTTP_PORT@", "15501").Replace(string(raw))
			enabled, err := Render(base, Settings{Enabled: true, Port: 18443})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(enabled, "18443 ssl;") || !strings.Contains(enabled, "https://$host:18443$request_uri") {
				t.Fatal("enabled port not rendered")
			}
			disabled, err := Render(base, Settings{Enabled: false, Port: 18443})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(disabled, "listen      18443 ssl;") || strings.Contains(disabled, "return 301 https:") || strings.Contains(disabled, "ssl_certificate ") {
				t.Fatal("TLS remains enabled")
			}
			for _, route := range []string{"location /api/", "location /gb28181/", "location /public/uploads/", "location /static/", "try_files $uri $uri/ /index.html;"} {
				if !strings.Contains(disabled, route) {
					t.Fatalf("route lost: %s", route)
				}
			}
		})
	}
}
func fixture(t *testing.T) *Manager {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"run", "data/secrets", "vendor/nginx/conf", "vendor/nginx/sbin", "vendor/zlm"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := os.ReadFile("../../../deploy/standalone/nginx.conf.template")
	raw = []byte(strings.NewReplacer("@HTTPS_PORT@", "51000", "@PLAIN_HTTP_PORT@", "51001").Replace(string(raw)))
	for _, file := range []string{"run/nginx.base.conf", "run/nginx.conf"} {
		if err := os.WriteFile(filepath.Join(root, file), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	crt, key := material(t, time.Now().Add(time.Hour))
	os.WriteFile(filepath.Join(root, "vendor/nginx/conf/uvp.crt"), crt, 0600)
	os.WriteFile(filepath.Join(root, "vendor/nginx/conf/uvp.key"), key, 0600)
	os.WriteFile(filepath.Join(root, "data/secrets/zlm-ssl.pem"), append(append([]byte{}, key...), crt...), 0600)
	os.WriteFile(filepath.Join(root, "vendor/zlm/config.ini"), []byte("[http]\nsslport=51007\n[rtc]\nsignalingSslPort=51010\n"), 0600)
	m := New(root, "linux")
	m.run = func(context.Context, string, ...string) error { return nil }
	m.probe = func(context.Context, int, string, bool) error { return nil }
	m.nginxProbe = m.probe
	m.checkPort = func(int) error { return nil }
	return m
}
func TestApplyDoesNotSynchronizeUnlessSelected(t *testing.T) {
	m := fixture(t)
	before, _ := os.ReadFile(m.zlmPEM())
	crt, key := material(t, time.Now().Add(time.Hour))
	restarts := 0
	m.run = func(_ context.Context, name string, args ...string) error {
		if strings.Contains(strings.Join(args, " "), "restart-zlm") {
			restarts++
		}
		return nil
	}
	result := m.apply(context.Background(), Request{Settings: Settings{Enabled: false, Port: 51000}, Certificate: crt, PrivateKey: key})
	if result.Error != "" {
		t.Fatal(result.Error)
	}
	after, _ := os.ReadFile(m.zlmPEM())
	if string(before) != string(after) || restarts != 0 {
		t.Fatal("unselected ZLM was changed")
	}
	settings, err := m.Settings()
	if err != nil || settings.Enabled {
		t.Fatalf("settings not persisted: %+v %v", settings, err)
	}
	result = m.apply(context.Background(), Request{Settings: Settings{Enabled: false, Port: 51000}, Certificate: crt, PrivateKey: key, SyncZLM: true})
	if result.Error != "" || restarts != 1 {
		t.Fatalf("sync did not restart ZLM: %+v %d", result, restarts)
	}
	after, _ = os.ReadFile(m.zlmPEM())
	if string(before) == string(after) {
		t.Fatal("ZLM cert not updated")
	}
}
func TestNginxFailureRestoresMaterialAndSettings(t *testing.T) {
	m := fixture(t)
	original, _ := os.ReadFile(m.certPath())
	crt, key := material(t, time.Now().Add(time.Hour))
	m.run = func(_ context.Context, _ string, args ...string) error {
		if strings.Contains(strings.Join(args, " "), "-t") {
			return errors.New("invalid config")
		}
		return nil
	}
	result := m.apply(context.Background(), Request{Settings: Settings{Enabled: true, Port: 18443}, Certificate: crt, PrivateKey: key, SyncZLM: true})
	if result.Error == "" {
		t.Fatal("failure reported as success")
	}
	restored, _ := os.ReadFile(m.certPath())
	if string(original) != string(restored) {
		t.Fatal("certificate not restored")
	}
	if _, err := os.Stat(m.settingsPath()); !os.IsNotExist(err) {
		t.Fatal("failed settings persisted")
	}
}

func TestZLMProbesHTTPSAndSignalingWSS(t *testing.T) {
	m := fixture(t)
	os.WriteFile(filepath.Join(m.root, "vendor/zlm/config.ini"), []byte("[http]\nsslport=51007\n[rtc]\nsignalingSslPort=51010\n"), 0600)
	ports := m.zlmPorts()
	if len(ports) != 2 || ports[0] != 51007 || ports[1] != 51010 {
		t.Fatalf("HTTPS/WSS ports not both parsed: %v", ports)
	}
}

func TestHTTPProbeRejectsUnappliedRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://localhost:51000/")
		w.WriteHeader(http.StatusMovedPermanently)
	}))
	defer server.Close()
	port, _ := strconv.Atoi(strings.Split(server.Listener.Addr().String(), ":")[1])
	m := New(t.TempDir(), "linux")
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	if err := m.waitProbe(ctx, port, "", false); err == nil {
		t.Fatal("unchanged redirect accepted as HTTP configuration")
	}
}
func TestHTTPProbeAcceptsAppliedServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/__uvp_https_probe" {
			t.Error(r.URL.Path)
		}
		w.Header().Set("X-UVP-HTTPS-Mode", "http")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	port, _ := strconv.Atoi(strings.Split(server.Listener.Addr().String(), ":")[1])
	m := New(t.TempDir(), "linux")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := m.waitProbe(ctx, port, "", false); err != nil {
		t.Fatal(err)
	}
}
func TestExpiredCertificateDoesNotPreventDisablingHTTPS(t *testing.T) {
	m := fixture(t)
	crt, key := material(t, time.Now().Add(-time.Minute))
	os.WriteFile(m.certPath(), crt, 0600)
	os.WriteFile(m.keyPath(), key, 0600)
	result := m.apply(context.Background(), Request{Settings: Settings{Enabled: false, Port: 51000}})
	if result.Error != "" {
		t.Fatal(result.Error)
	}
}

func TestRenderedProbeIsValidNginxText(t *testing.T) {
	raw, err := os.ReadFile("../../../deploy/standalone/nginx.conf.template")
	if err != nil {
		t.Fatal(err)
	}
	base := strings.NewReplacer("@HTTPS_PORT@", "51000", "@PLAIN_HTTP_PORT@", "51001").Replace(string(raw))
	for _, enabled := range []bool{true, false} {
		rendered, err := Render(base, Settings{enabled, 51000})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(rendered, "\\n        location") {
			t.Fatal("literal newline escape in nginx configuration")
		}
		if !strings.Contains(rendered, "\n        location = /__uvp_https_probe") {
			t.Fatal("missing probe endpoint")
		}
	}
}

func TestStartupPreservesDisabledStateAndSeparateZLMCertificate(t *testing.T) {
	for _, platform := range []string{"linux", "windows"} {
		t.Run(platform, func(t *testing.T) {
			m := fixture(t)
			m.platform = platform
			oldZLM := []byte("original ZLM certificate")
			os.MkdirAll(filepath.Dir(m.zlmPEM()), 0700)
			os.WriteFile(m.zlmPEM(), oldZLM, 0600)
			raw, _ := os.ReadFile("../../../deploy/standalone/nginx.conf.template")
			base := []byte(strings.NewReplacer("@HTTPS_PORT@", "51000", "@PLAIN_HTTP_PORT@", "51001").Replace(string(raw)))
			if err := writeJSON(m.settingsPath(), Settings{false, 18443}); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				os.WriteFile(m.confPath(), base, 0600)
				if err := RenderStartup(m.root, platform); err != nil {
					t.Fatal(err)
				}
				conf, _ := os.ReadFile(m.confPath())
				if strings.Contains(string(conf), " ssl;") || !strings.Contains(string(conf), "X-UVP-HTTPS-Mode http") {
					t.Fatal("disabled state lost at startup")
				}
				pem, _ := os.ReadFile(m.zlmPEM())
				if string(pem) != string(oldZLM) {
					t.Fatal("startup replaced ZLM certificate")
				}
			}
		})
	}
}
func TestStartupDefaultsToPlainHTTPAndPersistsChoice(t *testing.T) {
	for _, platform := range []string{"linux", "windows"} {
		t.Run(platform, func(t *testing.T) {
			m := fixture(t)
			m.platform = platform
			// The platform script writes the substituted template before the
			// render step runs.
			if err := os.MkdirAll(filepath.Dir(m.confPath()), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(m.confPath(), mustRead(t, m.basePath()), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(m.settingsPath()); !os.IsNotExist(err) {
				t.Fatal("fixture must not ship saved HTTPS settings")
			}
			settings, err := m.Settings()
			if err != nil {
				t.Fatal(err)
			}
			if settings.Enabled {
				t.Fatal("fresh installation must default to plain HTTP")
			}
			if settings.Port != 51000 {
				t.Fatalf("default HTTPS port = %d, want 51000", settings.Port)
			}
			if err := RenderStartup(m.root, platform); err != nil {
				t.Fatal(err)
			}
			first, err := os.ReadFile(m.confPath())
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(first), " ssl;") || strings.Contains(string(first), "return 301 https:") {
				t.Fatal("startup rendered a TLS management entry by default")
			}
			if !strings.Contains(string(first), "X-UVP-HTTPS-Mode http") {
				t.Fatal("startup did not render the plain HTTP entry")
			}
			if _, err := os.Stat(m.settingsPath()); err != nil {
				t.Fatalf("default settings not persisted: %v", err)
			}
			// The second start reads back the rendered configuration; it must
			// neither fail nor silently flip the entry back to TLS.
			if err := RenderStartup(m.root, platform); err != nil {
				t.Fatalf("second startup failed: %v", err)
			}
			second, _ := os.ReadFile(m.confPath())
			if string(second) != string(first) {
				t.Fatal("second startup rendered a different configuration")
			}
			if saved, err := readSettings(m.settingsPath()); err != nil || saved.Enabled || saved.Port != 51000 {
				t.Fatalf("saved settings changed: %+v %v", saved, err)
			}
		})
	}
}

func TestStartupReusesBaseWhenConfigurationAlreadyRendered(t *testing.T) {
	m := fixture(t)
	rendered, err := Render(string(mustRead(t, m.confPath())), Settings{false, 51000})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.confPath(), []byte(rendered), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RenderStartup(m.root, "linux"); err != nil {
		t.Fatalf("startup on a rendered configuration: %v", err)
	}
	base := string(mustRead(t, m.basePath()))
	if !strings.Contains(base, httpBegin) || !strings.Contains(base, tlsBegin) {
		t.Fatal("rendered configuration replaced the pristine base")
	}
	if !strings.Contains(string(mustRead(t, m.confPath())), "X-UVP-HTTPS-Mode http") {
		t.Fatal("disabled entry lost after restart")
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestZLMFailurePreservesAppliedNginxAndRestoresOldPEM(t *testing.T) {
	m := fixture(t)
	old, _ := os.ReadFile(m.zlmPEM())
	crt, key := material(t, time.Now().Add(time.Hour))
	calls := 0
	m.run = func(_ context.Context, _ string, args ...string) error {
		if strings.Contains(strings.Join(args, " "), "restart-zlm") {
			calls++
			if calls == 1 {
				return errors.New("restart failed")
			}
		}
		return nil
	}
	result := m.apply(context.Background(), Request{Settings: Settings{false, 51000}, Certificate: crt, PrivateKey: key, SyncZLM: true})
	if result.State != "partial" || result.Nginx != "applied" || result.ZLM != "restored" || calls != 2 {
		t.Fatalf("wrong partial result: %+v restarts=%d", result, calls)
	}
	restored, _ := os.ReadFile(m.zlmPEM())
	if string(old) != string(restored) {
		t.Fatal("old PEM not restored")
	}
	nginx, _ := os.ReadFile(m.certPath())
	if string(nginx) != string(crt) {
		t.Fatal("Nginx successful material undone")
	}
}
func TestApplySerializesChangesAndDoesNotRestartWithoutUpload(t *testing.T) {
	m := fixture(t)
	os.WriteFile(m.nginx(), []byte("fixture"), 0700)
	entered, release := make(chan struct{}), make(chan struct{})
	m.run = func(_ context.Context, _ string, args ...string) error {
		if strings.Contains(strings.Join(args, " "), "-t") {
			close(entered)
			<-release
		}
		return nil
	}
	if _, err := m.Start(Request{Settings: Settings{false, 51000}, SyncZLM: true}); err == nil {
		t.Fatal("sync without upload accepted")
	}
	first, err := m.Start(Request{Settings: Settings{false, 51000}})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	if _, err := m.Start(Request{Settings: Settings{true, 18443}}); err == nil {
		t.Fatal("concurrent apply accepted")
	}
	close(release)
	for i := 0; i < 100; i++ {
		m.mu.Lock()
		done := !m.busy
		result := m.last
		m.mu.Unlock()
		if done {
			if result == nil || result.ID != first.ID || result.State != "succeeded" {
				t.Fatalf("unexpected result %+v", result)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("background apply did not finish")
}

func TestSyncRejectsMissingOldZLMMaterialBeforeChanges(t *testing.T) {
	m := fixture(t)
	os.Remove(m.zlmPEM())
	before, _ := os.ReadFile(m.certPath())
	crt, key := material(t, time.Now().Add(time.Hour))
	restarts := 0
	m.run = func(context.Context, string, ...string) error { restarts++; return errors.New("unavailable") }
	result := m.apply(context.Background(), Request{Settings: Settings{false, 51000}, Certificate: crt, PrivateKey: key, SyncZLM: true})
	if result.Error == "" || !strings.Contains(result.Error, "ZLM") || restarts != 0 {
		t.Fatalf("missing rollback material must fail preflight: %+v commands=%d", result, restarts)
	}
	after, _ := os.ReadFile(m.certPath())
	if string(before) != string(after) {
		t.Fatal("Nginx changed before ZLM preflight")
	}
}

func TestValidateRSAMaterialAndValidityDates(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	for _, tc := range []struct {
		name       string
		start, end time.Time
		valid      bool
	}{
		{"valid", time.Now().Add(-time.Hour), time.Now().Add(time.Hour), true},
		{"expired", time.Now().Add(-2 * time.Hour), time.Now().Add(-time.Hour), false},
		{"future", time.Now().Add(time.Hour), time.Now().Add(2 * time.Hour), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cert := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: tc.start, NotAfter: tc.end}
			der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
			if err != nil {
				t.Fatal(err)
			}
			crt := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
			_, err = ValidateCertificate(crt, keyPEM)
			if (err == nil) != tc.valid {
				t.Fatalf("validity validation: %v", err)
			}
		})
	}
}

func TestHTTPSProbeChecksServedFingerprint(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-UVP-HTTPS-Mode", "https")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	port := server.Listener.Addr().(*net.TCPAddr).Port
	fp := certificateInfo(server.Certificate()).Fingerprint
	if err := probeNginxHTTP(context.Background(), port, true, fp); err != nil {
		t.Fatal(err)
	}
	if err := probeNginxHTTP(context.Background(), port, true, "wrong"); err == nil {
		t.Fatal("wrong certificate accepted")
	}
}

func TestZLMRecoveryAcceptsExpiredOriginalCertificate(t *testing.T) {
	m := fixture(t)
	oldCrt, oldKey := material(t, time.Now().Add(-time.Hour))
	original := append(append([]byte{}, oldKey...), oldCrt...)
	os.WriteFile(m.zlmPEM(), original, 0600)
	crt, key := material(t, time.Now().Add(time.Hour))
	calls := 0
	m.run = func(_ context.Context, _ string, args ...string) error {
		if strings.Contains(strings.Join(args, " "), "restart-zlm") {
			calls++
			if calls == 1 {
				return errors.New("restart failed")
			}
		}
		return nil
	}
	result := m.apply(context.Background(), Request{Settings: Settings{false, 51000}, Certificate: crt, PrivateKey: key, SyncZLM: true})
	if result.State != "partial" || result.ZLM != "restored" || calls != 2 {
		t.Fatalf("expired previous material could not be restored: %+v calls=%d", result, calls)
	}
	restored, _ := os.ReadFile(m.zlmPEM())
	if string(restored) != string(original) {
		t.Fatal("original PEM changed")
	}
}

func TestNginxRecoveryFailureIsNotReportedAsRestored(t *testing.T) {
	m := fixture(t)
	m.nginxProbe = func(context.Context, int, string, bool) error { return errors.New("unreachable") }
	result := m.apply(context.Background(), Request{Settings: Settings{false, 51000}})
	if result.State != "recovery_required" || result.Nginx != "recovery_required" {
		t.Fatalf("unverified recovery reported as successful: %+v", result)
	}
	backup, err := os.ReadFile(filepath.Join(m.root, "data/https/recovery.json"))
	if err != nil || !strings.Contains(string(backup), "nginx") {
		t.Fatalf("recovery backup missing: %s %v", backup, err)
	}
}

func TestInterruptedOperationCannotOverwriteRecoveryMaterials(t *testing.T) {
	m := fixture(t)
	os.WriteFile(m.nginx(), []byte("fixture"), 0700)
	writeJSON(m.operationPath(), Result{State: "running"})
	if _, err := m.Start(Request{Settings: Settings{false, 51000}}); err == nil {
		t.Fatal("interrupted operation was overwritten")
	}
}
