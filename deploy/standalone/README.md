# UVP-GB28181 统一视频接入平台 · Linux 绿色安装包

解压即用。**不需要预装任何东西** —— 数据库（SQLite）、缓存（Redis）、流媒体
（ZLMediaKit）、Web 服务器（nginx）全部随包自带，**自签证书也随包自带**。只需要：

- x86_64 或 aarch64 的 Linux，**glibc 2.28 及以上**（银河麒麟 V10 SP2 / 统信 UOS /
  openEuler 等国产系统均可）。

> 从 v1.1.6 起**不再需要 Python**（也不再需要 openssl）：建库、改端口、对齐密钥、
> 重置口令全部由包内 `bin/uvp-setup`（纯 Go 静态二进制）完成。老版本需要
> python3 ≥ 3.5，这也是这版去掉它的原因 —— CentOS 7 只带 python2.7。

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
http://<本机IP>:51000
```

> **默认是明文 HTTP**（免证书、免浏览器安全提示）。要用 HTTPS，登录后到
> **系统设置 → 系统配置 → HTTPS** 打开开关、指定端口即可（**证书已随包内置**，不传也能用；
> 只有想换成客户自己的证书时才需要上传）；
> 改完平台会自动重载并给出新地址，重启后也保持你的选择。
> 开 HTTPS 时若继续用自签名证书，浏览器会提示「不安全 / 证书不受信任」，选择**继续访问**即可。

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
注册服务时会**同时**装一个 `uvp-gb28181-firewall-agent`（root，nftables）—— 见下面「主机防火墙代理」。

### 主机防火墙代理（国标接入安全 · 内核级封禁）

**国标接入安全**页面的「保护 / 严格」模式有两层防护：应用层准入（后端进程内直接丢弃报文）
和**内核级封禁**（把攻击源 IP 写进 nftables，在 raw/prerouting 上丢包）。第二层由
`bin/uvp-firewall-agent` 执行，它需要 **root 权限**：

| 启动方式 | 内核级封禁 | 说明 |
|---|---|---|
| `sudo ./uvp-gb28181-ctl.sh start` | ✅ | 代理以 root 启动，socket 在 `run/firewall-agent.sock` |
| `./uvp-gb28181-ctl.sh start`（普通用户） | ❌ 仅应用层 | 页面显示「主机防火墙 降级」 |
| `sudo ./uvp-gb28181-service-install.sh` | ✅ | 代理由 systemd 以 root 单独托管（推荐） |

> 普通用户启动不是错误：平台功能不受影响，只是少一层内核封禁。
> 内核封禁还需要目标机装了 **nftables**（`nft` 命令）；缺了代理会在 `logs/firewall-agent.log` 里报错，
> 页面同样显示「降级」，不会影响平台启动。
> ⚠️ 用 `sudo ./uvp-gb28181-ctl.sh start` 启动时，`data/`、`logs/`、`run/` 会变成 root 属主；
> 之后如果要改回普通用户或注册 systemd 服务，先 `sudo chown -R <用户>:<组> data logs run`。
> 想长期以内核封禁方式运行，**首选注册服务**（代理由 systemd 托管，业务仍按原用户跑）。
> 状态口径：`./uvp-gb28181-ctl.sh status` 会多打一行「防火墙代理」。

---

## 三、端口规划（局域网访问需放行）

整段 **51000-51064 连续无空洞**，一条防火墙规则即可覆盖：

| 端口 | 用途 |
|---|---|
| 51000 | 管理页面与设备明文入口（HTTP，**默认入口**）；开启 HTTPS 后自动跳转到 51001 |
| 51001 | 管理页面（HTTPS，**默认关闭**，在系统配置里开启后才监听） |
| 51002 | 后端 API（仅由本机 Nginx 转发） |
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

## 五、改端口（可选，**全部 16 个都能改**）

打开 `config/config.env`，**最上面就是「端口区」**（`# [uvp-ports]` 开头那一段）——
出包时已把 16 个端口连同当前默认值写好，直接改数字即可：

```ini
UVP_NGINX_HTTP_PORT=51000
UVP_HTTPS_PORT=51001
UVP_HTTP_PORT=51002
UVP_REDIS_PORT=51003
UVP_ZLM_HTTP_PORT=51004
UVP_ZLM_SSL_PORT=51007
UVP_ZLM_RTSP_PORT=51005
UVP_ZLM_RTMP_PORT=51006
UVP_ZLM_RTC_PORT=51008
UVP_ZLM_RTP_PROXY_PORT=51014
UVP_ZLM_RTP_RANGE=51014-51063
UVP_ZLM_SIGNALING_PORT=51009
UVP_ZLM_SIGNALING_SSL_PORT=51010
UVP_ZLM_SRT_PORT=51011
UVP_ZLM_ONVIF_PORT=51012
UVP_ZLM_ICE_PORT=51013
```

改完 `./uvp-gb28181-ctl.sh restart`，就这些。每个键的用途在文件里的注释段和
`deploy/standalone/PORTS.md` 都有说明。

> ⭐ **不用**再手写 `UVP_USER_SET_…=1` 标记。程序每次启动把生效值记在
> `config/.port-baseline`，下次发现文件里的值跟它不一样，就认定是你改的，
> 自动采纳并补上标记。老包留下的标记仍然有效。
>
> ⛔ `UVP_ZLM_RTP_RANGE` 与 `UVP_ZLM_RTP_PROXY_PORT` 必须同一个起点：
> 只改一个时程序会按你改的那个自动拉齐另一个；两个都改且互相矛盾 ⇒ **拒绝启动**并说明原因。
> 端口还必须 ≥ 1024（本包以普通用户运行，绑不上特权端口）。
>
> ⛔ **SIP 端口不在这个文件里**：登录后在引导页里设置（存数据库），
> 默认值 = RTP 段尾 + 1。改了它，存量设备必须逐台改成同一端口。
>
> ⛔ 改了端口后，`restart` 会重新打印「访问地址」和「防火墙放行命令」——
> 按新打印的来，旧规则里的端口可以删掉。

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
resource/               前端产物 + 建库脚本（baseline.sql）+ 内置证书（cert/）
```

---

## 八、常见问题

**页面打不开**

1. 先 `./uvp-gb28181-ctl.sh status`，看四项是否都「运行中」。
2. 都正常却打不开 → 以 `start` 打印的地址为准：**默认**是 **http://**、端口 **51000**；
   只有在系统配置里开过 HTTPS 才是 **https://** + **51001**。写成后端端口（51002）也不对。
3. 只有「管理页面」未运行 → 看 `logs/nginx.log`。

**登录页报 `no such table`**

库里没建好（多半是首次启动中途被打断）。先直接重跑 `./uvp-gb28181-ctl.sh start`
（脚本检测到库不完整会自动重建）；仍不行就停服后删掉 `data/uvp.db` 再 `start`
（**只在还没录入业务数据时**这么做 —— 删库等于清空业务数据）。

**提示 `bin/uvp-setup 没有执行权限` / `Permission denied`**

解压工具把可执行位丢了。执行一次即可（不需要装任何东西）：

```bash
chmod +x uvp-gb28181-ctl.sh bin/uvp-setup bin/uvp-server updater/uvp-updater
```

> 本包**不需要** Python、OpenSSL、Java、Docker，也不需要 root（注册开机自启才要）。

**浏览器提示证书不受信任 / 证书已过期**

包内自带一套自签证书（`resource/cert/`），装机时直接拷过去，**不再现场生成**。
它只保证"链路加密"，浏览器仍会提示不安全（点「继续访问」即可）；要用真证书，
登录后到 **系统设置 → 系统配置 → HTTPS** 上传。
想让每台机器用一把唯一私钥，执行（然后 `restart`）：

```bash
./bin/uvp-setup certgen --dir vendor/nginx/conf --zlm-pem data/secrets/zlm-ssl.pem --force
```

**启动时提示「端口已被占用」**

按提示先 `stop`；仍被占用就是别的软件在用 —— 改 `config/config.env` 的端口段（见第五节）
或先关掉那个软件。

**换了台机器，二维码扫不出接入信息**

二维码和抓拍回传地址由设备发起请求时的管理入口自动确定，无需在
`config/config.yml` 填写地址。换机或变更网络后，请从设备实际可访问的 Nginx
管理入口重新扫码或触发抓拍；确认防火墙已放行设备入口端口 51000。
