import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';

// ---------------------------------------------------------------------------
// 契约：`ctl.sh start` 必须把 config.yml 的 `processauthority.state_dir`
//       钉到**当前**安装根，且只在「非空 且 ≠ 目标」时才写。
//
// 起因（2026-11-09，220 上 1.1.13 → 1.1.17 换目录升级）：
//   新包解到新目录、config/data 原样搬过去，后端启动即退出，日志只有
//       ERROR startup … phase=process_authority reason=state_unavailable
//   真因是 state_dir 仍指着旧安装根的绝对路径（ctl.sh 只同步端口 5 处、
//   ZLM ini 18 处，从不碰这个键）⇒ 换目录升级必炸，且报错零信息量。
//
// 另外三条是**反向**契约，比正向更重要（写错任何一条都会制造更贵的故障）：
//   ① 空值 → 不写。出厂走用户级默认目录，那里可能已有旧锁；硬塞安装根 = 新锁 =
//      新 domainID ⇒ 与库里绑定的旧 domain 冲突（database_domain_mismatch）。
//   ② 已等于目标 → 不写。否则每次启动都改文件 mtime，现场比对全乱。
//   ③ 写失败 → 只告警、不阻断。段/键缺失是"包不完整"，让后端自己报，比 start 早退更好查。
// ---------------------------------------------------------------------------

const script = readFileSync(new URL('../deploy/standalone/uvp-gb28181-ctl.sh', import.meta.url), 'utf8');
const BASH = '/bin/bash';

function fn(name) {
  const multiLine = script.match(new RegExp(`^${name}\\(\\) \\{[\\s\\S]*?^\\}`, 'm'));
  assert.ok(multiLine, `ctl.sh 里找不到函数 ${name}()`);
  return multiLine[0];
}

const FUNCS = ['read_conf_scalar', 'sync_process_authority_dir'].map(fn).join('\n');

/**
 * 在临时包里跑一次 sync_process_authority_dir。
 * `uvp-setup` 用 stub 冒充：config-get 回 STUB_VALUE，config-set 只把参数记到日志。
 */
function run(stateDir, { setRc = 0, activeRoot = '' } = {}) {
  const dir = mkdtempSync(join(tmpdir(), 'uvp-pa-dir-'));
  const root = join(dir, 'uvp-gb28181-linux-amd64-1.1.17');
  mkdirSync(join(root, 'config'), { recursive: true });
  const callLog = join(dir, 'calls.log');
  const stub = join(dir, 'uvp-setup');
  writeFileSync(
    stub,
    [
      '#!/bin/bash',
      'if [ "$1" = "config-get" ]; then printf "%s" "${STUB_VALUE:-}"; exit 0; fi',
      'if [ "$1" = "config-set" ]; then printf "%s\\n" "$*" >> "$STUB_LOG"; exit "${STUB_RC:-0}"; fi',
      'exit 0',
    ].join('\n'),
  );
  chmodSync(stub, 0o755);

  const harness = [
    'set -Eeuo pipefail',
    `ROOT="${root}"`,
    `ACTIVE_ROOT="${activeRoot || root}"`,
    `CONF="${root}/config/config.yml"`,
    `SETUP_BIN="${stub}"`,
    'log() { printf "LOG:%s\\n" "$*"; }',
    'det() { printf "DET:%s\\n" "$*"; }',
    FUNCS,
    'sync_process_authority_dir',
  ].join('\n');

  // state_dir 允许传函数：要断言"等于目标"必须先用真实的临时 root 拼出目标值。
  const resolved = typeof stateDir === 'function' ? stateDir(root) : stateDir;
  const r = spawnSync(BASH, ['-c', harness], {
    encoding: 'utf8',
    env: { ...process.env, STUB_VALUE: resolved, STUB_LOG: callLog, STUB_RC: String(setRc) },
  });
  assert.equal(r.status, 0, `harness 失败：${r.stderr}`);
  const calls = existsSync(callLog) ? readFileSync(callLog, 'utf8').trim() : '';
  return { root, calls, stdout: r.stdout };
}

test('state_dir 为空：一个字都不写（出厂默认目录语义不能动）', () => {
  const { calls } = run('');
  assert.equal(calls, '', '空值不应触发 config-set');
});

test('state_dir 已等于当前安装根：不重写（幂等，别每次启动都改 mtime）', () => {
  const { calls } = run((root) => `${root}/data/process-authority`);
  assert.equal(calls, '', '已等于目标时不应再写');
});

test('state_dir 指向旧安装根：改写成当前安装根', () => {
  const { root, calls } = run('/home/ops/uvp-gb28181-linux-amd64-1.1.13/data/process-authority');
  assert.match(calls, /^config-set /, `应调用 config-set，实际：${calls}`);
  assert.match(calls, /--section processauthority/);
  assert.match(calls, /--key state_dir/);
  assert.ok(
    calls.includes(`--value ${root}/data/process-authority`),
    `目标必须是 $ROOT/data/process-authority，实际：${calls}`,
  );
});

test('切 current 软链时目标仍取安装根，不取 ACTIVE_ROOT', () => {
  const { root, calls } = run('/old/root/data/process-authority', {
    activeRoot: '/opt/releases/abc123',
  });
  assert.ok(calls.includes(`--value ${root}/data/process-authority`), calls);
  assert.ok(!calls.includes('/opt/releases/abc123'), '锁目录必须留在安装根，不能跟着 releases 走');
});

test('写入失败只告警、不阻断启动', () => {
  const { stdout, calls } = run('/old/root/data/process-authority', { setRc: 1 });
  assert.match(calls, /config-set/);
  assert.match(stdout, /⚠️/, `应输出告警，实际：${stdout}`);
  assert.match(stdout, /state_unavailable/, '告警要点明后端的报错特征，否则现场还是查不出来');
});

test('ctl.sh 的 start 分支必须在起后端之前调用它', () => {
  const startBlock = script.slice(script.indexOf('  start)'), script.indexOf('  restart-zlm)'));
  // ⛔ 必须匹配"整行就是这条调用"，不能用 indexOf 裸查子串 ——
  //   注释里提到 start_backend 会把顺序判反（本测试第一次就是这么假红的）。
  const syncIdx = startBlock.search(/^[ \t]*sync_process_authority_dir[ \t]*$/m);
  const backendIdx = startBlock.search(/^[ \t]*start_backend[ \t]*$/m);
  assert.ok(syncIdx > 0, 'start 分支没有调用 sync_process_authority_dir');
  assert.ok(backendIdx > syncIdx, '必须排在 start_backend 之前');
});
