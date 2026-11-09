"""UVP OpenAPI v1 客户端 —— 纯标准库，零第三方依赖（Python 3.8+）。

客户环境往往装不了包，所以这里唯一的依赖是 Python 自带的 http.client / hmac。
整个文件可以直接拷进项目，也可以只抄 sign_request() 那一段自己实现。

签名规范来自 api/openapi-v1.yaml 的 x-uvp-signature-contract，要点：

* canonical 是 **11 行**，用 LF 分隔，**末尾不带换行**：
  version / AK / Timestamp / Nonce / Method / Path / Query / ContentType /
  BodySHA256 / Idempotency-Key / Audience
* 第 10 行（幂等键）最容易漏。漏了照样算得出签名，但平台一律返回 401，
  而现象与"AK 错了"完全一样 —— 所以务必跑 python3 selftest.py 自证。
* 暗号：SK 是 base64url（无填充）编码的 **32 字节**，HMAC 用的是解码后的字节，
  不是 SK 这段文本本身。
* Query 必须先解码、再按 RFC3986 编码（空格是 %20，不是 +）、最后排序。
* Body 参与签名的是**原始字节**，不要解析后重新序列化。

自测：
    python3 sdk/python/selftest.py
"""

from __future__ import annotations

import base64
import hashlib
import hmac
import http.client
import json
import secrets
import ssl
import time
from typing import Any, Dict, Mapping, Optional, Union
from urllib.parse import urlsplit

SIGN_VERSION = "1"
MAX_BODY_BYTES = 64 * 1024
MAX_QUERY_BYTES = 8 * 1024

_SAFE_BYTES = frozenset(
    b"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"
)
_HEX_DIGITS = "0123456789ABCDEF"

# 平台返回的统一信封：{"code","message","requestId","data"}
HEADER_SIGN_VERSION = "X-UVP-Sign-Version"
HEADER_ACCESS_KEY = "X-UVP-Access-Key"
HEADER_TIMESTAMP = "X-UVP-Timestamp"
HEADER_NONCE = "X-UVP-Nonce"
HEADER_SIGNATURE = "X-UVP-Signature"
HEADER_IDEMPOTENCY = "Idempotency-Key"


class UvpSignatureError(ValueError):
    """本地就能发现的签名输入错误（拼错了、少了一行、编码不对）。"""


class UvpApiError(RuntimeError):
    """平台返回了非 2xx。requestId 是排障的唯一线索，务必打日志。"""

    def __init__(self, status: int, code: Optional[str], message: str,
                 request_id: Optional[str], body: str) -> None:
        super().__init__("%s %s (requestId=%s)" % (status, message, request_id))
        self.status = status
        self.code = code
        self.message = message
        self.request_id = request_id
        self.body = body


# ---------------------------------------------------------------- 编码工具

def _hex_value(byte: int) -> Optional[int]:
    if 0x30 <= byte <= 0x39:      # 0-9
        return byte - 0x30
    if 0x61 <= byte <= 0x66:      # a-f
        return byte - 0x61 + 10
    if 0x41 <= byte <= 0x46:      # A-F
        return byte - 0x41 + 10
    return None


def percent_encode(text: str) -> str:
    """严格 RFC3986：只放行 A-Z a-z 0-9 - . _ ~，其余一律 %XX（大写）。"""
    out = []
    for byte in text.encode("utf-8"):
        if byte in _SAFE_BYTES:
            out.append(chr(byte))
        else:
            out.append("%%%02X" % byte)
    return "".join(out)


def _percent_decode(raw: str) -> str:
    """只解 %XX，其它字节原样保留；非法转义或非法 UTF-8 直接报错。

    不能用 urllib.parse.unquote —— 它对非法转义过于宽容，而平台要求严格。
    """
    data = raw.encode("utf-8")
    out = bytearray()
    index = 0
    while index < len(data):
        byte = data[index]
        if byte != 0x25:                      # 不是 '%'
            out.append(byte)
            index += 1
            continue
        if index + 2 >= len(data):
            raise UvpSignatureError("query 里有被截断的百分号转义: %r" % raw)
        high = _hex_value(data[index + 1])
        low = _hex_value(data[index + 2])
        if high is None or low is None:
            raise UvpSignatureError("query 里有非法百分号转义: %r" % raw)
        out.append(high * 16 + low)
        index += 3
    try:
        return out.decode("utf-8")
    except UnicodeDecodeError as exc:
        raise UvpSignatureError("query 不是合法 UTF-8: %r" % raw) from exc


def canonical_query(raw_query: str) -> str:
    """解码一次 -> RFC3986 编码 -> 按 (key, value) 排序。"""
    if len(raw_query.encode("utf-8")) > MAX_QUERY_BYTES:
        raise UvpSignatureError("query 超过 %d 字节" % MAX_QUERY_BYTES)
    if raw_query == "":
        return ""
    pairs = []
    seen = set()
    for part in raw_query.split("&"):
        if part == "":
            raise UvpSignatureError("query 里有空段")
        if ";" in part:
            raise UvpSignatureError("query 里不允许分号: %r" % part)
        if "+" in part:
            raise UvpSignatureError("空格必须编码成 %20，不能用 +: %r" % part)
        if "=" not in part:
            raise UvpSignatureError("query 段缺少等号: %r" % part)
        raw_key, raw_value = part.split("=", 1)
        key = _percent_decode(raw_key)
        if key == "":
            raise UvpSignatureError("query 里有空 key")
        value = _percent_decode(raw_value)
        encoded_key = percent_encode(key)
        if encoded_key in seen:
            raise UvpSignatureError("query 里有重复 key: %r" % key)
        seen.add(encoded_key)
        pairs.append((encoded_key, percent_encode(value)))
    pairs.sort()
    return "&".join("%s=%s" % (key, value) for key, value in pairs)


# ---------------------------------------------------------------- 输入校验

def _normalize_method(method: str) -> str:
    if not method:
        raise UvpSignatureError("method 不能为空")
    for char in method:
        code = ord(char)
        if code > 0x7F or code <= 0x20 or code == 0x7F:
            raise UvpSignatureError("method 含非法字符: %r" % method)
    return method.upper()


def _validate_access_key(access_key: str) -> None:
    if not isinstance(access_key, str) or not access_key.startswith("uvp_") \
            or len(access_key) != 36:
        raise UvpSignatureError("access key 必须是 uvp_ + 32 位小写十六进制")
    if any(char not in "0123456789abcdef" for char in access_key[4:]):
        raise UvpSignatureError("access key 必须是 uvp_ + 32 位小写十六进制")


def _validate_timestamp(timestamp: str) -> None:
    if timestamp == "" or not timestamp.isdigit():
        raise UvpSignatureError("timestamp 必须是十进制秒（字符串）")
    if len(timestamp) > 1 and timestamp[0] == "0":
        raise UvpSignatureError("timestamp 不能有前导零")


def _validate_nonce(nonce: str) -> None:
    if len(nonce) != 32 or any(char not in "0123456789abcdef" for char in nonce):
        raise UvpSignatureError("nonce 必须是 32 位小写十六进制（16 字节随机数）")


def _validate_line_field(value: str, name: str) -> None:
    if not value:
        raise UvpSignatureError("%s 不能为空" % name)
    for char in value:
        if char in "\r\n\x00\x7f":
            raise UvpSignatureError("%s 不能含 CR/LF/NUL/DEL" % name)


def _validate_idempotency_key(value: str) -> None:
    if value == "":
        return
    if len(value) > 128:
        raise UvpSignatureError("Idempotency-Key 不能超过 128 字节")
    for char in value:
        if not (0x21 <= ord(char) <= 0x7E):
            raise UvpSignatureError("Idempotency-Key 只能是可见 ASCII")
    _validate_line_field(value, "Idempotency-Key")


def _validate_path(path: str) -> None:
    if not path or path[0] != "/":
        raise UvpSignatureError("path 必须以 / 开头")
    for index, char in enumerate(path):
        code = ord(char)
        if code > 0x7F or code <= 0x20 or code == 0x7F:
            raise UvpSignatureError("path 只能是可见 ASCII，路径参数请不要传中文")
        if char in "%\\":
            raise UvpSignatureError("path 不允许出现 %% 或反斜杠（平台会拒绝）")
        if index > 0 and char == "/" and path[index - 1] == "/":
            raise UvpSignatureError("path 不允许连续斜杠")
    if path != "/" and path.endswith("/"):
        raise UvpSignatureError("path 不允许以 / 结尾")
    for segment in path.split("/"):
        if segment in (".", ".."):
            raise UvpSignatureError("path 不允许 . 或 .. 段")


def _validate_json_body(body: bytes) -> None:
    def reject_duplicates(pairs):
        seen = set()
        for key, _ in pairs:
            if key in seen:
                raise UvpSignatureError("JSON 里有重复字段: %r" % key)
            seen.add(key)
        return dict(pairs)

    try:
        text = body.decode("utf-8")
    except UnicodeDecodeError as exc:
        raise UvpSignatureError("JSON body 不是合法 UTF-8") from exc
    try:
        json.loads(text, object_pairs_hook=reject_duplicates)
    except json.JSONDecodeError as exc:
        raise UvpSignatureError("JSON body 不合法: %s" % exc) from exc


# ---------------------------------------------------------------- 签名

def decode_secret_key(secret_key: str) -> bytes:
    """SK 是 base64url 无填充的 32 字节；HMAC 用解码后的字节。"""
    if not secret_key or not isinstance(secret_key, str):
        raise UvpSignatureError("secret key 不能为空")
    body = secret_key
    if any(char not in "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
           for char in body):
        raise UvpSignatureError("secret key 必须是 base64url（无填充），不要带 = 或空格")
    padded = body + "=" * (-len(body) % 4)
    try:
        key = base64.urlsafe_b64decode(padded)
    except (ValueError, TypeError) as exc:
        raise UvpSignatureError("secret key 不是合法 base64url") from exc
    if len(key) != 32:
        raise UvpSignatureError("secret key 必须解码成 32 字节，当前 %d 字节" % len(key))
    return key


def sha256_hex(body: bytes) -> str:
    return hashlib.sha256(body).hexdigest()


def canonical(method: str, path: str, raw_query: str = "", content_type: str = "",
              body: Union[bytes, None] = None, access_key: str = "",
              timestamp: str = "", nonce: str = "", idempotency_key: str = "",
              audience: str = "") -> str:
    """生成 11 行 canonical 字符串（UTF-8，LF 分隔，末尾无换行）。"""
    body = b"" if body is None else body
    method = _normalize_method(method)
    _validate_access_key(access_key)
    _validate_timestamp(timestamp)
    _validate_nonce(nonce)
    _validate_line_field(audience, "audience")
    _validate_idempotency_key(idempotency_key)
    _validate_path(path)
    if len(body) > MAX_BODY_BYTES:
        raise UvpSignatureError("body 超过 %d 字节" % MAX_BODY_BYTES)

    query = canonical_query(raw_query)

    if method == "GET":
        if content_type != "" or len(body) != 0:
            raise UvpSignatureError("GET 不能带 body 和 Content-Type")
    elif method == "POST":
        if len(body) == 0 and content_type not in ("", "application/json"):
            raise UvpSignatureError("POST 的 Content-Type 只能是 application/json 或空")
        if len(body) > 0 and content_type != "application/json":
            raise UvpSignatureError("POST 有 body 时 Content-Type 必须是裸的 application/json")
    else:
        if content_type not in ("", "application/json"):
            raise UvpSignatureError("Content-Type 只能是 application/json 或空")

    if content_type == "application/json" and len(body) > 0:
        _validate_json_body(body)

    return "\n".join([
        "UVP-HMAC-SHA256/1",
        access_key,
        timestamp,
        nonce,
        method,
        path,
        query,
        content_type,
        sha256_hex(body),
        idempotency_key,
        audience,
    ])


def sign(secret_key: str, canonical_string: str) -> str:
    """小写十六进制 HMAC-SHA256。"""
    key = decode_secret_key(secret_key)
    return hmac.new(key, canonical_string.encode("utf-8"), hashlib.sha256).hexdigest()


def sign_request(access_key: str, secret_key: str, audience: str, method: str,
                 path: str, raw_query: str = "", body: Union[bytes, str, None] = None,
                 idempotency_key: str = "",
                 timestamp: Optional[str] = None,
                 nonce: Optional[str] = None) -> Dict[str, str]:
    """返回可直接塞进请求的签名头（含 Content-Type 与幂等键）。"""
    body_bytes = b"" if body is None else (body.encode("utf-8") if isinstance(body, str) else body)
    content_type = "application/json" if body_bytes else ""
    timestamp = timestamp or str(int(time.time()))
    nonce = nonce or secrets.token_hex(16)

    canonical_string = canonical(
        method=method, path=path, raw_query=raw_query, content_type=content_type,
        body=body_bytes, access_key=access_key, timestamp=timestamp, nonce=nonce,
        idempotency_key=idempotency_key, audience=audience,
    )
    headers = {
        HEADER_SIGN_VERSION: SIGN_VERSION,
        HEADER_ACCESS_KEY: access_key,
        HEADER_TIMESTAMP: timestamp,
        HEADER_NONCE: nonce,
        HEADER_SIGNATURE: sign(secret_key, canonical_string),
    }
    if content_type:
        headers["Content-Type"] = content_type
    if idempotency_key:
        headers[HEADER_IDEMPOTENCY] = idempotency_key
    return headers


def build_query(params: Mapping[str, Any]) -> str:
    """把 dict 变成 rawQuery（已经 RFC3986 编码，不含 +）。"""
    parts = []
    for key, value in params.items():
        if value is None:
            continue
        parts.append("%s=%s" % (percent_encode(str(key)), percent_encode(str(value))))
    return "&".join(parts)


# ---------------------------------------------------------------- 客户端

class UvpClient:
    """最小可用客户端：签名、发请求、拆信封、映射错误。

    只覆盖对接必需的能力，不做重试策略 —— GET 超时可以用新 nonce 重试，
    POST 超时**不要**自动重试，结果未知时用同一个 Idempotency-Key 决定是否重放。
    """

    def __init__(self, base_url: str, access_key: str, secret_key: str,
                 audience: str, timeout: float = 15.0) -> None:
        parts = urlsplit(base_url)
        if parts.scheme not in ("http", "https") or not parts.hostname:
            raise ValueError("base_url 形如 http://<平台IP>:51002")
        self._scheme = parts.scheme
        self._host = parts.hostname
        self._port = parts.port
        self._prefix = parts.path.rstrip("/")
        self._access_key = access_key
        self._secret_key = secret_key
        self._audience = audience
        self._timeout = timeout
        decode_secret_key(secret_key)          # 提前失败，别等到第一次请求

    def close(self) -> None:
        pass

    def __enter__(self) -> "UvpClient":
        return self

    def __exit__(self, *exc_info) -> None:
        self.close()

    def request(self, method: str, path: str, query: Optional[Mapping[str, Any]] = None,
                body: Union[bytes, str, None] = None, idempotency_key: str = "",
                timeout: Optional[float] = None) -> Any:
        raw_query = build_query(query or {})
        body_bytes = b"" if body is None else (
            body.encode("utf-8") if isinstance(body, str) else body)
        headers = sign_request(
            self._access_key, self._secret_key, self._audience, method,
            path, raw_query, body_bytes, idempotency_key,
        )
        target = path if not raw_query else path + "?" + raw_query
        status, payload = self._send(method, target, headers, body_bytes,
                                     self._timeout if timeout is None else timeout)
        if 200 <= status < 300:
            return payload
        raise _to_api_error(status, payload)

    def _send(self, method: str, target: str, headers: Dict[str, str],
              body: bytes, timeout: float):
        if self._scheme == "https":
            context = ssl.create_default_context()
            connection = http.client.HTTPSConnection(
                self._host, self._port, timeout=timeout, context=context)
        else:
            connection = http.client.HTTPConnection(self._host, self._port, timeout=timeout)
        try:
            connection.request(method, self._prefix + target, body=body or None,
                               headers=headers)
            response = connection.getresponse()
            text = response.read().decode("utf-8", errors="replace")
            try:
                payload = json.loads(text) if text else None
            except json.JSONDecodeError:
                payload = text
            return response.status, payload
        finally:
            connection.close()

    # 便捷方法 -------------------------------------------------------
    def get(self, path: str, query: Optional[Mapping[str, Any]] = None,
            timeout: Optional[float] = None) -> Any:
        return self.request("GET", path, query=query, timeout=timeout)

    def post(self, path: str, body: Union[bytes, str, None] = None,
             idempotency_key: str = "", timeout: Optional[float] = None) -> Any:
        return self.request("POST", path, body=body,
                            idempotency_key=idempotency_key, timeout=timeout)


def _to_api_error(status: int, payload: Any) -> UvpApiError:
    if isinstance(payload, dict):
        return UvpApiError(
            status,
            payload.get("code"),
            str(payload.get("message") or "empty message"),
            payload.get("requestId"),
            json.dumps(payload, ensure_ascii=False),
        )
    return UvpApiError(status, None, str(payload)[:200], None, str(payload)[:200])
