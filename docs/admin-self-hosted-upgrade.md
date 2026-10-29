# 自建发布站点与在线升级接入

本文面向负责发布和运维的管理员，说明如何把签名升级清单放到自建 HTTPS 站点，并让 UVP 实例从该站点检查和下载更新。运行中的 UVP 不需要访问 GitHub。

## 适用范围

当前发布器处理 Linux 的六个目标：

```text
standalone/linux/amd64
systemd/linux/amd64
compose/linux/amd64
standalone/linux/arm64
systemd/linux/arm64
compose/linux/arm64
```

正式发布时必须提供完整矩阵，除非在隔离测试中显式传入 `--target` 收窄范围。缺少目标、多出未声明目标、制品大小或 SHA-256 不匹配都会阻止发布。

本文不是 Linux 双架构、systemd、Docker daemon、外置数据库恢复或浏览器端到端验收的替代证据。目前尚未完成真实六目标 Linux 矩阵验收，也没有配置生产发布域名、Registry 或生产签名密钥。

## 站点约定

设发布根地址为 `https://updates.example.com/uvp-gb28181`。静态站点应保留以下结构：

```text
channels/stable/manifest.json
channels/beta/manifest.json
releases/<release_id>/manifest.json
releases/<release_id>/<artifact-file>
```

`releases/<release_id>` 是不可变目录。发现错误时创建新的 `release_id` 和新的签名清单，不能覆盖已发布制品。通道指针在所有制品和 release manifest 均可用后最后更新。

建议对 `channels/*/manifest.json` 设置短缓存或 `Cache-Control: no-store`，对 `releases/<release_id>/*` 使用长期不可变缓存。站点必须使用受实例信任的 HTTPS 证书，不能把通道请求重定向到未登记的域名或 HTTP。

`scripts/publish-upgrade-release.mjs` 只写入本地静态站点目录，不会访问网络或自动上传。发布平台应先把该目录同步到隔离的站点暂存区，验证下载路径后再按“release 目录先到位、通道指针最后切换”的顺序完成站点侧发布。

## 构建身份

每个发行使用一个唯一的 `build_id`。正常发行建议使用完整 Git commit；从非干净工作区构建时必须显式分配独立 ID，不能把本地改动伪装成某个 commit。

同一 `build_id` 会进入以下位置：

- 后端二进制的 Go `-ldflags`；
- 前端 Vite 的 `VITE_BUILD_ID`，并在浏览器根元素上写入非展示型 `data-uvp-build-id`；
- 包内后端和前端 `version.json`；
- 已签名 release manifest 的 `build_id`。

发行脚本会扫描构建后的 `dist/**/*.js`，确认编译产物实际带有该标识。仅修改 `version.json` 不能通过该检查。

## 签名与本地发布

签名私钥只应存在于受控 CI 密钥存储或隔离发布机。不要将 PEM 文件提交到仓库、放进升级包或写入应用配置。

准备 release payload 时，`version` 和 `build_id` 必须与本次全部制品一致，URL 必须指向最终自建站点的不可变 release 目录。使用测试密钥和测试域名验证流程时，可按如下形态生成签名清单：

```bash
node scripts/sign-upgrade-manifest.mjs \
  --payload release.json \
  --key-id release-2026 \
  --private-key /secure/release-2026.pem \
  --artifact 'standalone/linux/amd64|out/standalone-amd64.tar.gz|https://updates.example.com/uvp-gb28181/releases/r-20261016/standalone-amd64.tar.gz' \
  --artifact 'systemd/linux/amd64|out/systemd-amd64.tar.gz|https://updates.example.com/uvp-gb28181/releases/r-20261016/systemd-amd64.tar.gz' \
  --artifact 'compose/linux/amd64|out/compose-amd64.tar.gz|https://updates.example.com/uvp-gb28181/releases/r-20261016/compose-amd64.tar.gz' \
  --artifact 'standalone/linux/arm64|out/standalone-arm64.tar.gz|https://updates.example.com/uvp-gb28181/releases/r-20261016/standalone-arm64.tar.gz' \
  --artifact 'systemd/linux/arm64|out/systemd-arm64.tar.gz|https://updates.example.com/uvp-gb28181/releases/r-20261016/systemd-arm64.tar.gz' \
  --artifact 'compose/linux/arm64|out/compose-arm64.tar.gz|https://updates.example.com/uvp-gb28181/releases/r-20261016/compose-arm64.tar.gz' \
  --out out/r-20261016-manifest.json
```

先执行 dry-run。未传 `--target` 时，发布器要求上述完整六目标矩阵：

```bash
node scripts/publish-upgrade-release.mjs \
  --site-dir /srv/uvp-updates-staging \
  --base-url https://updates.example.com/uvp-gb28181 \
  --release-manifest out/r-20261016-manifest.json \
  --artifact out/standalone-amd64.tar.gz \
  --artifact out/systemd-amd64.tar.gz \
  --artifact out/compose-amd64.tar.gz \
  --artifact out/standalone-arm64.tar.gz \
  --artifact out/systemd-arm64.tar.gz \
  --artifact out/compose-arm64.tar.gz \
  --private-key /secure/release-2026.pem \
  --key-id release-2026 \
  --published-at 2026-10-16T00:00:00Z \
  --expires-at 2026-11-16T00:00:00Z \
  --dry-run
```

确认暂存站点内容、签名、下载地址和 SHA-256 后，移除 `--dry-run` 才会写入本地站点目录。该操作不会把内容发布到外部网络；外部站点发布仍由发布平台的受控部署步骤负责。

重新签发同一 release 的通道有效期时，使用 `--refresh --channel-sequence <new-sequence>`，并且不传制品。`--refresh` 只更新已存在且摘要一致的通道指针，不会改写 release 目录。

## 实例接入

每个实例的 `config.yml` 配置受信任发布源和公钥。值示例中的域名、公钥和部署类型必须替换成该实例的实际值：

```yaml
upgrade:
  release_source: "https://updates.example.com/uvp-gb28181/channels/stable/manifest.json"
  trusted_keys:
    release-2026: "<Ed25519 public key in base64 or hex>"
  deployment: "standalone"
```

绿色安装包构建时可用 `UVP_UPGRADE_TRUSTED_KEYS` 注入同一组发行公钥，值为 JSON 对象，键是签名清单使用的 `key_id`，值为 Base64 或十六进制 Ed25519 公钥。例如 Linux 构建：

```bash
UVP_UPGRADE_TRUSTED_KEYS='{"release-2026":"<Ed25519 public key>"}' bash deploy/standalone/build-standalone.sh
```

Windows 构建前在 PowerShell 设置同名环境变量即可。Compose 包在安装时生成配置，安装时也可用同名变量注入：

```bash
sudo UVP_UPGRADE_TRUSTED_KEYS='{"release-2026":"<Ed25519 public key>"}' ./install.sh
```

该输入用于配置发行公钥；发行私钥仍只交给签名工具，不能放进包或构建配置。未设置时保留源配置的 `upgrade.trusted_keys`；当前默认模板为空，因此不传公钥时离线升级不可用。`upgrade.release_source` 仍可按实例单独配置。

`deployment` 只能使用该实例已经注册的 `standalone`、`systemd` 或 `compose` 方式。不要让页面或调用者提交任意下载地址、unit 名称、Compose 文件或宿主机命令。

首次把历史安装纳入在线升级前，需要先完成实例注册并验证助手 socket、共享状态目录、数据目录、业务用户以及部署适配器。升级助手会在停止业务前检查来源构建、迁移声明、配置摘要、备份条件和候选启动条件；通过在线检查不等于可以直接应用。

### Docker Compose 首次接入

> **现状（2026-10-29）：容器版部署方式尚未对外发布，官方升级通道里也没有 `compose` 制品。**
> 发布脚本默认只给 Linux 绿色包标 `standalone` 与 `systemd`；`manifest.ArtifactFor` 按
> `deployment/os/arch` 精确匹配，所以容器实例「检查更新」会直接报
> `manifest has no artifact for compose/<os>/<arch>`，这是**预期行为**，不是故障。
>
> 要让容器实例真正可升级，两件事缺一不可：
> 1. 产出**容器版升级包**（包内需有注册的 `compose.yml` 与 `images/*.tar`，并在制品里写 `images` 元数据）。
>    拿 Linux 绿色包冒充 compose 制品会在准备阶段报
>    `upgrade package is missing registered compose file`——虽然此时还没停服务，但升级必然失败。
> 2. 按下面步骤完成宿主机助手接入；默认安装脚本只建目录，**不会**自动装 `uvp-updater.service`。

Compose 实例的升级助手运行在宿主机 systemd 中，不运行在业务容器内。这样升级期间停止或重建后端容器时，任务记录、回退和 gate 仍然存在。首次安装包应包含 `updater/uvp-updater`；接入脚本只在安装根缺少稳定助手时复制它，之后的应用升级不会替换正在执行的助手。

先在 `config/config.yml` 将 `upgrade.deployment` 设为 `compose`，并配置本实例的 `release_source` 和 `trusted_keys`。然后以 root 执行接入脚本；已有项目通过环境变量登记其真实 project 目录、名称和 Compose 文件：

```bash
sudo \
  UVP_ROOT=/opt/uvp-gb28181 \
  UVP_COMPOSE_PROJECT_DIR=/opt/uvp-gb28181/current \
  UVP_COMPOSE_PROJECT=uvp-gb28181 \
  UVP_COMPOSE_FILE=/opt/uvp-gb28181/current/compose.yml \
  /opt/uvp-gb28181/current/updater/uvp-compose-upgrade-install.sh
```

脚本会校验 Compose 配置、写入仅 root 可读的注册记录，并启动宿主 `uvp-updater.service`。之后可按已登记的参数执行普通 `docker compose up -d`；模板中的一次性 gate 初始化服务会在首次启动时创建只读挂载源，不会覆盖已有 candidate 或 recovery gate。使用 Docker user namespace 或 rootless Docker 时，把容器在宿主机上的实际 UID 通过 `UVP_UPDATER_ALLOWED_UID` 显式登记后再执行脚本。

## 无人值守策略

无人值守默认关闭。管理员确认以下条件后，才可设置维护窗口自动应用：

- 发布源和可信公钥已在目标实例配置完成；
- 目标部署类型、架构和升级助手协议与已签名清单匹配；
- 备份和候选验证路径已在相同部署形态中完成验收；
- 当前版本与迁移状态在清单的兼容声明内。

窗口外只下载、校验和预检；进入窗口后仍可能因空间、备份、迁移、就绪或签名检查失败而保持阻止状态。出现 `rolled_back` 或 `recovery_required` 时，以助手任务记录和本机诊断为准，不按浏览器页面是否恢复来判断升级成功。

## 发布前核对

- `build_id`、后端运行时身份、前端已编译 JavaScript、包内元数据和签名 manifest 一致。
- 六目标制品均来自同一个已核对的构建输入；签名后任一制品替换、截断或缺失都会被 SHA-256 检查拒绝。
- release manifest 和 channel pointer 的 Ed25519 签名均可用目标实例内置的公钥验证。
- 静态站点从目标网络可访问，且运行时请求不会触及 GitHub。
- 生产发布前在隔离环境完成对应的 Linux 架构、部署方式、数据库和恢复测试；不要用本地脚本测试代替这些证据。
