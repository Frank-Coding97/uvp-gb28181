# Windows ZLMediaKit 原生契约 probe

这个独立模块用于 T04 的 Windows 原生验证。它要求一个完整的
`MediaServer.exe` 运行目录，先复制整个目录到系统临时目录下带有中文和空格的
隔离路径，再生成只绑定 `127.0.0.1` 的临时配置和 HTTP 端口，最后启动、检查和
停止它自己的进程。不会安装服务，也不会按进程名停止已有的 ZLMediaKit。

构建与单测：

```sh
go test ./...
go vet ./...
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o zlm-probe.exe .
```

在目标 Windows 10 x64 环境中运行：

```powershell
.\zlm-probe.exe `
  -root 'D:\UVP\media' `
  -exe 'D:\UVP\media\MediaServer.exe' `
  -fixture 'D:\UVP\input\zlm-fixture.mp4' `
  -expected-commit 'b422fb016da1c3989b725dde3bd0292613b12060'
```

探针覆盖：

- 完整资源树复制、可执行文件哈希、中文/空格路径和第二次启动；
- `getApiList`、`getServerConfig` 与构建版本前缀；
- `openRtpServer`、`listRtpServer`、`closeRtpServer` 的真实生命周期；
- MP4 加载后的 HTTP-FMP4 播放者、`getMediaPlayerList`、
  `getMediaTrafficStatistic` 和 `addProbe`；
- 中文/空格目录下的 `startRecord`、`isRecording`、`stopRecord` 及非空 MP4
  文件；
- 无 Cookie 重放的错误 secret 拒绝，响应码必须为 ZLMediaKit 的 `-100`。

标准输出只写不含 secret 的 JSON。ZLMediaKit stdout/stderr 仅写入隔离目录的
`probe-stdout.log` 和 `probe-stderr.log`；默认运行结束后删除临时目录，排查失败时
可使用 `-keep-temp` 保留它。

Hook 回调鉴权需要 UVP 的真实 Hook 接收端和节点凭据派生契约，探针会明确输出
`hook_callback_auth: not_executed`，不会用模拟 HTTP 响应冒充通过。没有提供
`-fixture` 时媒体/录制用例同样标记为 `not_executed`，整体不会返回通过。

测试 fixture 可由 FFmpeg 生成（FFmpeg 只是测试输入工具，不属于 Windows 运行依赖）：

```sh
ffmpeg -hide_banner -loglevel error -f lavfi -i testsrc2=size=320x240:rate=15 \
  -t 8 -c:v libx264 -pix_fmt yuv420p -g 15 -movflags +faststart -an -n \
  /tmp/uvp-zlm-p0-fixture.mp4
```
