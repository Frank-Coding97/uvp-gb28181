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
"""

from __future__ import annotations

import argparse
import re
from pathlib import Path

# 这些开关必须全部置 0（除目标库那个）
INIT_SWITCHES = (
    "isinitglobalgormmysql",
    "isinitglobalgormpostgresql",
    "isinitglobalgormsqlserver",
    "isinitglobalgormsqlite",
)


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


def build(source: Path, target: Path, http_port: str, redis_port: str,
          zlm_secret: str = "", zlm_media_server_id: str = "") -> None:
    text = source.read_text(encoding="utf-8")

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

    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(text, encoding="utf-8")
    print(f"✅ 已生成 {target}")
    print(f"   数据库 sqlite / 缓存 redis(127.0.0.1:{redis_port}) / HTTP :{http_port}")
    if zlm_secret and zlm_media_server_id:
        print(f"   ZLM secret / 节点标识已固定（各 {len(zlm_secret)}/{len(zlm_media_server_id)} 字符）")


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


def add_qr_provision_section(target: Path, host: str, port: str) -> None:
    """写入「扫码接入基址」段（``gb28181.qr_provision``）。

    ⛔⛔ 为什么必须在**出包时**就写好，而不能让前端自己推断：
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
    want = f"http://{host}:{port}"

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
    print(f"   扫码接入基址: {want}（已写入并通过结构校验）")


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
    want = f"http://{host}:{port}"
    if "qr_provision" not in gb:
        problems.append("qr_provision 不在 gb28181 段下（缩进错了）")
    if str(qr.get("base_url", "")) != want:
        problems.append(f"gb28181.qr_provision.base_url={qr.get('base_url')!r}（应为 {want!r}）")
    if problems:
        raise SystemExit("qr_provision 段结构校验失败：\n  - " + "\n  - ".join(problems))


def _detect_lan_ip() -> str:
    """探测出包机的局域网 IPv4。

    ⛔⛔ 出包时探测 ≠ 客户机上的地址。绿色包在客户机上跑时这个值可能不对,
    所以它**只是兜底**:现场可改 config.yml 的 ``gb28181.qr_provision.base_url``。
    写成127.0.0.1 会让二维码指向客户机自己 ⇒ 手机必然连不上,
    因此这里明确拒绝回环地址。
    """
    import socket

    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    try:
        # 不发包,只是让内核按路由表选出「出去」的源地址
        sock.connect(("192.0.2.1", 9))     # TEST-NET-1,保证不会真发出去
        ip = sock.getsockname()[0]
    except OSError:
        ip = ""
    finally:
        sock.close()
    if not ip or ip.startswith("127."):
        # 连路由都探测不到时给一个占位值,由出包断言/现场配置兜住
        return "127.0.0.1"
    return ip


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", required=True, type=Path, help="仓库里的 config.yml")
    parser.add_argument("--target", required=True, type=Path, help="输出到包内的路径")
    parser.add_argument("--http-port", default="8280")
    parser.add_argument("--redis-port", default="6379")
    parser.add_argument("--db-path", default="./data/uvp.db",
                        help="SQLite 文件路径（相对应用根目录；绝对路径也行）")
    parser.add_argument("--zlm-secret", default="",
                        help="ZLM API secret（建议固定值；留空则保留示例里的占位符）")
    parser.add_argument("--zlm-media-server-id", default="",
                        help="ZLM 节点标识 mediaserverid（建议固定 UUID；留空=每次 seed 随机）")
    parser.add_argument("--qr-provision-host", default="",
                        help="扫码接入基址里的设备可达地址；留空则取本机主 IP")
    args = parser.parse_args()

    build(args.source, args.target, args.http_port, args.redis_port,
          args.zlm_secret, args.zlm_media_server_id)
    add_sqlite_section(args.target, args.db_path)
    add_qr_provision_section(args.target, args.qr_provision_host or _detect_lan_ip(),
                             args.http_port)


if __name__ == "__main__":
    main()