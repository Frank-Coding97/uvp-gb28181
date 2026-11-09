# HTTP 调试集合（Postman / Apifox）

不用写代码就能把接口调通的两份文件：

| 文件 | 给谁用 |
| --- | --- |
| `uvp-openapi.postman_collection.json` | Postman（也可以被 Apifox、Insomnia 直接导入） |
| `uvp-openapi.postman_environment.json` | 可选，多环境切换时用 |
| `uvp-openapi.apifox.json` | Apifox 原生格式（带分组前置脚本） |

两份文件都由 `api/openapi-v1.yaml` 生成，覆盖全部 84 个接口，分成 9 组：
设备、通道、云台 PTZ、设备配置、直播、录像与回放、图像与截图、对讲与广播、报警与布防。

**签名不用你算。** 集合里带了一段前置脚本，每次发请求自动算好 5 个签名头。
手算这条路的坑太深：canonical 是 11 行，少拼一行照样算得出签名，但平台一律返回 401，
而 401 和"AK 填错"看起来完全一样。

---

## 一、导入

**Postman**：`File → Import` → 选 `uvp-openapi.postman_collection.json`。

**Apifox**：二选一

- 推荐：`项目设置 → 导入数据 → Postman` → 选同一个 Postman 文件。脚本会被一并转换。
- 或者：选 `Apifox` 格式导入 `uvp-openapi.apifox.json`。

> Apifox 的原生格式导出较新，若导入后分组或脚本没带过来，直接导入 Postman 文件即可，
> 效果一样。

---

## 二、导入后先填 4 个变量

Postman：点集合名 → `Variables` 标签。Apifox：`环境管理` 里加同名的 4 个变量。

| 变量 | 填什么 | 从哪拿 |
| --- | --- | --- |
| `baseUrl` | `http://<平台IP>:51002` | **必须是 51002** |
| `accessKey` | `uvp_` + 32 位小写十六进制 | 平台后台创建 AK 时给出 |
| `secretKey` | base64url 字符串（无 `=` 填充） | 创建 AK 时**只显示一次**，要当场存好 |
| `audience` | 一串实例标识，如 `uvp-d0353b5113af2724` | 服务器 `<安装目录>/config/config.yml` 的 `openapi.audience` |

`audience` 页面上看不到，只能找运维从服务器配置文件里取。它不在请求头里传，
但参与签名，所以填错一样是 401。

**为什么是 51002**：51000 是前端页面端口，nginx 只转发 `/api/`、`/gb28181/`、`/public/`。
拿 51000 去调 `/openapi/v1/...` 会拿到 **HTTP 200 + 一段 HTML**（前端兜底页），
现象很像"接口坏了"，其实是走错门。

---

## 三、路径变量给了占位值

`deviceId`、`channelId`、`sessionId` 这些是集合变量，预填了**格式合法的假值**，
比如 `deviceId = 34020000001320000001`。

这样设计是为了让"点开就能发"：签名能通过，你会拿到真实的业务响应（比如 404 找不到设备），
而不是缺变量导致的 401。换成你自己的设备/通道编号即可。

`sessionId` 这类只在运行期才存在的值，占位值会让接口返回 404 —— 这是预期的，
把上一步返回的真实 ID 填进去就行。

---

## 四、幂等键

需要幂等的接口（云台控制、录像回放、下载、对讲等）预置了一个
`Idempotency-Key` 占位值，脚本每次发请求都会替换成新的 UUID。

想测**重放**（同一个 key 发两次，看平台是否拒绝或返回缓存结果）：
把那个头的值改成任意固定字符串，比如 `replay-test-1`，脚本会原样保留。

---

## 五、调不通时按这个表查

| 现象 | 原因 | 处理 |
| --- | --- | --- |
| `401` / `AUTHENTICATION_FAILED` | AK、SK、`audience` 三者任一不对 | 逐项核对；SK 是**解码后的 32 字节**参与 HMAC，不是那段文字 |
| `401` / `REQUEST_EXPIRED` | 本机时间与服务器差超过 300 秒 | 对时 |
| `401` / `REQUEST_REPLAYED` | 同一个 nonce 用过一次 | 正常防护；重新发一次即可（脚本每次生成新 nonce） |
| `400 INVALID_REQUEST` | `Content-Type` 带了 `; charset=utf-8` | 必须是**恰好** `application/json`。脚本已处理，手动加头时注意 |
| `400 INVALID_REQUEST` | `X-UVP-Sign-Version` 写成了 `UVP-HMAC-SHA256/1` | 头里只填 `1`；带斜杠那串只是 canonical 的第一行文字 |
| `400 INVALID_REQUEST` | `X-UVP-Nonce` 不是 32 位小写十六进制 | 脚本生成的就是对的，手填时注意 |
| `400` 且提示 query 非法 | query 里出现了 `+` 或空值 | 空格写成 `%20`；平台拒绝原始 `+` |
| `200` 但响应是一段 HTML | 用了 51000 端口 | 换 51002 |
| 脚本报错「缺少变量 audience」 | 变量没填在集合/环境里 | 见第二节 |

集合的前置脚本遇到明显错误会**在发请求前直接抛错**（缺变量、SK 长度不对、
路径里还有 `{{变量}}`、GET 带请求体、query 里有 `+`），错误信息会显示在 Postman 的响应区。
这比让你对着 401 猜要省事得多。

---

## 六、契约变了要重新生成

```bash
python3 sdk/scripts/gen-http-collection.py     # 重新生成三个文件
node   sdk/scripts/verify-http-collection.js   # 验证脚本签名仍与夹具一致
```

不要手改生成出来的 JSON —— 改了会在下次重新生成时丢掉，而且别人不知道。

`verify-parity.py` 会重跑一次生成器并逐字节比对，所以"改了契约忘了重新生成"
会被直接测出来：

```bash
python3 sdk/scripts/verify-parity.py
```

---

## 七、安全提醒

`secretKey` 填进集合后，导出/分享/提交到仓库都会把 SK 一起带出去。
共用机器上建议用环境变量而不是直接写在集合变量里，用完清掉。
平台侧的原则不变：**SK 只放服务端**，浏览器、前端、App 都不该持有。
