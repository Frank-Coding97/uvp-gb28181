# UVP 接入套件

UVP GB28181 平台对外开放接口（`/openapi/v1/*`）的客户端与示例。按语言挑一个目录，抄过去即可。

| 你的技术栈 | 去哪 | 形态 |
|---|---|---|
| Java / Spring Boot | [`java/`](java/) | 完整 SDK（签名、重试、错误映射、幂等键）+ starter + 可运行 demo |
| Python | [`python/`](python/) | **零依赖**客户端（只用标准库）+ 示例，单文件可直接抄 |
| Go | [`go/`](go/) | 标准库参考实现 + 示例，`go run` 就能跑 |
| 先不写代码 | [`http/`](http/) | Postman / Apifox 集合，84 个接口全带上，签名自动算 |
| 任意语言 | 本文「签名规范」一节 | 自己实现只要对齐这一页，务必用 [`fixtures/`](fixtures/) 自证 |

本文路径都相对本仓库根目录。

---

## 接入前必须确认的三件事

这三条占了所有接入问题的九成，而且**报错表现完全一样**（全是 401 或"接口挂了"），只能靠逐条排除。

**1. 用后端端口，不是浏览器端口**

绿色包对外是 51000（nginx，带自签证书），但 nginx 只转发 `/api/`、`/gb28181/`、`/public/`。
`/openapi/v1` **不在其中**，打到 51000 会拿到前端页面的 HTML（HTTP 200），看起来像"接口返回格式不对"。

✅ 用后端端口 `51002`，纯 HTTP，同时绕开自签证书问题：

```
http://<平台IP>:51002/openapi/v1/devices
```

**2. `audience` 必须与平台配置一致**

它参与签名，来自平台 `config.yml` 的 `openapi.audience`。填错不会提示，只会稳定 401。

**3. 本机时钟偏差不超过 300 秒**

平台不提供 server-time 接口，只能靠 NTP 对齐。偏差超限的报错和签名错一样是 401。

---

## Java / Spring Boot

### 拿到依赖

三种方式，按你所在网络选一种：

**A. 内网 Nexus（推荐，需管理员先开通宿主仓）**

```xml
<dependency>
  <groupId>com.uvp</groupId>
  <artifactId>uvp-openapi-spring-boot-starter</artifactId>
  <version>1.0.0</version>
</dependency>
```

```xml
<!-- ~/.m2/settings.xml 或项目 pom.xml -->
<repositories>
  <repository>
    <id>uvp-nexus</id>
    <url>http://192.168.10.220:18083/repository/maven-public/</url>
    <snapshots><enabled>true</enabled></snapshots>
  </repository>
</repositories>
```

> 现状：Nexus 上目前只有 `maven-public` 只读组（账号 `maven-reader`）。
> 要真正 `mvn deploy` 进去，需要管理员新建 `maven-releases` 宿主仓并给一个部署账号。
> 在那之前用 B 或 C。

**B. Release 附件（无需私服）**

从 Releases 下载 `uvp-openapi-*.jar`，装进本地仓一行命令：

```bash
mvn install:install-file -Dfile=uvp-openapi-sdk-1.0.0.jar \
  -DgroupId=com.uvp -DartifactId=uvp-openapi-sdk -Dversion=1.0.0 -Dpackaging=jar
```

**C. 源码（本仓 `java/` 目录）**

```bash
cd java && mvn install -DskipTests
```

### Spring Boot 最小接入

```yaml
uvp:
  openapi:
    base-url: http://<平台IP>:51002   # 后端端口，不是 51000
    access-key: ${UVP_ACCESS_KEY:}
    secret-key: ${UVP_SECRET_KEY:}
    audience: ${UVP_AUDIENCE:}             # 必须等于平台 config.yml 的 openapi.audience
```

```java
@Service
@RequiredArgsConstructor
public class CameraService {

    private final UvpClient uvp;

    public String hlsUrl(String deviceId, String channelId) {
        LivePlaybackResponseData data = uvp.live()
                .startLivePlayback(deviceId, channelId).getData();
        return (String) data.getUrls().get("hls");
    }
}
```

不用 Spring 就直接 new：

```java
try (UvpClient uvp = new UvpClient(
        UvpClientConfig.of(baseUrl, accessKey, secretKey, audience))) {
    DeviceListResponse devices = uvp.devices().listDevices(1, 50, null, "online");
}
```

完整说明（8 个 API 分组、错误分类、幂等、重试策略、真实平台联调记录）见 [`java/README.md`](java/README.md)。
可运行 demo 在 `java/uvp-openapi-demo/`。

---

## Python

零第三方依赖，只要 Python 3.8+。`python/uvp_openapi.py` 可以整个拷进你的项目。

```bash
export UVP_BASE_URL=http://<平台IP>:51002
export UVP_ACCESS_KEY=uvp_xxxxxxxx
export UVP_SECRET_KEY=xxxxxxxx        # base64url，32 字节
export UVP_AUDIENCE=uvp-xxxxxxxx
python3 python/demo_devices.py
```

```python
from uvp_openapi import UvpClient

with UvpClient(base_url, access_key, secret_key, audience) as client:
    page = client.get("/openapi/v1/devices", query={"page": 1, "pageSize": 20})
    for device in page["data"]["items"]:
        print(device["deviceId"], device["name"])
```

只需要签名、不需要客户端时，用 `sign_request()` 拿请求头，自己发请求：

```python
from uvp_openapi import sign_request

headers = sign_request(access_key, secret_key, audience,
                       "POST", "/openapi/v1/devices/{id}/catalog/refresh",
                       idempotency_key="catalog-refresh-0001")
```

---

## Go

```bash
cd go
go run ./cmd/demo --self-test   # 不联网，验证签名实现和平台一致
go run ./cmd/demo               # 真连平台
```

签名部分在 `go/uvp/sign.go`，客户端在 `go/uvp/client.go`，都只用标准库。

---

## 签名规范（自己实现看这一页）

**算法**：HMAC-SHA256，输出小写十六进制。

**请求头**（缺一不可，且每个只能出现一次）：

```
X-UVP-Sign-Version: 1
X-UVP-Access-Key: uvp_<32 位小写十六进制>
X-UVP-Timestamp: <Unix 秒，十进制，无前导零>
X-UVP-Nonce: <32 位小写十六进制，16 字节随机数>
X-UVP-Signature: <64 位小写十六进制>
Idempotency-Key: <写接口可选/必填，128 字节内可见 ASCII>
```

**canonical 字符串**：11 行，`\n` 分隔，**末尾不带换行**：

```
UVP-HMAC-SHA256/1
<AK>
<Timestamp>
<Nonce>
<UPPERCASE METHOD>
<Path>                              ← 不含 query，不以 / 结尾，不含 % 和反斜杠
<CanonicalQuery 或空行>
<ContentType 或空行>                 ← GET 必须空；有 body 时必须是裸 application/json
<BodySHA256 小写十六进制>            ← 原始字节的 SHA-256，空 body 是 e3b0c442...b855
<Idempotency-Key 或空行>            ← ⚠️ 最容易漏，漏了必 401
<Audience>
```

**键**：SK 是 base64url（**无填充**）编码的 **32 字节**，HMAC 用解码后的字节，不是 SK 文本。

**Query 规范**：拆 `&` 与 `=` → 解码一次 → 按 RFC3986 重编码（空格是 `%20` 不是 `+`，大写十六进制）→ 按 key 排序。
空段、重复 key、缺等号、分号、裸 `+`、非法 `%` 转义全部拒绝。

**Body**：参与签名的是**原始字节**。不要解析 JSON 后重新序列化——字段顺序或空格变一位，签名就不同。

**自证**：`fixtures/signature-v1.json` 里有平台给出的 canonical 与签名。
你的实现必须原样复现，别信"跑通了就是对的"——错一行照样算得出签名，只是平台一律 401。

```bash
python3 scripts/verify-parity.py            # 对账夹具 + 跑 Python / Go
python3 scripts/verify-parity.py --with-java # 再加 Java
```

---

## 401 排查顺序

按命中概率排，逐条排除：

| 检查项 | 怎么确认 |
|---|---|
| `audience` 与平台 `config.yml` 的 `openapi.audience` 一致 | 让平台运维把该值给你，别猜 |
| canonical 是不是 11 行、第 10 行有没有漏 | 用 `fixtures/` 对账，别肉眼数 |
| SK 是不是被复制坏了（带空格/换行、少了字符） | 必须能 base64url 解出 **32 字节**，本地就能验 |
| 时间戳偏差是否 ≤ 300 秒 | `date +%s` 与平台对一下 |
| nonce 是否重复用过 | 平台按 `(client_id, nonce)` 去重，重试要换新的 |
| 是不是打到了 51000 | 换 51002 再试 |

还不行就把响应的 `requestId` 带上找平台侧查日志——那是唯一能反查的线索。

---

## 错误语义与重试

| 状态 | 含义 | 该怎么做 |
|---|---|---|
| 400 | 请求不合法（含 Content-Type 带了 charset） | 修请求，别重试 |
| 401 | 认证失败 / 过期 / nonce 重放 | 按上表排查 |
| 403 | 没有该资源权限，或资源不属于这个 AK | 查授权，别重试 |
| 404 | 资源不存在 | 别重试 |
| 409 | 设备当前状态不接受该操作 | 别重试，等状态变化 |
| 422 | 设备或协议不支持该能力 | 别重试 |
| 429 | 限流，响应头有 `Retry-After` | 按提示等待后重试 |
| 503 | 依赖不可用（媒体节点、设备离线） | 可退避重试 |

**重试铁律**：GET 可以用新 timestamp/nonce 重签重试；
**POST 超时不要自动重试**——结果未知，重复执行可能产生两次操作。
需要重试时用**同一个 `Idempotency-Key`** 重放，让平台去重。

---

## 契约与版本

- 接口契约：`api/openapi-v1.yaml`（OpenAPI 3.0.3，84 个操作）。可以拿它自己生成其他语言客户端。
- 签名向量：[`fixtures/signature-v1.json`](fixtures/signature-v1.json)，来源与同步方式见 [`fixtures/SOURCE.md`](fixtures/SOURCE.md)。
- 多语言一致性：`scripts/verify-parity.py`。任何一门语言的签名实现改动后都必须跑，且结果必须全绿。
- HTTP 集合：[`http/`](http/)，由契约生成，不要手改。

---

## 本仓的维护者看这里

**改签名逻辑时**：三份实现（Java `HmacSigner`、Python `uvp_openapi.canonical`、Go `uvp.Canonical`）
必须同步改，然后 `python3 scripts/verify-parity.py --with-java` 必须全绿。
只改一门语言是最典型的错误——客户换语言接入就会 401，而且在 Java 那边完全看不出来。

**对外交付前**：`python3 scripts/check-no-endpoints.py` 必须干净。
这个仓库会连源码一起交给客户，真机地址、联调平台 IP、客户内网地址都不能出现在
文档或注释里（`verify-parity.py` 每次都会先跑这一项）。内网 Nexus 这类确实要给客户的
地址走脚本里的白名单，并写清理由。

**契约变了（主仓 `api/openapi-v1.yaml`）**：

```bash
python3 scripts/sync-fixture.py                      # 重新同步向量（主仓内执行）
python3 java/codegen/import_models.py                # 重建模型
python3 java/codegen/generate_apis.py                # 重建 API 类
python3 scripts/gen-http-collection.py               # 重建 Postman / Apifox 集合
python3 scripts/verify-parity.py --with-java         # 三语言 + 集合对账
```

**改 `http/` 里的东西**：那三个 JSON 是生成出来的，手改会在下次重新生成时丢掉。
改 `scripts/gen-http-collection.py` 再重跑。集合里的签名脚本同样如此——
它被执行时能不能算对，由 `scripts/verify-http-collection.js` 拿真实夹具向量验，
不是"看起来对"就算了。

**新增一门语言**：放 `<语言>/` 目录，必须自带读 `fixtures/signature-v1.json` 的自测，
并接进 `verify-parity.py`。没有自测的示例等于没有示例。
