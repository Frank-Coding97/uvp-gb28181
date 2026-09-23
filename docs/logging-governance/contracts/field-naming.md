# 字段命名契约（C02）

> **一句话**：**一个名字一件事。** 名字不同应该是因为**东西不同**，
> 而不是"当时随手写的" —— 但也**不要**把两件不同的事合成一个名字。

本文是 C02 的判定正本。它的结论与计划**相反**：计划假设存在大量"同义别名"，
逐组去代码里核对之后，**绝大多数"看起来像别名"的名字指的是不同的对象**。

---

## 一、判据

每次要给字段改名或合并时，按顺序问三句：

1. **这个名字的值，能不能唯一指向一个对象？**（定位价值）
2. **换个名字，读者会不会误判值的含义？**（如 `duration` 是 `"1m0s"` 还是 `1500`）
3. **两个名字是不是同一个对象？** 是 → 收敛；**不是 → 必须写进本文第五节，说明为什么并存**。

第 3 条的"不是"也要留档 —— 否则下一个人会拿同样的理由再合并一次。

---

## 二、实测：命名风格已经收敛，真正的欠账不在"风格"

| 指标 | 现状 |
|---|---|
| `camelCase`（`deviceId`） | **0** |
| 驼峰+大写 ID（`streamID`） | **0** |
| `snake_case` | 100% |

C08 把它从 **58 → 0**（57 处 / 12 文件），C09 又用**纯格式规则**
`^[a-z][a-z0-9_]*$` 把它永久钉住（不依赖任何基线）。

**所以 C02 计划里的 C02.4–C02.6（按模块批量改 camelCase）已由 C08 完成，
C02.7（门禁校验字段名）已由 C09 完成。** 剩下要裁决的只有"同义不同名"。

---

## 三、真·同义别名的收敛（本次唯一一处）

| 别名 | 规范名 | 证据 |
|---|---|---|
| `body_len` | `body_bytes` | 同一个东西：HTTP body 的字节数（`zap.Int(..., len(body))`）。仓库里 `body_bytes` **4 处** vs `body_len` **2 处**；而 `_len` **不说单位**，`_bytes` 说了。收敛后 `registry.json` 的 `body_len` 条目一并 prune（门禁会报 `stale_field_dictionary_entry`） |

---

## 四、⚠️ 计划里的"别名映射表"大半不成立

`content-plan.md` 的 C02 假设了 6 组别名。逐组去代码里核对之后，**5 组是伪命题** ——
那两个名字指的是**不同的东西**：

| 计划里的假设 | 实测结论 | 证据 |
|---|---|---|
| `deviceCode` / `deviceID` 是 `device_id` 的别名 | ❌ **不同对象** | `device_code` 是**国标编码**（20 位），`device_id` 是**库主键**。`IDENT_BASE` 两者都收，因为排障时"按主键查"和"按国标编码对设备"是两条不同的路 |
| `channelCode` 是 `channel_id` 的别名 | ❌ 同上 | 同上 |
| `server_id` 是 `node_id` 的别名 | ❌ **不同对象** | `server_id` 是 **SIP ServerID**（`gb28181.register.server_id_mismatch`：上级平台编码与本平台 ServerID 不匹配），`node_id` 是**媒体节点主键**。一个是 SIP 域身份，一个是 ZLM 节点 —— 合并会让"注册被拒"和"节点掉了"看起来像同一件事 |
| `media_server_id` 是 `node_id` 的别名 | ❌ **不同对象** | `media_server_id` 是 ZLM 的 **UUID 字符串**（`hook_auth` 拿它比对节点），`node_id` 是**主键**。hook 链路里 UUID 才是载荷给的东西 |
| `peer` 是 `source_ip` 的别名 | ❌ **不同对象、且值格式不同** | C05 已裁决并写进 `scan-logging.py` 的 `IDENT_BASE` 注释：`peer` 是**SIP 复合身份**（`host:port/transport from=用户`），`source_ip` 是**ZLM Hook 回调的裸 IP**。它们同属"对端地址"**维度**，但不是同一件事 —— 合并会制造"同名不同格式"，比两个名字更糟 |
| `state` 是 `status` 的别名 | ❌ **不同对象** | `state` 是**节点状态机**（`active`/`offline`，见 `zlm.probe.*` 的 `state_before`），`status` 是**响应码**（SIP/业务层，见 `cascade.*` 与 `ptz.response.ignored`） |

> ⚠️ **这是 C02 最重要的一条结论。**
> `deviceId` / `channelId` / `streamID` 这类**大小写变体**才是真别名 —— 而它们在 C08 已清零。
> **"看起来像同一件事的两个名字"，绝大多数是两件不同的事。**
>
> 同 C06 的 `is_first` / `catalog_triggered`：**计划里的"改名 / 合并"建议，
> 一律先去代码里核对语义再执行。** 合并两个不同对象的名字，
> 是把"信息不完整"变成"信息错误"。

---

## 五、单位规范：名字要不要带单位，取决于**值的类型**

| 值的类型 | 名字 | 为什么 |
|---|---|---|
| `zap.Duration("interval", d)` | **不带** `_ms` / `_seconds` | 编码后值自带单位（`"1m0s"`）；名字再带单位会与值自相矛盾 |
| `zap.Int64("duration_ms", n)` | **必须带** | 编码后是裸数字 `1500`，读者无法判断单位 |

实测两侧**本来就一致**：

- `zap.Duration` 侧 12 处：`interval` 6 / `elapsed` 2 / `duration` 2 / `offline_threshold` 1 / `check_interval` 1
- 裸数字侧 17 处：`duration_ms` 11 / `body_bytes` 6 / `interval_seconds` 2

唯一例外是 `body_len`（裸数字却不带单位），已在第三节收敛。

⚠️ **不做成门禁**：判断"这个数是不是时间/大小"是自然语言判断，写成正则必然误报
（`count` / `total` / `item_count` 都是裸数字，都不需要单位）。
门禁只做能零例外的那条（`field_name_not_snake_case`）。

---

## 六、不能改的字段：JSON 载荷直通

| 字段 | 来源 | 为什么不能"规范化" |
|---|---|---|
| `regist` | `Regist bool \`json:"regist"\`` | **ZLM Hook 上报载荷的原始字段名**。日志里用同名是故意的 —— 运维拿日志里的 `regist=false` 去对 ZLM 的 JSON，是同一个词 |
| `player` | `Player bool \`json:"player"\`` | 同上 |
| `stream` → ~~`stream_id`~~ | — | ⚠️ 反例：这个**改过**（C06 把 hook/zlm 的 `stream` 统一成 `stream_id`），因为载荷字段名是 `Stream` 而日志里写的是自造名。**判定口诀：这个字段名是"载荷给的"还是"我们自己起的"？** 载荷给的保留，自己起的按字典 |

⛔ **`nodeId` / `channelId` / `serverId` 等同时也是 HTTP API 的 JSON 字段名** ——
所以改日志字段名**永远不能全局字符串替换**，只能匹配 `zap.<Method>("camelName"`。
这条 C08 已写入 `platform-support.md` §五，C02 重申（C02.6 的"批量改"如果按名字硬扫，会破坏接口契约）。

---

## 七、复跑

```bash
cd <repo>
# 命名风格（目标：只剩 snake_case / 单段小写）
python3 docs/logging-governance/scan-logging.py --root . | sed -n '/字段命名风格/,+6p'
# 字典与源码是否一致（含 stale_field_dictionary_entry）
cd server && go test ./internal/loggingcatalog/
```

**验收**：命名风格扫描只剩 `snake_case` + 单段小写 `event`；
本文第三节的别名（`body_len`）出现次数归零（`grep -rn '"body_len"' app/` 无输出）。
