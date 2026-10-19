---
title: 服务升级 SQLite 路径修复任务与测试结果
---

# SQLite 路径修复任务与测试结果

日期：2026-10-19。老板授权直接修复；范围为升级助手与 220:51001 当前安装实例。

## 任务与用例

- [x] 注册时从业务配置取得 SQLite 路径。输入：standalone/systemd 的相对路径、自定义绝对路径、显式覆盖及 MySQL 配置；期望：注册文件保存实际绝对路径，外置数据库不误用 SQLite。`TestRegistrationSQLiteDatabaseResolvesInstallationConfig` 通过。
- [x] 保留 Compose 宿主路径边界。输入：容器内配置路径、显式宿主路径；期望：缺宿主路径拒绝注册，显式宿主路径保持原值。上述注册测试通过。
- [x] 在停服前检查备份来源。输入：standalone/systemd/compose × 缺登记、文件缺失、目录；期望：明确报错、没有运行停止服务或 Docker 命令。`TestSQLiteBackupRequirementsFailBeforeServiceStop` 九项通过。
- [x] 预检只检查、不提前备份。输入：有效 SQLite 文件；期望：不创建备份、不改变原数据。`TestStandaloneSQLitePreparationDoesNotCreateSnapshotWhileServiceRuns` 通过。
- [x] 回归现有 SQLite 快照、恢复、回退和不完整备份重试保护。本地两包全量通过；220 Linux 上隔离临时目录运行相关测试全部通过。测试不使用业务数据库。
- [x] 更新已安装实例的助手和登记。期望：仅新增 SQLite 路径，可信密钥、UID/GID 和布局不变；后端不重启，历史不变。远端核验通过。
- [ ] 老板重新创建并确认升级任务，验收实际升级到 1.1.6。此次没有代为执行。

## 源码与本地验证

修改 `server/cmd/uvp-updater/main.go` 与 `server/internal/upgrade/adapters.go`，对应测试分别位于 `main_test.go` 与 `sqlite_preparation_test.go`。

- RED：新注册路径测试在函数缺失时编译失败；原 standalone/systemd 会先执行 stop 再在备份报错，已复现。
- GREEN：`umask 027; go test ./cmd/uvp-updater ./internal/upgrade -count=1` 通过。最终日志：`/tmp/uvp-sqlite-registration-tests-final.log`。
- `gofmt -l` 无输出，`git diff --check` 通过。
- Linux amd64 助手 SHA-256：`a2bb4f5a1d2e2fb3fd5d2b01dad5e808f476c593a345e3020e584b87ec45f7d6`。

## 220 更新与验证

- SSH 别名：`开发环境支撑220`。只操作 51001 对应实例，未修改 52000。
- 安装根：`/home/superadmin/uvp-https-1.1.4-20261018/uvp-gb28181-linux-amd64-1.1.4`。
- SQLite 登记：安装根下 `data/uvp.db`，与后端已打开的文件句柄一致。
- 新旧注册文件对比：唯一新增字段为 `standalone.sqlite_database`；密钥与运行身份保持一致。
- 原助手和注册文件保存在 `/home/superadmin/uvp-sqlite-registration-fix-20261019/old-20261019191105`。
- 旧助手 PID 1712497，新助手 PID 1034272；业务后端 PID 967509 保持不变。
- 新助手运行文件 SHA-256 与上传文件一致；socket 状态 `ok=true`、`offline_import=true`，注册文件 0600，socket 0660。
- 页面 `http://127.0.0.1:51001/` 返回 200；后端 51002 保持监听。51001 当前为 HTTP，HTTPS 探测不适用。
- 门禁 `normal`，当前业务仍为 1.1.4。旧任务 `3a0d6dd4d1e962d71c57b69dbd0d439e` 保留 `rolled_back`。
- Linux 隔离测试日志：`/home/superadmin/uvp-sqlite-registration-fix-20261019/linux-tests.log`。
- 没有复制或修改运行中的业务数据库；没有应用业务升级。

## 发布边界

此次部署的是稳定安装根下的升级助手；现有 1.1.6 离线包未重打，不能声称包已包含本次修复。现有 standalone 激活流程只切换 `current`，控制脚本仍从稳定安装根使用 `updater/uvp-updater`，不会由候选业务包覆盖此次助手。后续正式发布包构建应包含已修源码。
