package controllers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// ⛔ 上传地址不能是**设备够不着**的地方。
//
// `UploadURL` 由**浏览器请求的 Host** 派生（前端 dev server 的 `xfwd: true` 会把它放进
// `X-Forwarded-Host`，见 `web/vite.config.ts`），所以"操作员用 localhost 打开平台"
// 会直接下发出 `http://localhost:5177/…` —— 设备把它解析成自己，**永远传不上来**，
// 而平台侧会话只是停在 waiting、日志全绿，唯一的现象是"设备没上传"。
//
// 这与 2026-09-20 那次"抓拍报文根本没发出去"是同一类**静默失效**，所以这里钉住
// "当场拒发 + 文案说清"，而不是让故障沉到运维去猜。
//
// 本文件用**内部包**（`package controllers`）：`snapshotUploadURL` 未导出，
// 走 HTTP 路由断言状态码也能测，但那样只能覆盖到"回环被拒"，
// 覆盖不到"合法 Host 拼出来的 URL 到底长什么样"这半边。
func TestSnapshotUploadURLRejectsDeviceUnreachableHost(t *testing.T) {
	for _, test := range []struct {
		name    string
		host    string
		proto   string
		reject  bool
		wantURL string
	}{
		{name: "localhost", host: "localhost:5177", reject: true},
		{name: "localhost 无端口", host: "localhost", reject: true},
		{name: "ipv4 回环", host: "127.0.0.1:8280", reject: true},
		{name: "ipv6 回环", host: "[::1]:8280", reject: true},
		{name: "v4 通配", host: "0.0.0.0:8280", reject: true},
		{name: "局域网 IP", host: "192.168.10.120:5177",
			wantURL: "http://192.168.10.120:5177/api/gb28181/device-snapshots/uploads/tok/"},
		{name: "域名 + https", host: "camera.example.com", proto: "https",
			wantURL: "https://camera.example.com/api/gb28181/device-snapshots/uploads/tok/"},
		// ⭐ 域名一律放行：平台判定不了它解析到哪里，判错了会把正常部署挡在门外。
		{name: "公网域名", host: "60.216.1.8",
			wantURL: "http://60.216.1.8/api/gb28181/device-snapshots/uploads/tok/"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/", nil)
			ctx.Request.Host = test.host
			if test.proto != "" {
				ctx.Request.Header.Set("X-Forwarded-Proto", test.proto)
			}
			got, err := snapshotUploadURL(ctx, "tok")
			if test.reject {
				require.Error(t, err, "回环/通配地址必须拒发: %s", test.host)
				require.Empty(t, got)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.wantURL, got)
		})
	}
}
