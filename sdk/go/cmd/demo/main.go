// 对接示例 + 签名自测。
//
//	cd sdk/go
//	go run ./cmd/demo --self-test    # 不需要网络，用仓库里的向量自证签名正确
//
//	go run ./cmd/demo                # 真连平台（需要下面四个环境变量）
//
// 三个最常见的 401 原因：base_url 用了浏览器端口（51000）而不是后端端口（51002）；
// audience 与平台 config.yml 的 openapi.audience 不一致；本机时钟偏差超过 300 秒。
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"uvp-openapi-demo/uvp"
)

type fixture struct {
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
	Timestamp string `json:"timestamp"`
	Nonce     string `json:"nonce"`
	Audience  string `json:"audience"`
	Vectors   []struct {
		Name           string `json:"name"`
		Method         string `json:"method"`
		Path           string `json:"path"`
		RawQuery       string `json:"rawQuery"`
		ContentType    string `json:"contentType"`
		Body           string `json:"body"`
		BodySHA256     string `json:"bodySHA256"`
		Canonical      string `json:"canonical"`
		CanonicalBytes int    `json:"canonicalBytes"`
		Signature      string `json:"signature"`
		IdempotencyKey string `json:"idempotencyKey"`
	} `json:"vectors"`
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--self-test" {
		if err := selfTest(); err != nil {
			fmt.Fprintln(os.Stderr, "✗ "+err.Error())
			os.Exit(1)
		}
		return
	}
	if err := demo(); err != nil {
		fmt.Fprintln(os.Stderr, "✗ "+err.Error())
		os.Exit(1)
	}
}

func selfTest() error {
	path, err := locateFixture()
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var data fixture
	if err := json.Unmarshal(raw, &data); err != nil {
		return fmt.Errorf("解析向量失败: %w", err)
	}

	failures := 0
	for _, vector := range data.Vectors {
		produced, err := uvp.Canonical(uvp.Input{
			Method: vector.Method, Path: vector.Path, RawQuery: vector.RawQuery,
			ContentType: vector.ContentType, Body: []byte(vector.Body),
			AccessKey: data.AccessKey, Timestamp: data.Timestamp, Nonce: data.Nonce,
			IdempotencyKey: vector.IdempotencyKey, Audience: data.Audience,
		})
		if err != nil {
			return fmt.Errorf("%s: 本地校验直接报错: %w", vector.Name, err)
		}
		signature, err := uvp.Sign(data.SecretKey, produced)
		if err != nil {
			return err
		}
		checks := []struct {
			field     string
			got, want interface{}
		}{
			{"canonical", produced, vector.Canonical},
			{"canonicalBytes", len([]byte(produced)), vector.CanonicalBytes},
			{"signature", signature, vector.Signature},
		}
		for _, check := range checks {
			if fmt.Sprint(check.got) != fmt.Sprint(check.want) {
				fmt.Fprintf(os.Stderr, "  ✗ %s.%s 不一致\n    实际: %v\n    期望: %v\n",
					vector.Name, check.field, check.got, check.want)
				failures++
			}
		}
		if signature == vector.Signature {
			fmt.Printf("  ✓ %s  signature=%s...\n", vector.Name, signature[:16])
		}
	}
	if failures > 0 {
		return fmt.Errorf("Go 签名实现与平台向量不一致（%d 处）", failures)
	}
	fmt.Printf("\n✓ Go 签名自测通过：%d 个向量\n", len(data.Vectors))
	return nil
}

func demo() error {
	baseURL := required("UVP_BASE_URL")
	client, err := uvp.NewClient(baseURL, required("UVP_ACCESS_KEY"),
		required("UVP_SECRET_KEY"), required("UVP_AUDIENCE"))
	if err != nil {
		return err
	}

	data, err := client.Get("/openapi/v1/devices", map[string]string{"page": "1", "pageSize": "20"})
	if err != nil {
		return err
	}
	var page struct {
		Items []struct {
			DeviceID string `json:"deviceId"`
			Name     string `json:"name"`
		} `json:"items"`
		Total int64 `json:"total"`
	}
	if err := json.Unmarshal(data, &page); err != nil {
		return fmt.Errorf("设备列表结构不符: %w", err)
	}
	fmt.Printf("共 %d 台设备：\n", page.Total)
	for _, device := range page.Items {
		fmt.Printf("  %s  %s\n", device.DeviceID, device.Name)
	}
	if len(page.Items) == 0 {
		fmt.Println("平台里还没有设备，先接入一台再来。")
		return nil
	}

	channels, err := client.Get("/openapi/v1/devices/"+page.Items[0].DeviceID+"/channels", nil)
	if err != nil {
		if apiError, ok := err.(*uvp.ApiError); ok {
			fmt.Fprintf(os.Stderr, "查通道失败: code=%s requestId=%s message=%s\n",
				apiError.Code, apiError.RequestID, apiError.Message)
			return nil
		}
		return err
	}
	pretty, _ := json.MarshalIndent(channels, "", "  ")
	fmt.Printf("\n第一台设备的通道：\n%s\n", truncate(string(pretty), 1200))
	return nil
}

func required(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		fmt.Fprintf(os.Stderr, "缺少环境变量 %s\n", name)
		os.Exit(2)
	}
	return value
}

// locateFixture 从当前目录向上找，跑在仓库里或拆出去单独跑都能定位。
func locateFixture() (string, error) {
	current, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(current, "fixtures", "signature-v1.json")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("找不到 fixtures/signature-v1.json；请在 sdk/ 或 sdk/go/ 目录下运行")
		}
		current = parent
	}
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}
