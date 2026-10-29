// Package httpsconfig manages only the reverse proxy and media server bundled
// with a standalone installation. No request can select paths or commands.
package httpsconfig

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const MaxMaterialBytes = 1 << 20

type Settings struct {
	Enabled bool `json:"enabled"`
	Port    int  `json:"port"`
}
type CertificateInfo struct {
	Subject     string    `json:"subject"`
	DNSNames    []string  `json:"dnsNames"`
	NotBefore   time.Time `json:"notBefore"`
	NotAfter    time.Time `json:"notAfter"`
	Fingerprint string    `json:"fingerprint"`
}

func certificateInfo(cert *x509.Certificate) CertificateInfo {
	sum := sha256.Sum256(cert.Raw)
	return CertificateInfo{cert.Subject.String(), cert.DNSNames, cert.NotBefore, cert.NotAfter, hex.EncodeToString(sum[:])}
}
func ValidateCertificate(crt, key []byte) (CertificateInfo, error) {
	return parseCertificate(crt, key, true)
}
func parseCertificate(crt, key []byte, checkDates bool) (CertificateInfo, error) {
	if len(crt) == 0 || len(key) == 0 || len(crt) > MaxMaterialBytes || len(key) > MaxMaterialBytes {
		return CertificateInfo{}, errors.New("请上传证书及私钥，每个文件不能超过 1 MiB")
	}
	pair, err := tls.X509KeyPair(crt, key)
	if err != nil {
		return CertificateInfo{}, errors.New("证书或未加密 PEM 私钥格式不正确，或两者不匹配")
	}
	var leaf *x509.Certificate
	for i, der := range pair.Certificate {
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			return CertificateInfo{}, errors.New("证书链包含无效证书")
		}
		if checkDates && (time.Now().Before(cert.NotBefore) || time.Now().After(cert.NotAfter)) {
			return CertificateInfo{}, errors.New("证书链包含已过期或尚未生效的证书")
		}
		if i == 0 {
			leaf = cert
		}
	}
	return certificateInfo(leaf), nil
}

const tlsBegin = "# UVP_HTTPS_BEGIN"
const tlsEnd = "# UVP_HTTPS_END"
const httpBegin = "# UVP_HTTP_BEGIN"
const httpEnd = "# UVP_HTTP_END"

var sslListen = regexp.MustCompile(`(?m)^(\s*listen\s+)(\[::\]:)?\d+ ssl;`)
var httpListen = regexp.MustCompile(`(?m)^\s*listen\s+(?:\[::\]:)?(\d+);`)
var redirect = regexp.MustCompile(`return 301 https://\$host(?::\d+)?\$request_uri;`)

func section(base, begin, end string) (int, int, string, error) {
	start := strings.Index(base, begin)
	finish := strings.Index(base, end)
	if start < 0 || finish <= start {
		return 0, 0, "", errors.New("安装包 Nginx 模板不支持 HTTPS 管理，请更新完整安装包")
	}
	return start, finish + len(end), base[start+len(begin) : finish], nil
}
func Render(base string, s Settings) (string, error) {
	if s.Port < 1 || s.Port > 65535 {
		return "", errors.New("HTTPS 端口必须在 1—65535 之间")
	}
	start, end, tlsBlock, err := section(base, tlsBegin, tlsEnd)
	if err != nil {
		return "", err
	}
	hs, he, httpBlock, err := section(base, httpBegin, httpEnd)
	if err != nil {
		return "", err
	}
	if end > hs {
		return "", errors.New("Nginx 模板分段顺序不正确")
	}
	portMatch := httpListen.FindStringSubmatch(httpBlock)
	if len(portMatch) != 2 {
		return "", errors.New("无法识别 Nginx HTTP 端口")
	}
	if strconv.Itoa(s.Port) == portMatch[1] {
		return "", errors.New("HTTPS 端口不能与 HTTP 端口相同")
	}
	if s.Enabled {
		tlsBlock = sslListen.ReplaceAllString(tlsBlock, "${1}${2}"+strconv.Itoa(s.Port)+" ssl;")
		httpBlock = redirect.ReplaceAllStringFunc(httpBlock, func(string) string { return "return 301 https://$host:" + strconv.Itoa(s.Port) + "$request_uri;" })
		tlsBlock = addProbe(tlsBlock, "https")
		return base[:start] + tlsBegin + tlsBlock + tlsEnd + base[end:hs] + httpBegin + httpBlock + httpEnd + base[he:], nil
	}
	// Reuse every route from the HTTPS server so disabling TLS cannot remove
	// static assets, upload aliases or proxy settings.
	plain := sslListen.ReplaceAllString(tlsBlock, "${1}${2}"+portMatch[1]+";")
	var lines []string
	for _, line := range strings.Split(plain, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "ssl_") || strings.HasPrefix(trimmed, "http2 ") {
			continue
		}
		lines = append(lines, line)
	}
	return base[:start] + addProbe(strings.Join(lines, "\n"), "http") + base[end:hs] + base[he:], nil
}

// AtomicWrite also replaces existing material on Windows (os.Rename uses
// MoveFileEx with REPLACE_EXISTING there). Private material is never logged.
func AtomicWrite(path string, data []byte, mode os.FileMode) error {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("拒绝替换符号链接文件")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".https-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}
func writeJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return AtomicWrite(path, b, 0600)
}

// RenderStartup runs after the platform script has substituted all paths and
// ports. It keeps the unmodified base for future HTTP <-> HTTPS changes.
func RenderStartup(root, platform string) error {
	m := New(root, platform)
	base, err := os.ReadFile(m.confPath())
	if err != nil {
		return err
	}
	// Keep the pristine template as the base. A rendered configuration no
	// longer carries the standalone HTTP section (it is merged into the entry
	// server) and is recognisable by the probe endpoint, so storing it as the
	// base would make the next HTTP <-> HTTPS switch fail to resolve ports.
	// The platform script regenerates the file from the template on every
	// start, so this keeps the base current without losing either section.
	source := base
	if strings.Contains(string(base), probeEndpoint) {
		// Already rendered: re-render from the stored base instead.
		if source, err = os.ReadFile(m.basePath()); err != nil {
			return err
		}
	} else if err := AtomicWrite(m.basePath(), base, 0600); err != nil {
		return err
	}
	s, err := m.Settings()
	if err != nil {
		return err
	}
	// Persist the resolved default on the first start. Without it the next
	// start would have to recognise the port from an already rendered
	// (possibly TLS-free) configuration, which is not always unambiguous.
	if _, statErr := os.Stat(m.settingsPath()); os.IsNotExist(statErr) {
		if err := writeJSON(m.settingsPath(), s); err != nil {
			return err
		}
	}
	rendered, err := Render(string(source), s)
	if err != nil {
		return err
	}
	return AtomicWrite(m.confPath(), []byte(rendered), 0600)
}
func readSettings(path string) (Settings, error) {
	var s Settings
	b, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	err = json.Unmarshal(b, &s)
	if err == nil && (s.Port < 1 || s.Port > 65535) {
		err = fmt.Errorf("无效的 HTTPS 配置端口")
	}
	return s, err
}

// SettingsPath 是页面保存的 HTTPS 状态落盘位置（相对包根）。
// ⛔ 启动脚本要按它决定 nginx 监听哪个端口，路径只能在**这里**定义一次。
func SettingsPath(root string) string { return filepath.Join(root, "data/https/settings.json") }

// ReadSettingsFile 读取落盘的 HTTPS 状态。
//
// exists=false 表示还没落盘（首次启动前）——调用方应回退到自己的默认端口，
// 这不是错误。文件存在但内容非法则返回 error：那种情况下无法判断该听哪个端口，
// 静默回退会让页面在"HTTPS 已开"和"以为没开"之间反复横跳。
func ReadSettingsFile(path string) (s Settings, exists bool, err error) {
	if _, statErr := os.Stat(path); statErr != nil {
		if os.IsNotExist(statErr) {
			return Settings{}, false, nil
		}
		return Settings{}, false, statErr
	}
	s, err = readSettings(path)
	if err != nil {
		return Settings{}, true, fmt.Errorf("HTTPS 配置文件无效（%s）: %w", path, err)
	}
	return s, true, nil
}

// WriteSettingsFile 把落盘的 HTTPS 状态原子写回（0600）。
//
// ⛔ 供 `uvp-setup https-repair-port` 做**端口重排后的存量迁移**用：
//   写成 shell 里的 sed 会在 JSON 被格式化/换行后静默改错字段，
//   而这里与 ReadSettingsFile 共用同一份结构体定义，字段名不可能漂移。
func WriteSettingsFile(path string, s Settings) error {
	if s.Port < 1 || s.Port > 65535 {
		return fmt.Errorf("无效的 HTTPS 端口: %d", s.Port)
	}
	return writeJSON(path, s)
}

const probeEndpoint = "/__uvp_https_probe"

func addProbe(block, mode string) string {
	i := strings.Index(block, "server_name _;")
	if i < 0 {
		return block
	}
	i += len("server_name _;")
	return block[:i] + "\n        location = " + probeEndpoint + " { add_header X-UVP-HTTPS-Mode " + mode + " always; return 204; }\n" + block[i:]
}
