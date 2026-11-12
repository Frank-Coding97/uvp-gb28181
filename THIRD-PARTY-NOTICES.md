# 第三方组件版权与许可声明

UVP-GB28181（下称"本项目"）在开发过程中使用或衍生自下列第三方开源项目。
本项目自身的著作权归 UVP-GB28181 作者所有，下列项目的著作权归各自权利人所有。

分发本项目（含源码与二进制）时，请保持本文件与 `LICENSE` 一同分发。

---

## GinFast / ginfast-ui

- 项目地址：<https://github.com/tiger1103/gfast> 、<https://github.com/qxkjsoft/ginfast-ui>
- 许可证：MIT License
- 版权声明：Copyright (c) 2025 云南奇讯科技

本项目 `server/` 与 `web/` 的工程骨架、用户/角色/菜单/部门/字典/API 权限等基础管理模块、
JWT 认证、Casbin 授权、代码生成器、日志中间件、Swagger 集成，以及 Vue 3 + Vite + Arco Design
的前端工程结构与通用组件，均基于 GinFast / ginfast-ui 二次开发。上述部分遵循 MIT License，
其版权声明与许可声明按 MIT 条款随本文件一并保留。

GB/T 28181 协议栈、SIP 信令、设备目录与注册管理、媒体调度、ZLMediaKit 节点管理、
录像与回放、云端录像、语音对讲、级联、图像库、运维可观测性等模块为本项目独立开发，
不来源于 GinFast。

## ZLMediaKit

- 项目地址：<https://github.com/ZLMediaKit/ZLMediaKit>
- 许可证：MIT License
- 版权声明：Copyright (c) ZLMediaKit 作者与贡献者

本项目以 ZLMediaKit 作为流媒体能力底座，通过其 HTTP API 与 Hook 回调完成媒体接入、分发、
录制、协议转换与 WebRTC 播放。ZLMediaKit 以独立进程方式随交付包分发，其许可证与版权
声明见其项目仓库。

## EasyPlayer.js

- 项目地址：<https://github.com/EasyDarwin/EasyPlayer.js>
- 许可证：MIT License
- 版权声明：Copyright (c) EasyDarwin 团队

本项目前端使用 EasyPlayer.js 提供浏览器端多协议播放能力，其版权与许可声明见其项目仓库。

## 其他依赖

本项目后端 Go 依赖与前端 npm 依赖的许可证信息，分别见 `server/go.mod`（及
`server/go.sum`）与 `web/package.json`（及 `web/pnpm-lock.yaml`）。各依赖遵循其自身许可证。

---

以上项目名称与商标归各自权利人所有。本项目与上述项目维护方无隶属或代理关系。
