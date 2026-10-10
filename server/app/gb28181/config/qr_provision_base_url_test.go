package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 扫码接入基址 —— 设备扫码后要访问的地址。
//
// ⛔⛔ 这个值的**唯一正确来源是配置/后端下发**,绝不能是「用户打开平台的浏览器 origin」。
// 2026-10-08 的线上故障就是这么来的:绿色包前端走 nginx 自签 HTTPS,
// 二维码里带上了那个地址,设备侧 Ktor CIO 默认校验证书直接握手失败,
// 失败又被归类成 NetworkError ⇒ UI 骗用户「去检查手机与平台是否同网络」。
func TestQRProvisionEffectiveBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		config  QRProvisionConfig
		want    string
		wantErr bool
	}{
		{
			name:   "host and port compose a plaintext base url",
			config: QRProvisionConfig{Host: "192.168.10.220", Port: 51010},
			want:   "http://192.168.10.220:51010",
		},
		{
			name:   "bracketed ipv6 host",
			config: QRProvisionConfig{Host: "[fe80::1]", Port: 51010},
			want:   "http://[fe80::1]:51010",
		},
		{
			name:   "explicit base url wins over host and port",
			config: QRProvisionConfig{BaseURL: "http://10.8.0.3:51010", Host: "192.168.10.220", Port: 51010},
			want:   "http://10.8.0.3:51010",
		},
		{
			name:   "trailing slash is trimmed",
			config: QRProvisionConfig{BaseURL: "http://10.8.0.3:51010/"},
			want:   "http://10.8.0.3:51010",
		},
		{
			name:   "https is allowed when tls is not required",
			config: QRProvisionConfig{BaseURL: "https://uv.example.com"},
			want:   "https://uv.example.com",
		},
		{
			name:    "tls required rejects plaintext",
			config:  QRProvisionConfig{BaseURL: "http://10.8.0.3:51010", RequireTLS: true},
			wantErr: true,
		},
		{
			name:   "tls required accepts https",
			config: QRProvisionConfig{BaseURL: "https://uv.example.com", RequireTLS: true},
			want:   "https://uv.example.com",
		},
		{
			name:    "missing entirely",
			config:  QRProvisionConfig{},
			wantErr: true,
		},
		{
			name:    "host without port",
			config:  QRProvisionConfig{Host: "192.168.10.220"},
			wantErr: true,
		},
		{
			name:    "port out of range",
			config:  QRProvisionConfig{Host: "192.168.10.220", Port: 70000},
			wantErr: true,
		},
		{
			name:    "relative base url",
			config:  QRProvisionConfig{BaseURL: "/gb28181"},
			wantErr: true,
		},
		{
			name:    "userinfo is rejected",
			config:  QRProvisionConfig{BaseURL: "http://user:pass@10.8.0.3:51010"},
			wantErr: true,
		},
		{
			name:    "query is rejected",
			config:  QRProvisionConfig{BaseURL: "http://10.8.0.3:51010?a=1"},
			wantErr: true,
		},
		{
			name:    "fragment is rejected",
			config:  QRProvisionConfig{BaseURL: "http://10.8.0.3:51010#x"},
			wantErr: true,
		},
		{
			name:    "unsupported scheme",
			config:  QRProvisionConfig{BaseURL: "ftp://10.8.0.3:51010"},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.config.EffectiveBaseURL()
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

// ⛔ 防线：base_url 与 host/port **同时为空**必须报错而不是回退成浏览器 origin ——
// 出一个扫了必然失败的码，比明确报错更糟：现场在手机上，排查成本全落在用户那边。
func TestQRProvisionEmptyNeverFallsBack(t *testing.T) {
	_, err := QRProvisionConfig{}.EffectiveBaseURL()
	require.Error(t, err)
	require.Contains(t, err.Error(), "扫码接入基址")
}
