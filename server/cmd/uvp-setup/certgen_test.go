package main

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

// TestCertgenProducesUsableMaterial 覆盖「证书生成 + ZLM 合并 PEM」两条链路。
//
// ⛔ 两条断言是踩过的坑，别删：
//   - 证书必须带 SAN（Linux 版在 OpenSSL 1.0.2 上只能降级成不带 SAN，
//     浏览器对 IP 访问会直接拦）；
//   - ZLM 的 PEM 必须**先私钥后证书**（反了 ZLM 报「找不到私钥」，
//     listen 成功、握手失败，极难定位）。
func TestCertgenProducesUsableMaterial(t *testing.T) {
	dir := t.TempDir()
	confDir := filepath.Join(dir, "conf")
	zlmPEM := filepath.Join(dir, "secrets", "zlm-ssl.pem")

	err := runCertgen([]string{
		"--dir", confDir,
		"--zlm-pem", zlmPEM,
		"--cn", "uvp-local",
		"--ip", "192.168.1.10",
	})
	if err != nil {
		t.Fatalf("certgen 失败: %v", err)
	}

	crtPEM, err := os.ReadFile(filepath.Join(confDir, "uvp.crt"))
	if err != nil {
		t.Fatalf("读证书失败: %v", err)
	}
	keyPEM, err := os.ReadFile(filepath.Join(confDir, "uvp.key"))
	if err != nil {
		t.Fatalf("读私钥失败: %v", err)
	}

	block, _ := pem.Decode(crtPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatalf("证书 PEM 形态不对: %#v", block)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("解析证书失败: %v", err)
	}
	if cert.Subject.CommonName != "uvp-local" {
		t.Errorf("CN = %q，期望 uvp-local", cert.Subject.CommonName)
	}
	if len(cert.IPAddresses) == 0 || cert.IPAddresses[0].String() != "127.0.0.1" {
		t.Errorf("SAN IP 缺 127.0.0.1: %v", cert.IPAddresses)
	}
	found := false
	for _, ip := range cert.IPAddresses {
		if ip.String() == "192.168.1.10" {
			found = true
		}
	}
	if !found {
		t.Errorf("--ip 192.168.1.10 没进 SAN: %v", cert.IPAddresses)
	}
	if !cert.IsCA {
		t.Error("自签证书应当可作 CA（客户端导入根证书后即可信任）")
	}

	kb, _ := pem.Decode(keyPEM)
	if kb == nil {
		t.Fatal("私钥不是 PEM")
	}
	if kb.Type != "PRIVATE KEY" {
		t.Errorf("私钥 PEM 类型 = %q，期望 PRIVATE KEY(PKCS#8)", kb.Type)
	}

	combined, err := os.ReadFile(zlmPEM)
	if err != nil {
		t.Fatalf("读 ZLM PEM 失败: %v", err)
	}
	idxKey := indexOf(combined, []byte("PRIVATE KEY"))
	idxCrt := indexOf(combined, []byte("CERTIFICATE"))
	if idxKey < 0 || idxCrt < 0 {
		t.Fatalf("ZLM PEM 缺私钥或证书: key=%d crt=%d", idxKey, idxCrt)
	}
	if idxKey > idxCrt {
		t.Error("ZLM PEM 顺序错了：必须私钥在前、证书在后")
	}

	// 幂等：再跑一次不应改动文件（不加 --force）
	before, _ := os.ReadFile(filepath.Join(confDir, "uvp.crt"))
	if err := runCertgen([]string{"--dir", confDir, "--zlm-pem", zlmPEM, "--ip", "192.168.1.10"}); err != nil {
		t.Fatalf("第二次 certgen 失败: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(confDir, "uvp.crt"))
	if string(before) != string(after) {
		t.Error("不带 --force 时不应重新生成证书")
	}
}

func indexOf(haystack, needle []byte) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if string(haystack[i:i+len(needle)]) == string(needle) {
			return i
		}
	}
	return -1
}
