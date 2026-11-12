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
        # 解析失败要在这里转换成明确结论，避免把 YAML 的行号 traceback
        # 直接暴露给打包现场。
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


def _section_lines(lines: list[str], name: str) -> tuple[int, int] | None:
    """返回 ``name:`` 段在 lines 中的 [start, end) 区间（含子键，不含段名行）。

    判据同时看缩进和行尾，因此可识别顶级段和嵌套段，并拒绝同层重复段。
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
    #   而运维看文件时会以为改生效了。
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
    add_openapi_section(args.target, args.openapi_enabled == "true",
                        args.openapi_audience or _detect_openapi_audience(),
                        args.openapi_master_key_id)


if __name__ == "__main__":
    main()
