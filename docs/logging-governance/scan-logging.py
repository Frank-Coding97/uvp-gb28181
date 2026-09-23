#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""日志治理 —— 内容质量扫描（验收工具）

用法：
    cd server && python3 ../docs/logging-governance/scan-logging.py
    python3 docs/logging-governance/scan-logging.py --root server
    # 逐模块核对（C04 的验收动作）：
    cd server && python3 ../docs/logging-governance/scan-logging.py --filter play --limit 0

输出指标（对应 README「实测基线」与各 C 项的完成判据）：
    1. 无任何定位字段的调用点占比（目标 < 10%，进程级事件豁免）
    2. 等级分布（Warn 目标 < 10%）
    3. 字段命名风格（目标只剩 snake_case）
    4. 唯一 event 数 / 唯一字段名数
    5. 按命名空间拆解

实现说明（踩过的坑，别改回去）：
  * 必须先做「字符串感知」的词法处理：朴素的 `//[^\\n]*` 正则会吃掉字符串里的 `//`
    （如 URL），导致括号平衡算错、结论荒谬（曾误报 99% 无定位）。
  * 字段名必须**归一化后**比对（`lower()` 去掉 `_`）：仓库里 `stream_id` / `streamId` /
    `streamID` 三种写法并存。早期漏认驼峰大写 ID，把 play 误判为 95% 无定位（实为 50%）。
  * `zap.Field` 变量攒好后 `append(...)...` 展开的字段静态看不见（门禁的
    `unresolved_logger` 报的就是同一件事）。本脚本对文件名下的变量做保守合并。
    这个盲区与门禁的盲区**是同一处**，故保留在输出里作为提示。
  * 字段由**返回 `[]zap.Field` 的 helper** 组装的调用点也看不见：正则会把
    `append(stopLogFields(...), zap.String("event", ...))` 的第一个标识符当成变量名
    （实际是函数名），于是展开不出来、被计入"无定位"。例如
    `play.Service.Stop` 的 `gb28181.play.stop_requested`（运行时**确实**带 device_id）。
    这类点的真实覆盖靠单测（`stopLogFields` / `TestStopEventsCarryDeviceAndChannel`），
    不靠本脚本 —— **别为了指标好看去放宽这里的判定**，那会把"条件省略"也算成"已达标"。
"""

import argparse
import os
import re
import sys
from collections import Counter, defaultdict

LEVELS = ("Debug", "Info", "Warn", "Error", "DPanic", "Panic", "Fatal")

# 定位字段基线（归一化为小写去下划线后比对）
IDENT_BASE = {
    "deviceid", "devicecode", "channelid", "channelcode",
    "callid", "platformid", "streamid", "nodeid",
    "serverid", "mediaserverid", "requestid", "operationid",
    "jobid", "workorderid", "orderid", "sn",
    "endpoint", "route", "userid", "subscriptionid",
    "traceid", "correlationid",
    # peer = 本次报文实际观测到的对端身份（`host:port/transport from=用户`）。
    # 级联是纯 SIP 链路，"谁在跟我说话"就是它，与 endpoint 同属"对端地址"这一维度；
    # 尤其当**认定平台失败**时（多个候选 / 认不出来 / 配置里没这台），
    # 它是唯一的身份线索 —— `cascade.video.failed` / `cascade.catalog.query_failed`。
    # ⚠️ 这是"把真实存在的定位字段登记进基线"，不是放宽判定：全仓仅 3 个调用点，
    # 且都确实用它定位对端。别往里加"看起来像定位"的字段。
    "peer",
    # source_ip = ZLM Hook 回调的对端地址。hook 链路没有 SIP call_id、也没有 HTTP
    # request id 可以继承（它是 ZLM 主动打过来的），"谁在跟我说话"只能由它回答：
    # 认证拒绝（`gb28181.hook.auth.rejected`，对方还没通过认证所以没有 node_id 可信）、
    # 心跳载荷读不出（`keepalive.read_failed`）都靠它。与 `peer` 同属"对端地址"维度。
    "sourceip",
    # ---- C08（2026-09-15）新增：**非设备类**定位维度 ----------------------------
    # 到此为止基线全是"设备/通道/流/平台/节点/请求"这一族。C08 处理平台支撑域
    # （`auth` `casbin` `sysmenu` `sysrole` `codegen` `sysaffix`）时，定位对象换了：
    # 不再是"哪一路",而是"谁 / 哪个角色 / 哪个模板 / 哪个文件"。
    #
    # ⚠️ 收录标准**没有放宽**,仍是那一句：**这个字段的值能不能唯一指向
    # "出事时第一个要查的那个对象"**。下面每一条都配了实际补字段的调用点,
    # 不是"看起来像定位"就收 —— 反例：`phase`（哪一步）/ `status`（什么状态）/
    # `dialect`（什么方言）都是**状态性**字段，答不出"谁"，一律不收。
    #
    # username = 登录失败时的"谁"。没有 user_id 可用（认证还没过，拿不到），
    #            所以用户名是唯一可得的身份线索（`auth.login_audit.persist_failed`）。
    "username",
    # roleid / parentroleid = 角色授权的主体与它要继承的父角色。
    # `casbin.role_inheritance.*_rejected` 就是"你传的这对角色不合法"，
    # 不打出来等于没说清是谁的配置错了。
    "roleid", "parentroleid",
    # menuid = 菜单资产。`sysmenu.menu_api_*_failed` 报的就是"这个菜单的 API 关联写坏了"。
    "menuid",
    # templatename / templatepath / filepath = 代码生成器涉及的**具体资产**
    # （`codegen.template_read_failed` / `codegen.file_skipped` / `sysaffix.*` 三条）。
    # ⚠️ 只收 `file_path` 这种**明确的文件路径**语义，不收泛化的 `path`
    # （`models.area.load_failed` 的 `path` 是组件级豁免，见 contracts/platform-support.md）。
    "templatename", "templatepath", "filepath",
    # operatorid = **谁做的这次操作**。平台支撑域的写操作（改 SIP 配置、强退观看连接）
    # 出错时，"是哪个管理员动的"就是排障第一问（`setup.config_saved` / `gb28181.traffic.viewer_kicked`）。
    "operatorid",
    # uploadid = 分片上传会话（`sysaffix.chunk_upload_cancel_failed`）。
    "uploadid",
    # tablename = 代码生成器要处理的**目标表**（`sysgenservice.table_comment_read_failed`）。
    # 收 `table_name` 而不是 `table`：后者太泛，容易被别处的"表格/数据表"含义借道变假达标。
    "tablename",
}

# ---- 命名空间三分组（C08，2026-09-15）------------------------------------------
# 旧版只有一个 BUSINESS_NS 白名单，命中不了的**一律进「其他」桶**，于是 97 条
# 合法命名空间（`scheduler.*` `models.*` `casbin.*` `auth.*` …）全被算成"未归类"，
# 「其他」桶 71% 看着像"命名混乱"，实际大半只是**白名单没列**。
#
# 拆成三组之后，「其他」= **真的没归类**（应趋 0），而它归零靠的是"分类完整"，
# 不是"放宽判定" —— `has_loc`（无定位判定）与命名空间分组**完全无关**。
#
# ⚠️ 这个拆分**不改任何「无定位」判定**。它唯一的后果是让归属统计变得正确 ——
# 补分组前 `scheduler.demo.started`（带 job_id）就已经是"有定位"，只是被错算进「其他」。
#
# ⚠️ 但补分组**会掩盖事件名质量问题**（`sysaffix.upload.warn` 归到 sysaffix 后不再显眼）
# → 所以另设独立指标 ⑤ 事件名质量，两者必须一起看。

# 业务域：排障对象是"哪一路"（设备 / 通道 / 流 / 平台 / 节点）
BUSINESS_NS = ("gb28181", "cascade", "play", "ptz", "zlm",
               "recording", "recordingplan", "talk", "subscribe", "push", "security")

# 平台支撑域：排障对象是"哪个请求 / 谁 / 哪个任务 / 哪份资产"
# ⚠️ 不再列 `casbinservice` / `sysaffixservice` / `device_ptz_resources` / `device_traffic` ——
# C08 已把它们归并到同域的标准命名空间（`casbin.` / `sysaffix.` / `ptz.` / `gb28181.traffic.`），
# 因为**同一个文件里两套命名**（`device_ptz_resources.*` 与 `ptz.*` 并存）比"少列一个白名单"更坏。
PLATFORM_NS = ("scheduler", "models", "casbin", "auth", "audit", "db", "codegen",
               "http", "setup", "realtime_log", "streammonitor", "sip",
               "sysaffix", "sysmenu", "sysrole", "sysgenservice", "sysmenuservice",
               "websocket", "mcp")

# 进程 / 框架域：排障对象是"哪个进程 / 哪份配置 / 框架自身"
RUNTIME_NS = ("lifecycle", "migration", "logging", "startup", "legacy",
              "config", "plugin", "gin")

GROUP_ORDER = (("业务域", BUSINESS_NS),
               ("平台支撑域", PLATFORM_NS),
               ("进程框架域", RUNTIME_NS))

# 事件名质量（独立指标⑤）：末段复述等级 —— `sysaffix.upload.warn` 里 `.warn` 说的是
# 等级、不是"发生了什么"，于是按事件名检索时区分不出 `upload.warn` 与 `delete.warn`
# 之外的语义，而且改等级时事件名就跟着撒。
# ⚠️ **`panic` 不在这个集合里，是故意的**：`http.panic` / `play.reconcile.panic` 里的
# `.panic` 是**事件语义**（进程崩了），不是等级复述 —— 把它算进来会误报 3 条。
# 判定一个末段词是不是"等级复述"，看它**换掉之后事件还说不说得清发生了什么**。
EVENT_LEVEL_ECHO_RE = re.compile(r"\.(info|warn|error|debug|fatal)$")
# 疑似"函数名横拼"：某个段没有下划线且很长（`writehomepositionfailure` 24 字符）——
# 只做**提示**不做判定，因为英文单词本身也可能长且无下划线。
PLAIN_SEG_RE = re.compile(r"^[a-z][a-z0-9]*$")

LVL_RE = re.compile(r"\.(%s)\(" % "|".join(LEVELS))
FIELD_RE = re.compile(
    r'zap\.(?:String|Int|Int64|Uint|Uint64|Bool|Float64|Duration|Time|Any|Stringer|Error)\("([A-Za-z_][A-Za-z0-9_]*)"'
)
EVENT_RE = re.compile(r'"event"\s*,\s*"([^"]+)"')
# 事件名常量化（`zap.String("event", cascadeVideoEventFailed)`）——C01「事件唯一登记处」
# 的方向就是这个。不读常量，脚本会把这类调用点整条判成 "(无 event)" 并踢出业务命名空间，
# 于是**指标随治理推进而假跌**。这里只解析同文件的 const 块，不做跨文件推导。
CONST_EVENT_RE = re.compile(r'"event"\s*,\s*([A-Za-z_][A-Za-z0-9_]*)\s*[,)]')
CONST_BLOCK_RE = re.compile(r"\bconst\s*\(([^()]*)\)", re.S)
CONST_LINE_RE = re.compile(
    r"^\s*([A-Za-z_][A-Za-z0-9_]*)\s*(?:[A-Za-z_][\w.\[\]]*)?\s*=\s*\"([^\"]*)\"\s*(?://.*)?$", re.M
)
CONST_SINGLE_RE = re.compile(r"^\s*const\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*\"([^\"]*)\"", re.M)
VAR_DEF_RE = re.compile(r"([A-Za-z_][A-Za-z0-9_]*)\s*:?=\s*(?:make\(\[\]zap\.Field|\[\]zap\.Field\{)")
APPEND_RE = re.compile(r"append\(\s*([A-Za-z_][A-Za-z0-9_]*)")
BARE_RE = re.compile(r",\s*([A-Za-z_][A-Za-z0-9_]*)\.{3}\s*\)")

SKIP_DIRS = {"node_modules", ".git", "vendor", "third_party"}
# 隐藏目录一律跳过。仓库约定是「跨分支做事另建 worktree」，而 worktree 默认落在
# `.claude/worktrees/<name>/` 下——那是**整份 server/ 的拷贝**，扫进来会让所有指标
# 成倍虚高（实测 486 vs 351 调用点）。IDE 缓存目录同理。
SKIP_HIDDEN_DIRS = True
SKIP_PATH_PARTS = ("/loggingacceptance/", "/loggingcontract/")


def strip_comments(src):
    """字符串感知地清空注释；保持长度与偏移不变。"""
    out = list(src)
    n = len(src)
    i = 0
    while i < n:
        c = src[i]
        if c in '"`':
            q = c
            i += 1
            while i < n:
                if q == '"' and src[i] == "\\":
                    i += 2
                    continue
                if src[i] == q:
                    i += 1
                    break
                i += 1
        elif c == "'":
            i += 1
            while i < n:
                if src[i] == "\\":
                    i += 2
                    continue
                if src[i] == "'":
                    i += 1
                    break
                i += 1
        elif c == "/" and i + 1 < n and src[i + 1] == "/":
            j = i
            while j < n and src[j] != "\n":
                j += 1
            for k in range(i, j):
                out[k] = " "
            i = j
        elif c == "/" and i + 1 < n and src[i + 1] == "*":
            j = i + 2
            while j + 1 < n and not (src[j] == "*" and src[j + 1] == "/"):
                j += 1
            j = min(j + 2, n)
            for k in range(i, j):
                if src[k] != "\n":
                    out[k] = " "
            i = j
        else:
            i += 1
    return "".join(out)


def calls_in(src):
    """切出每个 logger 调用段，括号平衡 + 字符串感知。"""
    out = []
    for m in LVL_RE.finditer(src):
        i = m.end() - 1
        depth, j, instr, esc, inraw = 0, i, False, False, False
        while j < len(src):
            c = src[j]
            if instr:
                if esc:
                    esc = False
                elif c == "\\":
                    esc = True
                elif c == '"':
                    instr = False
            elif inraw:
                if c == "`":
                    inraw = False
            else:
                if c == '"':
                    instr = True
                elif c == "`":
                    inraw = True
                elif c == "(":
                    depth += 1
                elif c == ")":
                    depth -= 1
                    if depth == 0:
                        break
            j += 1
        seg = src[i:j + 1]
        if "zap." in seg or '"event"' in seg or "append(" in seg:
            out.append((m.group(1), seg, src[:m.start()].count("\n") + 1))
    return out


def const_strings(src):
    """同文件内 `const` 块 / 单行 const 的 `名 = "字面量"` 映射。

    给"事件名常量化"用（见 CONST_EVENT_RE）。只做文件内解析：跨文件推导会
    把准确性建立在猜上，不如让调用点自己可读。
    """
    out = {}
    for block in CONST_BLOCK_RE.finditer(src):
        for name, value in CONST_LINE_RE.findall(block.group(1)):
            out.setdefault(name, value)
    for name, value in CONST_SINGLE_RE.findall(src):
        out.setdefault(name, value)
    return out


def event_name(seg, consts):
    """取这条 logger 调用点的 event 值：优先字面量，其次同文件常量。"""
    literal = EVENT_RE.search(seg)
    if literal:
        return literal.group(1)
    reference = CONST_EVENT_RE.search(seg)
    if reference:
        return consts.get(reference.group(1))
    return None


def norm(key):
    return key.lower().replace("_", "")


def classify_style(name):
    if name.endswith("ID") and not name.endswith("_ID"):
        return "驼峰+大写ID (streamID)"
    if re.search(r"[a-z][A-Z]", name):
        return "camelCase (deviceId)"
    if "_" in name:
        return "snake_case (device_id)"
    return "单段小写 (event)"


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--root", default=".", help="代码根目录（默认当前目录；server 下运行则填 .）")
    ap.add_argument("--filter", default="", help="只看明细里的匹配行（路径或 event 子串，大小写敏感）")
    ap.add_argument("--limit", type=int, default=40, help="明细打印条数上限（默认 40；0 = 全部）")
    ap.add_argument("--dump-ns", default="",
                    help="列出该命名空间的**全部**调用点及字段（含带定位的），用于逐条判定；不改任何判定")
    args = ap.parse_args()

    root_dir = os.path.abspath(args.root)
    total = 0
    level_cnt = Counter()
    style_cnt = Counter()
    no_ident = []
    # event 名 → 是否存在"带定位字段"的调用点（用于明细行标注条件字段）
    event_has_loc = {}
    var_used = 0
    events = Counter()
    fields = Counter()
    by_ns = defaultdict(lambda: {"n": 0, "noid": 0, "warn": 0})
    # 逐命名空间的调用点明细（--dump-ns 用）：C04/C07 的"逐条判定"靠它，
    # 只看"无定位"汇总会漏掉"有定位但定位错了维度"的欠账。
    ns_rows = defaultdict(list)
    level_echo = []   # ⑤ 末段复述等级
    plain_seg = []    # ⑤ 疑似函数名横拼（仅提示）

    for root, dirs, files in os.walk(root_dir):
        dirs[:] = [
            d
            for d in dirs
            if d not in SKIP_DIRS and not (SKIP_HIDDEN_DIRS and d.startswith("."))
        ]
        for f in sorted(files):
            if not f.endswith(".go") or f.endswith("_test.go"):
                continue
            path = os.path.join(root, f)
            rel = os.path.relpath(path, root_dir)
            if any(p in path.replace(os.sep, "/") for p in SKIP_PATH_PARTS):
                continue
            try:
                src = strip_comments(open(path, encoding="utf-8", errors="replace").read())
            except OSError:
                continue

            all_fields = set(FIELD_RE.findall(src))
            var_fields = {m.group(1): all_fields for m in VAR_DEF_RE.finditer(src)}
            file_consts = const_strings(src)

            for level, seg, line in calls_in(src):
                total += 1
                level_cnt[level] += 1
                names = set(FIELD_RE.findall(seg))
                used = False
                for m in APPEND_RE.finditer(seg):
                    if m.group(1) in var_fields:
                        names |= var_fields[m.group(1)]
                        used = True
                for m in BARE_RE.finditer(seg):
                    if m.group(1) in var_fields:
                        names |= var_fields[m.group(1)]
                        used = True
                if used:
                    var_used += 1

                for n in names:
                    fields[n] += 1
                    style_cnt[classify_style(n)] += 1

                evn = event_name(seg, file_consts)
                if evn:
                    events[evn] += 1
                    if EVENT_LEVEL_ECHO_RE.search(evn):
                        level_echo.append((rel, line, evn))
                    else:
                        for part in evn.split(".")[1:]:
                            if len(part) >= 16 and PLAIN_SEG_RE.match(part):
                                plain_seg.append((rel, line, evn))
                                break
                ns = evn.split(".")[0] if evn else None
                if not evn:
                    key = "(无 event)"
                elif ns in BUSINESS_NS or ns in PLATFORM_NS or ns in RUNTIME_NS:
                    key = ns
                else:
                    key = "其他"
                by_ns[key]["n"] += 1
                if level == "Warn":
                    by_ns[key]["warn"] += 1

                has_loc = bool({norm(n) for n in names} & IDENT_BASE)
                ns_rows[key].append((rel, line, level, evn or "(无 event)", sorted(names), has_loc))
                if evn:
                    # 「同 event 有没有定位字段」按**所有调用点取并集**：同一事件在
                    # if/else 两支里带不同字段（有 stream 时带 stream_id、没有则**缺席**）
                    # 是正常设计，只取一支会把这种"条件字段"误报成欠账。
                    # ⚠️ 这只用于**明细行打标**，不改「无定位」判定 —— 判定仍逐调用点，
                    # 否则会出现"某 event 在 A 处带了 device_id，B 处没带，并集判成有定位"
                    # 的假绿（B 处那条依然是排障盲区）。
                    event_has_loc[evn] = event_has_loc.get(evn, False) or has_loc
                if not has_loc:
                    no_ident.append((rel, line, level, evn or "(无 event)"))
                    by_ns[key]["noid"] += 1

    if total == 0:
        print(f"未找到日志调用点，检查 --root（当前 {root_dir}）", file=sys.stderr)
        return 1

    pct = lambda a, b: (a * 100 // b) if b else 0
    print("=" * 68)
    print(f"扫描根目录: {root_dir}")
    print(f"日志调用点: {total}   （其中经 zap.Field 变量展开: {var_used}，静态不可见）")
    print("=" * 68)
    print()
    print(f"① 无任何定位字段 : {len(no_ident)} / {total} = {pct(len(no_ident), total)}%     目标 < 10%")
    print(f"② Warn 占比      : {level_cnt['Warn']} / {total} = {pct(level_cnt['Warn'], total)}%     目标 < 10%")
    print(f"③ 唯一 event 值  : {len(events)}")
    print(f"④ 唯一字段名     : {len(fields)}")
    print()
    print("等级分布:", dict(level_cnt))
    print()
    print("字段命名风格（出现次数）—— 目标只剩 snake_case：")
    for k, v in style_cnt.most_common():
        print(f"    {v:5d}  {k}")
    print()
    print("⑤ 事件名末段复述等级 : %d 个调用点 %s目标 0  ← 「.warn/.error 说的是等级不是事件」"
          % (len(level_echo), "" if not level_echo else "（明细见下）"))
    if plain_seg:
        print("   疑似函数名横拼（仅提示，不判定）: %d 个" % len(plain_seg))
    print()

    print("命名空间归属（三分组）——「其他／未归类」应趋 0：")
    print()
    def print_group(title, keys):
        rows = [k for k in keys if k in by_ns]
        if not rows:
            return
        print(f"  【{title}】")
        print(f"    {'命名空间':<12}{'总数':>6}{'无定位':>8}{'占比':>7}{'Warn':>7}{'Warn占比':>9}")
        sub = {"n": 0, "noid": 0, "warn": 0}
        for k in sorted(rows, key=lambda x: -by_ns[x]["n"]):
            r = by_ns[k]
            for kk in sub:
                sub[kk] += r[kk]
            print(f"    {k:<12}{r['n']:>6}{r['noid']:>8}{pct(r['noid'], r['n']):>6}%{r['warn']:>7}{pct(r['warn'], r['n']):>8}%")
        print(f"    {'小计':<12}{sub['n']:>6}{sub['noid']:>8}{pct(sub['noid'], sub['n']):>6}%{sub['warn']:>7}{pct(sub['warn'], sub['n']):>8}%")
        print()

    for title, keys in GROUP_ORDER:
        print_group(title, keys)

    rest = [k for k in by_ns if k not in ("其他", "(无 event)") and k not in set(
        x for _, ks in GROUP_ORDER for x in ks)]
    print("  【未归类】")
    print(f"    {'命名空间':<12}{'总数':>6}{'无定位':>8}{'占比':>7}{'Warn':>7}{'Warn占比':>9}")
    for k in sorted(["其他", "(无 event)"] + rest, key=lambda x: -by_ns[x]["n"]):
        r = by_ns[k]
        print(f"    {k:<12}{r['n']:>6}{r['noid']:>8}{pct(r['noid'], r['n']):>6}%{r['warn']:>7}{pct(r['warn'], r['n']):>8}%")
    print()
    if rest:
        print("    ⚠️ 上面未列进三分组的命名空间: %s" % " ".join(sorted(rest)))
        print()
    if args.dump_ns:
        targets = [args.dump_ns]
        for _title, _keys in GROUP_ORDER:
            if args.dump_ns == _title:
                targets = list(_keys)
        rows = [r for t in targets for r in ns_rows.get(t, [])]
        print(f"命名空间 {args.dump_ns!r} 的全部调用点（{len(rows)} 条）：")
        for rel, line, level, evn, names, has_loc in sorted(rows, key=lambda r: (r[0], r[1])):
            flag = "有定位" if has_loc else "无定位"
            print(f"    [{flag}] {level:<6}{rel}:{line}")
            print(f"           event={evn}")
            print(f"           fields={names if names else '[]'}")
        print()
    shown = no_ident
    if args.filter:
        shown = [r for r in no_ident if args.filter in r[0] or args.filter in (r[3] or "")]
    limit = len(shown) if args.limit <= 0 else args.limit
    scope = f"（过滤 {args.filter!r}）" if args.filter else ""
    print(f"无定位字段明细{scope}（共 {len(shown)} 条，显示前 {min(limit, len(shown))}）：")
    conditional = 0
    for rel, line, level, evn in shown[:limit]:
        # `[条件字段]` = 这条调用点自身没带定位字段，但**同 event 在别的调用点带了**
        # （典型是 if/else 两支：有值时带、取不到时让字段缺席）。
        # 标记只是提示"别看单条就下结论"，判定结果不变。
        mark = ""
        if evn != "(无 event)" and event_has_loc.get(evn):
            mark = "   ← [条件字段] 同 event 的其他调用点带了定位字段"
            conditional += 1
        print(f"    {level:<6}{rel}:{line}  {evn}{mark}")
    if conditional:
        print(f"    （其中 {conditional} 条是[条件字段]：字段在另一支才出现，读两条的并集才是全貌）")
    if len(shown) > limit:
        print(f"    …… 其余 {len(shown) - limit} 条略（--limit 0 显示全部）")
    return 0


if __name__ == "__main__":
    sys.exit(main())
