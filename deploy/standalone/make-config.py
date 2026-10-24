#!/usr/bin/env python3
"""把开发环境的 config.yml 改造成绿色安装包的生产配置。

为什么不是直接手写一份config.yml
--------------------------------
配置项有几十条，且后端**每次新增配置项**都要同步到这里。手工维护两份必然漂移，
而漂移的表现是「装完启动失败」或更糟的「用了错的库」。

做法：读仓库里的 config.yml，只改**必须改的那几项**，其余原样保留。
新增配置项会自动跟着走，不需要在这里补。

必须改的项（绿色包三条铁律）
---------------------------
1. ``usedbtype: sqlite`` + 只开sqlite 的 ``isinitglobalgorm*``
   ⛔ 必须把 mysql/postgresql/sqlserver 的开关全置 0 —— 后端启动会**逐个连**
   所有开了开关的库，任一失败就 ``startupFail("database")`` 退出。
   只改 usedbtype 不改开关 = 去连 127.0.0.1:3306，报错只说 "database"，
   根本看不出「你连了个不存在的 MySQL」（记忆里记录过这个坑）。
2. ``cachetype: redis`` + redis 指向 **127.0.0.1**（包内自带，不连开发机）
3. ``httpserver.port`` 与Redis 端口从命令行传进来，默认 8280 / 6379
4. ``httpserver.trustedproxies`` 信任本机反向代理链，避免在线用户记录为 127.0.0.1
"""

from __future__ import annotations

import argparse
import base64
import binascii
import json
import os
import re
from pathlib import Path

# 这些开关必须全部置 0（除目标库那个）
INIT_SWITCHES = (
    "isinitglobalgormmysql",
    "isinitglobalgormpostgresql",
    "isinitglobalgormsqlserver",
    "isinitglobalgormsqlite",
)

# 绿色包的后端只通过本机 Nginx 接收请求；外层代理可能运行在任意私有网段。
# 后端只监听本机，因此信任 RFC1918 代理网段不会把后端直接暴露给公网。
TRUSTED_PROXIES = ["127.0.0.1", "::1", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"]
UPGRADE_KEY_ID = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$")


def parse_upgrade_trusted_keys(value: str) -> dict[str, str] | None:
    """Validate the optional build-time JSON map and normalize public keys to Base64.

    An empty value preserves the existing ``upgrade.trusted_keys`` from the source
    config. This lets package builds use their normal config input when no override
    is needed, while release pipelines can inject their public key without a private
    signing key ever entering the package builder.
    """
    if not value:
        return None
    try:
        parsed = json.loads(value)
    except json.JSONDecodeError as exc:
        raise SystemExit("UVP_UPGRADE_TRUSTED_KEYS must be a JSON object") from exc
    if not isinstance(parsed, dict):
        raise SystemExit("upgrade trusted keys must be a JSON object")

    normalized = {}
    for key_id, encoded in parsed.items():
        if not isinstance(key_id, str) or not UPGRADE_KEY_ID.fullmatch(key_id):
            raise SystemExit("upgrade trusted key id is invalid")
        if not isinstance(encoded, str):
            raise SystemExit("upgrade trusted public key must be a Base64 or hex string")
        try:
            raw = bytes.fromhex(encoded) if len(encoded) == 64 else base64.b64decode(encoded, validate=True)
        except (ValueError, binascii.Error):
            raise SystemExit("upgrade trusted public key must be a Base64 or hex string") from None
        if len(raw) != 32:
            raise SystemExit("upgrade trusted public key must decode to 32 bytes")
        normalized[key_id] = base64.b64encode(raw).decode("ascii")
    return normalized


def set_scalar(text: str, key_path: str, value: str) -> str:
    """把形如 ``key: value`` 的键改成新值，**保留原缩进**。

    ⛔ ``key_path`` 只写**末级键名**（如 ``usedbtype``），不写完整路径。
       写完整路径（如 ``gormv2.usedbtype``）会匹配不到 —— 正则把整串当键名，
       而文件里只有末级名字 + 缩进。踩过一次：明明看见这行存在却报"找不到键"。

    用正则而不是 YAML 库：config.yml 里有 ``@`` 前缀的变量与重复键，
    通用 YAML 解析器要么报重复键要么把顺序/注释全丢掉 —— 而配置文件里的
    注释是给现场运维看的，不能丢。
    """
    pattern = re.compile(
        rf"^(?P<indent>\s*){re.escape(key_path)}\s*:\s*(?P<value>.*?)(?P<trail>\s*)$",
        re.MULTILINE,
    )
    replaced = 0

    def sub(match: re.Match[str]) -> str:
        nonlocal replaced
        replaced += 1
        return f"{match.group('indent')}{key_path}: {value}{match.group('trail')}"

    text = pattern.sub(sub, text)
    if replaced == 0:
        raise SystemExit(f"配置里找不到键 {key_path} —— 是不是改过结构？不要靠猜，直接失败")
    return text


def set_in_section(text: str, section: str, key: str, value: str) -> str:
    """只在指定段落内改键，**段落之后同缩进的其他键不动**。

    ⛔⛔ 绝对不要全局替换 ``host`` / ``port`` ——它们在 gormv2 的每个子段下
    都重名（mysql.host、redis.host、postgresql.host…），一替换就**把 mysql 的
    也改成127.0.0.1**，而那只在 usedbtype≠mysql 时无害 —— 客户哪天切回 MySQL
    就会连到本机、报 "connection refused"，且完全看不出是这个打包脚本干的。
       踩过一次：先写成全局替换，后来靠"只影响未选中的库"侥幸没出事 ——
       那不叫安全，叫运气。
    """
    # 定位段落：形如 `<缩进>mysql:` 的行。段名在文件里带缩进，
    # 用 startswith(f"{section}:") 匹配不到 —— 必须容忍前导空白。
    lines = text.split("\n")
    start = None
    for i, line in enumerate(lines):
        if line.strip() == f"{section}:":
            start = i
            break
    if start is None:
        raise SystemExit(f"配置里找不到段落 {section}")

    indent_of_section = len(lines[start]) - len(lines[start].lstrip())
    end = len(lines)
    for i in range(start + 1, len(lines)):
        line = lines[i]
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        if len(line) - len(line.lstrip()) <= indent_of_section:
            end = i
            break

    pattern = re.compile(
        rf"^(?P<indent>\s*){re.escape(key)}\s*:\s*.*?(?P<trail>\s*)$"
    )
    replaced = 0
    for i in range(start + 1, end):
        m = pattern.match(lines[i])
        if m:
            lines[i] = f"{m.group('indent')}{key}: {value}{m.group('trail')}"
            replaced += 1
            break
    if replaced == 0:
        raise SystemExit(f"段落 {section} 里找不到键 {key}")
    return "\n".join(lines)


JWT_SIGN_KEY_MIN_LEN = 32
JWT_SIGN_KEY_PLACEHOLDER_FRAGMENTS = (
    "change_me", "change-me", "changeme",
    "your_secret", "your-secret", "placeholder",
    "random_secret", "todo",
)


def _normalize_scalar(raw: str) -> str:
    return raw.strip().strip('"').strip("'").strip()


def sign_key_looks_placeholder(value: str) -> bool:
    """判据与后端 ``ginhelper.SignKeyLooksPlaceholder`` 保持一致。

    ⛔ 两处判据必须同构：出包侧只负责「别把占位符发出去」，运行侧负责
       「老现场升级后即便还带着占位符也不暴露调试面」。判据漂移的表现是
       包校验通过、运行时却仍然放行 —— 所以两边的特征串与长度阈值要一起改。
    """
    normalized = _normalize_scalar(value)
    if len(normalized) < JWT_SIGN_KEY_MIN_LEN:
        return True
    lowered = normalized.lower()
    return any(fragment in lowered for fragment in JWT_SIGN_KEY_PLACEHOLDER_FRAGMENTS)


def harden_security_defaults(text: str, jwt_sign_key: str = "") -> tuple[str, str]:
    """出厂安全默认值：关调试面 + 把占位 JWT 密钥换成随机值。

    ⛔⛔ 为什么必须在**出包时**做，而不是让现场去改：
      · ``config.example.yml`` 的 ``appdebug: true`` 会注册**免鉴权**的
        ``/viewCache``（整库缓存转储，refresh token 是原文存的）；
      · ``jwttokensignkey`` 若是模板占位符，等于全世界的包共用同一把签名密钥。
      这两项随包发给客户后，现场没有任何提示要求改 —— 实测 1.1.10 的包里
      两者都还是模板值。

    ⛔ 显式传入的 ``--jwt-sign-key`` 也要过同一套判据：占位符就**拒绝出包**，
      不能"传了就当合法"。
    """
    import secrets

    text = set_in_section(text, "server", "appdebug", "false")

    current = ""
    match = re.search(r"(?m)^\s*jwttokensignkey\s*:\s*(?P<value>[^#\n]*)", text)
    if match:
        current = _normalize_scalar(match.group("value"))

    chosen = _normalize_scalar(jwt_sign_key)
    if chosen:
        if sign_key_looks_placeholder(chosen):
            raise SystemExit(
                "传入的 JWT 签名密钥不合格（长度需 ≥32 且不含占位特征）—— 拒绝出包"
            )
    elif sign_key_looks_placeholder(current):
        chosen = secrets.token_hex(32)
    else:
        chosen = current

    text = set_in_section(text, "token", "jwttokensignkey", f'"{chosen}"')
    _assert_security_defaults(text, chosen)
    return text, chosen


def _assert_security_defaults(text: str, jwt_sign_key: str) -> None:
    """解析后断言两项默认值真的生效。

    ⛔ 只查「字符串出现过」会漏掉真正的失效形态：
       ``appdebug: true`` 与 ``appdebug: false`` 在文件里都"出现过"，
       而 ``set_in_section`` 匹配不到时是**静默不替换**、仍照常打印成功。
    """
    try:
        import yaml
    except ImportError:
        if "appdebug: false" not in text:
            raise SystemExit("调试开关写入失败：结果里找不到 appdebug: false")
        print("   ⚠️ 未安装 PyYAML，跳过安全默认值结构校验（pip install pyyaml 可启用）")
        return

    try:
        parsed = yaml.safe_load(text) or {}
    except Exception as exc:
        raise SystemExit(
            f"安全默认值写入后配置无法解析（{type(exc).__name__}）—— 多半是缩进或插入位置错了"
        ) from None

    problems = []
    if (parsed.get("server") or {}).get("appdebug") is not False:
        problems.append(
            f"server.appdebug={(parsed.get('server') or {}).get('appdebug')!r}（应为 False）"
        )
    got_key = str((parsed.get("token") or {}).get("jwttokensignkey") or "")
    if got_key != jwt_sign_key:
        problems.append("token.jwttokensignkey 与期望值不一致（写入没生效）")
    if sign_key_looks_placeholder(got_key):
        problems.append("token.jwttokensignkey 仍是空/占位/过短的值")
    if problems:
        raise SystemExit("安全默认值校验失败：\n  - " + "\n  - ".join(problems))


def build(source: Path, target: Path, http_port: str, redis_port: str,
          zlm_secret: str = "", zlm_media_server_id: str = "",
          upgrade_trusted_keys_json: str = "",
          zlm_api_port: str = "", hook_port: str = "",
          jwt_sign_key: str = "") -> None:
    text = source.read_text(encoding="utf-8")

    upgrade_trusted_keys = parse_upgrade_trusted_keys(upgrade_trusted_keys_json)
    if upgrade_trusted_keys is not None:
        text = set_in_section(
            text, "upgrade", "trusted_keys",
            json.dumps(upgrade_trusted_keys, separators=(",", ":")),
        )

    # 1) 数据库：默认 SQLite
    #    键名只写末级，且用「段落内替换」避免同名键被误伤。
    text = set_scalar(text, "usedbtype", "sqlite")
    # ⛔ 必须把 mysql/postgresql/sqlserver 的开关全置 0：后端启动会**逐个连**
    #   所有开了开关的库，任一失败就 startupFail("database") 退出。只改
    #   usedbtype 不改开关 = 去连 127.0.0.1:3306，报错只说 "database"。
    for name in ("mysql", "postgresql", "sqlserver"):
        text = set_in_section(text, f"{name}", "isinitglobalgorm" + name, "0")

    # 2) 缓存：包内自带 Redis，只连回环
    text = set_scalar(text, "cachetype", "redis")
    text = set_in_section(text, "redis", "host", "127.0.0.1")
    text = set_in_section(text, "redis", "port", redis_port)

    # 3) HTTP 端口
    text = set_in_section(text, "httpserver", "port", f":{http_port}")
    text = set_in_section(
        text,
        "httpserver",
        "trustedproxies",
        '["127.0.0.1", "::1", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"]',
    )

    # 4) ZLM 身份：secret 与节点标识都**固定写死**，不留占位符。
    #
    # ⛔⛔ 为什么必须在**出包时**就写好，而不是让启动脚本去对齐：
    #   · ZLM 的 config.ini 是随包发的，secret 已固化在包里；
    #   · 而 config.yml 是出包时生成的 —— 此处不写，启动脚本就得反推 ini，
    #     一旦反推失败（判据没匹配上）就是「配置文件静默 + 脚本谎报成功」。
    #   实测这个坑连踩两层：① zlm 是**嵌套**段、顶级段判据抓不到；
    #   ② 值后面跟着**行尾注释**，正则把注释吞进值里判成"不是占位符"。
    #   源头固定好，后面那两层就不需要存在了。
    # 节点标识（mediaserverid）：seed 进 meta_node.media_server_uuid，
    #   且被 apply.go 通过 setServerConfig **持久化回 ZLM 的 config.ini**，
    #   所以它必须稳定 —— 随机值一旦落盘就固化，事后改配置也没用。
    if zlm_secret:
        text = set_in_section(text, "zlm", "secret", zlm_secret)
    if zlm_media_server_id:
        text = set_in_section(text, "zlm", "mediaserverid", zlm_media_server_id)

    # 5) ZLM HTTP API 端口 与 hook 回调端口
    #
    # ⛔⛔ 这两项只影响**首次 seed** 的节点记录（zlm.httpport → meta_node.api_port），
    #   但它决定了「第一次启动时后端能不能连上 ZLM」——
    #   连不上就 seed 不成功、节点永久离线，之后改 UI 也没用（前提是先能进得去）。
    #
    # ⛔ 容器版与绿色包的区别：绿色包端口是 ctl.sh 统一规划的固定值
    #   （51004 / 51002），模板默认的 18080 / 8280 在绿色包**从不生效**；
    #   容器版允许客户改端口段（UVP_*_PORT），所以必须把实际值写进来。
    #   不写的话：ZLM 在 53004 而 config.yml 说 18080 ⇒
    #   `zlm.probe.failed endpoint=http://127.0.0.1:18080`，节点一直离线。
    if zlm_api_port:
        text = set_in_section(text, "zlm", "httpport", zlm_api_port)
    if hook_port:
        text = set_in_section(text, "media", "hookport", hook_port)

    # 6) 出厂安全默认值：关调试面 + JWT 密钥不许留占位符
    #    放在最后：前面几步只动库/缓存/端口，这一步会改写 server 与 token 段。
    text, jwt_sign_key = harden_security_defaults(text, jwt_sign_key)

    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(text, encoding="utf-8")
    print(f"✅ 已生成 {target}")
    print(f"   数据库 sqlite / 缓存 redis(127.0.0.1:{redis_port}) / HTTP :{http_port}")
    if zlm_secret and zlm_media_server_id:
        print(f"   ZLM secret / 节点标识已固定（各 {len(zlm_secret)}/{len(zlm_media_server_id)} 字符）")
    # ⛔ 只报「已生成」不打印密钥本身：出包日志会被归档/外发，密钥进日志等于泄露。
    print(f"   调试开关: 关闭 / JWT 签名密钥: 已就绪（{len(jwt_sign_key)} 位随机串）")


def add_sqlite_section(target: Path, db_path: str) -> None:
    """追加SQLite 段并开启它的初始化开关。

    ⛔ 后端读的是 ``gormv2.sqlite.isinitglobalgormsqlite`` 与
       ``gormv2.sqlite.write.database``，而config.example.yml 里没有这一段
       （只有 mysql/sqlserver/postgresql 三段），所以必须追加。
       只改usedbtype 是不够的 —— 开关与连接参数都无处可取，
       后端启动时会因「没有可初始化的库」直接退出。

    ⭐ 本函数有两条硬约束，都是踩出来的：
    1. 插入点必须**按行匹配实际缩进**。写死 ``"\n    usedbtype:"`` 而实际是 2 空格时，
       ``str.replace`` 静默返回原串、什么都不插，而调用方还照常打印「成功」。
    2. 缩进平移**只能动块的首行**。对每一行都``lstrip()`` 会把sqlite: 的子键
       （isinitglobalgormsqlite 等）提到与 sqlite: 同级，YAML 照样解析成功、
       不报错 —— 症状是后端启动即退且日志无内容。
    """
    text = target.read_text(encoding="utf-8")

    if re.search(r"^\s*sqlite:", text, re.MULTILINE):
        text = set_in_section(text, "sqlite", "isinitglobalgormsqlite", "1")
        text = set_in_section(text, "write", "database", db_path)
    else:
        block = [
            "",
            "# ---- SQLite（绿色安装包默认库；本段由打包脚本生成）----",
            "sqlite:",
            "    isinitglobalgormsqlite: 1",
            "    isopenreaddb: 0",
            "    loglevel: warn",
            "    slowthreshold: 30",
            "    timezone: Local",
            "    write:",
            f"        database: {db_path}",
        ]
        out: list[str] = []
        inserted = False
        for line in text.split("\n"):
            m = re.match(r"^(\s*)usedbtype\s*:", line)
            if m and not inserted:
                # 只把首行对齐到 usedbtype 的缩进，块内其余行保持相对缩进
                pad = m.group(1)
                out.extend(pad + b if b else "" for b in block)
                inserted = True
            out.append(line)
        if not inserted:
            raise SystemExit(
                "配置里找不到 usedbtype 键 —— 无法确定 SQLite 段的插入位置。"
                "不要靠猜，请检查 config.example.yml 的结构是否变了。"
            )
        text = "\n".join(out)

    _assert_sqlite_section_valid(text, db_path)
    target.write_text(text, encoding="utf-8")
    print(f"   SQLite 数据库路径: {db_path}（已写入并通过结构校验）")


def _assert_sqlite_section_valid(text: str, db_path: str) -> None:
    """校验 SQLite 段**结构上**真的对，而不只是「字符串出现过」。

    ⛔ 配置文件是静默的：少一层缩进 yaml照样解析成功、不报错，
    缺一段也不报错 —— 两者都只在客户机上表现为「后端启动即退、日志空白」。
    所以这里必须**解析后**断言子键存在且取值正确。
    """
    try:
        import yaml
    except ImportError:                      # 没装 PyYAML 就退回弱检查
        if "isinitglobalgormsqlite" not in text:
            raise SystemExit("SQLite 段写入失败：结果里找不到 isinitglobalgormsqlite")
        print("   ⚠️ 未安装 PyYAML，跳过结构校验（pip install pyyaml 可启用）")
        return

    try:
        parsed = yaml.safe_load(text) or {}
    except Exception as exc:
        # ⛔ 同 add_qr_provision_section：解析失败要在**这里**变成清晰结论，
        #   不能把 yaml 的「第 N 行」traceback 直接抛给出包现场（离真因很远）。
        raise SystemExit(
            f"配置无法解析（{type(exc).__name__}）——多半是某段插入位置或缩进错了"
        ) from None
    gorm = parsed.get("gormv2") or {}
    sqlite = gorm.get("sqlite") or {}
    problems = []
    if str(gorm.get("usedbtype", "")).strip("\"'") != "sqlite":
        problems.append(f"usedbtype={gorm.get('usedbtype')!r}（应为 sqlite）")
    if str(sqlite.get("isinitglobalgormsqlite")) != "1":
        problems.append(f"gormv2.sqlite.isinitglobalgormsqlite={sqlite.get('isinitglobalgormsqlite')!r}（应为 '1'）")
    if (sqlite.get("write") or {}).get("database") != db_path:
        problems.append(f"gormv2.sqlite.write.database={(sqlite.get('write') or {}).get('database')!r}（应为 {db_path!r}）")
    if problems:
        raise SystemExit("SQLite 段结构校验失败：\n  - " + "\n  - ".join(problems))


def set_log_filepath(target: Path, log_path: str) -> None:
    """把 ``logs.filepath`` 指到包内日志目录（绿色包：``./logs/app/…``）。

    ⛔ 为什么不直接改 config.example.yml 的默认值：那份模板**同时**被
      ``deploy/test`` 的 Docker 形态使用（configure_server.py 读它、其单测
      还断言里面有 ``./resource/logs/…``）——改模板会连带弄红那套部署。
      ⇒ 模板保持中立，绿色包在出包时在这里覆写。

    形态照 ``sqlite`` 段：**yaml 解析后**断言取值，而不是「字符串出现过」。
    """
    text = target.read_text(encoding="utf-8")
    text = set_in_section(text, "logs", "filepath", log_path)

    try:
        import yaml
    except ImportError:                      # 没装 PyYAML 就退回弱检查
        if log_path not in text:
            raise SystemExit("logs.filepath 写入失败：结果里找不到目标路径")
        print("   ⚠️ 未安装 PyYAML，跳过 logs.filepath 结构校验")
        target.write_text(text, encoding="utf-8")
        return

    try:
        parsed = yaml.safe_load(text) or {}
    except Exception as exc:
        raise SystemExit(
            f"配置无法解析（{type(exc).__name__}）——多半是 logs.filepath 改写时缩进错了"
        ) from None
    got = str((parsed.get("logs") or {}).get("filepath") or "")
    if got != log_path:
        raise SystemExit(
            f"logs.filepath 结构校验失败：得到 {got!r}（应为 {log_path!r}）。\n"
            "   出了这个包，后端业务日志会写到别处 —— 启动本身看起来完全正常，"
            "   只是现场排障时永远找不到那份日志。"
        )
    target.write_text(text, encoding="utf-8")
    print(f"   后端业务日志: {log_path}（已写入并通过结构校验）")


def add_qr_provision_section(target: Path, host: str, port: str) -> None:
    """写入「扫码接入基址」段（``gb28181.qr_provision``）。

    ⛔⛔ 默认**留空出包**（host 为空 ⇒ base_url 为空），由客户机首次启动时
       uvp-gb28181-ctl.sh 用本机实际地址兜底生成。理由是：出包机探到的 IP 是
       **我们打包机**的地址，写进包里发出去，客户机上那个地址永远指向一台不存在
       的主机 —— 2026-10-20 客户现场 `http://192.168.10.220:51002` 的残留就是
       这么来的：页面看起来"地址填好了"，设备扫了却连不上，且平台零报错。
       传了 ``--qr-provision-host`` 时仍按传入值写（我方预配置场景）。

    ⛔⛔ 为什么这个值必须有一个明确落点，而不能让前端自己推断：
      二维码是给**设备**扫的,它要访问的是后端地址,而不是「用户打开平台的地址」。
      绿色包的前端走 nginx 自签 HTTPS（SAN 只有 localhost/127.0.0.1），
      前端若用 ``window.location.origin`` 当默认值，二维码里就带上了那个自签地址 ⇒
      设备侧 Ktor CIO 默认校验证书、握手失败 ⇒ 失败又被归类成 NetworkError ⇒
      UI 骗用户「检查手机与平台是否同网络」。实测就是这样坏的（2026-10-08）。

    形态照 ``sqlite`` 段：**yaml 解析后**断言子键存在且在 ``gb28181`` 下。
    静默失败的老问题在这里同样成立 —— 配置写错不报错,只表现为「扫码不出码 / 出坏码」。

    ⛔⛔ **已存在就改写，绝不追加第二段**。
       ``config.example.yml`` 里本来就有样例段（给现场看的），
       无条件追加会让同一个键在文件里出现两次：YAML 解析时后者静默覆盖前者，
       行为上凑巧对，但**文件里两份配置**会直接把现场运维绕晕。
       （变异自证抓到的就是这个：删掉生成段，校验仍然"通过"——
         因为 example 那份还在，校验根本分不清包内配置到底有没有生效值。）
    """
    text = target.read_text(encoding="utf-8")
    want = f"http://{host}:{port}" if host else ""

    if _has_section(text, "qr_provision"):
        text = _rewrite_section(text, "qr_provision", {
            "base_url": f'"{want}"',
            "host": '""',
            "port": "0",
            "require_tls": "false",
        })
    else:
        text = _append_to_gb28181(text, [
            "",
            "# ---- 扫码接入基址（绿色安装包；本段由打包脚本生成）----",
            "# 二维码里带的**设备可达**地址。不要改成浏览器打开平台的地址。",
            "qr_provision:",
            f"    base_url: \"{want}\"",
            "    host: \"\"",
            "    port: 0",
            "    require_tls: false",
        ])

    _assert_qr_provision_valid(text, host, port)
    _assert_qr_provision_not_duplicated(text)
    target.write_text(text, encoding="utf-8")
    print(f"   扫码接入基址: {want or '（留空，首启按本机地址生成）'}（已写入并通过结构校验）")


def _section_lines(lines: list[str], name: str) -> tuple[int, int] | None:
    """返回 ``name:`` 段在 lines 中的 [start, end) 区间（含子键，不含段名行）。

    ⛔ 判据必须同时看缩进和行尾：``qr_provision:`` 与 ``  qr_provision:``
    在gb28181 段内外都可能出现，只认顶格会漏掉嵌套的那个。
    """
    start = None
    indent = 0
    for i, line in enumerate(lines):
        if line.strip() != f"{name}:":
            continue
        if start is None:
            start = i
            indent = len(line) - len(line.lstrip())
            continue
        if len(line) - len(line.lstrip()) == indent:
            return None  # 同缩进第二次出现 = 重复段，交给调用方报错
    if start is None:
        return None
    end = len(lines)
    for i in range(start + 1, len(lines)):
        stripped = lines[i].strip()
        if not stripped or stripped.startswith("#"):
            continue
        if len(lines[i]) - len(lines[i].lstrip()) <= indent:
            end = i
            break
    return start, end


def add_snapshot_upload_section(target: Path, base_url: str = "") -> None:
    """写入「图像抓拍回传基址」段（``gb28181.snapshot_upload.base_url``）。

    GB/T 28181-2022 §9.14：平台下发 SnapShotConfig 时把 UploadURL 交给设备，
    设备把拍好的图 POST 回这个地址。

    ⛔⛔ 默认**留空出包**，首启由 uvp-gb28181-ctl.sh 生成（指向 nginx 明文口）。
       理由与 qr_provision 完全相同：出包机 IP ≠ 客户机 IP。
    ⛔⛔ 而且这个地址**必须明文**：自签 HTTPS 的证书设备不认，TLS 握手直接失败，
       失败发生在握手层、不产生任何访问日志 —— 2026-10-20 客户现场就是
       「设备回了 OK、平台零报错、一张图都没有」。所以默认端口取 nginx 的
       明文口（有访问日志可查），而不是浏览器入口。
    """
    text = target.read_text(encoding="utf-8")
    want = base_url.strip()

    if _has_section(text, "snapshot_upload"):
        text = _rewrite_section(text, "snapshot_upload", {"base_url": f'"{want}"'})
    else:
        text = _append_to_gb28181(text, [
            "",
            "# ---- 图像抓拍回传基址（绿色安装包；本段由打包脚本生成）----",
            "# 设备把拍好的图 POST 回来的地址。设备不认自签证书，公网部署请填明文口。",
            "# 留空 = 未配置：首启由 uvp-gb28181-ctl.sh 按本机地址生成。",
            "snapshot_upload:",
            f'    base_url: "{want}"',
        ])

    _assert_simple_section_value(text, "snapshot_upload", "base_url", want)
    target.write_text(text, encoding="utf-8")
    print(f"   抓拍回传基址: {want or '（留空，首启按本机地址生成）'}（已写入并通过结构校验）")


def _assert_simple_section_value(text: str, section: str, key: str, want: str) -> None:
    """解析后断言 ``gb28181.<section>.<key>`` 等于 want，且该段只出现一次。

    ⛔ 断言的是「等于期望值」而不是「非空」：只判非空时，example 里那份占位段
       就能骗过校验，而包内配置压根没写进去（变异自证抓到的就是这个）。
    """
    hits = sum(1 for line in text.split("\n") if line.strip() == f"{section}:")
    if hits != 1:
        raise SystemExit(f"{section} 段出现了 {hits} 次（应为 1 次）—— "
                         "重复段会让现场改错地方：YAML 只认后者。")
    try:
        import yaml
    except ImportError:
        print("   ⚠️ 未安装 PyYAML，跳过结构校验（pip install pyyaml 可启用）")
        return
    try:
        parsed = yaml.safe_load(text) or {}
    except Exception as exc:
        raise SystemExit(
            f"{section} 写入后配置无法解析（{type(exc).__name__}）—— 多半是插入位置或缩进错了。"
        ) from None
    got = str((parsed.get("gb28181") or {}).get(section, {}).get(key, ""))
    if got != want:
        raise SystemExit(f"gb28181.{section}.{key}={got!r}（应为 {want!r}）")


def _has_section(text: str, name: str) -> bool:
    return _section_lines(text.split("\n"), name) is not None


def _rewrite_section(text: str, name: str, values: dict[str, str]) -> str:
    """改写已存在段里的子键，保持原有缩进与键序。"""
    lines = text.split("\n")
    span = _section_lines(lines, name)
    if span is None:
        raise SystemExit(f"配置里找不到或存在重复的段落 {name}")
    start, end = span
    key_indent = None
    for i in range(start + 1, end):
        m = re.match(r"^(?P<indent>\s*)(?P<key>[A-Za-z_][\w]*)\s*:", lines[i])
        if not m:
            continue
        key_indent = m.group("indent")
        key = m.group("key")
        if key in values:
            lines[i] = f"{key_indent}{key}: {values[key]}"
            values.pop(key)
    missing = set(values)
    if missing:
        raise SystemExit(f"段落 {name} 里找不到键 {sorted(missing)}")
    return "\n".join(lines)


def _append_to_gb28181(text: str, block: list[str]) -> str:
    """把 block 追加到 ``gb28181:`` 段末尾（下一个顶级键之前）。

    ⛔⛔ 插入点必须是段末尾，不能按「遇到第一个 N 空格行就插」。
       踩过一次:gb28181 下**子段**(``trace:``/``media:``)与**直接子键**(``enabled:``)
       缩进完全相同,光看缩进区分不出来 ⇒ 插进了 ``trace:`` 内部,
       yaml 直接报 ``expected <block end>``。

    ⛔⛔ block 的缩进**必须按实际段缩进平移**，不能硬编码 2 空格。
       踩过一次:``config.example.yml`` 用 2 空格而**出包后的 config.yml 是 4 空格**
       (make-config.py 会先重排缩进),硬编码写进去就与同级段不齐 ⇒ yaml ParserError。
       这里的做法:探测段内首个直接子键的缩进,子键缩进 = 段缩进 + 2。
    """
    lines = text.split("\n")
    start = None
    for i, line in enumerate(lines):
        if line.rstrip() == "gb28181:":
            start = i
            break
    if start is None:
        raise SystemExit(
            "配置里找不到 gb28181: 段 —— 无法确定 qr_provision 段的插入位置。"
            "不要靠猜，请检查 config.example.yml 的结构是否变了。"
        )

    # 段末尾 = 下一个**顶格**非注释非空行
    end = len(lines)
    for i in range(start + 1, len(lines)):
        stripped = lines[i].strip()
        if not stripped or stripped.startswith("#"):
            continue
        if not lines[i].startswith((" ", "\t")):
            end = i
            break

    # 从段内任意一个已知子段名反推缩进（比硬编码可靠）
    child_indent = None
    for line in lines[start + 1:end]:
        m = re.match(r"^(?P<indent>[ \t]+)(?P<key>[A-Za-z_][\w]*):\s*$", line)
        if m:
            child_indent = m.group("indent")
            break
    if child_indent is None:
        raise SystemExit(
            "gb28181 段里找不到任何子段，无法确定子键缩进 —— 不要靠猜。"
        )
    key_indent = child_indent + "  "

    shifted = []
    for line in block:
        if line.startswith("    "):
            shifted.append(key_indent + line[4:])
        elif line:
            shifted.append(child_indent + line)
        else:
            shifted.append(line)

    return "\n".join(lines[:end] + shifted + lines[end:])


def _assert_qr_provision_not_duplicated(text: str) -> None:
    """断言文件里 ``qr_provision`` 段**只出现一次**。

    ⛔ 重复键在 YAML 里不报错，只是后者静默覆盖前者 —— 配置"能跑"，
       但两份配置摆在文件里，现场运维改哪一份都说不清 ⇒ 必须拦住。
    """
    hits = 0
    for line in text.split("\n"):
        if line.strip() == "qr_provision:":
            hits += 1
    if hits != 1:
        raise SystemExit(
            f"qr_provision 段出现了 {hits} 次（应为 1 次）—— "
            "YAML 不会报错但后者会静默覆盖前者，现场运维会改错地方。"
        )


def _assert_qr_provision_valid(text: str, host: str, port: str) -> None:
    """解析后断言 qr_provision 段真的在 gb28181 下且取值正确。

    ⛔ 必须断言「值来自生成段」而不是「值非空」：只判非空的话，
       example 里那份 ``base_url: ""`` 的样例段就能骗过校验 ——
       而包内配置其实压根没写进去。
    """
    try:
        import yaml
    except ImportError:
        if "qr_provision" not in text:
            raise SystemExit("qr_provision 段写入失败：结果里找不到该段")
        print("   ⚠️ 未安装 PyYAML，跳过结构校验（pip install pyyaml 可启用）")
        return

    try:
        parsed = yaml.safe_load(text) or {}
    except Exception as exc:
        # ⛔ yaml 解析失败也必须**在这里**变成清晰的失败,不能往上抛 traceback:
        # 出包现场看到的是 yaml 的「第 329 行」,离真正的原因(插错位置)十万八千里。
        # 变异自证时就是踩这个:护栏确实抓到了,但报的是 ParserError 而不是人话。
        raise SystemExit(
            f"qr_provision 写入后配置无法解析（{type(exc).__name__}）—— "
            "多半是插入位置或缩进错了：gb28181 下既有子段(trace:/media:)"
            "也有直接子键(enabled:)，缩进相同，光看缩进区分不出来。"
        ) from None
    gb = parsed.get("gb28181") or {}
    qr = gb.get("qr_provision") or {}
    problems = []
    # 留空时 want 也是空串：断言「等于 want」同时锁住两种契约 ——
    #   传了 host ⇒ 必须写成那个地址；没传 ⇒ 必须是空（不许偷偷写死出包机 IP）。
    want = f"http://{host}:{port}" if host else ""
    if "qr_provision" not in gb:
        problems.append("qr_provision 不在 gb28181 段下（缩进错了）")
    if str(qr.get("base_url", "")) != want:
        problems.append(f"gb28181.qr_provision.base_url={qr.get('base_url')!r}（应为 {want!r}）")
    if problems:
        raise SystemExit("qr_provision 段结构校验失败：\n  - " + "\n  - ".join(problems))


def _detect_openapi_audience() -> str:
    """生成 OpenAPI 的部署环境标识（audience）。

    ⛔⛔ 不同环境**不得共享**同一个 audience：它是签发 token 的受众声明，
    共享后 A 环境签的 token 能到 B 环境用（JWT 的aud 校验形同虚设）。
    ⇒ 每份包生成一个随机值，写进 config.yml；客户想固定可自行改。
    """
    import secrets

    return "uvp-" + secrets.token_hex(8)


def add_openapi_section(target: Path, enabled: bool, audience: str,
                        master_key_id: str) -> None:
    """写入 ``openapi:`` 段：默认开启并填好环境标识与主密钥标识。

    ⛔⛔ 为什么必须在出包时就写好（老板 2026-10-09 定「开箱可用」）：
      后端装配 OpenAPI 有五道前置检查（app/openapi/routes/runtime.go），
      任何一道不过就统一返回 ``ErrUnavailable`` ⇒ 页面报 **503 SERVICE_UNAVAILABLE**，
      且**不告诉你是哪一道**。实测现场缺的就是这里两项：
        ① ``UVP_OPENAPI_MASTER_KEY`` 未注入（主密钥只能走环境变量，不在配置文件里）
        ② ``audience`` 为空
      另外三道（表结构 / write_timeout / 库连接）在绿色包里本来就满足。

    ⛔主密钥**不写进 config.yml**：那是配置文件、随包分发、可被读取；
      它只从环境变量 ``UVP_OPENAPI_MASTER_KEY`` 读（由出包脚本写进 config.env，
      uvp-gb28181-ctl.sh 再 export 给后端进程 —— 与 SIP 报文诊断密钥同一套机制）。
    """
    text = target.read_text(encoding="utf-8")

    # 已存在则**改写**而非追加（追加会出现重复键，YAML 不报错但会静默覆盖）。
    if _has_section(text, "openapi"):
        text = _rewrite_section(text, "openapi", {
            "enabled": "true" if enabled else "false",
            "audience": f'"{audience}"',
            # ⚠️ 只写标识，不写主密钥本身
            "master_key_id": f'"{master_key_id}"',
        })
    else:
        block = [
            "",
            "# ---- OpenAPI（第三方接入；本段由打包脚本生成）----",
            "# 主密钥**不在这里**：它只从环境变量 UVP_OPENAPI_MASTER_KEY 读，",
            "# 出包时已写进包根目录的 config.env。",
            "# ⚠️ audience 是部署环境标识，不同环境不得共享；换环境必须换值。",
            "openapi:",
            f"    enabled: {'true' if enabled else 'false'}",
            f'    audience: "{audience}"',
            f'    master_key_id: "{master_key_id}"',
        ]
        text = _append_top_level(text, block)

    _assert_openapi_valid(text, enabled, audience, master_key_id)
    target.write_text(text, encoding="utf-8")
    print(f"   OpenAPI: enabled={str(enabled).lower()} audience={audience}")


def _assert_openapi_valid(text: str, enabled: bool, audience: str,
                          master_key_id: str) -> None:
    """解析后断言 openapi 段真的生效。

    ⛔ yaml 解析失败也必须在这里变成**清晰**的失败，不能往上抛 traceback：
      出包现场看到的是「第 329 行」这种行号，离真因（缩进/插入位置）十万八千里。
    """
    try:
        import yaml
    except ImportError:
        if "openapi:" not in text:
            raise SystemExit("openapi 段写入失败：结果里找不到该段")
        print("   ⚠️ 未安装 PyYAML，跳过结构校验（pip install pyyaml 可启用）")
        return

    try:
        parsed = yaml.safe_load(text) or {}
    except Exception as exc:
        raise SystemExit(
            f"openapi 写入后配置无法解析（{type(exc).__name__}）—— 多半是插入位置或缩进错了"
        ) from None

    sec = parsed.get("openapi") or {}
    problems = []
    if "openapi" not in parsed:
        problems.append("openapi 不在顶级（缩进错了）")
    # ⛔⛔⛔ 必须查「顶级 openapi 键出现几次」——YAML 里**重复键不报错**，
    #   后者静默覆盖前者，解析结果依然是对的 ⇒ 只看值永远抓不到。
    #   现场含义：配置文件里两份 openapi 段，人改的那份可能被生成的那份盖掉，
    #   而运维看文件时会以为改生效了。（与 qr_provision 同一个教训。）
    if len(re.findall(r"(?m)^openapi:[ \t]*$", text)) != 1:
        problems.append(
            "openapi 顶级键出现了 %d 次（应为 1）—— 重复键 YAML 不报错但会静默覆盖"
            % len(re.findall(r"(?m)^openapi:[ \t]*$", text)))
    if bool(sec.get("enabled")) != enabled:
        problems.append(f"openapi.enabled={sec.get('enabled')!r}（应为 {enabled}）")
    if str(sec.get("audience") or "") != audience:
        problems.append(f"openapi.audience={sec.get('audience')!r}（应为 {audience!r}）")
    if str(sec.get("master_key_id") or "") != master_key_id:
        problems.append(
            f"openapi.master_key_id={sec.get('master_key_id')!r}（应为 {master_key_id!r}）")
    # 主密钥绝不能落进配置文件
    if "UVP_OPENAPI_MASTER_KEY=" in text:
        problems.append("主密钥被写进了 config.yml —— 必须只走环境变量")
    if problems:
        raise SystemExit("openapi 段结构校验失败：\n  - " + "\n  - ".join(problems))


def _append_top_level(text: str, block: list[str]) -> str:
    """把 block 追加到**顶级**（列首，无缩进）区段的最末尾。

    ⛔ 为什么不用「插到文件开头」：配置顶级段的顺序不影响解析，
    但插在开头会让「出包脚本生成的内容」与人工维护的内容混在最前面，
    现场改配置时容易漏看。追加到末尾更符合直觉。

    ⛔⛔ block 里的子键缩进**必须按文件实际顶级段缩进平移**，不能硬编码。
       踩过一次：``config.example.yml`` 用 2 空格而**出包后的 config.yml 是 4 空格**
       ⇒ 硬编码写进去与同级段不齐 ⇒ yaml ParserError。
       做法：探测已存在的某个顶级段的首个子键缩进，照它写。
    """
    lines = text.split("\n")
    child_indent = "    "
    for i, line in enumerate(lines):
        if not line.strip() or line.startswith((" ", "\t")) or line.startswith("#"):
            continue
        # 找到一个有子键的顶级段，用它的子键缩进
        for j in range(i + 1, len(lines)):
            nxt = lines[j]
            if not nxt.strip() or nxt.startswith("#"):
                continue
            if nxt.startswith((" ", "\t")):
                child_indent = nxt[: len(nxt) - len(nxt.lstrip())]
                break
            break
        break

    out = lines[:]
    while out and not out[-1].strip():
        out.pop()
    out.append("")
    for b in block:
        # 只平移 block 里以4 空格开头的子键行，顶级键（openapi:）与注释不动
        if b.startswith("    ") and not b.startswith("        "):
            out.append(child_indent + b.lstrip())
        else:
            out.append(b)
    return "\n".join(out) + "\n"


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", required=True, type=Path, help="仓库里的 config.yml")
    parser.add_argument("--target", required=True, type=Path, help="输出到包内的路径")
    parser.add_argument("--http-port", default="8280")
    parser.add_argument("--redis-port", default="6379")
    parser.add_argument("--db-path", default="./data/uvp.db",
                        help="SQLite 文件路径（相对应用根目录；绝对路径也行）")
    parser.add_argument("--log-path", default="./logs/app/uvp-gb28181.log",
                        help="后端业务日志路径（相对应用根目录）；绿色包统一放 logs/app/ 下")
    parser.add_argument("--zlm-secret", default="",
                        help="ZLM API secret（建议固定值；留空则保留示例里的占位符）")
    parser.add_argument("--zlm-media-server-id", default="",
                        help="ZLM 节点标识 mediaserverid（建议固定 UUID；留空=每次 seed 随机）")
    parser.add_argument("--zlm-api-port", default="",
                        help="ZLM HTTP API 端口（写入 gb28181.zlm.httpport；留空=保留模板值）")
    parser.add_argument("--hook-port", default="",
                        help="hook 回调端口（写入 gb28181.media.hookport；留空=保留模板值）")
    parser.add_argument(
        "--jwt-sign-key",
        default=os.environ.get("UVP_JWT_SIGN_KEY", ""),
        help="JWT 签名密钥（≥32 位随机串）；留空=占位符时自动生成。默认读 UVP_JWT_SIGN_KEY",
    )
    parser.add_argument("--qr-provision-host", default="",
                        help="扫码接入基址里的设备可达地址；留空=出包留空，由客户机首启自动生成")
    parser.add_argument("--snapshot-upload-base-url", default="",
                        help="抓拍回传基址；留空=出包留空，由客户机首启自动生成（nginx 明文口）")
    parser.add_argument("--openapi-audience", default="",
                        help="OpenAPI 部署环境标识（不同环境不得共享）")
    parser.add_argument("--openapi-master-key-id", default="uvp-openapi-k1",
                        help="OpenAPI 主密钥标识；须与包内 config.env 里的主密钥配套")
    parser.add_argument("--openapi-enabled", default="true",
                        choices=("true", "false"),
                        help="是否默认开启 OpenAPI（老板 2026-10-09 定：默认开）")
    parser.add_argument(
        "--upgrade-trusted-keys",
        default=os.environ.get("UVP_UPGRADE_TRUSTED_KEYS", ""),
        help="发行公钥 JSON 映射（key_id 到 Base64/hex 公钥），默认读取 UVP_UPGRADE_TRUSTED_KEYS",
    )
    args = parser.parse_args()

    build(args.source, args.target, args.http_port, args.redis_port,
          args.zlm_secret, args.zlm_media_server_id,
          args.upgrade_trusted_keys,
          zlm_api_port=args.zlm_api_port, hook_port=args.hook_port,
          jwt_sign_key=args.jwt_sign_key)
    add_sqlite_section(args.target, args.db_path)
    set_log_filepath(args.target, args.log_path)
    add_qr_provision_section(args.target, args.qr_provision_host, args.http_port)
    add_snapshot_upload_section(args.target, args.snapshot_upload_base_url)
    add_openapi_section(args.target, args.openapi_enabled == "true",
                        args.openapi_audience or _detect_openapi_audience(),
                        args.openapi_master_key_id)


if __name__ == "__main__":
    main()
