package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 抓拍上传基址 —— 设备把拍好的图 POST 回来的入口（SnapShotConfig.UploadURL）。
//
// ⛔⛔ 与扫码接入基址同一条铁律：唯一正确来源是配置，绝不能是「操作员浏览器
// 打开平台的地址」。2026-10-20 线上实锤：公网 NAT + nginx HTTPS 部署下，请求
// Host 派生的上传地址丢了端口、指向 443 死地址，设备图传不回来且平台零报错。
func TestSnapshotUploadEffectiveBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		config  SnapshotUploadConfig
		want    string
		wantErr bool
	}{
		{
			// ⭐ 与 QRProvision 的关键差异：未配置是**合法**状态（回退请求 Host
			// 派生），所以这里必须返回 ("", nil) 而不是报错。
			name:   "unconfigured yields empty and nil",
			config: SnapshotUploadConfig{},
			want:   "",
		},
		{
			name:   "whitespace only is unconfigured",
			config: SnapshotUploadConfig{BaseURL: "   "},
			want:   "",
		},
		{
			name:   "http base with port",
			config: SnapshotUploadConfig{BaseURL: "http://10.0.0.68:51002"},
			want:   "http://10.0.0.68:51002",
		},
		{
			name:   "https base with port",
			config: SnapshotUploadConfig{BaseURL: "https://124.236.76.233:51000"},
			want:   "https://124.236.76.233:51000",
		},
		{
			name:   "trailing slash is trimmed",
			config: SnapshotUploadConfig{BaseURL: "https://124.236.76.233:51000/"},
			want:   "https://124.236.76.233:51000",
		},
		{
			// 反代带路径前缀的场景：路径保留（拼上传路径时自然接上）。
			name:   "path prefix is kept",
			config: SnapshotUploadConfig{BaseURL: "https://uv.example.com/uvp"},
			want:   "https://uv.example.com/uvp",
		},
		{
			name:    "relative url",
			config:  SnapshotUploadConfig{BaseURL: "/api/gb28181"},
			wantErr: true,
		},
		{
			name:    "unsupported scheme",
			config:  SnapshotUploadConfig{BaseURL: "ftp://10.0.0.68"},
			wantErr: true,
		},
		{
			name:    "userinfo is rejected",
			config:  SnapshotUploadConfig{BaseURL: "http://user:pass@10.0.0.68:51002"},
			wantErr: true,
		},
		{
			name:    "query is rejected",
			config:  SnapshotUploadConfig{BaseURL: "http://10.0.0.68:51002?a=1"},
			wantErr: true,
		},
		{
			name:    "fragment is rejected",
			config:  SnapshotUploadConfig{BaseURL: "http://10.0.0.68:51002#x"},
			wantErr: true,
		},
		{
			// ⭐ 回环地址对**设备**不可达：配置了等于没配还更迷惑，
			// 当场报错优于发一个注定失败的上传地址。
			name:    "loopback is rejected",
			config:  SnapshotUploadConfig{BaseURL: "http://127.0.0.1:8280"},
			wantErr: true,
		},
		{
			name:    "localhost is rejected",
			config:  SnapshotUploadConfig{BaseURL: "http://localhost:8280"},
			wantErr: true,
		},
		{
			name:    "wildcard is rejected",
			config:  SnapshotUploadConfig{BaseURL: "http://0.0.0.0:8280"},
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

// 防线：报错文案必须带「抓拍上传基址」，管理界面据此指向正确的配置项。
func TestSnapshotUploadErrorMentionsConfigKey(t *testing.T) {
	_, err := SnapshotUploadConfig{BaseURL: "ftp://x"}.EffectiveBaseURL()
	require.Error(t, err)
	require.Contains(t, err.Error(), "抓拍上传基址")
}

// 键名契约：gb28181.snapshot_upload.base_url。
// LoadFrom 装配与 CurrentSnapshotUploadBaseURL 共用 SnapshotUploadConfigFrom，
// 这里钉住 From 的键名，免得两处写岔。
func TestSnapshotUploadConfigFromReadsKey(t *testing.T) {
	src := fakeSource{strings: map[string]string{"gb28181.snapshot_upload.base_url": "https://124.236.76.233:51000"}}
	got := SnapshotUploadConfigFrom(src)
	require.Equal(t, "https://124.236.76.233:51000", got.BaseURL)

	empty := SnapshotUploadConfigFrom(fakeSource{})
	require.Empty(t, empty.BaseURL)

	var nilSrc valueSource
	require.Empty(t, SnapshotUploadConfigFrom(nilSrc).BaseURL)
}
