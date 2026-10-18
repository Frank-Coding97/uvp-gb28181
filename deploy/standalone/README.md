# UVP-GB28181 统一视频接入平台 · Linux 绿色安装包

解压即用。**不需要预装任何东西** —— 数据库（SQLite）、缓存（Redis）、流媒体
（ZLMediaKit）、Web 服务器（nginx）全部随包自带。只需要：

- x86_64 或 aarch64 的 Linux，**glibc 2.28 及以上**（银河麒麟 V10 SP2 / 统信 UOS /
  openEuler 等国产系统均可）；
- **python3（3.5 及以上）** —— 首次启动建库、改端口要用它（多数发行版自带）。

---

## 一、启动

```bash
tar -xzf uvp-gb28181-*.tar.gz
cd uvp-gb28181-*            # 解压出来的目录就是「包根」，后面所有命令都在这里执行
./uvp-gb28181-ctl.sh start
```

> 如果提示 `Permission denied`，说明解压工具丢了可执行位：
> `chmod +x uvp-gb28181-ctl.sh` 即可（或直接用 `bash uvp-gb28181-ctl.sh start`）。

首次启动会自动建库（约 1 分钟，会打印进度），完成后输出访问地址：

```
https://<本机IP>:51000
```

> 用的是自签名证书，浏览器首次会提示「不安全 / 证书不受信任」，选择**继续访问**即可。

**初始账号：`admin` / `123456`** —— 登录后系统会**强制要求修改密码**。

> 口令来自 `config/config.yml` 的 `server.demoaccount.defaultpassword`，`start` 的输出里
> 也会把它打出来；如果你改过配置，以配置为准。**把自己设的新口令忘了**见第四节。

`start` 是**幂等**的：重复执行只会把没起来的组件补起来。

---

## 二、日常操作

| 操作 | 命令 |
|---|---|
| 启动 | `./uvp-gb28181-ctl.sh start` |
| 停止 | `./uvp-gb28181-ctl.sh stop` |
| 重启 | `./uvp-gb28181-ctl.sh restart` |
| 查看状态 | `./uvp-gb28181-ctl.sh status` |

**开机自启**（可选，需要 root）：

```bash
./uvp-gb28181-service-install.sh          # 注册 + 开机自启（不立即启动）
./uvp-gb28181-service-install.sh --now    # 注册并立即接管（会先停掉手工实例）
./uvp-gb28181-service-uninstall.sh        # 卸载服务注册（不动数据）
```

服务名是 `uvp-gb28181`，之后用 `systemctl status uvp-gb28181` 查看。

---

## 三、端口规划（局域网访问需放行）

整段 **51000-51064 连续无空洞**，一条防火墙规则即可覆盖：

| 端口 | 用途 |
|---|---|
| 51000 | 管理页面（HTTPS，浏览器入口） |
| 51001 | HTTP → 跳转 HTTPS；另保留明文 API 供设备扫码接入 |
| 51002 | 后端 API（设备/App 接入地址） |
| 51003 | Redis（只监听 127.0.0.1，无需放行） |
| 51004-51013 | 流媒体（ZLM）各协议：HTTP/RTSP/RTMP/RTC/WebRTC 信令/SRT/ONVIF/STUN |
| 51014-51063 | 流媒体 RTP 收发端口段（UDP + TCP 成对占用） |
| 51064 | SIP 信令（**由平台引导页录入**，不在启动预检范围内） |

本机若开了防火墙（firewalld / ufw），放行：

```bash
# firewalld（银河麒麟 / openEuler / RHEL 系）
sudo firewall-cmd --permanent --add-port=51000-51064/tcp
sudo firewall-cmd --permanent --add-port=51004-51064/udp
sudo firewall-cmd --reload

# ufw（Ubuntu / 统信 UOS）
sudo ufw allow 51000:51064/tcp
sudo ufw allow 51004:51064/udp
```

> `start` 收尾会把上面两条命令**按本机实际端口**打印出来，直接照抄即可。
> 启动前脚本还会**逐个端口真实绑定一次**做预检；被占用会明确报出「谁占着」，
> 而不是起一半留在那里（端口被别人占着却硬启动时，nginx 会连到别的系统上，
> 而 `status` 仍报「运行中」，浏览器里看到的是别人的页面）。

---

## 四、忘记了登录口令

平台上**没有**「忘记密码」自助入口（登录页那个链接是预留的），口令忘了只能把它
**还原成出厂口令**：

```bash
./uvp-gb28181-ctl.sh reset-password            # 默认重置 admin
./uvp-gb28181-ctl.sh reset-password <账号名>   # 指定账号（一般用不到）
```

它会：

1. 把账号口令还原成出厂口令（同上：`config/config.yml` 的 `server.demoaccount.defaultpassword`）；
2. 标记该账号「首次登录必须改密」—— 所以下次登录后会**强制**让你设一个新口令；
3. 顺手清掉 Redis 里的登录失败/锁定记录（连错 3 次会锁 10 分钟，这一步省得等）。

> ⚠️ 这是「重置」不是「找回」：旧口令**不会被恢复**，改成新口令后请记好。
> ⚠️ 提示「找不到数据库」= 这台机器还没启动过（库里还没有账号），先执行 `start`。
> ⚠️ 命令只动一个账号，**不碰**设备、通道、录像等任何业务数据，也不删库不重建库。
> ⚠️ 它工作在 `data/uvp.db` 这一个文件上，改完即可直接登录（无需重启）；仍登不上就
> `./uvp-gb28181-ctl.sh restart` 再试。

---

## 五、改端口（可选）

只有 4 个端口允许自定义，其余由它们派生。编辑 `config/config.env`：

```ini
UVP_HTTPS_PORT=51000
UVP_NGINX_HTTP_PORT=51001
UVP_HTTP_PORT=51002
UVP_REDIS_PORT=51003
```

> **必须**同时加一行 `UVP_USER_SET_UVP_HTTPS_PORT=1` 之类的标记，否则会被当成
> 「老包里的旧默认值」在下次启动时改回去。改完 `./uvp-gb28181-ctl.sh restart`。

`config.env` 里**其他内容请勿删除** —— 里面有 SIP 报文诊断密钥和 OpenAPI 主密钥，
缺了对应功能会**静默**不可用（页面能打开，但相关菜单永远是空的）。

---

## 六、备份 / 迁移 / 升级

**备份只需拷两个目录**：

```
data/      数据库（uvp.db）+ Redis AOF + 运行期状态
config/    全部配置（config.yml + config.env，含密钥）
```

停服后 `tar -czf uvp-backup-$(date +%F).tar.gz data config` 即可。

- **迁移到另一台机器**：拷 `data/` + `config/` 过去，装同版本包，`./uvp-gb28181-ctl.sh start`。
  ⚠️ 端口段跨平台不同（Linux 51000 段、Windows 15500 段），跨平台迁移要改 `config.env`。
- **升级**：解压新包覆盖原目录（`data/`、`config/` 会保留），再 `restart`。
  ⚠️ 不要删 `data/` 和 `config/`。

如果使用 MySQL 或 PostgreSQL，升级助手会按 `config/config.yml` 生成外置数据库备份注册；备份口令不会写入注册文件。请在 `config/config.env` 或 systemd 的 `EnvironmentFile` 中显式设置
`UVP_UPGRADE_DB_PASSWORD=<数据库备份账号口令>`，也可以用
`UVP_UPGRADE_DB_PASSWORD_ENV=<自定义变量名>` 指定变量名。口令缺失时升级会在停服前阻断，SQLite 不需要该变量。外置备份默认最多 8 GiB；数据库更大时由管理员显式设置
`UVP_UPGRADE_EXTERNAL_BACKUP_MAX_BYTES=<正整数字节数>`，容量预检会按该上限保留空间。SQL Server 可以正常启动，但当前版本不自动执行其备份或在线升级，会在停服前给出受控维护升级提示。

---

## 七、目录说明

```
uvp-gb28181-ctl.sh              唯一入口（启停 / 状态 / 重置口令）
uvp-gb28181-service-install.sh  注册成 systemd 服务（可选，开机自启）
uvp-gb28181-service-uninstall.sh
bin/                    本平台自己的程序（uvp-server）
vendor/redis/           自带 Redis
vendor/zlm/             自带流媒体服务 ZLMediaKit（MediaServer）
vendor/nginx/           自带 Web 服务器（前端 + HTTPS 终止）
config/                 config.yml（后端配置）+ config.env（环境变量/密钥）
data/                   uvp.db（数据库）+ redis AOF + secrets/（运行期密钥）
logs/                   全部日志（含 app/ 后端业务日志、zlm/ 流媒体日志）
run/                    运行期文件（pid 文件）
resource/               前端产物 + 建库脚本（baseline.sql）
```

---

## 八、常见问题

**页面打不开**

1. 先 `./uvp-gb28181-ctl.sh status`，看四项是否都「运行中」。
2. 都正常却打不开 → 地址要用 **https://**、端口 **51000**（写 `http://` 或写成后端端口都不对）。
3. 只有「管理页面」未运行 → 看 `logs/nginx.log`。

**登录页报 `no such table`**

库里没建好（多半是首次启动中途被打断）。先直接重跑 `./uvp-gb28181-ctl.sh start`
（脚本检测到库不完整会自动重建）；仍不行就停服后删掉 `data/uvp.db` 再 `start`
（**只在还没录入业务数据时**这么做 —— 删库等于清空业务数据）。

**提示 python3 版本过旧 / 找不到 python3**

```bash
sudo apt install python3          # 或 yum install python3
# 装了新版但默认 python3 仍偏旧时，可显式指定：
UVP_BUILD_PY=/usr/bin/python3.6 ./uvp-gb28181-ctl.sh start
```

**启动时提示「端口已被占用」**

按提示先 `stop`；仍被占用就是别的软件在用 —— 改 `config/config.env` 的端口段（见第五节）
或先关掉那个软件。

**换了台机器，二维码扫不出接入信息**

二维码里带的是「设备能访问到的地址」，在 `config/config.yml` 的
`gb28181.qr_provision.base_url`。这台机器换了 IP 就把它改成新的本机 IP 并重启
（形如 `http://192.168.1.10:51002`）。
