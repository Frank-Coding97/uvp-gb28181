import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { existsSync, mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

// 绿色包的建库器就藏在 uvp-gb28181-ctl.sh 的 `<<'DB_INIT_PY'` heredoc 里。
// 它有三个"静默出错"的风险点，这里逐条钉住（每一条都对应一次真实事故）：
//   ① 语句切分规则必须与基线生成器 generate.py 的 split_sql() 一致
//      —— 不一致时切坏的语句报的错与真实原因无关；
//   ② 进度分母必须是"真正会建出来的对象数"，不是语句条数
//      —— SQLite 的索引名是库级唯一、MySQL 是表级，重复名会被 IF NOT EXISTS 静默跳过；
//   ③ 建库结果必须逐项对账（表数/索引数/admin），不能只看"没报错"。
const BASELINE = fileURLToPath(
  new URL('../server/resource/database/sqlitebaseline/baseline.sql', import.meta.url),
);
const MANIFEST = fileURLToPath(
  new URL('../server/resource/database/sqlitebaseline/manifest.json', import.meta.url),
);
const GENERATOR_DIR = dirname(BASELINE);

const script = readFileSync(
  new URL('../deploy/standalone/uvp-gb28181-ctl.sh', import.meta.url), 'utf8',
);

// 从 shell 里原样抠出建库器 —— 抠不出来本身就是失败（有人重命名了 heredoc）。
const loader = script.match(/<<'DB_INIT_PY'\n([\s\S]*?)\nDB_INIT_PY/)?.[1];
assert.ok(loader, "在 uvp-gb28181-ctl.sh 里找不到 <<'DB_INIT_PY' heredoc（被重命名了？）");

const manifest = JSON.parse(readFileSync(MANIFEST, 'utf8'));

function runLoader(dbPath, extraEnv = {}) {
  return spawnSync('python3', ['-', BASELINE, dbPath], {
    input: loader,
    encoding: 'utf8',
    env: { ...process.env, ...extraEnv },
  });
}

function withTempDir(fn) {
  const directory = mkdtempSync(join(tmpdir(), 'uvp-baseline-loader-'));
  try {
    return fn(directory);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
}

// 直接从 baseline.sql 数声明名（不依赖任何被测代码），用来独立算出"应该有几个索引"。
function declaredObjectNames() {
  const named = [...readFileSync(BASELINE, 'utf8').matchAll(
    /CREATE\s+(?:UNIQUE\s+)?(TABLE|INDEX)\s+(?:IF\s+NOT\s+EXISTS\s+)?"([^"]+)"/g,
  )];
  const tables = named.filter((m) => m[1] === 'TABLE').map((m) => m[2]);
  const indexes = named.filter((m) => m[1] === 'INDEX').map((m) => m[2]);
  return {
    tableDeclarations: tables.length,
    uniqueTables: new Set(tables).size,
    indexDeclarations: indexes.length,
    uniqueIndexes: new Set(indexes).size,
  };
}

test('建库器的切分与基线生成器 generate.split_sql 逐字一致', () => {
  // 切分指纹的算法在**两边各写一遍**（这里是故意的）：它是"两边切法是否同源"的判据，
  // 若只在一处维护，改坏了另一边也发现不了。
  const viaLoader = runLoader('/dev/null', { UVP_DB_SPLIT_DIGEST: '1' });
  assert.equal(viaLoader.status, 0, viaLoader.stderr);
  assert.equal(viaLoader.stderr, '');
  assert.match(viaLoader.stdout.trim(), /^stmts=\d+ sha256=[0-9a-f]{64}$/);

  const viaGenerate = spawnSync('python3', ['-c', `
import hashlib, sys
sys.path.insert(0, sys.argv[1])
from generate import split_sql
from pathlib import Path
stmts = split_sql(Path(sys.argv[2]).read_text(encoding="utf-8"))
print("stmts=%d sha256=%s" % (
    len(stmts),
    hashlib.sha256("\\n\\x1e\\n".join(stmts).encode("utf-8")).hexdigest(),
))
`.trim(), GENERATOR_DIR, BASELINE], { encoding: 'utf8' });
  assert.equal(viaGenerate.status, 0, viaGenerate.stderr);

  assert.equal(
    viaLoader.stdout.trim(),
    viaGenerate.stdout.trim(),
    '建库器的 split_sql 与 generate.py 漂移了 —— 改一边必须同步改另一边',
  );

  // digest 模式只算指纹，不许碰数据库
  const declared = declaredObjectNames();
  const stmts = Number(viaLoader.stdout.match(/stmts=(\d+)/)[1]);
  assert.equal(
    stmts,
    declared.tableDeclarations + declared.indexDeclarations + manifest.seed_statements + 2,
    '语句总数应等于 建表 + 建索引 + 种子 + 2 条 PRAGMA',
  );
});

test('建库结果逐项对账：表数/索引数/admin，且索引数按去重后的真实对象算', () => {
  const declared = declaredObjectNames();

  withTempDir((directory) => {
    const dbPath = join(directory, 'uvp.db');
    const result = runLoader(dbPath);
    assert.equal(result.status, 0, result.stderr);
    assert.equal(result.stderr, '', '建库不应往 stderr 写东西');

    // 非交互（管道）下不该出现进度条控制符，只应有阶段行
    assert.ok(!result.stdout.includes('\r'), '非 TTY 下不该画进度条');
    assert.match(result.stdout, /建表 \d+\/\d+/);
    assert.match(result.stdout, /灌种子数据 \d+\/\d+/);

    const summary = result.stdout.match(
      /✓ (\d+) 张表 · (\d+) 个索引 · (\d+) 行种子数据 · 管理员账号已就位/,
    );
    assert.ok(summary, `结尾对账行不见了:\n${result.stdout}`);
    const tables = Number(summary[1]);
    const indexes = Number(summary[2]);
    const seeded = Number(summary[3]);

    assert.equal(tables, manifest.tables);
    assert.equal(tables, declared.uniqueTables);
    // ⛔ 索引数必须等于**去重后**的声明数：重复的 CREATE INDEX 会被 SQLite 以
    //    IF NOT EXISTS 静默跳过（当前基线有 9 条），按语句条数算就会多报。
    assert.equal(indexes, declared.uniqueIndexes,
      '索引数必须等于去重后的真实索引数');
    assert.ok(seeded > 0);

    // 被跳过的条数必须显式报出来，不能在进度分母里悄悄扣掉
    const warned = result.stdout.match(/! (\d+) 条 CREATE INDEX 未生效/);
    assert.ok(warned, `没报出被跳过的建索引语句:\n${result.stdout}`);
    assert.equal(Number(warned[1]), declared.indexDeclarations - declared.uniqueIndexes);

    assert.ok(existsSync(dbPath), '数据库文件应已生成');
  });
});
