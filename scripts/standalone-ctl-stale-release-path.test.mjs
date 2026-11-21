import assert from 'node:assert/strict';
import { spawn, spawnSync } from 'node:child_process';
import { chmodSync, copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';

// ---------------------------------------------------------------------------
// 契约：**版本切换之后，stop 必须仍然停得掉旧路径上起的那只进程。**
//
// 2026-10-27 线上升级 1.1.13 → 1.1.14 时踩到（home-relay）：
//   ① 安装布局从「就地」变成「releases/<ver> + current 软链」；
//   ② 先切 current、再 restart；
//   ③ stop 的兜底是按**当前** $ZLM_BIN 的绝对路径找进程 ——
//      旧路径（$ROOT/vendor/zlm/MediaServer）起的那只 ZLM **永远匹配不到**；
//   ④ 结果：stop 全程报「已停止」，ZLM 仍占着 51004-51014，
//      紧接着 start 在端口预检处 fail「11 个端口已被占用」，
//      服务停在**半停**状态，而提示把锅甩给了"端口被别人占"。
//
// 本文件钉住四条，缺一条这个坑就会复发：
//   · 判据是「安装根前缀 + /<程序名> 结尾」，**不是**裸程序名匹配
//     （裸名字会杀掉机器上另一套部署的同名 MediaServer —— 本包红线）；
//   · stop_by_pidfile 必须有这一路兜底，且 kill 完要复核、停不掉要明说；
//   · restart 在 stop 与 start 之间要等端口释放（否则立刻预检会误报）；
//   · stop 收尾要用判活函数复核，残留时不得再打「已停止」。
// ---------------------------------------------------------------------------

const script = readFileSync(new URL('../deploy/standalone/uvp-gb28181-ctl.sh', import.meta.url), 'utf8');
const BASH = '/bin/bash';

// fn(name) 从 ctl.sh 里原样抽出函数定义（与 standalone-ctl-updater-lifecycle 同一套做法）。
function fn(name) {
  const oneLine = script.match(new RegExp(`^${name}\\(\\) \\{[^\\n]*\\}$`, 'm'));
  const multiLine = script.match(new RegExp(`^${name}\\(\\) \\{[\\s\\S]*?^\\}`, 'm'));
  const hit = oneLine || multiLine;
  assert.ok(hit, `ctl.sh 里找不到函数 ${name}()`);
  return hit[0];
}

// branches() 把顶层 case 分支切开（正文里嵌着 if/then/else，靠缩进猜边界必错）。
function branches() {
  const lines = script.split('\n');
  let start = -1;
  for (let i = lines.length - 1; i >= 0; i -= 1) {
    if (/^case .+ in$/.test(lines[i])) {
      start = i;
      break;
    }
  }
  assert.ok(start > 0, 'ctl.sh 里找不到顶层 case 分派');
  const body = lines.slice(start + 1);
  const marks = [];
  body.forEach((line, i) => {
    const m = line.match(/^ {2}([a-z][a-z-]*)\)$/);
    if (m) marks.push({ name: m[1], line: i });
  });
  assert.ok(marks.length > 5, `分支解析失败，只认出 ${marks.length} 个分支`);
  const out = {};
  for (let i = 0; i < marks.length; i += 1) {
    const end = i + 1 < marks.length ? marks[i + 1].line : body.length;
    out[marks[i].name] = body.slice(marks[i].line + 1, end).join('\n');
  }
  return out;
}

// ---------------------------------------------------------------- 静态契约 ----

test('判据是「安装根前缀 + /程序名 结尾」，不是裸程序名', () => {
  const body = fn('proc_pids_under_root_by_name');
  assert.match(body, /local prefix="\$\{root%\/\}\/"/, '前缀必须由 $ROOT 规范化（去尾斜杠再加 /）');
  assert.match(body, /"\$prefix"\*\)/, '必须先判 argv 落在安装根之内');
  assert.match(body, /\*\/"\$name"\)/, '再判 argv 以 /<程序名> 结尾');
  // ⛔ 反向断言：不允许出现「只按名字 kill」的退化成因。
  assert.doesNotMatch(body, /kill[^\n]*\$name/, '不能拿程序名直接 kill —— 会误杀别人部署的同名进程');
});

test('stop_by_pidfile 有第三路兜底：跑在切换前路径上的进程也要停掉', () => {
  const body = fn('stop_by_pidfile');
  assert.match(body, /local exe_name="\$\{exe_path##\*\/\}"/, '程序名要用参数展开（精简系统不保证有 basename）');
  assert.match(body, /proc_pids_under_root_by_name "\$ROOT" "\$exe_name"/, '缺少「安装根兜底」这一路');
  // ⛔ 只 kill 不复核等于没修：必须留下「停不掉就说出来」的分支。
  assert.match(body, /仍无法停止/, 'kill 完必须复核，残留时要明确告警');
});

test('zlm_healthy 也认「安装根之下」的同名进程，避免误判成没在跑', () => {
  const body = fn('zlm_healthy');
  assert.match(body, /proc_pids_under_root_by_name "\$ROOT" "\$\{ZLM_BIN##\*\/\}"/,
    '判活只按当前 $ZLM_BIN 匹配时，切换前起的 ZLM 会被判成"没在跑"⇒ start 重复启动');
});

test('restart 必须 stop → 等端口释放 → start', () => {
  const b = branches();
  assert.ok(b.restart, '找不到 restart 分支');
  // ⛔ 只看**非注释行**：分支里的注释会提到 wait_ports_released（"见 xxx 的注释"），
  //   拿整段正文取 indexOf 会把注释当成调用，位置全错。
  const code = b.restart
    .split('\n')
    .filter((line) => !line.trim().startsWith('#'))
    .join('\n');
  const stopIdx = code.indexOf('stop --quiet');
  const waitIdx = code.indexOf('wait_ports_released');
  const startIdx = code.indexOf('"$0" start');
  assert.ok(stopIdx >= 0, 'restart 没有先 stop');
  assert.ok(waitIdx > stopIdx, 'restart 在 stop 之后没有等端口释放');
  assert.ok(startIdx > waitIdx, 'wait_ports_released 必须在 start 之前');
});

test('stop 收尾用判活函数复核，残留时不再打「已停止」', () => {
  const b = branches();
  assert.match(b.stop, /_still=""/, 'stop 分支没有复核变量');
  for (const h of ['redis_healthy', 'zlm_healthy', 'backend_healthy', 'nginx_healthy']) {
    assert.match(b.stop, new RegExp(`\\b${h}\\b`), `stop 的收尾复核漏了 ${h}`);
  }
  assert.match(b.stop, /停止不完整/, '残留时没有明确告警');
  assert.match(b.stop, /_stop_suffix/, '「已停止」这句话必须随复核结果变化');
});

test('wait_ports_released 超时也放行，交给预检报具体占用者', () => {
  const body = fn('wait_ports_released');
  assert.match(body, /return 0/, '超时不能卡死在这里');
  // 只轮询、不 assert 端口是空的：卡住比误报更糟（预检会把占用者名字打出来）。
  assert.doesNotMatch(body, /\bfail\b/, 'wait_ports_released 不该自己 fail');
});

// ---------------------------------------------------------------- 行为契约 ----
// 只在有 /proc 的 Linux 上跑（macOS 没有 /proc，函数按设计返回空）。
const hasProc = existsSync('/proc/self/cmdline');
const skipNoProc = hasProc ? false : '/proc 不可用（非 Linux），行为契约跳过';

function runUnderRoot(root, name) {
  const src = [
    'set -uo pipefail',
    'has_pgrep() { command -v pgrep >/dev/null 2>&1; }',
    fn('proc_pids_under_root_by_name'),
    'proc_pids_under_root_by_name "$1" "$2"',
    '',
  ].join('\n');
  const r = spawnSync(BASH, ['-c', src, 'harness', root, name], { encoding: 'utf8' });
  assert.equal(r.status, 0, `harness 执行失败：${r.stderr}`);
  return r.stdout.split('\n').map((s) => s.trim()).filter(Boolean);
}

test('行为：认得「切换前路径」上的本包进程', { skip: skipNoProc }, () => {
  const root = mkdtempSync(join(tmpdir(), 'uvp-ctl-stale-'));
  // 造出升级前的布局：$ROOT/vendor/zlm/MediaServer（此时 current 已指向新版本目录）
  const oldBin = join(root, 'vendor', 'zlm', 'MediaServer');
  mkdirSync(join(root, 'vendor', 'zlm'), { recursive: true });
  copyFileSync('/bin/sleep', oldBin);
  chmodSync(oldBin, 0o755);
  const child = spawn(oldBin, ['60'], { stdio: 'ignore' });
  try {
    assert.ok(child.pid, '子进程没起来');
    const found = runUnderRoot(root, 'MediaServer');
    assert.ok(found.includes(String(child.pid)),
      `没认出旧路径上的进程 ${child.pid}（拿到：${found.join(',') || '空'}）`);
  } finally {
    child.kill('SIGKILL');
    rmSync(root, { recursive: true, force: true });
  }
});

test('行为：不认安装根之外的同名进程（不误杀别人部署的）', { skip: skipNoProc }, () => {
  const root = mkdtempSync(join(tmpdir(), 'uvp-ctl-own-'));
  const other = mkdtempSync(join(tmpdir(), 'uvp-ctl-other-'));
  const otherBin = join(other, 'MediaServer');
  copyFileSync('/bin/sleep', otherBin);
  chmodSync(otherBin, 0o755);
  const child = spawn(otherBin, ['60'], { stdio: 'ignore' });
  try {
    assert.ok(child.pid, '子进程没起来');
    const found = runUnderRoot(root, 'MediaServer');
    assert.ok(!found.includes(String(child.pid)),
      `误把安装根之外的 ${child.pid} 算成了本包进程（拿到：${found.join(',')}）`);
  } finally {
    child.kill('SIGKILL');
    rmSync(root, { recursive: true, force: true });
    rmSync(other, { recursive: true, force: true });
  }
});
