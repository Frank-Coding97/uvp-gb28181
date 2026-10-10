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
const PROBE = `
import ast, re, sys

HEREDOC = re.compile(r"<<-?\\s*'?([A-Za-z_][A-Za-z0-9_]*)'?")
NEW_NODES = {
    "JoinedStr": "f-string (3.6+)",
    "AnnAssign": "变量注解 (3.6+)",
    "NamedExpr": "海象运算符 := (3.8+)",
    "Match": "match 语句 (3.10+)",
}

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

test('内嵌 Python 不含 Python 3.6+ 才支持的语法（f-string / 变量注解 / 海象 …）', () => {
  const files = scriptFiles();
  assert.ok(files.length > 0, '没找到任何 deploy/standalone/*.sh');

  const result = spawnSync('python3', ['-', ...files], { input: PROBE, encoding: 'utf8' });
  assert.equal(
    result.status, 0,
    '内嵌 Python 里出现了当前目标机（Ubuntu 16 / python3.5）不认识的语法。\n' +
    '这类语法会让**整段 heredoc 编译失败、一行都不执行**，报错行还指向那句语法，\n' +
    '现场完全看不出是解释器版本问题。请改用 `%` 或字符串拼接。\n' +
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
