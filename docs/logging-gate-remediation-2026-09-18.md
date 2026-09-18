# 日志门禁违规清理记录（2026-09-18）

## 一、背景：为什么是 54 条而不是 15 条

审计报告给出的基线是「干净 HEAD 上 15 条违规」。本次动手前实测为 **54 条**，差额 39 条的来源已定位清楚：

审计之后，`codex/aksk-openapi` 被合并进 `develop`（新 HEAD `09de97ee`）。该分支做了两件影响门禁的事：

1. `app/gb28181/bootstrap.go` 里的 `startSIPDependencies` 被重命名为
   `startSIPDependenciesWithFactory`，并新增 `authority`、`factory` 参数（原函数保留为 3 参数的 wrapper）；
2. `reviewed_legacy.json` 里 32 条例外的 `function` 字段**没有跟着改**。

后果是链式的：8 条 `startSIPDependencies` 例外 + 1 条 `Start` 例外匹配不到任何调用点 → 报 9 条
`legacy_exception_mismatch`；而 `missing_event` 的判定是「没有 legacy 例外就报」，例外一失配，
原本被压住的 24 条 `missing_event` 全部冒了出来。

**隔离验证**：在独立 worktree 里重建「HEAD + 工作区 server/ 改动」并跑门禁，结果同为 54 条 →
这 39 条与工作区里那些未提交改动（process-authority 相关）**无关**，是已提交代码的账。

## 二、修复内容

### 1. 机制层：让 legacy 例外跟上重构（消掉 9 + 8 条）

`server/internal/loggingcontract/reviewed_legacy.json`：8 条例外的 `function`
`startSIPDependencies` → `startSIPDependenciesWithFactory`。

另外删除 1 条**已失效**的例外：`Start` 里的 `"GB28181 SIP 已运行,忽略重复启动"` —— 该日志
在重构中被替换成了带 event 的 `gb28181.sip.duplicate_start_rejected`，标的已不存在，登记只能删。

### 2. 代码层：给缺 event 的日志补事件（17 处，涉及 11 个文件）

事件命名沿用仓库既有规范（`<域>.<子域>.<动作>`，全小写 snake_case），并遵守
`message.go` 注释里写明的 C01.4 判据「**一个 event 名只装一件事**」——例如
`bootstrap_openapi_trust.go` 里「缺启动信任」和「缺节点注册表」两条跳过日志，刻意拆成两个事件名。

| 文件 | 事件 |
|---|---|
| `app/gb28181/bootstrap.go` (547) | `gb28181.sip.start_rejected_unauthorized` |
| `app/gb28181/bootstrap.go` (825) | `gb28181.lifecycle.playback_recovery_incomplete` |
| `app/gb28181/bootstrap.go` (1093) | `gb28181.lifecycle.playback_trust_missing` |
| `app/gb28181/bootstrap_openapi_trust.go` (46/50) | `gb28181.lifecycle.rtp_cleanup_trust_missing` / `...registry_missing` |
| `app/gb28181/controllers/play.go` (333) | `gb28181.play.security_projection_mismatch` |
| `app/gb28181/handler/hook_openapi_flow.go` (69) | `gb28181.hook.flow.observer_failed` |
| `app/gb28181/ptz/scheduler.go` (259) | `ptz.scheduler.stop_flush_failed` |
| `app/gb28181/ptz/service.go` (132) | `ptz.recovery_skipped` |
| `app/gb28181/subscribe/position.go` (78) | `subscribe.position.partial_persist` |
| `app/gb28181/zlm/service/node_purge.go` (69) | `zlm.node.purged_unreachable` |
| `app/routes/routes.go` (51) + `bootstrap/init.go` (47) | `security.lock_overrides_authoff`（同一件事，共用一个名） |
| `main.go` (150/152/159/161/168) | `startup.openapi_revocation_maintenance_failed` / `..._pending_overdue` / `..._unconfigured` / `..._startup_failed` / `startup.openapi_maintenance_failed` |

### 3. 字段层：驼峰改 snake_case（4 处）

字典里已有正确名字，直接改调用点即可，无需登记：

- `play.go:333`：`deviceId`→`device_id`、`channelId`→`channel_id`
- `subscribe/position.go:78`：`deviceId`→`device_id`
- `ptz/service.go:132`：`attemptIds`→`attempt_ids`（该名需新登记，见下）

### 4. 静态消息：`link_watch.go` 的 `warn(message, ...)`

`warn` 把函数参数直接交给 `logger.Warn`，门禁无法判定其为编译期常量。改法：4 个原因提为包级常量，
`warn` 内按常量 `switch` 分派到 4 条静态 logger 调用，未登记的原因统一落到 `linkLossUnclassified`。
调用点不动（字面量与常量同值）。

### 5. 登记层：`registry.json` 用仓库自带的 regenerate 流程更新

```bash
cd server && UVP_LOGGING_CATALOG_UPDATE=1 go test ./internal/loggingcatalog/ \
  -run TestLoggingCatalogRegenerate -count=1
```

这条流程是仓库自己设计的（`catalog_test.go:35-67` 注释：*the gate never writes, so an addition only
lands after someone runs this and reads the diff*）。实测结果**只增不减**：

- 事件 342 → 364（+22，其中 17 条是本轮补的，5 条是原本就存在但一直没登记的）
- 字段 163 → 172（+9）
- **既有 342 条事件的定位字段零改写**（这是判断该流程是否安全的关键指标）

### 6. 测试层：两处过时断言（存量问题，非本轮改动引入）

`internal/loggingcontract` 里有 2 个测试在改动前就是红的，根因同样是 codex 分支的改名没同步：

- `logging_cascade_test.go`：还在找 `startSIPDependencies` 的函数体（实现已移到 `...WithFactory`）；
  断言参数个数 2 → 实际 3；参数索引 1 → 2（`authority` 插到了第 2 位）。
- `entrypoint_test.go`：断言 `StartServer(engine, stopApplication)`，但代码挂的是 `shutdown` 闭包
  （闭包体内才调 `stopApplication`）。**没有放松断言**——改成「闭包已挂上 **且** 闭包体内确实调用了
  `stopApplication`」，避免一个空转的闭包也能骗过检查。

## 三、新增的事件与字段（需要你审阅的部分）

事件名一旦入 registry 即成为仓库契约，命名判断请重点看这几条：

```
gb28181.sip.start_rejected_unauthorized      启动期：缺有效进程授权
gb28181.lifecycle.playback_recovery_incomplete   持久回放恢复未完成
gb28181.lifecycle.playback_trust_missing         强制鉴权回放缺启动信任
gb28181.lifecycle.rtp_cleanup_trust_missing      持久RTP清理缺启动信任
gb28181.lifecycle.rtp_cleanup_registry_missing   持久RTP清理缺节点注册表
gb28181.play.security_projection_mismatch        后台播放设备安全投影不一致
gb28181.hook.flow.observer_failed                OpenAPI flow 观察失败
ptz.scheduler.stop_flush_failed                  PTZ 停止后仍有未持久化结果
ptz.recovery_skipped                            PTZ 恢复扫描跳过异常旧进程记录
subscribe.position.partial_persist              位置通知部分条目未落地
zlm.node.purged_unreachable                     强制移除不可达 ZLM 节点
security.lock_overrides_authoff                 安全锁覆盖 authoff 配置
startup.openapi_revocation_maintenance_failed   OpenAPI 撤销维护不可用
startup.openapi_revocation_pending_overdue      撤销逾期仍未完成
startup.openapi_revocation_unconfigured         撤销控制未配置
startup.openapi_revocation_startup_failed       撤销启动不可用
startup.openapi_maintenance_failed              OpenAPI 维护不可用
```

（另 5 条是原本代码里就写着、只是从未登记：`gb28181.sip.duplicate_start_rejected`、
`gb28181.device.link_closed_offline`、`gb28181.message.device_config_failed`、
`gb28181.register.version_header_abnormal`、`gb28181.sip.link_superseded`。）

新增字段 9 个：`alarms`、`attempt_ids`、`detached_rows`、`effective_version`、`pending`、
`remote_addr`、`saved`、`warning_code`、`x_gb_ver`。

> `x_gb_ver` 保留的是代码里的原始写法（它对应附录 I 的 `X-GB-Ver` 报文头）。若你认为该按语义
> 改名（例如 `raw_gb_version`），改代码后重跑 regenerate 即可，我可以一并处理。

## 四、验证结果

```
go build ./...                                  退出码 0
go vet（改动涉及的 6 个包）                      干净
go test ./internal/loggingcontract/             ok   17.9s
go test ./internal/loggingcatalog/              ok
```

门禁 findings：**54 → 0**。

## 五、遗留问题（本轮未处理）

1. **37 个 Go 文件不符合 `gofmt`**（缩进层级或 import 排序）。
   注意：这**不是**本轮引入的，也不是工作区改动的产物 —— `git show HEAD:<file> | gofmt -l` 同样报错。
   一次性清理命令：`cd server && find . -name '*.go' -not -path './third_party/*' -print0 |
   xargs -0 gofmt -w`（建议单独一个 commit）。
2. 审计报告 `code-standards-audit-2026-09-18.html` 里「gofmt 零不合规」是**错的**（原因：当时
   `gofmt` 不在 PATH，报错被 `2>/dev/null` 吞掉，`wc -l` 把零输出读成零不合规）。报告已就地勘误。
