# UVP-GB28181 统一视频接入平台 · Windows 绿色安装包

解压即用。**不需要预先安装任何东西** —— 数据库（SQLite）、缓存（Redis）、
流媒体（ZLMediaKit）、Web 服务器（nginx）全部随包自带。

---

## 一、启动

1. 把整个压缩包解压到一个**路径不含中文、不含空格**的目录，例如 `D:\uvp`。
2. 双击 **`start.cmd`**。

首次启动会自动完成三件事（大约 1 分钟）：

- 建库：创建 SQLite 数据库 `data\uvp.db`（111 张表 + 初始数据 + 管理员账号）
- 发证书：生成 nginx 与流媒体用的自签名证书
- 写配置：把端口、ZLM 密钥等同步进 `config\` 下的配置文件

启动完成后，按提示在浏览器打开：

```
https://<本机IP>:51000
```

> 用的是自签名证书，浏览器首次会提示「不安全 / 证书不受信任」，选择**继续访问**即可。

**初始账号：`admin` / `admin123`** —— 登录后请立即修改密码。

---

## 二、停止 / 查看状态

| 操作 | 方式 |
|---|---|
| 停止全部服务 | 双击 `stop.cmd`（或 `.\uvp-gb28181-ctl.ps1 stop`） |
| 查看运行状态 | 双击 `status.cmd`（或 `.\uvp-gb28181-ctl.ps1 status`） |
| 重启 | `.\uvp-gb28181-ctl.ps1 restart` |

也可以用管理员 PowerShell 手工执行：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\uvp-gb28181-ctl.ps1 start
```

> 提示：如果双击 `.cmd` 一闪而过，说明脚本内部报错了。请在**当前目录**打开
> PowerShell（地址栏输入 `powershell` 回车）再手工跑一次上面那条命令，就能看到完整报错。

---

## 三、端口规划（局域网访问需要放行）

| 端口 | 用途 |
|---|---|
| 51000 | 管理页面（HTTPS，浏览器入口） |
| 51001 | HTTP → 跳转 HTTPS；另保留明文 API 供设备扫码接入 |
| 51002 | 后端 API（仅本机） |
| 51003 | Redis（仅本机） |
| 51004-51013 | 流媒体（ZLMediaKit）各协议 |
| 51014-51063 | 流媒体 RTP 动态端口段 |
| 51064 | SIP 信令（**由平台引导页录入**，不在启动预检范围内） |

端口是**连续无空洞**的 51000-51064，一条防火墙规则即可覆盖。以管理员 PowerShell 执行一次：

```powershell
New-NetFirewallRule -DisplayName 'UVP'    -Direction Inbound -Action Allow -Protocol TCP -LocalPort 51000-51064
New-NetFirewallRule -DisplayName 'UVP-UDP' -Direction Inbound -Action Allow -Protocol UDP -LocalPort 51011,51013,51014-51064
```

---

## 四、改端口（可选）

只有 4 个端口允许自定义，其余端口由它们派生。编辑 `config\config.env`：

```ini
UVP_HTTPS_PORT=51000
UVP_NGINX_HTTP_PORT=51001
UVP_HTTP_PORT=51002
UVP_REDIS_PORT=51003
```

> **必须**同时加一行 `UVP_USER_SET_UVP_HTTPS_PORT=1` 之类的标记，否则会被当成
> "老包里的旧默认值"在下次启动时改回去。改完执行 `stop.cmd` → `start.cmd`。

`config.env` 里**其他内容请勿删除** —— 里面有 SIP 报文诊断密钥和 OpenAPI 主密钥，
缺了对应功能会静默不可用（页面能打开，但相关菜单永远是空的）。

---

## 五、目录说明

```
uvp-gb28181-ctl.ps1     唯一入口（PowerShell 控制脚本）
start.cmd / stop.cmd / status.cmd    供双击的包装
bin\                    本平台自己的程序（uvp-server.exe / uvp-setup.exe）
vendor\redis\           自带 Redis
vendor\zlm\             自带流媒体服务 ZLMediaKit（MediaServer.exe）
vendor\nginx\           自带 Web 服务器（前端 + HTTPS 终止）
config\                 config.yml（后端配置）+ config.env（环境变量/密钥）
data\                   uvp.db（数据库）+ redis AOF + secrets\（证书）
logs\                   全部日志（后端、nginx、流媒体、Redis）
run\                    运行期文件（pid 文件、临时目录）
resource\               前端页面产物 + 建库脚本
```

**备份**只需拷贝 `data\` 和 `config\` 两个目录。

---

## 六、常见问题

**浏览器打不开页面**

1. 先执行 `status.cmd`，看四项是否都「运行中」。
2. 四项都正常却打不开 → 检查地址是不是写成了 `http://`（要用 **https://**，端口 **51000**）。
3. 只有「管理页面」未运行 → 看 `logs\nginx-error.log`。

**启动时提示「以下端口已被占用」**

说明端口被别的程序占了。按提示先执行 `stop.cmd`；如果仍被占用，就是别的软件在用，
需要改端口（见第四节）或先关掉那个软件。

**端口被占着而强行启动会怎样**

nginx 会连到别的服务上，而 `status.cmd` 仍报「运行中」——
浏览器里看到的会是**另一个系统的页面**，且全程没有任何报错。

**换了台机器，二维码扫不出接入信息**

二维码里带的是「设备能访问到的地址」，在 `config\config.yml` 的
`gb28181.qr_provision.base_url`。如果这台机器换了 IP，把它改成新的本机 IP
并重启即可（形如 `http://192.168.1.10:51002`）。
