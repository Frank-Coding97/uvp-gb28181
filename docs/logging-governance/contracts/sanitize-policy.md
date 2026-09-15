# 脱敏策略契约（`app/utils/logging/sanitize.go`）

> 层：**第一层 · 管道**（T03 的收尾）。T01–T16 把脱敏管道建起来了，但
> **"规则本身是否还有意义"从未审计过**。本文档就是那次审计。
> - **状态**：✅ 2026-09-15 完成（`reason` 取消特例 + 两处数组字段改可见文本）
> - 判据来源：本目录上级 [`README.md`](../README.md) 第「判据三问」

---

## 一、为什么这算治理项，而不是"安全加固"

判据 ②是：**看到这条日志，运维要做什么动作？**

被 `[text omitted]` 吞掉的字段有两种可能：

| 情形 | 后果 | 处置 |
|---|---|---|
| 这条日志本来就没用 | 删掉即可 | 判据②，属 C03 |
| **这条日志很有用，但答案正好在被我吞的字段里** | **看起来正常、实际空心** | 本文档 |

第二种比噪声贵得多：噪声你会删，空心日志你会**当成已经有定位信息**。
它同时污染了治理本身——扫描脚本、门禁、人眼都会把 `reason="[text omitted]"`
算作"这个字段带了"，于是**指标是绿的，排障是瞎的**。

---

## 二、审计（2026-09-15，口径：`server/**/*.go` 排除 `_test.go`）

### 2.1 字符串规则的可达性

`stringFieldPolicy()` 只有两种特殊字符串规则。全仓库 **110 个 `zap.String` 字段键**逐一核对后：

| 键 | 规则 | 生产调用点 | 判定 |
|---|---|---|---|
| `reason` | `stringFieldOmit` | **10** | ⛔ **误判**——取值全是受控码，见 §2.2 |
| `error` | `stringFieldOmit` | 0 | ✅ 守卫保留（§3.3） |
| `err` | `stringFieldOmit` | 0 | ✅ 守卫保留 |
| `panic` | `stringFieldOmit` | 0 | ✅ 守卫保留 |
| `recover` | `stringFieldOmit` | 0 | ✅ 守卫保留 |
| `detail` | `stringFieldOmit` | 0 | ✅ 守卫保留 |
| `endpoint` / `url` | `stringFieldURL` | **9** | ✅ 活的，且**有门禁依赖**，不要改名（§3.4） |

**结论：6 条 omit 规则里 5 条是死的，唯一活着的那条是错的。**

### 2.2 `reason` 装的是受控码，不是自由文本

10 个调用点逐一看过，取值全部是程序自己选的短码：

```
no_candidate · ambiguous_candidate · ambiguous_attempt · attempt_terminal
invalid_correlation_key · attempt_lookup_failed · candidate_lookup_failed
protocol_key · attempt · timeout · device-offline · owner-node-mismatch
required-node-unavailable · recovery-pending · channel-not-found · accepted …
```

来源：`ptz/handler.go:findPTZMessageOperation()` 的 8 个返回点、
`play/auto_start_dispatcher.go:autoStartResultReason()` 的 switch、`hook.go` 的固定文案。
**没有一个是由外部输入拼接的**——`reason` 是"结论标签"，不是"原文"。

### 2.3 实测证据：代价有多大

当前日志文件里：

```
$ grep -c '\[text omitted\]' server/resource/logs/ginfast.log
600
```

600 条**全部来自同一个来源**——`ptz.response.unmatched`（GB28181 PTZ 应答无法唯一关联），
约 5 秒一条：

```
WARN ptz  GB28181 PTZ 应答无法唯一关联  sn=27955 event=ptz.response.unmatched
  deviceCode=35020000001310000999 cmdType=PTZPosition
  candidateOperationIds=[omitted:zap.stringArray] reason="[text omitted]" body_bytes=196
```

这一行的**全部意义**就是回答"为什么关联不上"，而答案由两个字段承载——**两个都被吞了**。
`deviceCode`/`cmdType`/`sn` 都在，唯独结论不在。

### 2.4 类型省略的生产调用面

`sanitizeField()` 对数组/反射/Stringer/Any 一律省略。生产调用点只有 3 处：

| 调用点 | 判定 |
|---|---|
| `bridge.go:120`（`zap.Reflect` 桥接第三方 slog 值） | ✅ **必须保留**——第三方值不可信 |
| `ptz/handler.go:201`（`candidateOperationIds`） | ⛔ 已知受控列表，改成可见文本 |
| `main.go:159`（`newly_applied` 迁移版本列表） | ⛔ 同上 |

---

## 三、改动

### 3.1 `reason` 取消错误特例（`sanitize.go`）

`reason` 从 omit 列表移出，走普通字符串路径：**保留、受 2048 字节裁剪、被裁时置 `truncated=true`**。
不新增规则、不新增代码路径——修法就是"停止把它当特例对待"。

### 3.2 两处数组字段改为单行文本

清洗核心**永不调用第三方 ArrayMarshaler**（`TestLoggingSanitizeUnknownObjects` 用
带副作用的 `hostileValue` 锁死了这一点，所以我不能"顺手支持数组"）。做法是改调用点：

- `candidateOperationIds`：`strings.Join(candidateIDs, ",")`
- `newly_applied`：`strings.Join(added, ",")`

**空列表用 `zap.Skip()` 让字段缺席**，而不是打出 `candidateOperationIds=""` 冒充有值——
空串会被脚本与门禁算成"已带定位字段"，正是 §一 说的那种假阳性。

### 3.3 保留的 5 个守卫，以及为什么不删

`error` / `err` / `panic` / `recover` / `detail` 当前**零调用点**。保留理由是它们是**绊线**：

- 全部是"错误派生文本"的命名，且都有**结构化替代品**—— `logging.Error(err)` 会渲染
  `class`/`type`/`code`（`summarizeError()`），比原文更有用且不泄漏。
  省略字符串形式，就是**逼调用点走结构化那条路**。
- 一旦有人写 `zap.String("error", err.Error())`，原文可能含 DSN、主机名、路径。
  守卫让它在**第一次提交时**变成 `[text omitted]`，而不是悄悄进生产日志。
- 代价为零：反正没有调用点。

**注意**：删掉它们不等于"清理死代码"，等于撤掉绊线。要删必须同时想清楚用什么替代。

### 3.4 不要改名 `endpoint`

`endpoint` 命中 `stringFieldURL` 规则（裁到 `scheme://host`，丢掉 userinfo），
而 `internal/loggingcontract/entrypoint_test.go:TestLoggingSeedNodeUsesSanitizedEndpoint`
**正是一条门禁测试**，它断言 `bootstrap.go` 必须用 `endpoint` 打节点地址。
改名会让节点密码随 URL userinfo 直接进日志。**这条字段名是契约，不是习惯。**

---

## 四、新增调用点时的约束

1. **受控码**（程序自己选的枚举/标签）→ 可以用 `reason`。
2. **错误原文 / 外部输入 / 自由文本** → 用 `logging.Error(err)`；**不要**塞进 `reason`。
   真有需要就预清洗后再传，或另用一个语义更窄的键。
3. **列表** → 汇成单行文本（`strings.Join`）；**空列表用 `zap.Skip()`**，不要留空串。
4. **地址类** → 必须用 `endpoint` / `url`（§3.4）。
5. **不要为了指标好看去放宽判定**：脚本认不出 ≠ 治理失败；让脚本撒谎才是。

---

## 五、验收（可复跑）

```bash
cd server
# 1. 规则仍是活的：reason 保留、守卫仍生效、超长被裁且报 truncated、控制台仍单行
go test -count=1 -run TestLoggingSanitize ./app/utils/logging/
# 2. 快路径不被绕过：reason 走快路径且与慢路径逐字节等价；omit 键仍被拒
go test -count=1 -run 'TestLoggingUnchangedEntry' ./app/utils/logging/
# 3. 端到端：走真实 runtime，reason 与候选列表都能落到 sink
go test -count=1 -run TestLoggingBackgroundEventsUnmatchedKeepsAnswer ./app/gb28181/ptz/
# 4. 生产代码里除桥接层外不应再有类型省略调用点
grep -rnE 'zap\.(Strings|Any|Reflect|Stringer|Array)\(' --include=*.go . | grep -v _test.go | grep -v bridge.go
```

第 4 条目前应为**空**。

**实机复核（需重启后端，本机未做）**：重启后对上表那条 PTZ WARN 取一行，应看到
`reason=no_candidate` 之类的真实码、且不再出现 `[text omitted]`。

---

## 六、残留风险（明确记录，不隐藏）

- **`reason` 现在会保留文本。** 若将来某个调用点把含凭据的字符串塞进 `reason`，它会进日志。
  约束见 §四第 2 条；`sensitiveKey()` 只管**键名**，不会救这个场景。
- **2048 字节上界**对"一行一段码"绰绰有余，对"一段错误原文"不算宽——这是有意的：
  真要打原文应该先想清楚为什么要打。
- **本文档只审计了字符串与类型两条省略路径**，未审计 `sensitiveKey()` 的键名匹配是否
  有"该脱敏却漏了"的反向问题（那是另一条线）。
