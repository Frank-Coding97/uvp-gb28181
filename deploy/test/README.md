# UVP GB28181 日志部署说明

`deploy/test/configure_server.py` 仍是完整的部署初始化脚本。使用
`--logging-profile` 只选择日志策略，不改变现有的 wvp 容器 DNS、数据库、
Redis、ZLMediaKit 或网络拓扑；默认值为 `container`。`server.appdebug` 只
控制 Gin 模式和调试路由，不会隐式切换日志策略。

| profile | `logs.outputs` | 文件格式 | 标准输出格式 | 适用场景 |
| --- | --- | --- | --- | --- |
| `development` | `file`, `stdout` | 文本/console | 文本/console | 本地开发 |
| `direct` | `file` | JSON | JSON（被 outputs 关闭） | 直接运行/systemd |
| `container` | `stdout` | JSON（未使用） | JSON | Docker Compose |

三个 profile 都生成相同的日志基础参数：相对路径
`./resource/logs/uvp-gb28181.log`，根级别和 access/scheduler 模块级别为
`info`，关闭路由登记日志，单文件 5 MiB、最多 7 个备份、保留 15 天。仓库
模板仍保留本地开发基线 `./resource/logs/ginfast.log`；生成配置时显式选择
profile 才覆盖为部署路径。

相对路径按应用 `BasePath` 解析，而 `BasePath` 由进程启动时的工作目录设定。
因此 `/app` 下的落点是
`/app/resource/logs/uvp-gb28181.log`，systemd 服务的落点是
`/opt/uvp-gb28181/current/backend/resource/logs/uvp-gb28181.log`，不会把
`/app` 与 `./resource` 直接拼成 `/app./resource`。旧字段里的
`/resource/logs/...` 仍按项目相对路径处理；新字段 `logs.filepath` 中的
`/var/...` 等绝对路径则直接作为绝对落点。

新旧字段同时存在时，优先级固定为：

- `logs.filepath > logs.zaplogname`
- `logs.outputs > logs.console`
- `logs.modules.scheduler > scheduler.log.level`

Compose backend 使用有界的 Docker `json-file` 驱动：`max-size: "5m"`、
`max-file: "8"`，并设置 `stop_grace_period: 45s`。应用共用 30 秒优雅
退出窗口，45 秒部署窗口可避免 Compose 默认的较短窗口提前强制终止；具体
语义见 [Compose stop_grace_period 文档](https://docs.docker.com/reference/compose-file/services/#stop_grace_period)。
容器驱动的容量预算独立于应用文件预算，不能把两者相加后宣称总量仍为
40 MiB。

systemd 服务同样设置 `TimeoutStopSec=45s`。直接部署选择 `file`，普通标准
输出关闭；应急 stderr 仍交给现有服务管理器处理，本次不修改宿主机全局
journal 配额。

`data/logs` 是持久化主机目录，`deploy-uvp.sh` 保留 release 侧的
`resource/logs` 目录链接。旧 `scheduler` 目录和未知历史文件不删除。新应用
文件预算为稳态 40 MiB（活动文件 5 MiB 加 7 个备份），轮转临界时按 46 MiB
保守估算；这两个数字都不含历史存量。

变更日志配置需要受控重启。`configure_server.py` 的 `main` 还会检查容器、
初始化或修改数据库用户和 schema 等部署状态，因此不能把它当作只改日志的
无副作用工具，也不要在本地开发为切换 profile 直接执行它。已有部署只需按
上表调整 `logs` 区并重启服务。回滚时旧二进制必须匹配旧配置；现有部署脚本
只切换 binary/release 链接，不会自动恢复配置文件，需在重启前显式恢复旧
配置。回滚不删除日志，也不执行数据库迁移。

仓库内只做以下纯本地检查：

```sh
PYTHONDONTWRITEBYTECODE=1 python3 deploy/test/configure_server_test.py
bash -n deploy/test/deploy-uvp.sh
```

这些检查不会运行配置生成器、Docker、部署脚本、远程主机或数据库。
