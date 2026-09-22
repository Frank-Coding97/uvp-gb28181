# 国标服务配置「一个配置一个接口」设计评审

- 日期：2026-09-21
- 被评审对象：`/api/gb28181/sip/service-config/*` 这一组接口的**拆分粒度**，以及使用它的前端页面
  `web/src/views/gb28181/sip/ServiceConfig.vue`
- 评审视角：架构师视角（拆分是否支付了成本、是否兑现了收益）
- 一句话结论：**保存语义层是对的，资源粒度层是错的** —— 当前按「一个配置项 = 一个读写资源」一刀切，
  但拆分本该换来的三样东西（权限粒度、跨项原子性、批量读写）一样都没换到，成本却全额付了。

---

## 1. 总评与评级

| 维度 | 评级 | 依据（一句话） |
|---|---|---|
| 单项保存语义 | **B+** | 每项 PUT 是真正的保存单元：校验 → Set → SaveConfig → 失败回滚内存值，58 条单测齐 |
| 验收有效性 | **A-** | `go vet` 干净、测试真跑真过（见附录）；每项都有 defaults / persists / rolls back / rejects 四类断言 |
| 批量读写与一致性 | **D** | 17 个独立读接口无原子快照；17 次写 = 17 次全量重写 config.yml；跨项零原子性 |
| 权限与可运营粒度 | **C-** | 34 个接口只挂在 **2 个**权限码上；且已漏登记 4 条 GET（静默） |
| 前端健壮性 | **D+** | 任一 GET 失败 ⇒ 保存按钮永久禁用，其余 16 项全被锁死 |

**定性成因**：拆分颗粒度选在了「资源」层，而收益全部在「组」层。
17 个配置项里，**只有 3 项**真正需要独立端点（有独立校验约束或后置副作用），
另外 14 项是「一个 bool 一个 URL」的同构复制 —— 拆开它们没有换来任何能力，只换来 14 份样板与 14 次写盘。

> 直接回答「合不合理」：
> - **读接口拆 17 个**（`GET /service-config/<item>` × 17）：**不合理**。读是纯快照、无副作用，
>   拆开只让页面加载发 17 个请求，且拿不到一致快照（并发改两项时读到的是拼接的中间态）。
> - **写接口拆 17 个**：**部分合理**，但理由用错了。3 项有独立副作用/约束，值得独立端点；
>   另外 14 项应合进一个批量端点。
> - 正确形态不是「全拆」也不是「全合」，是 **少数独立 + 一个批量**。

---

## 2. 事实基线（被评审对象的实际形态）

**后端**

- 34 个接口，17 对 GET/PUT，全平铺在一级路径下：`server/app/gb28181/routes/routes.go:712-748`
  （`gb.Group("/sip/service-config")` 块，34 行注册）。
- 控制器 `server/app/gb28181/controllers/service_config.go`（765 行）；
  16 个 PUT handler 结构同构：`bind 指针字段 → 判 nil 即 400 → 校验 → Set×N → SaveConfig → 失败回滚 → 回显`。
- 拆分动机写在源码注释里（`service_config.go:101-102`）：
  > 「这里不复用 `/api/config/update`，避免页面提交时覆盖系统和安全配置」
  —— 动机本身是对的（防止整页提交覆盖无关配置），问题出在实现的粒度选择。

**前端**

- `ServiceConfig.vue` 共 1530 行（template 从 846 起，脚本 845 行）。
- 17 组 `xxxLoading / xxxSaving / xxxReady` + 17 组 `savedXxx` = 79 行 ref 声明（`L62-L140`）。
- 三个巨型聚合 computed：`hasChanges`（`L207-226`，17 项 `||`）、
  `configSaving`（`L227-246`）、`configReady`（`L247-266`，17 项 `&&`）。
- 17 个 `loadXxxConfig()`，`onMounted` 里 `Promise.all` 并发发出 **18 个 GET**
  （17 项 + `loadPlaybackProtocolOptions`，`L822-843`）。
- **整页只有一个「保存配置」按钮**（`L864-873`），点击后执行 `saveConfig()`（`L627-820`，194 行）：
  按 17 个 `xxxChanged` 判断，**串行**调用最多 17 个 PUT。

**权限**

- 全部 34 个接口只映射到 2 个权限码：`gb28181:sip:config:view`（GET）/ `gb28181:sip:config:update`（PUT）
  —— `server/resource/database/gb28181/button-permissions.json`。
- 操作日志模块名也是整段一个：`server/app/middleware/operationlog.go:278`
  （`{"/gb28181/sip/service-config", "GB28181服务配置"}`）。

**同仓的相反范式（对照物）**

- 设备配置中心把 7 个 `ConfigType` 家族压成 **2 个接口**（通用容器 + 参数区分）：
  `device-management/channel/:id/device-configs` GET/POST —— `routes.go:930-931`。
  同一个仓库里，另一处同类问题（多个配置族读写）选的是「一个容器接口 + 类型参数」。
  两套范式并存本身说明**这件事没有统一的设计口径**。

---

## 3. 做对了什么（不要误伤的部分）

1. **每项 PUT 内部的失败回滚是真的**：`SaveConfig` 失败会把内存值恢复（如
   `service_config.go:166-172`、`config.go:269-274`、`config.go:367-371`），
   避免了「文件没写成功但内存已改」的运行时漂移。
2. **逐项校验比集中校验更严格**：`UpdatePlaybackSettings` 强制「必须提交完整配置」
   （`service_config.go:136-144`），`ValidatePlayAuthSettings` 还做了跨项约束
   （`config.go:242-251`，绑定 IP 必须开鉴权）。
3. **拆分的正当理由确实存在，而且真被判定了**：只有 3 项有真实差异 ——
   - SIP 日志：保存后要触发 **SIP 服务重载**，且要回报 `applied` 失败态（`service_config.go:301-311`）；
   - 播放鉴权：保存后要**更新运行时 TTL**（`L248-253`），且被 OpenAPI 播放隔离锁定时必须拒绝停用（`L236-247`）；
   - 固定地址播放：`autoOnDemandEnabled` 依赖 `fixedAddressEnabled`（`config.go:333-338`）。
4. **验收覆盖扎实**：`service_config_test.go` 1248 行 / 58 个用例，每项都有
   defaults、persists、rolls back、rejects invalid 四类；`go vet` 无告警 ⇒ 测试真在跑。
5. **前端只发变更项**（17 个 `xxxChanged` 判断），没有无脑全量 PUT。

---

## 4. 问题清单

### P0-1 ⛔⛔ 「一个保存按钮」掩盖了「17 次独立提交」——部分成功时用户不知情

- 位置：`ServiceConfig.vue:627-820`。
- 机制：`saveConfig()` 串行 await 17 个 PUT；任一项 `response.code !== 0` 就 `throw`
  （`L640 / L651 / L660 / …`），后面的项**不再执行**，走 `catch`（`L796-800`）：
  - `catch` 只恢复 3 个草稿（`restoreFixedAddressPlaybackDraft / restorePlayAuthDraft / restorePlaybackSettingsDraft`，`L797-799`）；
  - 已成功的前 N 项**已经落盘、不回滚**，但页面只弹一句 `保存国标服务配置失败`（`L800`）；
  - 因为 `Message.success("国标服务配置已更新")` 只在**全部走到最后**才发（`L795`），
    部分成功时用户**只看到失败**，会以为全都失败了。
- 影响：运维在「改了 5 项、第 4 项失败」时无法判断现场真实状态，只能逐项去核对配置文件。
- 这**不是接口拆分的必然结果**，而是"拆了 17 个保存单元，却只给了一个汇总反馈通道"的直接后果。
- 修法（低成本，不需要动接口）：`catch` 里按 `xxxSaving` 状态回报**已成功 / 已失败**的确切项名清单；
  或把串行循环改成收集「(项名, 结果)」后统一提示。

### P0-2 ⛔⛔ 任一读接口失败 ⇒ 保存按钮永久禁用，其余 16 项全部被锁死

- 位置：`configReady`（`ServiceConfig.vue:247-266`）是 17 个 `xxxReady` 的**逻辑与**；
  而每个 `loadXxxConfig()` 的 `catch` **不设 ready**（如 `L515-519`）。
- 后果链：某项 GET 失败 → 该 `xxxReady` 保持 `false` → `configReady=false` →
  保存按钮 `:disabled="… || !configReady …"`（`L868`）**永久禁用**；
  该项显示的是默认值（`createStaticServiceConfigDraft()` 的初值），控件同时被
  `!xxxReady` 禁用（如 `L889`）。用户唯一出路是刷新页面；若该 GET 持续失败，
  **整页永远无法保存任何配置**。
- 讽刺之处：17 个接口拆分本意是「互不影响」，前端一个 `&&` 又把它聚合回「全或无」。
- 修法：`configReady` 拆成"按本次要保存的项"判断；失败项给独立的「重新读取」入口，不牵连其它项。

### P0-3 ⛔⛔ 17 次保存 = 17 次全量重写整个 config.yml

- 位置：每个 PUT 最终都调 `app.ConfigYml.SaveConfig()`（`service_config.go` 全篇；
  `config.go:269`、`config.go:312`、`config.go:367`）。
- `SaveConfig()` 的实现是**全量重写**：`AllSettings()` → `ReadInConfig()` → 逐个 `Set` 回来 → `WriteConfig()`
  （`server/app/utils/ymlconfig/ymlconfig.go:262-284`）。
- 于是：改 5 个配置项 = **5 次全量重写 config.yml**（本机该文件约 7KB）+ 5 次 fsnotify WRITE 事件。
- viper 的 `WriteConfig` 是**覆盖写、非原子**（无 tmp + rename）。写窗口被放大 17 倍；
  一旦写出半截文件，重启时 `CreateYamlFactory` 会直接 `os.Exit(1)`（`ymlconfig.go:18-28`），
  即**整个平台起不来**（这个爆炸半径是既有的，不是拆分引入的，但被拆发放大了暴露面）。
- 对照：一个批量端点只需 1 次 `Set×N` + 1 次 `SaveConfig`。
- 修法见 §5 的 P1。

### P1-1 ⛔ 拆分没有换来权限粒度（收益未兑现）

- 34 个接口 → **2 个**权限码（`button-permissions.json` 全量核对结果）。
- 拆分接口最常被引用的正当理由（"按配置项授权"）在这里不存在：
  能改 SIP 日志的人必然能改云台速度，反之亦然。
- 也就是说，这一层拆分实际只得到了**「一个配置一个 URL」**，没有得到**「一个配置一个权限」**。
  若未来真的要按项授权（例如"只许运维改日志保留天数"），当前路径结构反而要先做一次破坏性重构。

### P1-2 ⛔ 无聚合读接口 ⇒ 页面拿不到一致快照，且首屏 18 个请求

- `onMounted` 里 `Promise.all` 并发 18 个 GET（`L822-843`）。
- 「并发」≠「一致」：某管理员点保存的同时另一个人在改另一项，页面读到的是**跨 17 个接口时间点拼接**的状态，
  且没有任何 `version` / `etag` 能发现这件事。
- 已有前车之鉴：本仓 `TestFixedAddressPlaybackRuntimeReadIsAtomicDuringSave`
  （`service_config_test.go:243`）专门测了"运行期读到的一定是完整的一对值"——
  说明团队**已经意识到成对配置的原子读问题**，但只在这一对内部解决了，没有提升到整面配置。

### P1-3 ⭕ 并发保护只做了 1/17

- 只有 playback-settings 有 `playbackSettingsMu`（`service_config.go:108 / 129 / 155`）
  并配了 `TestServiceConfigController_UpdatePlaybackSettingsSerializesConcurrentWrites`
  （`service_config_test.go:380`）。
- 其余 16 项靠 `ymlConfig` 的 `Set`/`SaveConfig` 各自加锁，但 **`Set` 与 `SaveConfig` 不在同一临界区**，
  跨项的读-改-写序列不原子；失败回滚路径也只恢复自己那几个 key。
- 单管理员场景下实际风险低（且 `SaveConfig` 是全量写，不会丢别的 key），
  但这说明"逐项独立"的假设在代码里**没有被系统性地兑现**，只是零散补了一处。

### P2-1 登记面已产生实际遗漏（拆分成本的账单）

- 17 条 PUT **全部**登记；17 条 GET **只登记了 13 条**，缺 4 个：
  `GET /service-config/position-history`、`/sdp-extension`、`/default-channel-stream-transport`、`/playback-settings`。
- 因为 `gb28181:sip:config:view` 已被其它 GET 覆盖，**功能上完全无感，所以静默**——
  正是"接口数 × N 带来的登记负担"才让这种遗漏成为低概率但必然发生的事故。
- 注：这条本身属于前端按钮权限 ↔ 菜单库登记面的范围（细节归 `uvp-button-permission-audit`），
  此处只作为**拆分的运营成本证据**记录，不与主结论合并。

### P2-2 前端样板代码量与新增一项的成本

- 新增 1 个配置项要动 **10 处**：后端 struct + 2 个 handler + 2 条路由 + 前端 2 个 api 函数 +
  2 个类型 + 3 个 ref + 1 个 changed + 1 个 loader + `saveConfig` 里 1 个分支 +
  3 个聚合 computed 各加一行 + `button-permissions.json`。
- 17 项已经让三个聚合 computed 各 20 行、`saveConfig` 194 行。
  继续按此模式加项，成本是**超线性**的。

---

## 5. 建议处理顺序

| 顺序 | 动作 | 成本 | 收益 | 是否动接口 |
|---|---|---|---|---|
| 1 | **P0-2**：`configReady` 改为"只 gate 本次要保存的项"；失败项加「重新读取」 | 低（纯前端） | 解开"一个 GET 挂掉锁死整页" | 否 |
| 2 | **P0-1**：`saveConfig()` 失败时回报已成功/失败的项清单，别只弹总错误 | 低（纯前端） | 运维能自证现场状态 | 否 |
| 3 | **P1-2**：新增 `GET /api/gb28181/sip/service-config` 聚合读（返回全量 + 一个 `revision`） | 中 | 18 请求 → 1；拿到一致快照 | 加（不改旧） |
| 4 | **P0-3 + P1 主体**：新增 `PUT /api/gb28181/sip/service-config` 批量写（body 只带变更项，服务端一次 `Set×N` + **一次** `SaveConfig`；把 sip-log 重载、play-auth TTL 等后置动作在内部按序触发） | 中高 | 写盘 17 次 → 1 次；跨项真正原子；权限/审计可收成一个"保存配置"动作 | 加（旧 17 个 PUT 保留） |
| 5 | **P1-1**：批量端点落地后，把 14 个"无副作用"的 PUT 标为 deprecated / 下线，只留 3 个有副作用的独立端点 | 中 | 34 → 约 5 个接口 | 是（需迁移窗口） |
| 6 | **P1-3**：给批量端点一把锁 + 一条并发单测（照抄 `Test…SerializesConcurrentWrites` 的写法） | 低 | 把 1/17 的并发保护补成 1/1 | 否 |
| 7 | **P2-1**：补齐 4 条未登记的 GET | 低 | 消除静默遗漏 | 否 |
| 8 | **P2-2 / 健壮性**：`ymlconfig.SaveConfig` 改 tmp + rename 原子写 | 低 | 消除"写坏即启动即死" | 否 |

**推荐的最小可行改法**：先做 1+2（一天内的前端改动，零风险），
再做 3+4（加两个聚合端点、旧端点全部保留），确认页面切过去之后再执行 5。

---

## 6. 附录：复现命令

```bash
cd /Users/menglulu/code/uvp/UVP-GB28181

# 1) 接口数量与形状
sed -n '712,748p' server/app/gb28181/routes/routes.go

# 2) 验收有效性（技能 §2 的手法）
cd server && export PATH=/opt/homebrew/Cellar/go/1.25.6/bin:$PATH
go vet ./app/gb28181/controllers/... ./app/gb28181/config/...   # 实测：无输出、exit 0
go test ./app/gb28181/controllers/ -run 'TestServiceConfig' -count=1
# 实测：ok  uvplatform.cn/uvp-gb28181/app/gb28181/controllers  1.104s

# 3) 前端聚合逻辑（17 项 && / || 的位置）
grep -nE "const (hasChanges|configSaving|configReady) =" web/src/views/gb28181/sip/ServiceConfig.vue
#   207:hasChanges  227:configSaving  247:configReady

# 4) 全量写盘证据
sed -n '262,284p' server/app/utils/ymlconfig/ymlconfig.go

# 5) 权限粒度（34 个接口 → 2 个权限码；GET 少登记 4 条）
python3 - <<'EOF'
import json
d=json.load(open('server/resource/database/gb28181/button-permissions.json'))
m={}
for b in d['buttons']:
    for a in b.get('apis',[]):
        p=a.get('path') or ''
        if 'service-config' in p:
            m.setdefault(p,set()).add((a.get('method'), b.get('permission')))
print('路径数', len(m))
for p in sorted(m): print(' ', sorted(m[p]), p)
EOF
```

---

## 7. 本次未覆盖范围（声明）

- **只审了**「国标服务配置」这一套（后端控制器 + 配置层 + 前端页面 + 权限登记）。
- 同页面的 `/api/gb28181/sip/setup/*`（初始配置向导）与「国标级联相关」tab 只在路由层扫过，
  未逐行审；它们是否也沾同一套问题，**未结论**。
- 「17 次写盘」是**读代码**得到的结论（`SaveConfig` 全量重写 + 17 个独立 PUT），
  **没有做磁盘 IO 实测**，也没打真流量验证并发场景。
- 未评估「去掉 14 个 PUT」对**存量第三方调用方**的影响（`api/gb28181.yaml` 是否对外发布未核）。
- 未审查这 17 项配置的**取值口径本身**是否合理（默认值、范围）——
  那是产品口径问题，不属于"接口拆分形状"的评审范围。
