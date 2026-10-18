import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

// ============================================================================
// 契约：`uvp-gb28181-ctl.sh reset-password`（忘记登录口令时把账号还原成出厂口令）
//
// 这条命令只在**客户已经登不进去**的时候才被用到 —— 也就是现场没有任何别的手段可以
// 交叉验证它做得对不对。所以它不能"看起来对"，只能被钉死：
//
//   ① 出厂口令必须与 config.example.yml 里 `server.demoaccount.defaultpassword` 同源
//      （后端 `/api/config/get` 在待改密状态下回显的就是那个值，两边不一致 = 页面提示
//      的口令登不进去）；
//   ② 写进库的哈希必须就是包内 baseline.sql 里 admin 那一行的哈希
//      （= 出厂口令的 bcrypt，由 Go 侧 password_bootstrap_contract_test.go 用真 bcrypt 断言）；
//   ③ 行为：写完后 must_change_password=1、只动一个账号、认错库/认错账号必须**报错退出**
//      且一行都不改。
//
// ⛔ 为什么 Linux 版写死哈希而 Windows 版现场算：bcrypt 不在 Python 标准库里
//   （Linux 的 crypt 只认 $1$/$5$/$6$，不认 $2a$）。写死 + 这三条锁，等于把
//   "常量漂移"这件事变成了红灯，而不是一个查不出来的静默故障。
// ============================================================================

const ROOT = fileURLToPath(new URL('../', import.meta.url));
const CTL_PATH = join(ROOT, 'deploy/standalone/uvp-gb28181-ctl.sh');
const CONFIG_EXAMPLE = join(ROOT, 'server/config/config.example.yml');
const BASELINE = join(ROOT, 'server/resource/database/sqlitebaseline/baseline.sql');

const ctl = readFileSync(CTL_PATH, 'utf8');

function ctlConstant(name) {
  const m = ctl.match(new RegExp('^' + name + "='([^']*)'$", 'm'));
  assert.ok(m, CTL_PATH + ' 里找不到常量 ' + name + '（被改名/删掉了？）');
  return m[1];
}

// 与 standalone-py35-compat.test.mjs 同一套抠法：`<<'TAG'` 到行首 TAG。
function grab(tag) {
  const m = ctl.match(new RegExp("<<'" + tag + "'\\n([\\s\\S]*?)\\n" + tag + "\\n"));
  assert.ok(m, '在 uvp-gb28181-ctl.sh 里找不到 <<' + tag + '>> heredoc（被改名了？）');
  return m[1];
}

function runPython(code, args, options = {}) {
  return spawnSync('python3', ['-', ...args], {
    input: code, encoding: 'utf8', ...options,
  });
}

test('出厂口令常量与 config.example.yml 的 demoaccount.defaultpassword 同源', () => {
  const yml = readFileSync(CONFIG_EXAMPLE, 'utf8');
  // 段落内定位：只认 demoaccount 段里的 defaultpassword（别段可能也有同名键）。
  const block = yml.match(/^([ \t]*)demoaccount:[ \t]*$/m);
  assert.ok(block, 'config.example.yml 里找不到 demoaccount 段');
  const rest = yml.slice(block.index + block[0].length).split('\n');
  const indent = block[1].length;
  let found = null;
  for (const line of rest) {
    if (line.trim() === '' || line.trim().startsWith('#')) { continue; }
    const cur = line.length - line.trimStart().length;
    if (cur <= indent) { break; }
    const m = line.trim().match(/^defaultpassword\s*:\s*(.*)$/);
    if (m) { found = m[1].replace(/\s*#.*$/, '').trim().replace(/^"(.*)"$/, '$1'); break; }
  }
  assert.ok(found, 'config.example.yml 的 demoaccount 段里找不到 defaultpassword');
  assert.equal(ctlConstant('CTL_FACTORY_PASSWORD'), found,
    'ctl.sh 的出厂口令常量与 config.example.yml 不一致：' +
    '后端登录页回显的是配置里那个值，两边不同 ⇒ 客户按提示登录必然失败');
});

test('出厂哈希常量与包内 baseline.sql 里 admin 的哈希一致', () => {
  const sql = readFileSync(BASELINE, 'utf8');
  const m = sql.match(/'admin'\s*,\s*'(\$2[aby]\$[^']+)'/);
  assert.ok(m, 'baseline.sql 里找不到 admin 的 bcrypt 哈希（正则/基线结构变了？请同步本用例）');
  assert.equal(ctlConstant('CTL_FACTORY_HASH'), m[1],
    'ctl.sh 的出厂哈希常量与基线 seed 不一致：重置会把口令写成另一个字符串，' +
    '而脚本仍然打印 config 里的口令 ⇒ 谁也登不进去，且现场查不出来');
  assert.match(m[1], /^\$2[aby]\$10\$[./A-Za-z0-9]{53}$/, 'seed 哈希不是 cost=10 的 bcrypt 形态');
});

// ---------------------------------------------------------------- 行为用例 ----

const TINY_BASELINE = [
  'CREATE TABLE "sys_users" (',
  '  "id" INTEGER PRIMARY KEY AUTOINCREMENT,',
  '  "username" TEXT NOT NULL,',
  '  "password" TEXT NOT NULL DEFAULT \'\',',
  '  "status" INTEGER NOT NULL DEFAULT 1,',
  '  "must_change_password" INTEGER NOT NULL DEFAULT 0',
  ');',
  "INSERT INTO \"sys_users\" (\"username\",\"password\") VALUES ('admin', 'KEEP-ME-OUT');",
  "INSERT INTO \"sys_users\" (\"username\",\"password\") VALUES ('operator', 'KEEP-ME-OUT');",
  '',
].join('\n');

// 2026-10-08 之前出的包建出来的库：没有 must_change_password 列。
const LEGACY_BASELINE = TINY_BASELINE.replace(',\n  "must_change_password" INTEGER NOT NULL DEFAULT 0', '');

const READBACK = [
  'import sqlite3, sys',
  'db = sqlite3.connect(sys.argv[1])',
  'cols = [r[1] for r in db.execute(\'PRAGMA table_info("sys_users")\').fetchall()]',
  'if "must_change_password" not in cols:',
  '    print("NONE")',
  'else:',
  '    for row in db.execute(\'SELECT "username","password","must_change_password" FROM "sys_users" ORDER BY "id"\').fetchall():',
  '        print("%s|%s|%s" % row)',
  '',
].join('\n');

function makeDB() {
  const dir = mkdtempSync(join(tmpdir(), 'uvp-reset-pw-'));
  return { dir, dbPath: join(dir, 'uvp.db') };
}

function buildDB(dir, dbPath, ddl) {
  const code = 'import sqlite3, sys\ndb = sqlite3.connect(sys.argv[1], timeout=10)\ndb.executescript(sys.argv[2])\ndb.commit()\ndb.close()\n';
  const r = runPython(code, [dbPath, ddl]);
  assert.equal(r.status, 0, '建临时库失败: ' + r.stderr);
}

function readBack(dbPath) {
  const r = runPython(READBACK, [dbPath]);
  assert.equal(r.status, 0, '回读失败: ' + r.stderr);
  return r.stdout.trim().split('\n').filter((l) => l !== '');
}

function resetHeredoc() {
  return grab('RESET_ADMIN_PY');
}

test('reset-admin：口令写成出厂哈希 + 强制改密，且只动一个账号', () => {
  const { dir, dbPath } = makeDB();
  try {
    buildDB(dir, dbPath, TINY_BASELINE);
    const r = runPython(resetHeredoc(),
      [dbPath, 'admin', ctlConstant('CTL_FACTORY_HASH'), ctlConstant('CTL_FACTORY_PASSWORD')]);
    assert.equal(r.status, 0, '重置应当成功: ' + r.stderr + r.stdout);
    assert.match(r.stdout, /账号 admin 的口令已还原为出厂口令/, r.stdout);

    const rows = readBack(dbPath);
    const hash = ctlConstant('CTL_FACTORY_HASH');
    assert.deepEqual(rows, [
      'admin|' + hash + '|1',
      'operator|KEEP-ME-OUT|0', // 别的账号一行都不能动
    ], '库内容不对（口令或 must_change_password）');
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test('reset-admin：老版本库（缺 must_change_password 列）补列后再重置', () => {
  const { dir, dbPath } = makeDB();
  try {
    buildDB(dir, dbPath, LEGACY_BASELINE);
    const r = runPython(resetHeredoc(),
      [dbPath, 'admin', ctlConstant('CTL_FACTORY_HASH'), ctlConstant('CTL_FACTORY_PASSWORD')]);
    assert.equal(r.status, 0, '老库应当能补齐列后重置: ' + r.stderr + r.stdout);
    const rows = readBack(dbPath);
    assert.equal(rows[0], 'admin|' + ctlConstant('CTL_FACTORY_HASH') + '|1', rows.join(' / '));
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test('reset-admin：账号不存在时非零退出，且不碰任何数据', () => {
  const { dir, dbPath } = makeDB();
  try {
    buildDB(dir, dbPath, TINY_BASELINE);
    const r = runPython(resetHeredoc(),
      [dbPath, 'ghost', ctlConstant('CTL_FACTORY_HASH'), ctlConstant('CTL_FACTORY_PASSWORD')]);
    assert.notEqual(r.status, 0, '账号不存在必须报错退出');
    assert.match(r.stderr, /ghost/, '报错里要带上写错的账号名，便于现场核对');
    assert.match(r.stderr, /admin/, '报错里要列出真实存在的账号');
    assert.deepEqual(readBack(dbPath), ['admin|KEEP-ME-OUT|0', 'operator|KEEP-ME-OUT|0'],
      '账号名写错时一行都不能改');
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test('reset-admin：认错库（没有 sys_users）必须拒绝，且不建任何东西', () => {
  const { dir, dbPath } = makeDB();
  try {
    buildDB(dir, dbPath, 'CREATE TABLE "something_else" ("id" INTEGER PRIMARY KEY);');
    const r = runPython(resetHeredoc(),
      [dbPath, 'admin', ctlConstant('CTL_FACTORY_HASH'), ctlConstant('CTL_FACTORY_PASSWORD')]);
    assert.notEqual(r.status, 0, '不是本平台的库必须报错退出');
    assert.match(r.stderr, /sys_users/, '报错要说清是"没有 sys_users 表"');
    assert.deepEqual(readBack(dbPath), ['NONE'], '认错库时不得留下任何结构改动');
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

// ------------------------------------------------- config.yml 标量读取 ----

// ⛔ 一律对 stdout 做 trim()：嵌进 shell 时是用 `$( )` 取值的，命令替换会**吃掉**
//   末尾换行，所以这里不 trim 就等于在断言一个现场根本不存在的差异。
function stdoutOf(result) {
  return result.stdout.trim();
}

test('read_conf_scalar：段内定位 + 剥引号/行尾注释', () => {
  const dir = mkdtempSync(join(tmpdir(), 'uvp-conf-'));
  try {
    const conf = join(dir, 'config.yml');
    writeFileSync(conf, [
      'media:',
      '  defaultpassword: "DECOY-别段的同名键"',
      'server:',
      '  demoaccount:',
      '    defaultusername: "admin"   # 登录页默认账号',
      '    defaultpassword: "From-Config"   # 行尾注释不能算进值里',
      'redis:',
      '  host: "127.0.0.1"',
      '  port: 6379',
      '  indexdb: 1',
      '  password: ""',
      '',
    ].join('\n'), 'utf8');

    const code = grab('CONF_SCALAR_PY');
    const pw = runPython(code, [conf, 'demoaccount', 'defaultpassword']);
    assert.equal(pw.status, 0, pw.stderr);
    assert.equal(stdoutOf(pw), 'From-Config', '取到的不是 demoaccount 段里的值（或吞了行尾注释）');

    const idx = runPython(code, [conf, 'redis', 'indexdb']);
    assert.equal(stdoutOf(idx), '1', 'indexdb 应原样取出（没有引号）');

    const emptyPw = runPython(code, [conf, 'redis', 'password']);
    assert.equal(stdoutOf(emptyPw), '', '空值应回空串（此时调用方走默认值）');

    const missing = runPython(code, [conf, 'nosuch', 'key']);
    assert.equal(missing.status, 0, '找不到的段/键不该报错，应由调用方决定怎么办');
    assert.equal(stdoutOf(missing), '');
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test('read_conf_scalar：口令里带 # 时不能被当注释截断', () => {
  const dir = mkdtempSync(join(tmpdir(), 'uvp-conf2-'));
  try {
    const conf = join(dir, 'config.yml');
    writeFileSync(conf, [
      'server:',
      '  demoaccount:',
      "    defaultpassword: 'P#ss w0rd'",
      '',
    ].join('\n'), 'utf8');
    const r = runPython(grab('CONF_SCALAR_PY'), [conf, 'demoaccount', 'defaultpassword']);
    assert.equal(stdoutOf(r), 'P#ss w0rd', '口令里的 # 被当成注释起点截断了');
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});
