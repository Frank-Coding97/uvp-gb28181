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
https://<本机IP>:15500
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
| 15500 | 管理页面（HTTPS，浏览器入口） |
| 15501 | HTTP → 跳转 HTTPS；另保留明文 API 供设备扫码接入 |
| 15502 | 后端 API（仅本机） |
| 15503 | Redis（仅本机） |
| 15504-15513 | 流媒体（ZLMediaKit）各协议 |
| 15514-15563 | 流媒体 RTP 动态端口段 |
| 15564 | SIP 信令（**由平台引导页录入**，不在启动预检范围内） |

端口是**连续无空洞**的 15500-15564，一条防火墙规则即可覆盖。以管理员 PowerShell 执行一次：

```powershell
New-NetFirewallRule -DisplayName 'UVP'     -Direction Inbound -Action Allow -Protocol TCP -LocalPort 15500-15564
New-NetFirewallRule -DisplayName 'UVP-UDP' -Direction Inbound -Action Allow -Protocol UDP -LocalPort 15511,15513,15514-15564
```

> 启动脚本会把上面两条命令**按本机实际端口**打印出来，直接照抄即可，不用手改。

### 为什么 Windows 版是 15500 段，Linux 版却是 51000 段？

**不是笔误，是 Windows 逼出来的。** 两个原因：

1. **系统保留**：Windows 有一段「动态端口范围」（默认 `49152-65535`，本机实测被改成了
   `50000-55000`）。落在这段里的端口会被系统**保留**，任何程序去绑定都直接返回
   「拒绝访问」——而且**没有任何报错提示**：
   - 流媒体服务（ZLM）的 TCP 端口绑得上，一到 RTP 的 **UDP** 端口就失败 ⇒ 进程起来 1 秒后自己退出；
   - SIP 的 UDP 端口同样绑不上 ⇒ 客户的国标设备**永远注册不上**，而平台侧看不出任何异常。

2. **避开常见软件**：一开始选的段是 `15000-15064`，但实测发现 **`15000` 被深信服终端软件
   占用**（`SangforPromoteService`，政企内网机很常见）。绿色包默认端口撞上它就**开箱即挂**，
   所以整段上移到 `15500`，避开这一带。

所以 Windows 版整段用 **15500-15564**（在系统保留区之外，也避开常见软件）。
代价是**与 Linux 版端口不一致**，对照如下：

| 用途 | Windows 版 | Linux 版 |
|---|---|---|
| nginx HTTPS | 15500 | 51000 |
| nginx HTTP | 15501 | 51001 |
| 后端 API | 15502 | 51002 |
| Redis | 15503 | 51003 |
| 流媒体各协议 | 15504-15513 | 51004-51013 |
| 流媒体 RTP 段 | 15514-15563 | 51014-51063 |
| SIP 信令 | 15564 | 51064 |

**跨平台迁移（Linux → Windows 或反向）时**，把 `config\config.env` 里的 4 个端口
改成目标平台那一列，再 `stop` → `start` 即可；SIP 端口在**引导页**里改（不走 config.env）。

> 如果你想确认当前机器上哪些端口段被系统占着，管理员 CMD 执行：
> `netsh int ipv4 show excludedportrange protocol=udp`（`tcp` 同理）。
> 启动脚本的端口预检会**逐个真实绑定一次**，一旦发现被保留就直接报错并给出这条命令。

---

## 四、改端口（可选）

只有 4 个端口允许自定义，其余端口由它们派生。编辑 `config\config.env`：

```ini
UVP_HTTPS_PORT=15500
UVP_NGINX_HTTP_PORT=15501
UVP_HTTP_PORT=15502
UVP_REDIS_PORT=15503
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
data\                   uvp.db（数据库）+ redis AOF + process-authority\（运行期状态）
logs\                   全部日志（后端、nginx、流媒体、Redis）
run\                    运行期文件（pid 文件、临时目录）
resource\               前端页面产物 + 建库脚本
```

**备份**只需拷贝 `data\` 和 `config\` 两个目录。

---

## 六、常见问题

**浏览器打不开页面**

1. 先执行 `status.cmd`，看四项是否都「运行中」。
2. 四项都正常却打不开 → 检查地址是不是写成了 `http://`（要用 **https://**，端口 **15500**）。
3. 只有「管理页面」未运行 → 看 `logs\nginx-error.log`。

**启动时提示「以下端口已被占用」**

说明端口被别的程序占了。按提示先执行 `stop.cmd`；如果仍被占用，就是别的软件在用，
需要改端口（见第四节）或先关掉那个软件。

**启动时提示「以下端口被 Windows 系统保留」**

这**不是**被别的软件占用，而是这些端口落在系统的「动态端口范围」里，
任何程序都绑不上（原因与查法见第三节）。处理办法：把 `config\config.env` 的端口段
整体挪到保留区之外（保持连续即可），再 `stop.cmd` → `start.cmd`。

**`status.cmd` 里「流媒体」显示未运行，而启动时明明打了「已启动」**

ZLM 是**先绑 TCP、再绑 UDP**：它可能在 TCP 那一步成功、UDP 那一步失败后立刻退出。
启动脚本现在会在启动后再等一拍复查（进程是否还在、端口是否还在听、日志里有没有
绑定失败的记录），发现问题**当场报错**而不是留到 `status` 才暴露。

**端口被占着而强行启动会怎样**

nginx 会连到别的服务上，而 `status.cmd` 仍报「运行中」——
浏览器里看到的会是**另一个系统的页面**，且全程没有任何报错。

**换了台机器，二维码扫不出接入信息**

二维码里带的是「设备能访问到的地址」，在 `config\config.yml` 的
`gb28181.qr_provision.base_url`。如果这台机器换了 IP，把它改成新的本机 IP
并重启即可（形如 `http://192.168.1.10:15502`）。

**解压到 D:\ / E:\ / F:\ 这类数据盘，需要额外做什么吗**

不需要 —— 启动脚本会自动把 `data\process-authority` 的权限收紧成
「当前用户 + SYSTEM + 管理员」并断开继承。

这一步是必需的：Windows 给**数据盘根目录**的默认权限里包含
`Authenticated Users: 修改`，子目录会继承它；而后端出于防篡改考虑，
**拒绝**在「其他账户可写」的目录上启动（它要保证进程互斥记录不被篡改）。

如果这一步没成功（例如包是从**别的账户**拷贝过来的，当前用户不是该目录的所有者），
表现为：`logs\app\` 里出现 `phase=process_authority reason=state_unavailable`，
且后端进程起来几秒就退出。用**管理员**身份执行一次下面的命令即可：

```
icacls "本包路径\data\process-authority" /inheritance:r ^
       /grant:r "%USERDOMAIN%\%USERNAME%:(OI)(CI)F" ^
       /grant:r "SYSTEM:(OI)(CI)F" /grant:r "Administrators:(OI)(CI)F" /T
```
