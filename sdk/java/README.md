# UVP OpenAPI Java SDK

Java 客户端，封装 UVP GB28181 平台的机器对接接口（`/openapi/v1/*`）。签名、幂等键、重试、错误映射全部在 SDK 内完成，业务代码不接触这些细节。

- 69 个接口，按业务域分成 8 组
- 编译目标 Java 8（Spring Boot 3 用户请用 starter 模块，需要 Java 17）
- 运行时依赖只有 okhttp + gson + slf4j

## 快速开始（Spring Boot）

```xml
<dependency>
  <groupId>com.uvp</groupId>
  <artifactId>uvp-openapi-spring-boot-starter</artifactId>
  <version>1.0.0-SNAPSHOT</version>
</dependency>
```

```yaml
uvp:
  openapi:
    base-url: http://192.168.1.220:8280
    access-key: ${UVP_ACCESS_KEY:}
    secret-key: ${UVP_SECRET_KEY:}
    audience: uvp-dev-local        # 必须等于平台 config.yml 里的 openapi.audience
```

```java
@Service
@RequiredArgsConstructor
public class CameraService {

    private final UvpClient uvp;

    public String openLive(String deviceId, String channelId) {
        LivePlaybackResponseData data = uvp.live().startLivePlayback(deviceId, channelId).getData();
        return (String) data.getUrls().get("hls");
    }

    public void gotoPreset(String deviceId, String channelId, String presetId) {
        uvp.ptz().callPTZPreset(deviceId, channelId, presetId, new EmptyJSONRequest());
    }
}
```

没有 Spring 时直接 new：

```java
try (UvpClient uvp = new UvpClient(UvpClientConfig.of(baseUrl, accessKey, secretKey, audience))) {
    DeviceListResponse devices = uvp.devices().listDevices(1, 50, null, "online");
}
```

## API 分组

| 注入字段 | 覆盖范围 |
|---|---|
| `uvp.devices()` | 设备、通道、状态、存储卡 |
| `uvp.live()` | 点播（返回播放地址，不代理媒体） |
| `uvp.playback()` | 录像检索、回放会话、下载会话 |
| `uvp.ptz()` | 云台方向、预置位、巡航、精确定位 |
| `uvp.talk()` | 对讲、广播（SDP 透传，媒体面不处理） |
| `uvp.alarms()` | 报警、状态事件、订阅、上报位置 |
| `uvp.configs()` | 设备配置、视频参数、固件升级任务 |
| `uvp.control()` | 录像启停、布防撤防、重启、格式化、任务轮询 |

## 错误处理

所有失败都抛 `UvpApiException`，已经分类好：

```java
try {
    uvp.control().channelRecordingStart(deviceId, channelId, body);
} catch (UvpApiException e) {
    switch (e.getKind()) {
        case CONFLICT -> // 设备当前状态不接受该操作
        case AUTHORIZATION -> // 没有该通道的权限，或通道不属于这个 AK
        case TIMEOUT -> // 结果未知，用同一个 Idempotency-Key 重试
        default -> throw e;
    }
    log.warn("requestId={} platformCode={}", e.getRequestId(), e.getCode());
}
```

`requestId` 是平台侧唯一的排障线索，务必转发或记录。

## 幂等

标记为 `required` 的 30 个写接口会自动生成 `Idempotency-Key`。**同一个逻辑操作重试时必须复用同一个键**，否则平台会当作两次独立操作：

```java
String key = UvpIdempotency.newKey();
try {
    UvpIdempotency.run(key, () -> uvp.control().channelRecordingStart(d, c, body));
} catch (UvpApiException e) {
    if (e.getKind() == UvpErrorKind.TIMEOUT) {
        UvpIdempotency.run(key, () -> uvp.control().channelRecordingStart(d, c, body));  // 复用
    }
}
```

## 重试策略

| 场景 | 行为 |
|---|---|
| GET 遇到 429 / 5xx / 网络错误 | 自动重试，最多 `max-retries` 次（默认 2），每次换新的 nonce 重新签名 |
| POST / PUT / DELETE | **从不自动重试**。结果未知时由调用方用同一个幂等键决定是否重放 |
| 4xx（除 429） | 不重试，直接抛出 |

## 常见问题

**1. `PKIX path building failed`**

平台绿色包用的是 nginx 自签证书，证书 SAN 里通常只有 `localhost`，不含服务器 IP。Java 会直接拒绝。三种解法：内网改用 `http://`；给平台换一张 SAN 包含 IP 的证书；或把平台证书导入 JVM 的 truststore。

**2. 所有请求都返回 401，但 AK/SK 明明是对的**

按出现概率排序：

- `audience` 与平台 `config.yml` 里的 `openapi.audience` 不一致。这个值参与签名，不一致只会表现为 401，没有任何提示。
- 本机时钟偏差超过 300 秒。平台不做 server-time 接口，只能靠 NTP 对齐。
- secret key 被复制时带了空格或换行。SDK 会解码失败并给出明确异常，但如果只拷贝了前 32 个字符，签名会静默不匹配。

**3. 点播接口很慢甚至超时**

点播要等设备真正推流上来，属于慢接口，用的是 `slow-read-timeout`（默认 60 秒），普通接口是 15 秒。如果设备响应更慢，单独调大这个值，不要调 `read-timeout`。

**4. 返回的对象字段是 null**

模型严格按规范生成，`additionalProperties: false`，平台新增的字段不会被带到客户端模型里。需要透传新字段时，等规范更新后重新生成（见下）。

## 重新生成

规范在 `api/openapi-v1.yaml`，改完后：

```bash
python3 sdk/java/codegen/import_models.py    # 重建模型层
python3 sdk/java/codegen/generate_apis.py    # 重建 8 个 API 类
```

生成结果需要提交到仓库，SDK 使用者不需要有 JDK 或生成器。

## 构建与测试

```bash
cd sdk/java
mvn test          # 签名对账 + 请求管线行为测试
mvn install       # 产出 3 个 jar
```

签名测试直接读取服务端的 `server/app/openapi/auth/testdata/signature-v1.json`，不复刻一份：复刻的夹具会漂移，而漂移后的夹具仍然显示绿色，直到真实请求全部 401。

运行 demo：

```bash
export UVP_BASE_URL=http://192.168.1.220:51002   # 绿色包的后端端口，不是 51000
export UVP_ACCESS_KEY=uvp_xxx
export UVP_SECRET_KEY=xxx
export UVP_AUDIENCE=uvp-xxxxxxxxxxxxxxxx   # 平台 config.yml 的 openapi.audience
java -jar uvp-openapi-demo/target/uvp-openapi-demo-1.0.0-SNAPSHOT.jar

curl 'http://localhost:8088/demo/devices'
```

⛔ **接入地址不是浏览器访问的那个地址。** 绿色包的 nginx 只转发 `/api/`、`/gb28181/`、`/public/`，
`/openapi/v1` 不在其中，访问 `https://<host>:51000/openapi/v1/...` 拿到的是前端页面的 HTML（HTTP 200），
看起来像"接口挂了"或"返回格式不对"。**必须用后端端口 51002（纯 HTTP）**，而且它同时绕开了自签证书问题。

## 真实平台联调

单元测试用 mock server，证明不了"平台真的认这个签名"。`RealPlatformIT` 直接打真实平台，
未设置 `UVP_IT_AK` 时自动跳过，所以 `mvn test` 永远不需要网络。

```bash
cd sdk/java
export UVP_IT_BASE_URL=http://124.236.76.233:51002
export UVP_IT_AK=uvp_xxx
export UVP_IT_SK=xxx
export UVP_IT_AUDIENCE=uvp-xxx
mvn -B -pl uvp-openapi-sdk test -Dtest=RealPlatformIT
```

它覆盖 11 条链路：设备/通道事实、点播（校验回传了可播放地址）、录像检索、回放会话、云台、报警与状态事件、
订阅生命周期、设备配置与视频参数、录像控制、对讲、下载会话、错误映射。用例名以 `IT` 结尾，
Maven 默认不拾取，必须像上面那样显式指定。

首次联调（2026-10-22，真实海康设备）发现的问题，已在代码里对应修复或标注：

| 现象 | 结论 |
|---|---|
| 所有带 body 的写接口 400 `INVALID_REQUEST` | **SDK 的 bug**：`Content-Type` 带了 `charset=utf-8`，平台只接受裸的 `application/json`。已修，并加了断言 |
| 录像检索跨 25 小时必 400 | 平台限制查询窗口 ≤ 24 小时，规范里没写。SDK 不限制，调用方需自己切窗 |
| 回放「停止」对刚创建的会话稳定 400 | **平台侧缺陷**：用独立签名脚本复现，与 SDK 无关。用例里已标注，修好后断言会翻转 |
| 对讲创建返回 503 | 平台侧：无媒体节点接受 SDP。属设备/媒体面问题，不是签名问题 |
| 手动录像返回 409 `CAPABILITY_NOT_SUPPORTED` | 设备能力，不是故障。SDK 正确归类为 `CONFLICT` |

