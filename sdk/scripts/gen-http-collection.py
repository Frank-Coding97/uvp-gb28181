#!/usr/bin/env python3
"""从 api/openapi-v1.yaml 生成 Postman / Apifox 可直接导入的调试集合。

为什么用生成而不是手写：
    84 个接口，手写一次就会和契约漂移；而漂移的集合比没有集合更糟 ——
    客户照着调，路径/参数对不上，会以为平台有问题。

为什么签名要写成脚本：
    这个平台的签名是 11 行 canonical（少拼一行照样算得出签名，但一律 401，
    现象和"AK 填错"完全一样）。所以集合内置一个 collection 级 pre-request
    脚本，保证每次请求都自动签名 —— 客户不需要手算，也就不会算错。

产物：
    sdk/http/uvp-openapi.postman_collection.json
    sdk/http/uvp-openapi.postman_environment.json
    sdk/http/uvp-openapi.apifox.json

    python3 sdk/scripts/gen-http-collection.py
    python3 sdk/scripts/gen-http-collection.py --check   # 只校验能否解析，不写文件
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import uuid
from typing import Any, Dict, List, Optional, Tuple

try:
    import yaml
except ImportError:  # pragma: no cover
    sys.exit("需要 PyYAML：pip install pyyaml")

SDK_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
REPO_ROOT = os.path.dirname(SDK_ROOT)
OUT_DIR = os.path.join(SDK_ROOT, "http")

# 拆仓后契约副本会在 SDK 仓内，所以两个位置都要找
SPEC_CANDIDATES = [
    os.path.join(REPO_ROOT, "api", "openapi-v1.yaml"),
    os.path.join(SDK_ROOT, "contract", "openapi-v1.yaml"),
]

METHODS = ("get", "post", "put", "patch", "delete", "head")

# 生成脚本自己产出的签名头，不出现在「让用户填」的头列表里
SIGNATURE_HEADERS = {
    "X-UVP-Sign-Version",
    "X-UVP-Access-Key",
    "X-UVP-Timestamp",
    "X-UVP-Nonce",
    "X-UVP-Signature",
}

# 幂等键的占位值。
# 为什么不用空字符串：空值 header 在 Postman 里可能被当成"不发送"，
# 脚本就读不到这一项、也就不生成 key，而服务端对 required 接口会直接拒。
# 用一个非空占位值，header 一定发得出去；脚本识别到它才换成新 UUID，
# 用户手填别的值则原样保留（重放测试需要固定 key）。
IDEMPOTENCY_PLACEHOLDER = "00000000-0000-4000-8000-000000000001"

# path 参数 -> collection 变量默认值。
# 全部给合法占位值，这样"点开就能发"：签名通过、能拿到真实的 404/业务错，
# 而不是缺变量导致的 401。
PATH_DEFAULTS = {
    "deviceId": "34020000001320000001",
    "channelId": "34020000001320000002",
    "sessionId": "00000000-0000-4000-8000-000000000000",
    "subscriptionId": "00000000-0000-4000-8000-000000000000",
    "operationId": "00000000-0000-4000-8000-000000000000",
    "snapshotId": "1",
    "fileName": "snapshot.jpg",
    "trackId": "1",
    "presetId": "1",
}

# 分组规则：顺序敏感，先匹配到的先赢；设备放最后兜底。
# 注意所有 needle 必须先小写 —— path 已经 lower() 过，漏转就永远匹配不上
# （踩过：设备组整个变空，21 个接口全挤进"通道"）。
GROUP_RULES: List[Tuple[str, Tuple[str, ...]]] = [
    ("05 直播", ("/live",)),
    ("06 录像与回放", ("recording", "playback", "download", "timeline")),
    ("07 图像与截图", ("snapshot",)),
    ("08 对讲与广播", ("talk", "broadcast")),
    ("09 报警与布防", ("alarm", "status-events")),
    ("04 设备配置", ("device-config", "video-params", "config-operation")),
    ("03 云台 PTZ", ("/ptz",)),
    ("02 通道", ("/channels",)),
    ("01 设备", ("/devices",)),
]

GROUP_ORDER = [
    "01 设备",
    "02 通道",
    "03 云台 PTZ",
    "04 设备配置",
    "05 直播",
    "06 录像与回放",
    "07 图像与截图",
    "08 对讲与广播",
    "09 报警与布防",
]

SCRIPT = r"""/*
 * UVP OpenAPI v1 请求签名器（collection 级 pre-request 脚本）
 *
 * 由 sdk/scripts/gen-http-collection.py 生成，请勿手改 —— 改生成器再重跑。
 * 规范来源：api/openapi-v1.yaml 的 x-uvp-signature-contract
 *
 * 它只做三件事，但这三件恰恰是手写最容易错的地方：
 *   1. 拼出 11 行 canonical（少拼一行也算得出签名，但平台一律返回 401）
 *   2. 用 SK 的**字节**（base64url 解码后，不是这段文字）算 HMAC-SHA256
 *   3. 把 query 重写成规范要求的 RFC3986 形式，保证"签的就是发的"
 *
 * 需要 4 个变量：baseUrl / accessKey / secretKey / audience
 * audience 不在任何页面上，只能由运维从服务器 config.yml 的 openapi.audience 取。
 */
(function () {
  'use strict';

  var HEX = CryptoJS.enc.Hex;

  /* 与生成器常量同源，构建时被替换；用来区分"占位值"和"用户手填的固定 key" */
  var IDEMPOTENCY_PLACEHOLDER = '@@IDEMPOTENCY_PLACEHOLDER@@';

  function fail(message) {
    throw new Error('[UVP 签名] ' + message);
  }

  function variable(name, hint) {
    var value = pm.variables.get(name);
    if (value === undefined || value === null || String(value).trim() === '') {
      fail('缺少变量 ' + name + '。' + hint);
    }
    return String(value).trim();
  }

  /* RFC3986：unreserved 之外全部百分号编码；空格是 %20，不是 + */
  function rfc3986(value) {
    return encodeURIComponent(String(value)).replace(/[!'()*]/g, function (ch) {
      return '%' + ch.charCodeAt(0).toString(16).toUpperCase();
    });
  }

  /* base64url（可能无填充）-> hex。SK 是 32 字节，HMAC 的 key 是解码后的字节 */
  function secretKeyHex(secretKey) {
    var text = String(secretKey).replace(/-/g, '+').replace(/_/g, '/');
    while (text.length % 4 !== 0) {
      text += '=';
    }
    var hex;
    try {
      hex = CryptoJS.enc.Base64.parse(text).toString(HEX);
    } catch (e) {
      fail('secretKey 不是合法的 base64url：' + e.message);
    }
    if (hex.length !== 64) {
      fail('secretKey 解码后是 ' + (hex.length / 2) + ' 字节，规范要求 32 字节（base64url 无填充）');
    }
    return hex;
  }

  function requestPath() {
    var url = pm.request.url;
    var path = '';
    if (typeof url.getPath === 'function') {
      path = url.getPath();
    }
    if (!path && url.path) {
      path = url.path.length !== undefined ? '/' + url.path.join('/') : String(url.path);
    }
    if (!path) {
      fail('取不到请求路径');
    }
    if (path.charAt(0) !== '/') {
      path = '/' + path;
    }
    if (path.indexOf('{{') >= 0 || /\{[A-Za-z]/.test(path)) {
      fail('路径里还有未替换的变量（' + path + '）。请在集合变量里填好 deviceId / channelId 等值');
    }
    return path;
  }

  function rawQueryString() {
    var url = pm.request.url;
    if (typeof url.getQueryString === 'function') {
      var query = url.getQueryString();
      if (query !== undefined && query !== null && query !== '') {
        return String(query);
      }
    }
    var text = String(url);
    var start = text.indexOf('?');
    if (start < 0) {
      return '';
    }
    var rest = text.slice(start + 1);
    var hash = rest.indexOf('#');
    return hash < 0 ? rest : rest.slice(0, hash);
  }

  /* 解码一次 -> RFC3986 重编码 -> 按 key、value 排序。与服务端 canonicalQuery 同义 */
  function canonicalQuery(raw) {
    if (!raw) {
      return '';
    }
    var pairs = [];
    var seen = {};
    var parts = raw.split('&');
    for (var i = 0; i < parts.length; i++) {
      var part = parts[i];
      if (part === '') {
        fail('query 里有空的 & 分隔符：' + raw);
      }
      if (part.indexOf('+') >= 0) {
        fail('query 里出现了 +，平台会直接拒绝；空格请写成 %20：' + part);
      }
      var eq = part.indexOf('=');
      if (eq < 0) {
        fail('query 参数缺少 = 号：' + part);
      }
      var key;
      var value;
      try {
        key = decodeURIComponent(part.slice(0, eq));
        value = decodeURIComponent(part.slice(eq + 1));
      } catch (e) {
        fail('query 里有非法的百分号编码：' + part);
      }
      if (key === '') {
        fail('query 参数名不能为空：' + part);
      }
      var encodedKey = rfc3986(key);
      if (seen[encodedKey]) {
        fail('query 参数重复，平台会拒绝：' + encodedKey);
      }
      seen[encodedKey] = true;
      pairs.push([encodedKey, rfc3986(value)]);
    }
    pairs.sort(function (a, b) {
      if (a[0] !== b[0]) {
        return a[0] < b[0] ? -1 : 1;
      }
      if (a[1] !== b[1]) {
        return a[1] < b[1] ? -1 : 1;
      }
      return 0;
    });
    var encoded = [];
    for (var j = 0; j < pairs.length; j++) {
      encoded.push(pairs[j][0] + '=' + pairs[j][1]);
    }
    return encoded.join('&');
  }

  function requestBody() {
    var body = pm.request.body;
    if (!body) {
      return '';
    }
    if (body.mode === 'raw') {
      return body.raw === undefined || body.raw === null ? '' : String(body.raw);
    }
    if (!body.mode) {
      return '';
    }
    fail('平台只接受 application/json 原始请求体（Body → raw → JSON），当前模式是 ' + body.mode);
  }

  function setHeader(key, value) {
    pm.request.headers.upsert({ key: key, value: value });
  }

  function removeHeader(key) {
    if (typeof pm.request.headers.remove === 'function') {
      pm.request.headers.remove(key);
    }
  }

  /*
   * 幂等键：请求里显式填了非占位值就用（重放测试需要同一个 key），
   * 占位值 / 空值 / 模板变量则自动生成一个新的。
   * 完全没有这个头 = 该接口不需要幂等，canonical 第 10 行留空。
   */
  function idempotencyKey() {
    var existing = pm.request.headers.get('Idempotency-Key');
    if (existing === undefined || existing === null) {
      return '';
    }
    var text = String(existing).trim();
    if (text !== '' && text !== IDEMPOTENCY_PLACEHOLDER && text.indexOf('{{') < 0) {
      return text;
    }
    var generated = CryptoJS.lib.WordArray.random(16).toString(HEX);
    setHeader('Idempotency-Key', generated);
    return generated;
  }

  var accessKey = variable('accessKey', '形如 uvp_ + 32 位小写十六进制');
  var secretKey = variable('secretKey', '创建 AK 时一次性返回的 SK（base64url 无填充）');
  var audience = variable('audience', '服务器 config.yml 的 openapi.audience，页面上看不到');
  var baseUrl = variable('baseUrl', '形如 http://<平台IP>:51002，51000 是前端页面不转发 OpenAPI');

  var method = String(pm.request.method || '').toUpperCase();
  if (!method) {
    fail('取不到请求方法');
  }

  var path = requestPath();
  var query = canonicalQuery(rawQueryString());
  var body = requestBody();

  var contentType = '';
  if (method === 'GET' || method === 'HEAD') {
    if (body !== '') {
      fail('GET/HEAD 不能带请求体，平台会拒绝');
    }
    removeHeader('Content-Type');
  } else {
    /* 必须恰好 application/json：带 ;charset=utf-8 会在验签前被判 400 */
    contentType = 'application/json';
    setHeader('Content-Type', 'application/json');
  }

  var bodyHash = CryptoJS.SHA256(CryptoJS.enc.Utf8.parse(body)).toString(HEX);
  var timestamp = String(Math.floor(Date.now() / 1000));
  var nonce = CryptoJS.lib.WordArray.random(16).toString(HEX);
  var idempotency = idempotencyKey();

  var canonical = [
    'UVP-HMAC-SHA256/1',
    accessKey,
    timestamp,
    nonce,
    method,
    path,
    query,
    contentType,
    bodyHash,
    idempotency,
    audience
  ].join('\n');

  var signature = CryptoJS.HmacSHA256(
    CryptoJS.enc.Utf8.parse(canonical),
    HEX.parse(secretKeyHex(secretKey))
  ).toString(HEX);

  setHeader('X-UVP-Sign-Version', '1');
  setHeader('X-UVP-Access-Key', accessKey);
  setHeader('X-UVP-Timestamp', timestamp);
  setHeader('X-UVP-Nonce', nonce);
  setHeader('X-UVP-Signature', signature);

  /* 把 query 换成规范形式，否则 ! ' ( ) * 这六个字符会被客户端编成别的样子 */
  if (typeof pm.request.url.update === 'function') {
    try {
      pm.request.url.update(
        String(baseUrl).replace(/\/+$/, '') + path + (query ? '?' + query : '')
      );
    } catch (e) {
      /* 个别客户端不接受整串更新；差异只会落在那六个字符上 */
    }
  }

  /* 只在本次运行内可见，不写进环境文件，排障时可在控制台打印 */
  pm.variables.set('uvp_canonical', canonical);
  pm.variables.set('uvp_signature', signature);
})();
"""


# ---------------------------------------------------------------- spec helpers


def load_spec() -> Tuple[Dict[str, Any], str]:
    for candidate in SPEC_CANDIDATES:
        if os.path.isfile(candidate):
            with open(candidate, encoding="utf-8") as handle:
                return yaml.safe_load(handle), candidate
    sys.exit("找不到契约文件，试过:\n  " + "\n  ".join(SPEC_CANDIDATES))


def resolve(spec: Dict[str, Any], node: Any) -> Dict[str, Any]:
    """只解析 $ref（本契约里没有循环引用）。"""
    seen = 0
    while isinstance(node, dict) and "$ref" in node:
        target = node["$ref"].lstrip("#/").split("/")
        current: Any = spec
        for part in target:
            current = current[part]
        node = current
        seen += 1
        if seen > 8:
            break
    return node if isinstance(node, dict) else {}


def param_name(spec: Dict[str, Any], ref_node: Dict[str, Any]) -> Dict[str, Any]:
    if "$ref" in ref_node:
        return resolve(spec, ref_node)
    return ref_node


def collect_operations(spec: Dict[str, Any]) -> List[Dict[str, Any]]:
    operations = []
    for path, methods in spec.get("paths", {}).items():
        for method in METHODS:
            op = methods.get(method)
            if not op:
                continue
            operations.append({"path": path, "method": method.upper(), "op": op})
    return operations


def operation_group(path: str) -> str:
    lowered = path.lower()
    for name, needles in GROUP_RULES:
        for needle in needles:
            if needle.lower() in lowered:
                return name
    return "01 设备"


def replace_path_params(path: str) -> str:
    """{deviceId} -> {{deviceId}}，交给集合变量。"""
    out = path
    for name in PATH_DEFAULTS:
        out = out.replace("{%s}" % name, "{{%s}}" % name)
    return out


def example_value(schema: Dict[str, Any], spec: Dict[str, Any], depth: int = 0) -> Any:
    schema = resolve(spec, schema)
    if "enum" in schema and schema["enum"]:
        return schema["enum"][0]
    if "default" in schema:
        return schema["default"]
    if "example" in schema:
        return schema["example"]
    kind = schema.get("type")
    if kind == "integer":
        return schema.get("minimum", 0)
    if kind == "number":
        return schema.get("minimum", 0)
    if kind == "boolean":
        return False
    if kind == "string":
        fmt = schema.get("format")
        if fmt == "date-time":
            return "2026-01-01T00:00:00Z"
        if fmt == "date":
            return "2026-01-01"
        if fmt == "uuid":
            return "00000000-0000-4000-8000-000000000000"
        if schema.get("minLength"):
            return "string"
        return ""
    if kind == "array" and depth < 3:
        items = schema.get("items") or {}
        return [example_value(items, spec, depth + 1)]
    if kind == "object" and depth < 3:
        return object_example(schema, spec, depth + 1)
    return None


def object_example(schema: Dict[str, Any], spec: Dict[str, Any], depth: int = 0) -> Dict[str, Any]:
    """只放必填字段：契约里普遍是 additionalProperties: false，多塞字段会被拒。"""
    schema = resolve(spec, schema)
    properties = schema.get("properties") or {}
    required = schema.get("required") or []
    result: Dict[str, Any] = {}
    for name in required:
        if name not in properties:
            continue
        prop = resolve(spec, properties[name])
        if prop.get("readOnly"):
            continue
        result[name] = example_value(prop, spec, depth)
    return result


def request_body_text(spec: Dict[str, Any], op: Dict[str, Any]) -> str:
    body = op.get("requestBody")
    if not body:
        return ""
    content = body.get("content") or {}
    media = content.get("application/json")
    if not media:
        return ""
    schema = resolve(spec, media.get("schema") or {})
    return json.dumps(object_example(schema, spec), ensure_ascii=False, indent=2)


def describe_schema(spec: Dict[str, Any], schema: Dict[str, Any], indent: str = "") -> List[str]:
    schema = resolve(spec, schema)
    properties = schema.get("properties") or {}
    required = set(schema.get("required") or [])
    if not properties:
        return []
    lines = []
    for name, raw in properties.items():
        prop = resolve(spec, raw)
        kind = prop.get("type") or "object"
        bits = [kind]
        if "enum" in prop:
            bits.append("取值: " + " / ".join(str(v) for v in prop["enum"]))
        if prop.get("format"):
            bits.append("格式: " + str(prop["format"]))
        if prop.get("minimum") is not None or prop.get("maximum") is not None:
            low = prop.get("minimum")
            high = prop.get("maximum")
            if low is not None and high is not None:
                bits.append("范围: %s~%s" % (low, high))
            elif low is not None:
                bits.append("最小: %s" % low)
            else:
                bits.append("最大: %s" % high)
        if prop.get("maxLength"):
            bits.append("最长: %s" % prop["maxLength"])
        flag = "必填" if name in required else "可选"
        lines.append("%s- `%s`（%s，%s）%s" % (
            indent, name, "，".join(bits), flag, prop.get("description", "")))
    return lines


def describe_operation(spec: Dict[str, Any], op: Dict[str, Any], path: str, method: str) -> str:
    lines = [
        "`%s` · scope `%s` · risk `%s` · 幂等 `%s`" % (
            op.get("operationId", ""),
            op.get("x-uvp-scope", "-"),
            op.get("x-uvp-risk", "-"),
            op.get("x-uvp-idempotency-mode", "none"),
        ),
        "",
        op.get("summary", "").strip(),
    ]

    query_lines = []
    path_lines = []
    for ref_node in op.get("parameters") or []:
        prm = param_name(spec, ref_node)
        where = prm.get("in")
        if where == "query":
            schema = resolve(spec, prm.get("schema") or {})
            bits = [schema.get("type", "string")]
            if "enum" in schema:
                bits.append("取值: " + " / ".join(str(v) for v in schema["enum"]))
            if schema.get("default") is not None:
                bits.append("默认: %s" % schema["default"])
            flag = "必填" if prm.get("required") else "可选"
            query_lines.append("- `%s`（%s，%s）%s" % (
                prm.get("name"), "，".join(bits), flag, prm.get("description", "")))
        elif where == "path":
            path_lines.append("- `%s` ← 变量 `{{%s}}`" % (prm.get("name"), prm.get("name")))

    if path_lines:
        lines += ["", "**路径变量**"] + path_lines
    if query_lines:
        lines += ["", "**Query 参数**（按需加，空值会被平台拒绝的不要留空）"] + query_lines

    body = op.get("requestBody")
    if body:
        content = (body.get("content") or {}).get("application/json")
        if content:
            schema = resolve(spec, content.get("schema") or {})
            fields = describe_schema(spec, schema)
            if fields:
                lines += ["", "**请求体字段**"] + fields
            else:
                lines += ["", "**请求体**：`{}`（空对象，但不能省略）"]

    lines += ["", "签名头由集合的 pre-request 脚本自动写入，不要在请求里手填。"]
    return "\n".join(line for line in lines if line is not None)


# ---------------------------------------------------------------- postman


def script_text() -> str:
    """占位符只在这个函数里被替换，保证生成器与脚本不会各写一份而漂移。"""
    return SCRIPT.replace("@@IDEMPOTENCY_PLACEHOLDER@@", IDEMPOTENCY_PLACEHOLDER)


def postman_query(spec: Dict[str, Any], op: Dict[str, Any]) -> List[Dict[str, str]]:
    """只把有默认值的参数预先填上：空值会被平台拒绝的参数留空反而制造 400。"""
    out = []
    for ref_node in op.get("parameters") or []:
        prm = param_name(spec, ref_node)
        if prm.get("in") != "query":
            continue
        schema = resolve(spec, prm.get("schema") or {})
        if schema.get("default") is None:
            continue
        out.append({"key": prm.get("name"), "value": str(schema["default"])})
    return out


def postman_url(path: str, query: List[Dict[str, str]]) -> Dict[str, Any]:
    replaced = replace_path_params(path)
    segments = [seg for seg in replaced.split("/") if seg]
    raw = "{{baseUrl}}" + replaced
    if query:
        raw += "?" + "&".join("%s=%s" % (q["key"], q["value"]) for q in query)
    url: Dict[str, Any] = {
        "raw": raw,
        "host": ["{{baseUrl}}"],
        "path": segments,
    }
    if query:
        url["query"] = query
    return url


def build_postman(spec: Dict[str, Any], operations: List[Dict[str, Any]]) -> Dict[str, Any]:
    groups: Dict[str, List[Dict[str, Any]]] = {name: [] for name in GROUP_ORDER}

    for item in operations:
        op = item["op"]
        query = postman_query(spec, op)
        request: Dict[str, Any] = {
            "method": item["method"],
            "header": [],
            "url": postman_url(item["path"], query),
            "description": describe_operation(spec, op, item["path"], item["method"]),
        }
        if op.get("x-uvp-idempotency-mode") == "required":
            request["header"].append({
                "key": "Idempotency-Key",
                "value": IDEMPOTENCY_PLACEHOLDER,
                "description": "占位值，脚本会自动换成新 UUID；要测重放就把它改成固定字符串",
            })

        body_text = request_body_text(spec, op)
        if item["method"] != "GET" and item["method"] != "HEAD":
            request["body"] = {
                "mode": "raw",
                "raw": body_text if body_text else "{}",
                "options": {"raw": {"language": "json"}},
            }
            request["header"].append({"key": "Content-Type", "value": "application/json"})

        name = op.get("summary", op.get("operationId", "")).strip() or item["path"]
        groups[operation_group(item["path"])].append({
            "name": name,
            "request": request,
            "response": [],
        })

    items = []
    for name in GROUP_ORDER:
        if groups[name]:
            items.append({"name": name, "item": groups[name]})

    description = "\n".join([
        "UVP 平台对外 OpenAPI v1 调试集合（{} 个接口）。".format(len(operations)),
        "",
        "**导入后先填 4 个变量**（集合 → Variables）：",
        "",
        "| 变量 | 说明 |",
        "| --- | --- |",
        "| `baseUrl` | `http://<平台IP>:51002`。51000 是前端页面，**不转发** `/openapi/v1` |",
        "| `accessKey` | 形如 `uvp_` + 32 位小写十六进制 |",
        "| `secretKey` | 创建 AK 时一次性返回的 SK（base64url，无填充） |",
        "| `audience` | 服务器 `config.yml` 的 `openapi.audience`，页面上看不到 |",
        "",
        "另外 `deviceId` / `channelId` / `sessionId` 等路径变量给了合法占位值，",
        "换成你自己的即可。",
        "",
        "签名由集合级 pre-request 脚本自动完成，**不需要手填任何 X-UVP-\\* 头**。",
        "脚本报错会在响应区直接显示原因，比 401 好排查。",
        "",
        "⚠️ 填了真实 SK 之后不要再把这个集合分享/提交到仓库。",
    ])

    return {
        "info": {
            "_postman_id": str(uuid.uuid5(uuid.NAMESPACE_URL, "uvp-openapi-v1-postman")),
            "name": "UVP OpenAPI v1",
            "description": description,
            "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json",
        },
        "item": items,
        "event": [{
            "listen": "prerequest",
            "script": {"type": "text/javascript", "exec": script_text().split("\n")},
        }],
        "variable": [
            {"key": "baseUrl", "value": "http://127.0.0.1:51002"},
            {"key": "accessKey", "value": "uvp_000102030405060708090a0b0c0d0e0f"},
            {"key": "secretKey", "value": "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"},
            {"key": "audience", "value": "uvp-openapi-test-audience-v1"},
        ] + [{"key": key, "value": value} for key, value in PATH_DEFAULTS.items()],
    }


def build_environment() -> Dict[str, Any]:
    return {
        "id": str(uuid.uuid5(uuid.NAMESPACE_URL, "uvp-openapi-v1-env")),
        "name": "UVP OpenAPI v1（示例）",
        "values": [
            {"key": "baseUrl", "value": "http://127.0.0.1:51002", "type": "default", "enabled": True},
            {"key": "accessKey", "value": "uvp_000102030405060708090a0b0c0d0e0f", "type": "default", "enabled": True},
            {"key": "secretKey", "value": "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8", "type": "secret", "enabled": True},
            {"key": "audience", "value": "uvp-openapi-test-audience-v1", "type": "default", "enabled": True},
        ],
        "_postman_variable_scope": "environment",
    }


# ---------------------------------------------------------------- apifox


def apifox_query(spec: Dict[str, Any], op: Dict[str, Any], counter: List[int]) -> List[Dict[str, Any]]:
    out = []
    for ref_node in op.get("parameters") or []:
        prm = param_name(spec, ref_node)
        if prm.get("in") != "query":
            continue
        schema = resolve(spec, prm.get("schema") or {})
        default = schema.get("default")
        counter[0] += 1
        out.append({
            "id": "q%d" % counter[0],
            "name": prm.get("name"),
            "required": bool(prm.get("required")),
            "description": prm.get("description", ""),
            "type": schema.get("type", "string"),
            "example": "" if default is None else str(default),
            "enable": default is not None,
        })
    return out


def apifox_path(spec: Dict[str, Any], op: Dict[str, Any], counter: List[int]) -> List[Dict[str, Any]]:
    out = []
    for ref_node in op.get("parameters") or []:
        prm = param_name(spec, ref_node)
        if prm.get("in") != "path":
            continue
        name = prm.get("name")
        counter[0] += 1
        out.append({
            "id": "p%d" % counter[0],
            "name": name,
            "required": True,
            "description": prm.get("description", ""),
            "type": "string",
            "example": PATH_DEFAULTS.get(name, ""),
            "enable": True,
        })
    return out


def apifox_request_body(spec: Dict[str, Any], op: Dict[str, Any], counter: List[int]) -> Dict[str, Any]:
    body = op.get("requestBody")
    if not body:
        return {"type": "none", "parameters": []}
    content = (body.get("content") or {}).get("application/json")
    if not content:
        return {"type": "none", "parameters": []}
    schema = resolve(spec, content.get("schema") or {})
    counter[0] += 1
    return {
        "type": "application/json",
        "parameters": [],
        "jsonSchema": schema,
        "id": "b%d" % counter[0],
        "required": bool(body.get("required")),
    }


def build_apifox(spec: Dict[str, Any], operations: List[Dict[str, Any]]) -> Dict[str, Any]:
    groups: Dict[str, List[Dict[str, Any]]] = {name: [] for name in GROUP_ORDER}
    counter = [0]
    folder_id = 1000
    api_id = 20000

    for item in operations:
        op = item["op"]
        counter[0] += 1
        api_id += 1
        header_params = []
        if op.get("x-uvp-idempotency-mode") == "required":
            counter[0] += 1
            header_params.append({
                "id": "h%d" % counter[0],
                "name": "Idempotency-Key",
                "value": IDEMPOTENCY_PLACEHOLDER,
                "enable": True,
                "required": True,
                "description": "占位值，脚本会自动换成新 UUID；要测重放就把它改成固定字符串",
            })
        api = {
            "id": api_id,
            "method": item["method"].lower(),
            "path": item["path"],
            "parameters": {
                "query": apifox_query(spec, op, counter),
                "path": apifox_path(spec, op, counter),
            },
            "auth": {},
            "commonParameters": {
                "query": [], "body": [], "cookie": [], "header": header_params,
            },
            "responses": [{
                "id": api_id * 10,
                "name": "200",
                "code": 200,
                "contentType": "json",
                "jsonSchema": {"type": "object"},
            }],
            "responseExamples": [],
            "requestBody": apifox_request_body(spec, op, counter),
            "description": describe_operation(spec, op, item["path"], item["method"]),
            "tags": [],
            "status": "released",
            "serverId": "",
            "operationId": op.get("operationId", ""),
            "sourceUrl": "",
            "ordering": counter[0] * 10,
            "cases": [],
            "advancedSettings": {"isDefaultUrlEncoding": 1},
            "mockScript": {},
            "codeSamples": [],
            "commonResponseStatus": {},
            "responseChildren": [],
            "preProcessors": [],
            "postProcessors": [],
            "inheritPostProcessors": {},
            "inheritPreProcessors": {},
        }
        name = op.get("summary", op.get("operationId", "")).strip() or item["path"]
        groups[operation_group(item["path"])].append({"name": name, "api": api})

    collection = []
    for name in GROUP_ORDER:
        if not groups[name]:
            continue
        folder_id += 1
        collection.append({
            "name": name,
            "id": folder_id,
            "auth": {},
            "parentId": 0,
            "serverId": "",
            "description": "",
            "identityPattern": {
                "httpApi": {"type": "methodAndPath", "bodyType": "", "fields": []}
            },
            "preProcessors": [{"type": "customScript", "data": script_text()}],
            "postProcessors": [{"type": "inheritProcessors", "data": {}}],
            "inheritPostProcessors": {},
            "inheritPreProcessors": {},
            "items": groups[name],
        })

    return {
        "apifoxProject": "1.0.0",
        "$schema": {"app": "apifox", "type": "project", "version": "1.2.0"},
        "info": {
            "name": "UVP OpenAPI v1",
            "description": (
                "UVP 平台对外 OpenAPI v1（%d 个接口）。导入后请在「环境管理」新建环境，"
                "前置 URL 填 http://<平台IP>:51002，并添加 accessKey / secretKey / audience "
                "三个变量；签名由每个目录的前置脚本自动完成。" % len(operations)
            ),
            "mockRule": {"rules": [], "enableSystemRule": True},
        },
        "apiCollection": collection,
    }


# ---------------------------------------------------------------- entry


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="只校验契约可解析，不写文件")
    parser.add_argument("--out", default=OUT_DIR, help="输出目录（默认 sdk/http）")
    args = parser.parse_args()

    spec, spec_path = load_spec()
    operations = collect_operations(spec)
    if not operations:
        sys.exit("契约里没有解析到任何操作")

    postman = build_postman(spec, operations)
    environment = build_environment()
    apifox = build_apifox(spec, operations)

    counts = {}
    for name, group in [(g["name"], g["item"]) for g in postman["item"]]:
        counts[name] = len(group)

    print("契约: %s" % spec_path)
    print("操作: %d 个" % len(operations))
    for name in GROUP_ORDER:
        if name in counts:
            print("  %-16s %d" % (name, counts[name]))

    if args.check:
        print("--check：不写文件")
        return 0

    os.makedirs(args.out, exist_ok=True)
    targets = [
        ("uvp-openapi.postman_collection.json", postman),
        ("uvp-openapi.postman_environment.json", environment),
        ("uvp-openapi.apifox.json", apifox),
    ]
    for filename, payload in targets:
        path = os.path.join(args.out, filename)
        with open(path, "w", encoding="utf-8") as handle:
            json.dump(payload, handle, ensure_ascii=False, indent=2)
            handle.write("\n")
        print("写入 %s（%.1f KB）" % (path, os.path.getsize(path) / 1024.0))
    return 0


if __name__ == "__main__":
    sys.exit(main())
