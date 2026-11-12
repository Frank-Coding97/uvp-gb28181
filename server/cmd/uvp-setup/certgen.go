package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// runCertgen 生成自签证书（nginx 用）并合成 ZLM 要的那份单文件 PEM。
//
// 为什么不用 openssl：
//   - Linux 版 ctl.sh 用 `openssl req -x509 …`，而**客户 Windows 机没有 openssl**；
//     用 Go 的 crypto/x509 生成是零外部依赖的做法。
//   - 顺带解决 Linux 版踩过的坑：Ubuntu 16 自带的 OpenSSL 1.0.2 没有 `-addext`
//     ⇒ 带 SAN 生成会失败、只能降级成没有 SAN 的证书。Go 没有这个历史包袱，
//     **永远带 SAN**（浏览器对「IP 访问 + 无 SAN」是会直接拦的）。
func runCertgen(args []string) error {
	fs := newFlagSet("certgen")
	dir := fs.String("dir", "", "证书输出目录（写 uvp.crt / uvp.key）")
	zlmPEM := fs.String("zlm-pem", "", "ZLM 用合并 PEM 的输出路径（私钥在前、证书在后）")
	cn := fs.String("cn", "uvp-local", "证书 CN")
	days := fs.Int("days", 3650, "有效期天数")
	force := fs.Bool("force", false, "已存在也重新生成")
	var ips ipList
	fs.Var(&ips, "ip", "加入 SAN 的 IP（可重复或逗号分隔）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*dir) == "" {
		return errors.New("certgen: 缺少 --dir")
	}
	absDir, err := resolvePath(*dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return fmt.Errorf("创建证书目录失败: %w", err)
	}
	crtPath := filepath.Join(absDir, "uvp.crt")
	keyPath := filepath.Join(absDir, "uvp.key")

	_, crtErr := os.Stat(crtPath)
	_, keyErr := os.Stat(keyPath)
	haveBoth := crtErr == nil && keyErr == nil
	if haveBoth && !*force {
		note("证书已存在（%s），跳过生成", crtPath)
	} else {
		if err := writeSelfSigned(crtPath, keyPath, *cn, *days, ips); err != nil {
			return err
		}
		success("已生成自签证书 %s（CN=%s，有效期 %d 天）", crtPath, *cn, *days)
	}

	// ---- ZLM 的 PEM：ZLM 只认「私钥+证书合一」的单个文件 ----
	// ⛔ 顺序必须是**先私钥后证书**（照 ZLMediaKit 自带 default.pem 的顺序）；
	//   反过来 ZLM 会说「找不到私钥」而握手失败（listen 成功、握手挂，极难看出）。
	if strings.TrimSpace(*zlmPEM) != "" {
		absZLM, err := resolvePath(*zlmPEM)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(absZLM), 0o700); err != nil {
			return fmt.Errorf("创建 ZLM 证书目录失败: %w", err)
		}
		if _, err := os.Stat(absZLM); err == nil && !*force {
			note("ZLM 证书已存在（%s），跳过合成", absZLM)
			return nil
		}
		keyPEM, err := os.ReadFile(keyPath)
		if err != nil {
			return fmt.Errorf("读取私钥失败: %w", err)
		}
		crtPEM, err := os.ReadFile(crtPath)
		if err != nil {
			return fmt.Errorf("读取证书失败: %w", err)
		}
		combined := append(append([]byte{}, keyPEM...), crtPEM...)
		if err := os.WriteFile(absZLM, combined, 0o600); err != nil {
			return fmt.Errorf("写入 ZLM 证书失败: %w", err)
		}
		success("已合成 ZLM 共用证书 %s", absZLM)
	}
	return nil
}

func writeSelfSigned(crtPath, keyPath, cn string, days int, ips []string) error {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("生成私钥失败: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("生成序列号失败: %w", err)
	}
	// ⛔ SAN 必须带 localhost + 回环；客户机自己的局域网 IP 由 ctl 用 --ip 传进来。
	dns := []string{"localhost"}
	addrs := []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}
	for _, raw := range ips {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		if ip := net.ParseIP(s); ip != nil {
			addrs = append(addrs, ip)
			continue
		}
		dns = append(dns, s)
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: cn, Organization: []string{"UVP"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(0, 0, days),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              dns,
		IPAddresses:           addrs,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("签发证书失败: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return fmt.Errorf("编码私钥失败: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	crtPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return fmt.Errorf("写入私钥失败: %w", err)
	}
	if err := os.WriteFile(crtPath, crtPEM, 0o644); err != nil {
		return fmt.Errorf("写入证书失败: %w", err)
	}
	return nil
}
