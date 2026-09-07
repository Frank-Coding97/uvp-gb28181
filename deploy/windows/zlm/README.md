# ZLMediaKit Windows 构建包

这个目录只负责构建项目锁定的 ZLMediaKit fork，并把运行所需的 `MediaServer.exe`、`config.ini`、`default.pem`、`www/`、非系统 DLL 和第三方许可复制到独立输出目录。它不修改 ZLMediaKit 源码，也不安装全局工具。

构建输入必须是 `sources.lock.json` 中 `zlm.revision` 与五个子模块都一致、且包含 ignored 文件在内完全干净的 Git checkout。`build.ps1` 在构建前拒绝错误提交、缺失子模块、dirty checkout 和复用的 CMake cache。

在 Windows 10 x64 构建机上，先在专用目录准备固定 vcpkg revision `efcfaaf60d7ec57a159fc3110403d939bfb69729`，并安装 `openssl` `3.5.1`、`libsrtp` `2.7.0#1` 与 `usrsctp` `0.9.5.0#4` 到 `x64-windows-static` triplet。脚本会校验这些版本并把实际 vcpkg revision/包版本写入 manifest。构建命令示例：

```powershell
Set-Location G:\uvp-p0-zlm
& .\build.ps1 `
  -Source G:\uvp-p0-zlm\source\zlm `
  -Output G:\uvp-p0-zlm\package `
  -BuildRoot G:\uvp-p0-zlm\cmake-build `
  -LockFile G:\uvp-p0-zlm\sources.lock.json `
  -VcpkgRoot G:\uvp-p0-zlm\vcpkg `
  -SourceRevision 550d929b4fb9af8e2bbf0b2530634d4b40af6537 `
  -Parallel 8 *> G:\uvp-p0-zlm\zlm-build.log
```

`-SourceRevision` 仅用于已从锁定基线派生的项目 fork 修复；省略时脚本要求源码恰好等于锁定基线。当前 Windows 运行库修复提交为 `550d929b4fb9af8e2bbf0b2530634d4b40af6537`，脚本同时验证它以锁定基线为祖先，并在 manifest 中保留两者。

脚本使用 VS2022 的 `Visual Studio 17 2022` x64 生成器，并显式设置、校验 `CMAKE_MSVC_RUNTIME_LIBRARY=MultiThreaded`，构建时固定开启 RTPProxy、MP4、HLS、OpenSSL、WebRTC 和 SCTP，关闭 FFmpeg、SRT、MySQL、Python 与测试目标。配置输出必须确认每个必需能力，随后使用 `dumpbin /DEPENDENTS` 检查 DLL 闭包；非系统依赖会从本地 vcpkg 复制到 `media/`，找不到则失败。

输出目录中的 `zlm-manifest.json` 记录源码/子模块 SHA、CMake/生成器/triplet、特性、依赖和每个文件的 SHA-256。`licenses/zlm/` 保留 ZLMediaKit、五个已锁定子模块以及实际链接的 OpenSSL、libsrtp、usrsctp 许可文件。

本脚本只证明构建和资源闭包。Windows 10 干净运行机启动、项目 Hook/RTP/媒体鉴权契约、中文/空格录像路径、真实设备播放/录制与重启仍需单独执行 T04-A/B/C；构建成功不等于 T04 完成。
