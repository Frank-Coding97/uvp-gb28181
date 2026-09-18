# 第一层 · 管道建设档案（T01–T16）

**状态：全部完成。** 判定依据 `git cherry develop codex/logging-*` 输出 `未进develop=0`。

这套任务是 TDD 推进的：每个提交都在 message 里写了 **RED（失败断言）→ GREEN（通过）** 的验收叙述。
下面逐项记录**目标 / 验收原文 / 产物**，供回溯与核对。

> 复跑总判定：
> ```bash
> cd server && for b in $(git branch --list "codex/logging-*" | sed 's/^[* +]*//'); do
>   printf "%-38s 未进develop=%s\n" "$b" "$(git cherry develop $b | grep -c '^+')"
> done
> ```

---

## T01 · HTTP 访问清单归入 API 迁移任务

- **提交**：`32abc518`
- **目标**：把 HTTP 访问类日志的现状盘成清单，挂到 API 迁移任务上，作为后续关联改造的输入。
- **产物**：`internal/loggingcontract/` 的 `scanner.go` / `sites.go` / `resolve.go` / `routes.go` / `types.go` + `testdata/`（6 个）

## T02 · 配置快照 + 模块感知运行时

- **提交**：`09bb991e`
- **验收原文**：
  > RED: config/output/format/module behavior assertions failed against inert runtime.
  > GREEN: five config/runtime tests pass with immutable identity bindings and restart-only config notices.
  > Real sinks and safety filtering follow in T03/T04 before bootstrap integration.
- **产物**：`app/utils/logging/config.go` (+251)、`logger.go` (+224)、`config_test.go` (+193)
- **要点**：身份绑定（service/version/instance）不可变；配置变更只发"需重启"通知。

## T03 · 字段脱敏 + 旧日志桥接

- **提交**：`cb3cb20e`
- **验收原文**：
  > RED: six safety contracts exposed credentials, object formatting, CheckedEntry bypass and oversized output;
  > extra invalid-UTF8 case exposed 49KiB output and missing event.
  > GREEN: all seven safety tests pass.
  > Bound unknown objects/errors and third-party text before Tee/With encoding.
- **产物**：`sanitize.go` (+341)、`bridge.go` (+124)、`logger.go`、`sanitize_test.go` (+179)
- **要点**：这是"日志不会反噬自己"的核心——脱壳式错误处理（不调用任意 `Error()`）、对象/第三方文本有界化。

## T04 · 文件保留上界 + sink 故障上报

- **提交**：`4b73ed48`
- **验收原文**：
  > RED: rotation/retention/fault/locking/link/Close assertions failed;
  > added reproductions for invalid emergency window and deletion before gzip publication.
  > GREEN: 14 writer/sink tests and failure matrix pass. Darwin race and Linux amd64 compile verified.
  > Fixed dirfd/no-follow operations, exclusive publish, bounded compression recovery, per-sink counters and safe emergency output.
- **产物**：`writer.go` (+500)、`sink.go` (+224)、`writer_fs_unix.go` (+150)、
  `writer_rename_{darwin,linux}.go`、`writer_test.go` (+492)、`sink_test.go` (+135)
- **要点**：`O_NOFOLLOW` + `flock` + inode 校验防符号链接攻击；`.tmp` 可恢复压缩；每 sink 计数。

## T05 · 启动期与 SIP 日志桥接

- **提交**：`845fe68c`
- **产物**：`bootstrap.go` (+31)、`server/bootstrap/init.go`（-197 净减，把内联 zap 构造换掉）、
  `sip/server.go`、`uac/uac.go`、`ymlconfig.go`、`config.example.yml`、
  **删除** `app/service/zaphooks.go`(-27)
- **要点**：启动期与 SIP 接入新框架，配置结构随之收敛。

## T06 · HTTP 访问关联 + 安全恢复

- **提交**：`2344746d`
- **产物**：`app/utils/ginhelper/logging.go`（新增 +98）、`logging_test.go` (+361)、
  `ginhelper.go`（-140 净减）、`app/utils/logging/context.go` (+31)、`app/utils/response/response.go`
- **要点**：HTTP 访问日志与请求上下文关联，崩溃/异常路径也能安全落日志。

## T07 · GORM 日志结构化 + 保留 DB 上下文

- **提交**：`321bd522`
- **产物**：`app/utils/gormhelper/log.go`（重写，317 行变更）、`logging_test.go` (+238)、
  `app/global/app/logging.go`、`gormhelper/client.go`
- **要点**：SQL 日志结构化（不再是整段 SQL 塞进一个中文键），并保留 DB 上下文。

## T08 · HTTP 基线与审计上下文传播

- **子分支**：`t08-audit`（+1）、`t08-datascope`（+2）、`t08-models`（+1）
- **产物**：`app/utils/datascope/`（4 文件）、`middleware/demoaccount.go`、
  `service/loginlogservice.go`、`models/{sysmenu,sysrole,sysdepartment}.go`、`logging_models_test.go`
- **要点**：审计与数据范围（datascope）上下文能传播到日志。

## T09 · 国标 HTTP 与业务结果关联

- **子分支**：`t09-hook`（+1）、`t09-play`（+1）、`t09-zlm`（+1）
- **产物**：`app/gb28181/play/`（3 文件，含 `logging_async_context_test.go`）、
  `handler/`（3）、`zlm/`（2）、`snapshot/`（2，含 `logging_async_context_test.go`）
- **要点**：**异步路径**也要保住请求上下文（goroutine 里日志仍能关联到原请求）。

## T10 · 调度器执行作用域关联

- **提交**：`2980b8f6`、`7850a864`、`87ac00f0`
- **产物**：`app/utils/schedulerhelper/`（5 文件）、`gb28181/recordingplan/`（2）、`scheduler/executors/`
- **要点**：调度执行有作用域标识，且补了动作日志等级的测试。

## T11 · 重复失败限流 + 共享维护

- **子分支**：`t11-cascade`（+2）、`t11-heartbeat`（+1）
- **产物**：`gb28181/zlm/`（3）、`bootstrap.go`、`cascade_runtime.go`、
  `internal/loggingcontract/logging_cascade_test.go`
- **验收叙述**：`settle stale repeat state before a new failure` / `count maintenance failures without regressing totals`
- **要点**：重复失败聚合（避免刷屏），且计数不能因限流而失真。

## T12 · 关闭前排空应用工作

- **子分支（10 个）**：`async` / `background` / `bootstrap` / `config` / `generation` / `register` /
  `scheduler` / `sip` / `startup-failure` / `trace`
- **产物**：**59 个文件**。含 `third_party/sipgo/sip`（12）、`gb28181/zlm`（7）、`handler`（6）、
  `traffic`（3）、`ginhelper`（3）、`asyncgroup`（2）、`trace`（2）
- **要点**：关闭序列有界——HTTP drain、SIP 关闭失败传播、job 结果持久化等待、
  config 回调优雅停止。**这条保证"最后一条日志一定写得下去"。**

## T13 · 部署模板与持久化契约

- **子分支**：`t13-deployment`（+2）
- **产物**：`deploy/test/{compose.yml,configure_server.py,configure_server_test.py,uvp-backend.service,README.md}`、
  `config/config.example.yml`、`app/utils/logging/`
- **验收叙述**：`verify deployment YAML and persistence contract` / `retire scheduler fields from the deployment template`
- **要点**：部署模板与运行时配置一致，旧 scheduler 字段退役。

## T14 · 媒体与辅助业务事件命名

- **子分支（7 个）**：`casbin` / `demoaccount` / `policy` / `reconcile-events` / `scheduler-policy` /
  `sip-events` / `subscribe-events`
- **产物**：**50 个文件**。含 `loggingcontract/testdata`（11）、`handler`（8）、`play`（5）、
  `ptz`（4）、`recording`（3）、`casbinhelper`（3）、`schedulerhelper`（3）、`streammonitor`（2）
- **要点**：**这是事件命名最集中的一批**——媒体与辅助业务的事件补齐命名。
  它就是第二层 C01（事件目录）的直接前身：命名有了，但**没成册**。

## T15 · HTTP 日志预算与负载边界

- **子分支**：`t15-http`（+2）、`t15-load`（+2）
- **产物**：`ginhelper`（1）、`app/utils/logging`（1）
- **验收叙述**：`measure HTTP request logging budget` / `cover load and resource bounds` /
  `count repeated sink maintenance faults`
- **要点**：单请求日志有预算（不能一个请求打爆磁盘），负载下有界。

## T16 · 隔离 MySQL 验收辅助

- **子分支**：`t16-database`（+1）、`t16-routes`（+1）
- **产物**：`internal/loggingacceptance/database_test.go`、
  `internal/loggingacceptance/routes_logging_acceptance{,_test}.go`、`server/main_logging_acceptance.go`
- **要点**：验收套件能连真库跑完整链路。

---

## 遗留观察（不属于任何 T 的收尾）

1. **验收套件不在任何自动流程里**：`internal/loggingacceptance` 带 `//go:build logging_acceptance`
   标签，需显式开 tag 才编译。T16 写了隔离 MySQL 验收，但没人自动跑它。
2. **分支残留**：36 个 `codex/logging-*` 分支全部已等价并入 develop，可视为可清理的残留
   （清理前先确认对应的 36 个 worktree 无未跟踪改动）。
