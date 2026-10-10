import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

// ============================================================================
// 契约：deploy/standalone 下所有 shell 脚本里**内嵌的 Python 代码**必须兼容
//       Python 3.5 —— 因为目标机可能是 Ubuntu 16.04（自带 python3 = 3.5）。
//
// 真实事故（2026-10-10，客户现场）：Ubuntu 16 上跑 `./uvp-ctl.sh start`，在
//   「正在初始化数据库（111 张表）」之后直接抛
//     File "<stdin>", line 17
//       raise SystemExit(f"建库失败: {type(exc).__name__}: {exc}")
//     SyntaxError: invalid syntax
//   根因是那句 f-string（f-string 是 **Python 3.6+** 语法）。脚本把 Python 代码用
//   heredoc 喂给 `python3 -`，而 Python **整段一次性编译** ⇒ 只要有一句语法不认识，
//   就整个 heredoc 编译失败、**一行都不执行**；报错行指向那句语法，与"建库失败"
//   这个字面完全无关 —— 现场根本联想不到是解释器太老。
//
// 所以这里的判据不是"看起来像不像 f-string"（正则极易漏/误判），而是：
//   把每个 heredoc 抠出来交给 CPython 解析，再遍历 AST —— f-string 会形成
//   `JoinedStr` 节点，变量注解是 `AnnAssign`，海象是 `NamedExpr` …… 节点类型
//   才是真正能命中的判据。
//   ⛔ 别改用 `ast.parse(..., feature_version=(3, 5))`：实测 CPython 的 parser
//     只对少数特性做版本降级，**f-string 根本不报错**，会漏检（本项目踩过）。
// ============================================================================

const DEPLOY_DIR = fileURLToPath(new URL('../deploy/standalone/', import.meta.url));

// 交付到客户机的脚本（必须 3.5 兼容）；构建脚本虽只在构建机上跑，但同属一个
// deploy 目录、同一类坑，一并纳入，避免"以后换台老机器构建又炸"。
function scriptFiles() {
  return readdirSync(DEPLOY_DIR)
    .filter((name) => name.endsWith('.sh'))
    .sort()
    .map((name) => join(DEPLOY_DIR, name));
}

// 交给 CPython 做「抠 heredoc + AST 判版本」的探针。
// ⛔ 探针自身必须只用 3.5 就有的语法（它自己也得能在老解释器上跑）。
const PROBE_COMMON = `
import ast, re, sys

HEREDOC = re.compile(r"<<-?\\s*'?([A-Za-z_][A-Za-z0-9_]*)'?")

def heredocs(text):
    lines = text.split("\\n")
    out = []
    i = 0
    while i < len(lines):
        m = HEREDOC.search(lines[i])
        if m:
            tag = m.group(1)
            j = i + 1
            body = []
            while j < len(lines) and lines[j].strip() != tag:
                body.append(lines[j])
                j += 1
            if j < len(lines):
                out.append((i + 2, tag, "\\n".join(body)))
                i = j + 1
                continue
        i += 1
    return out

def is_python(body):
    for line in body.split("\\n"):
        s = line.strip()
        if s.startswith("import ") or s.startswith("from "):
            return True
    return False
`;

const PROBE_SYNTAX = PROBE_COMMON + `
NEW_NODES = {
    "JoinedStr": "f-string (3.6+)",
    "AnnAssign": "变量注解 (3.6+)",
    "NamedExpr": "海象运算符 := (3.8+)",
    "Match": "match 语句 (3.10+)",
}

bad = 0
for path in sys.argv[1:]:
    text = open(path, encoding="utf-8").read()
    for start, tag, body in heredocs(text):
        if not is_python(body):
            continue
        tree = ast.parse(body, filename=path)
        for node in ast.walk(tree):
            name = type(node).__name__
            if name in NEW_NODES:
                bad += 1
                line = body.split("\\n")[node.lineno - 1].strip()
                print("%s:%d: %s  <<%s>>  %s"
                      % (path, start + node.lineno - 1, NEW_NODES[name], tag, line))
            if name == "arguments" and getattr(node, "posonlyargs", None):
                bad += 1
                print("%s:%d: 仅位置参数 / (3.8+)  <<%s>>" % (path, start, tag))
        for off, line in enumerate(body.split("\\n")):
            if re.search(r"\\b[0-9]+_[0-9]+", line):
                bad += 1
                print("%s:%d: 下划线数字字面量 (3.6+)  <<%s>>  %s"
                      % (path, start + off, tag, line.strip()))
sys.exit(1 if bad else 0)
`;

// 静态判据：内嵌 Python 不得 import pathlib。
// 理由见下面的 test 注释（3.5 的 C 层函数不认 Path 对象 —— 这是**运行期**差异，
// 语法扫描器抓不到，只能靠"别用 pathlib"这条规则兜住整类问题）。
const PROBE_PATHLIB = PROBE_COMMON + `
bad = 0
for path in sys.argv[1:]:
    text = open(path, encoding="utf-8").read()
    for start, tag, body in heredocs(text):
        if not is_python(body):
            continue
        tree = ast.parse(body, filename=path)
        for node in ast.walk(tree):
            mod = None
            if isinstance(node, ast.Import):
                for alias in node.names:
                    if alias.name.split(".")[0] == "pathlib":
                        mod = alias.name
            elif isinstance(node, ast.ImportFrom):
                if (node.module or "").split(".")[0] == "pathlib":
                    mod = node.module
            if mod is not None:
                bad += 1
                line = body.split("\\n")[node.lineno - 1].strip()
                print("%s:%d: pathlib（%s）  <<%s>>  %s"
                      % (path, start + node.lineno - 1, mod, tag, line))
sys.exit(1 if bad else 0)
`;

test('内嵌 Python 不含 Python 3.6+ 才支持的语法（f-string / 变量注解 / 海象 …）', () => {
  const files = scriptFiles();
  assert.ok(files.length > 0, '没找到任何 deploy/standalone/*.sh');

  const result = spawnSync('python3', ['-', ...files], { input: PROBE_SYNTAX, encoding: 'utf8' });
  assert.equal(
    result.status, 0,
    '内嵌 Python 里出现了当前目标机（Ubuntu 16 / python3.5）不认识的语法。\n' +
    '这类语法会让**整段 heredoc 编译失败、一行都不执行**，报错行还指向那句语法，\n' +
    '现场完全看不出是解释器版本问题。请改用 `%` 或字符串拼接。\n' +
    '命中：\n' + result.stdout + result.stderr,
  );
});

// ============================================================================
// 契约（第二层，运行期）：内嵌 Python 不得用 pathlib。
//
// 真实事故（2026-10-10，同一台 Ubuntu 16，客户第二次报障）：语法那关过了之后，
// 卡在「正在初始化数据库」——
//     File "<stdin>", line 189, in <module>
//     TypeError: argument 1 must be str, not PosixPath
//   该行是 `db = sqlite3.connect(db_path)`，而上游 `db_path = Path(sys.argv[2])`。
//
// 根因：**path-like 支持（PEP 519）是 Python 3.6 才进标准库的**。3.5 的
//   `sqlite3.connect` 走 `PyArg_ParseTupleAndKeywords(..., "s|diOiOip")`，格式串 "s"
//   只认 str ⇒ 直接把 PosixPath 拒了。同一类还适用 `os.remove` / 内置 `open` /
//   `os.stat` … 一切用 C 层参数解析的入口。
//   ⛔ 这属于"语法完全合法、在我们的新解释器上也永远正常"，只有目标机 3.5 才会炸 ——
//      静态语法扫描（上面那条）看不见它，必须另立一条规则。
//
// 判据选"一律不许 import pathlib"而不是"逐个调用点加 str()"：
//   path-like 传参的调用点是**无限**的（以后谁新加一句 open(p) 就又中招），
//   而"内嵌脚本不用 pathlib"是**有限且可判定**的规则 —— 内嵌脚本都很小，
//   用 os.path + 显式 str() 完全够用。
// ============================================================================
test('内嵌 Python 不得使用 pathlib（3.5 下 Path 传给 C 层函数直接 TypeError）', () => {
  const files = scriptFiles();
  const result = spawnSync('python3', ['-', ...files], { input: PROBE_PATHLIB, encoding: 'utf8' });
  assert.equal(
    result.status, 0,
    '内嵌 Python 里 import 了 pathlib。目标机可能是 Ubuntu 16（python3.5），\n' +
    '而 path-like（PEP 519）是 3.6 才有的：Path 对象交给 sqlite3.connect / os.remove /\n' +
    'open() 这些 C 层入口会抛 `TypeError: argument 1 must be str, not PosixPath`。\n' +
    '这类错**语法完全合法**，在构建机上永远复现不了。请改用 os.path + 显式 str()。\n' +
    '命中：\n' + result.stdout + result.stderr,
  );
});

test('脚本顶层不出现 "import f-string 时代" 的新模块（构建脚本除外，仅报告）', () => {
  // secrets 是 3.6+ 才有的模块；它只被**构建脚本**用（构建机是 macOS，python3.13）。
  // 交付到客户机的三个脚本不允许依赖它 —— 这里只对交付脚本硬断言。
  const shipped = ['uvp-gb28181-ctl.sh', 'uvp-gb28181-service-install.sh', 'uvp-gb28181-service-uninstall.sh'];
  for (const name of shipped) {
    const text = readFileSync(join(DEPLOY_DIR, name), 'utf8');
    assert.doesNotMatch(text, /\bimport\s+secrets\b/,
      name + ' 依赖了 Python 3.6+ 的 secrets 模块（Ubuntu 16 的 python3.5 没有）');
  }
});

// ---------------------------------------------------------------------------
// 下面是"重构没改行为"的证据测试：SECRET_SYNC_PY / SYNC_PY 两个 heredoc 做过
// f-string → % / 拼接的等价改写，这里用夹具锁住结果（改坏了立刻红）。
// ---------------------------------------------------------------------------

const ctl = readFileSync(join(DEPLOY_DIR, 'uvp-gb28181-ctl.sh'), 'utf8');

function grab(tag) {
  const m = ctl.match(new RegExp("<<'" + tag + "'\\n([\\s\\S]*?)\\n" + tag + "\\n"));
  assert.ok(m, '在 uvp-gb28181-ctl.sh 里找不到 <<' + tag + '>> heredoc（被改名了？）');
  return m[1];
}

function runHeredoc(code, args, fixtureText) {
  const dir = mkdtempSync(join(tmpdir(), 'uvp-heredoc-'));
  try {
    const file = join(dir, 'config.yml');
    writeFileSync(file, fixtureText, 'utf8');
    const r = spawnSync('python3', ['-', file, ...args], { input: code, encoding: 'utf8' });
    return { status: r.status, stdout: r.stdout, stderr: r.stderr,
      out: readFileSync(file, 'utf8') };
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

// 贴近真实 config.yml：zlm / media 是**嵌套**在 gb28181 下的（不是顶级段）。
const FIXTURE =
  'httpserver:\n  port: ":8080"\n' +
  'redis:\n  port: 6379\n' +
  'mysql:\n  port: 3306\n' +
  'gb28181:\n  zlm:\n    httpport: 18080\n' +
  '    secret: "CHANGE_ME"           # ZLM API secret(部署时填 ZLM 实际 secret)\n' +
  '    rtpport: 40000\n  media:\n    hookport: 8280\n';

test('SECRET_SYNC_PY：替换占位符 secret，且保留行尾注释、不碰其它段', () => {
  const r = runHeredoc(grab('SECRET_SYNC_PY'), ['S3CR3T-abc123'], FIXTURE);
  assert.equal(r.status, 0, r.stderr);
  assert.match(r.stdout, /ZLM secret 已按 config\.ini 对齐写入 config\.yml（13 字符）/);
  assert.match(r.out, /secret: "S3CR3T-abc123"    # ZLM API secret\(部署时填 ZLM 实际 secret\)/,
    '行尾注释必须保留（曾被正则连注释一起吞掉 → 静默跳过）');
  assert.match(r.out, /mysql:\n  port: 3306/, '别的段不能被改动');
});

test('SECRET_SYNC_PY：已有真值时不覆盖（幂等）', () => {
  const filled = FIXTURE.replace('"CHANGE_ME"', '"REAL-SECRET"');
  const r = runHeredoc(grab('SECRET_SYNC_PY'), ['OTHER'], filled);
  assert.equal(r.status, 0, r.stderr);
  assert.match(r.stdout, /ZLM secret 与 config\.ini 一致，无需改动/);
  assert.match(r.out, /"REAL-SECRET"/, '运维手工填过的值不能被覆盖');
});

test('SYNC_PY：httpserver/redis/zlm/media 端口按规划改写，mysql 不受影响', () => {
  const r = runHeredoc(grab('SYNC_PY'), ['51000', '51001', '51002', '51003'], FIXTURE);
  assert.equal(r.status, 0, r.stderr);
  assert.match(r.out, /httpserver:\n  port: :51000/);
  assert.match(r.out, /redis:\n  port: 51001/);
  assert.match(r.out, /zlm:\n    httpport: 51002\n[^\n]*\n    rtpport: 51003/);
  assert.match(r.out, /media:\n    hookport: 51000/);
  assert.match(r.out, /mysql:\n  port: 3306/, '宿主重名的 port 键（mysql）不能被连带改掉');
});

test('SYNC_PY：重复同步幂等（内容不再变化）', () => {
  const args = ['51000', '51001', '51002', '51003'];
  const first = runHeredoc(grab('SYNC_PY'), args, FIXTURE);
  assert.equal(first.status, 0, first.stderr);
  const second = runHeredoc(grab('SYNC_PY'), args, first.out);
  assert.equal(second.status, 0, second.stderr);
  assert.equal(second.out, first.out, '同步两次的最终内容必须一致（幂等）');
});

// ---------------------------------------------------------------------------
// 行为式证据：把**真实的** DB_INIT_PY 放进"python3.5 语义模拟器"里跑一遍。
//
// 为什么光有静态规则不够：上面那条"不许 import pathlib"是**规则**，
// 而这里要证明的是"改完之后，3.5 语义下建库真的能通"——尤其是
// `isolation_level=None` 这个改动确实把 3.5 的隐式事务挡住了。
// 把 `isolation_level=None` 拿掉（或把 db_path 换回 Path），这个测试必须立刻变红。
// ---------------------------------------------------------------------------
const SHIM = `
# python3.5 语义模拟器（只在契约测试的临时目录里生效，靠 PYTHONPATH 注入，不进交付物）。
#
# 用新解释器模拟 3.5 的两处**运行期**行为 —— 它们都"语法完全合法、在构建机上永远正常"，
# 只有目标机（Ubuntu 16 / python3.5）才会现形：
#   ① C 层入口只认 str（path-like = PEP 519 是 3.6 才有的）。
#   ② isolation_level 非 None 时，模块会在**非 DML 语句之前先自动 COMMIT 一次**。
# 依据（CPython 3.5 源码）：
#   Modules/_sqlite/connection.c：connect 的参数格式串是 "s|diOiOip"（"s" 只认 str）；
#     且 isolation_level == Py_None 时把 begin_statement 置 NULL，否则置成 "BEGIN" 等。
#   Modules/_sqlite/cursor.c：begin_statement 非 NULL 时，先看语句类型再动手 ——
#     INSERT/UPDATE/DELETE/REPLACE 且不在事务里 → 隐式 BEGIN；
#     其它语句（DDL / PRAGMA / COMMIT …，即 STATEMENT_OTHER）且**在事务里 → 隐式 COMMIT**。
import builtins
import os
import sqlite3

PathLike = getattr(os, "PathLike")

_DML = ("insert", "update", "delete", "replace")


def _reject(value):
    if isinstance(value, PathLike) or type(value).__name__ in ("PosixPath", "WindowsPath"):
        raise TypeError("argument 1 must be str, not %s" % type(value).__name__)


def _kind(sql):
    s = sql.strip()
    while s.startswith("--") or s.startswith("/*"):
        if s.startswith("--"):
            s = s.split("\\n", 1)[1].strip() if "\\n" in s else ""
        else:
            s = s.split("*/", 1)[1].strip() if "*/" in s else ""
    if not s:
        return "other"
    word = s.split(None, 1)[0].lower()
    if word in _DML:
        return "dml"
    if word == "select":
        return "select"
    return "other"


class _Conn35(object):
    def __init__(self, real, managed):
        self._real = real
        self._managed = managed

    def __getattr__(self, name):
        return getattr(self._real, name)

    @property
    def in_transaction(self):
        return self._real.in_transaction

    def execute(self, sql, *args):
        if self._managed:
            kind = _kind(sql)
            if kind == "dml" and not self._real.in_transaction:
                self._real.execute("BEGIN")
            elif kind == "other" and self._real.in_transaction:
                self._real.execute("COMMIT")
        return self._real.execute(sql, *args)

    def executemany(self, sql, seq):
        return self._real.executemany(sql, seq)

    def close(self):
        return self._real.close()


_real_connect = sqlite3.connect


def connect(database=":memory:", *args, **kwargs):
    _reject(database)
    level = kwargs.get("isolation_level", args[2] if len(args) >= 3 else "")
    return _Conn35(_real_connect(database, *args, **kwargs), level is not None)


sqlite3.connect = connect

_real_remove = os.remove
_real_open = builtins.open


def remove(path, *args, **kwargs):
    _reject(path)
    return _real_remove(path, *args, **kwargs)


def open(path, *args, **kwargs):
    _reject(path)
    return _real_open(path, *args, **kwargs)


os.remove = os.unlink = remove
builtins.open = open
`;

// 小基线：覆盖 heredoc 会走到的全部 phase（pragma / table / index / seed），
// 并满足它自己的收尾自检（sys_users 里恰好 1 个 admin、外键开着、无悬挂引用）。
const TINY_BASELINE = [
  'PRAGMA foreign_keys = ON;',
  'PRAGMA busy_timeout = 5000;',
  'CREATE TABLE IF NOT EXISTS "sys_users" (',
  '  "id" INTEGER PRIMARY KEY AUTOINCREMENT,',
  '  "username" TEXT NOT NULL,',
  '  "created_at" DATETIME NOT NULL',
  ');',
  'CREATE TABLE IF NOT EXISTS "gb_device" (',
  '  "id" INTEGER PRIMARY KEY AUTOINCREMENT,',
  '  "owner_id" INTEGER NOT NULL REFERENCES "sys_users"("id")',
  ');',
  'CREATE INDEX IF NOT EXISTS "idx_gb_device_owner" ON "gb_device" ("owner_id");',
  "INSERT INTO \"sys_users\" (\"id\", \"username\", \"created_at\") VALUES (1, 'admin', '2026-10-10 00:00:00');",
  "INSERT INTO \"gb_device\" (\"id\", \"owner_id\") VALUES (1, 1);",
  '',
].join('\n');

// ⛔ 回读用**新连接**：`PRAGMA foreign_keys` 是**连接级**的，新连接读回来必然是 0，
//   拿它断言会假红（第几次踩这个了）。外键是否开启由 heredoc 自己的收尾自检断言。
// ⛔ 表数要排除 `sqlite_%`：AUTOINCREMENT 会带出一个 sqlite_sequence。
const READBACK_PY = [
  'import sqlite3, sys',
  'db = sqlite3.connect(sys.argv[1])',
  "tables = db.execute(\"select count(*) from sqlite_master where type='table'"
  + " and name not like 'sqlite_%'\").fetchone()[0]",
  "admins = db.execute(\"select count(*) from sys_users where username='admin'\").fetchone()[0]",
  'print(tables, admins)',
  '',
].join('\n');

test('建库脚本在「python3.5 语义」下能跑通（拒绝 path-like + 3.5 的隐式事务）', () => {
  const dir = mkdtempSync(join(tmpdir(), 'uvp-py35-shim-'));
  const env = { ...process.env, PYTHONPATH: dir, PYTHONIOENCODING: 'utf-8' };
  try {
    writeFileSync(join(dir, 'sitecustomize.py'), SHIM, 'utf8');

    // ① 先证明模拟器真的生效 —— 否则这个测试可能"因为什么都没模拟"而假绿。
    const sense = spawnSync('python3',
      ['-c', 'import sqlite3,pathlib;sqlite3.connect(pathlib.Path(":memory:"))'],
      { encoding: 'utf8', env });
    assert.notEqual(sense.status, 0, '模拟器没生效：3.5 下 connect(Path) 必须被拒');
    assert.match(sense.stderr, /must be str, not PosixPath/,
      '模拟器报的不是 3.5 那句错：' + sense.stderr);

    // ② 真实 heredoc + 小基线，在模拟器下必须建库成功
    const base = join(dir, 'baseline.sql');
    const dbPath = join(dir, 'uvp.db');
    writeFileSync(base, TINY_BASELINE, 'utf8');
    const r = spawnSync('python3', ['-', base, dbPath],
      { input: grab('DB_INIT_PY'), encoding: 'utf8', env });
    assert.equal(r.status, 0,
      '在 python3.5 语义下建库失败（这正是现场 Ubuntu 16 会卡住的那一步）：\n' +
      r.stdout + r.stderr);
    assert.match(r.stdout, /管理员账号已就位/, r.stdout);

    // ③ 落盘的确实是个可用的库（表数、admin、外键都读回来）
    const probe = join(dir, 'readback.py');
    writeFileSync(probe, READBACK_PY, 'utf8');
    const back = spawnSync('python3', [probe, dbPath], { encoding: 'utf8', env });
    assert.equal(back.stdout.trim(), '2 1',
      '回读结果不对（应为 "2 张业务表 / 1 个 admin"）：' + back.stdout + back.stderr);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});
