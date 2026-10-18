package httpsconfig

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Request struct {
	Settings
	Certificate []byte
	PrivateKey  []byte
	SyncZLM     bool
}
type Result struct {
	ID        string    `json:"id"`
	State     string    `json:"state"`
	Nginx     string    `json:"nginx"`
	ZLM       string    `json:"zlm"`
	Error     string    `json:"error,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}
type Status struct {
	Supported        bool             `json:"supported"`
	Reason           string           `json:"reason,omitempty"`
	Settings         Settings         `json:"settings"`
	HTTPPort         int              `json:"httpPort"`
	Certificate      *CertificateInfo `json:"certificate,omitempty"`
	NginxFingerprint string           `json:"nginxFingerprint,omitempty"`
	ZLMFingerprint   string           `json:"zlmFingerprint,omitempty"`
	NginxHealthy     bool             `json:"nginxHealthy"`
	ProbeError       string           `json:"probeError,omitempty"`
	Operation        *Result          `json:"operation,omitempty"`
}
type Manager struct {
	root, platform string
	mu             sync.Mutex
	busy           bool
	last           *Result
	run            func(context.Context, string, ...string) error
	probe          func(context.Context, int, string, bool) error
	nginxProbe     func(context.Context, int, string, bool) error
	checkPort      func(int) error
}

func New(root, platform string) *Manager {
	m := &Manager{root: root, platform: platform, checkPort: checkPort}
	m.run = func(ctx context.Context, name string, args ...string) error {
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Dir = root
		// Control-script output may contain operational metadata. Only return a
		// fixed phase and exit status; never echo arbitrary configuration contents.
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("服务控制命令失败: %w", err)
		}
		return nil
	}
	m.probe = m.waitProbe
	m.nginxProbe = m.waitNginx
	return m
}
func (m *Manager) confPath() string {
	if m.platform == "windows" {
		return filepath.Join(m.root, "vendor/nginx/conf/nginx.conf")
	}
	return filepath.Join(m.root, "run/nginx.conf")
}
func (m *Manager) basePath() string { return filepath.Join(m.root, "run/nginx.base.conf") }
func (m *Manager) certPath() string { return filepath.Join(m.root, "vendor/nginx/conf/uvp.crt") }
func (m *Manager) keyPath() string  { return filepath.Join(m.root, "vendor/nginx/conf/uvp.key") }
func (m *Manager) zlmPEM() string {
	if m.platform == "windows" {
		return filepath.Join(m.root, "vendor/zlm/uvp-ssl.pem")
	}
	return filepath.Join(m.root, "data/secrets/zlm-ssl.pem")
}
func (m *Manager) settingsPath() string  { return filepath.Join(m.root, "data/https/settings.json") }
func (m *Manager) operationPath() string { return filepath.Join(m.root, "data/https/operation.json") }
func (m *Manager) recoveryPath() string  { return filepath.Join(m.root, "data/https/recovery.json") }
func (m *Manager) nginx() string {
	if m.platform == "windows" {
		return filepath.Join(m.root, "vendor/nginx/nginx.exe")
	}
	return filepath.Join(m.activeRoot(), "vendor/nginx/sbin/nginx")
}
func (m *Manager) Supported() error {
	if !filepath.IsAbs(m.root) || (m.platform != "linux" && m.platform != "windows") {
		return errors.New("仅支持由新版绿色安装包启动的内置 Nginx")
	}
	for _, path := range []string{m.basePath(), m.nginx()} {
		if _, err := os.Stat(path); err != nil {
			return errors.New("安装包缺少 HTTPS 管理所需的文件，请更新完整安装包")
		}
	}
	base, err := os.ReadFile(m.basePath())
	if err != nil {
		return err
	}
	_, _, _, err = section(string(base), tlsBegin, tlsEnd)
	return err
}
func (m *Manager) Settings() (Settings, error) {
	s, err := readSettings(m.settingsPath())
	if err == nil {
		return s, nil
	}
	if !os.IsNotExist(err) {
		return s, err
	}
	b, err := os.ReadFile(m.basePath())
	if err != nil {
		return s, err
	}
	matches := sslListen.FindStringSubmatch(string(b))
	if len(matches) == 0 {
		return s, errors.New("无法识别当前 HTTPS 端口")
	}
	// Capture the numeric port separately from the IPv6 prefix.
	p := regexp.MustCompile(`(\d+) ssl;`).FindStringSubmatch(matches[0])
	port, _ := strconv.Atoi(p[1])
	return Settings{true, port}, nil
}
func (m *Manager) httpPort() int {
	b, _ := os.ReadFile(m.basePath())
	_, _, block, _ := section(string(b), httpBegin, httpEnd)
	match := httpListen.FindStringSubmatch(block)
	if len(match) < 2 {
		return 0
	}
	p, _ := strconv.Atoi(match[1])
	return p
}
func checkPort(port int) error {
	if port < 1 || port > 65535 {
		return errors.New("端口必须在 1—65535 之间")
	}
	// Reserve neither listener: the final nginx reload and TLS probe are the
	// authoritative result if another process wins the race after this check.
	for _, network := range []string{"tcp4", "tcp6"} {
		host := "0.0.0.0"
		if network == "tcp6" {
			host = "::"
		}
		ln, err := net.Listen(network, net.JoinHostPort(host, strconv.Itoa(port)))
		if err != nil {
			if network == "tcp6" && strings.Contains(err.Error(), "address family not supported") {
				continue
			}
			return errors.New("端口不可绑定，可能已被占用或权限不足")
		}
		ln.Close()
	}
	return nil
}
func (m *Manager) CheckPort(port int) error {
	current, err := m.Settings()
	if err != nil {
		return err
	}
	if port == m.httpPort() {
		return errors.New("HTTPS 端口不能与 HTTP 端口相同")
	}
	if port == current.Port && current.Enabled {
		crt, _ := os.ReadFile(m.certPath())
		key, _ := os.ReadFile(m.keyPath())
		info, err := ValidateCertificate(crt, key)
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			fp, err := servedCertificate(ctx, port)
			if err == nil && fp == info.Fingerprint {
				return nil
			}
		}
	}
	return m.checkPort(port)
}
func servedCertificate(ctx context.Context, port int) (string, error) {
	d := tls.Dialer{NetDialer: &net.Dialer{Timeout: time.Second}, Config: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}} // Compare exact fingerprint instead of trusting an uploaded/self-signed CA.
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return "", err
	}
	defer conn.Close()
	return certificateInfo(conn.(*tls.Conn).ConnectionState().PeerCertificates[0]).Fingerprint, nil
}
func (m *Manager) waitProbe(ctx context.Context, port int, fingerprint string, tlsEnabled bool) error {
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		if tlsEnabled {
			fp, err := servedCertificate(ctx, port)
			if err == nil && fp == fingerprint {
				return nil
			}
		} else {
			if err := probeNginxHTTP(ctx, port, false, ""); err == nil {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("服务入口探测未通过")
		case <-tick.C:
		}
	}
}
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	status := Status{}
	if err := m.Supported(); err != nil {
		status.Reason = err.Error()
		return status
	}
	status.Supported = true
	s, err := m.Settings()
	if err != nil {
		status.ProbeError = err.Error()
		return status
	}
	status.Settings = s
	status.HTTPPort = m.httpPort()
	crt, _ := os.ReadFile(m.certPath())
	key, _ := os.ReadFile(m.keyPath())
	if info, err := ValidateCertificate(crt, key); err == nil {
		status.Certificate = &info
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if s.Enabled {
		status.NginxFingerprint, err = servedCertificate(ctx, s.Port)
		status.NginxHealthy = err == nil && status.Certificate != nil && status.NginxFingerprint == status.Certificate.Fingerprint
		if status.NginxHealthy {
			err = probeNginxHTTP(ctx, s.Port, true, status.NginxFingerprint)
			status.NginxHealthy = err == nil
		}
	} else {
		err = m.probe(ctx, status.HTTPPort, "", false)
		status.NginxHealthy = err == nil
	}
	if err != nil {
		status.ProbeError = "Nginx 入口探测失败"
	}
	ports := m.zlmPorts()
	if len(ports) > 0 {
		status.ZLMFingerprint, _ = servedCertificate(ctx, ports[0])
	}
	b, err := os.ReadFile(m.operationPath())
	if err == nil {
		var op Result
		if json.Unmarshal(b, &op) == nil {
			status.Operation = &op
		}
	}
	if m.last != nil {
		op := *m.last
		status.Operation = &op
	} else if status.Operation != nil && status.Operation.State == "running" && !m.busy {
		status.Operation.State = "recovery_required"
		status.Operation.Error = "上次 HTTPS 操作被中断，请检查当前证书及服务入口"
	}
	return status
}
func (m *Manager) zlmPorts() []int {
	b, _ := os.ReadFile(filepath.Join(m.activeRoot(), "vendor/zlm/config.ini"))
	section := ""
	var ports []int
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			section = strings.Trim(line, "[]\r ")
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 && ((section == "http" && strings.TrimSpace(parts[0]) == "sslport") || (section == "rtc" && strings.TrimSpace(parts[0]) == "signalingSslPort")) {
			if p, _ := strconv.Atoi(strings.TrimSpace(parts[1])); p > 0 {
				ports = append(ports, p)
			}
		}
	}
	return ports
}

func (m *Manager) activeRoot() string {
	if m.platform == "linux" {
		if active, err := filepath.EvalSymlinks(filepath.Join(m.root, "current")); err == nil {
			return active
		}
	}
	return m.root
}

// This endpoint is generated into the new server block. A successful TCP
// connect alone can still be the old HTTP redirect while nginx reloads.
func probeNginxHTTP(ctx context.Context, port int, secure bool, fingerprint string) error {
	scheme, mode := "http", "http"
	if secure {
		scheme, mode = "https", "https"
	}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, scheme+"://"+net.JoinHostPort("127.0.0.1", strconv.Itoa(port))+"/__uvp_https_probe", nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent || resp.Header.Get("X-UVP-HTTPS-Mode") != mode {
		return errors.New("Nginx 尚未应用新的管理入口配置")
	}
	if secure && (resp.TLS == nil || len(resp.TLS.PeerCertificates) == 0 || certificateInfo(resp.TLS.PeerCertificates[0]).Fingerprint != fingerprint) {
		return errors.New("Nginx 证书指纹尚未更新")
	}
	return nil
}
func (m *Manager) waitNginx(ctx context.Context, port int, fp string, secure bool) error {
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		if err := probeNginxHTTP(ctx, port, secure, fp); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("Nginx 新入口与证书探测未通过")
		case <-tick.C:
		}
	}
}
