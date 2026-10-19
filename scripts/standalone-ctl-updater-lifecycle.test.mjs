import assert from 'node:assert/strict';
import { spawn, spawnSync } from 'node:child_process';
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';

// ---------------------------------------------------------------------------
// 契约：升级助手（uvp-updater）的「生命周期」必须同时满足两件互相拉扯的事。
//
// ① **业务 stop 不许停它**（设计契约，不能翻）：
//    升级流程本身就是「先停业务 → 助手替换文件 → 再起业务」，助手被一起杀掉
//    等于升级断在半路。所以 `stop` 只停业务四件套。
//
// ② **但它不能被"看不见"**（2026-10-20 在 220 上真踩到）：
//    `ctl.sh stop` 之后 51000-51064 全释放、`status` 四行全是「未运行」，
//    可 ps 里 `uvp-updater serve` 还在跑（ppid=1 常驻）。用户以为整套都停了。
//    ⇒ 补维度：status 必须显示它、stop 必须提示它、并给一个显式的 stop-updater。
//
// 本文件钉住四条：
//   · `stop)` 分支**不**调 stop_updater（语义没被翻）；
//   · `stop-updater)` / `stop-all)` 存在且真的会停；
//   · `status` 会输出「升级助手」这一行；
//   · 判活不能只看 socket（残留 socket 在进程死后依然存在）；
//   · systemd 托管时走 systemctl 而不是 kill（否则 Restart=on-failure 会拉回来）。
// ---------------------------------------------------------------------------

const script = readFileSync(new URL('../deploy/standalone/uvp-gb28181-ctl.sh', import.meta.url), 'utf8');
const installScript = readFileSync(
  new URL('../deploy/standalone/uvp-gb28181-service-install.sh', import.meta.url),
  'utf8',
);
const BASH = '/bin/bash';

// fn(name) 从 ctl.sh 里原样抽出函数定义（单行 `f() { ... }` 与多行两种形态）。
function fn(name) {
  const oneLine = script.match(new RegExp(`^${name}\\(\\) \\{[^\\n]*\\}$`, 'm'));
  const multiLine = script.match(new RegExp(`^${name}\\(\\) \\{[\\s\\S]*?^\\}`, 'm'));
  const hit = oneLine || multiLine;
  assert.ok(hit, `ctl.sh 里找不到函数 ${name}()`);
  return hit[0];
}

// branches() 把 case 分支按 `  name)` 切开，返回 { 分支名: 分支正文 }。
// ⛔ 不能用简单的行正则：分支正文里嵌着 if/then/else，靠缩进猜边界必错。
//   按「两个字母间距的分支标签行」切，其余整段归上一个分支。
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

test('stop 只停业务四件套，不许停升级助手（设计语义未被翻转）', () => {
  const b = branches();
  assert.ok(b.stop, '找不到 stop 分支');
  // ⛔ 只看**非注释行**：分支里的注释会提到 stop_updater（"契约见 stop_updater 上方"），
  //   拿整段正文匹配会把注释当成调用，假红。
  const calls = b.stop
    .split('\n')
    .filter((line) => !line.trim().startsWith('#'))
    .join('\n');
  assert.doesNotMatch(calls, /stop_updater/, 'stop 分支调用了 stop_updater —— 升级会在半路被打断');
  for (const needed of ['stop_nginx', 'stop_backend', 'stop_zlm', 'stop_redis']) {
    assert.match(calls, new RegExp(needed), `stop 分支漏了 ${needed}`);
  }
});

test('stop 分支在助手还活着时必须明说，不能只打「已停止」', () => {
  const b = branches();
  assert.match(b.stop, /updater_running/, 'stop 分支没有判断助手是否还在跑');
  assert.match(b.stop, /stop-updater/, 'stop 分支没有告诉用户怎么一并停掉助手');
});

test('提供了 stop-updater 与 stop-all 两个显式入口', () => {
  const b = branches();
  assert.ok(b['stop-updater'], '缺少 stop-updater 分支');
  assert.match(b['stop-updater'], /stop_updater/);
  assert.ok(b['stop-all'], '缺少 stop-all 分支');
  for (const needed of ['stop_nginx', 'stop_backend', 'stop_zlm', 'stop_redis', 'stop_updater']) {
    assert.match(b['stop-all'], new RegExp(needed), `stop-all 分支漏了 ${needed}`);
  }
});

test('status 会输出升级助手一行（不让常驻进程隐形）', () => {
  const b = branches();
  assert.match(b.status, /升级助手/, 'status 没有显示升级助手');
  assert.match(b.status, /updater_status_line/, 'status 应复用 updater_status_line');
});

test('systemd 托管时走 systemctl stop，不能直接 kill', () => {
  const body = fn('stop_updater');
  assert.match(body, /updater_systemd_managed/, 'stop_updater 没有区分 systemd 托管');
  assert.match(body, /systemctl stop/, 'systemd 托管时必须走 systemctl stop');
  // kill 只允许出现在 else（非托管）分支里。
  const sysIdx = body.indexOf('systemctl stop');
  const killIdx = body.indexOf('stop_by_pidfile');
  assert.ok(sysIdx > 0 && killIdx > sysIdx, 'stop_by_pidfile 应在 systemctl 分支之后（作为 else 兜底）');
});

test('单元名在 ctl.sh 与 service-install.sh 里逐字一致', () => {
  const a = script.match(/^UPDATER_UNIT_NAME="([^"]+)"/m)?.[1];
  const b = installScript.match(/^UPDATER_UNIT_NAME="([^"]+)"/m)?.[1];
  assert.ok(a, 'ctl.sh 里没有 UPDATER_UNIT_NAME');
  assert.equal(a, b, 'ctl.sh 与 service-install.sh 的升级助手单元名不一致');
});

test('用法提示把两个新入口列出来了', () => {
  assert.match(script, /stop-all\|restart/, '用法行里没有 stop-all');
  assert.match(script, /stop-updater\|serve-updater/, '用法行里没有 stop-updater');
});

// ---------------------------------------------------------------- 行为契约 ----

const HELPERS = [
  fn('has_pgrep'),
  fn('proc_pids_by_exe'),
  fn('proc_alive_by_exe'),
  '_no_pgrep_noted=""',
  fn('note_no_pgrep_once'),
  fn('stop_by_pidfile'),
  fn('updater_pid'),
  fn('updater_running'),
  fn('updater_systemd_managed'),
  fn('updater_status_line'),
  fn('stop_updater'),
].join('\n');

// makePkg 造一个最小安装目录：真的可执行文件（停进程时兜底要求 -x）+ run/ 目录。
function makePkg() {
  const root = mkdtempSync(join(tmpdir(), 'uvp-updater-life-'));
  const bin = join(root, 'updater', 'uvp-updater');
  mkdirSync(join(root, 'updater'), { recursive: true });
  mkdirSync(join(root, 'run'), { recursive: true });
  // 桩程序：只能被 exec，不需要真的实现协议。
  writeFileSync(bin, '#!/bin/sh\nexit 0\n');
  chmodSync(bin, 0o755);
  return {
    root,
    bin,
    pidFile: join(root, 'run', 'updater.pid'),
    socket: join(root, 'run', 'updater.sock'),
    cleanup: () => rmSync(root, { recursive: true, force: true }),
  };
}

// spawnFakeUpdater 起一个 cmdline 里带 $bin 绝对路径的进程（冒充助手常驻）。
// 用 `exec -a` 伪造 argv[0]，正是 proc_pids_by_exe 要找的东西。
// ⛔ 必须等 exec 真正生效再返回：pgrep -f 是在 exec 之后才能看到新 argv 的，
//   不等就是「进程刚 fork、argv 还是 bash -c …」的假红。
async function spawnFakeUpdater(bin) {
  const child = spawn(BASH, ['-c', `exec -a ${JSON.stringify(bin)} sleep 30`], { stdio: 'ignore' });
  for (let i = 0; i < 40; i += 1) {
    const hit = spawnSync(BASH, ['-c', `pgrep -f ${JSON.stringify(bin)} || true`], { encoding: 'utf8' });
    if (hit.stdout.includes(String(child.pid))) return child;
    await new Promise((r) => setTimeout(r, 25));
  }
  return child;
}

// runHarness 在抽出来的函数上下文里跑一段动作脚本。
// ⛔ 目标路径一律走环境变量：写进正文会出现在 `bash -c` 的 argv 里，
//   而 proc_pids_by_exe 是「cmdline 含该路径」即命中 ⇒ 会把自己当目标（假红/假绿）。
function runHarness(pkg, action) {
  const body = `
set -uo pipefail
log()  { printf '[log] %s\\n' "$*"; }
ok()   { printf '[ok] %s\\n' "$*"; }
det()  { printf '[det] %s\\n' "$*"; }
fail() { printf '[fail] %s\\n' "$*" >&2; exit 1; }
UPDATER_UNIT_NAME="uvp-gb28181-updater.service"
UPDATER_PID_FILE="$UVP_T_PIDFILE"
UPDATER_SOCKET="$UVP_T_SOCKET"
UPDATER_BIN="$UVP_T_BIN"
${HELPERS}
${action}
`;
  return spawnSync(BASH, ['-c', body], {
    encoding: 'utf8',
    env: {
      PATH: process.env.PATH ?? '/usr/bin:/bin',
      HOME: process.env.HOME ?? '/tmp',
      UVP_T_PIDFILE: pkg.pidFile,
      UVP_T_SOCKET: pkg.socket,
      UVP_T_BIN: pkg.bin,
    },
  });
}

const alive = (pid) => {
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
};

test('pid 文件有效时：判活为真、status 带 PID、stop_updater 真能停掉', async () => {
  const pkg = makePkg();
  const fake = await spawnFakeUpdater(pkg.bin);
  try {
    writeFileSync(pkg.pidFile, `${fake.pid}\n`);
    const probe = runHarness(
      pkg,
      `
updater_running && printf 'RUNNING=1\\n' || printf 'RUNNING=0\\n'
printf 'LINE=%s\\n' "$(updater_status_line)"
`,
    );
    assert.equal(probe.status, 0, probe.stderr);
    assert.match(probe.stdout, /RUNNING=1/, '助手在跑却判成未运行');
    assert.match(probe.stdout, new RegExp(`LINE=运行中（PID ${fake.pid}）`));

    const stop = runHarness(pkg, 'stop_updater');
    assert.equal(stop.status, 0, stop.stderr);
    assert.match(stop.stdout, /升级助手已停止/);
    assert.equal(existsSync(pkg.pidFile), false, 'stop_updater 后 pid 文件应被清掉');
    await new Promise((r) => setTimeout(r, 200));
    assert.equal(alive(fake.pid), false, 'stop_updater 后进程应该真的没了');
  } finally {
    pkg.cleanup();
    try {
      fake.kill('SIGKILL');
    } catch {
      /* 已经退了 */
    }
  }
});

test('pid 文件丢了但进程还在：仍判运行中，且兜底能停掉（不能"什么都没停"）', async () => {
  const pkg = makePkg();
  const fake = await spawnFakeUpdater(pkg.bin);
  try {
    // 故意不写 pid 文件 —— 对应「rm -rf run/ 后重新解压」的现场。
    const probe = runHarness(
      pkg,
      `updater_running && printf 'RUNNING=1\\n' || printf 'RUNNING=0\\n'`,
    );
    assert.equal(probe.status, 0, probe.stderr);
    assert.match(probe.stdout, /RUNNING=1/, 'pid 文件缺失时按可执行路径应能找到助手');

    const stop = runHarness(pkg, 'stop_updater');
    assert.equal(stop.status, 0, stop.stderr);
    await new Promise((r) => setTimeout(r, 200));
    assert.equal(alive(fake.pid), false, '兜底路径没能停掉助手 —— 会留下看不见的常驻进程');
  } finally {
    pkg.cleanup();
    try {
      fake.kill('SIGKILL');
    } catch {
      /* 已经退了 */
    }
  }
});

test('只剩残留 socket、进程已死：必须判「未运行」（不能永远报运行中）', () => {
  const pkg = makePkg();
  try {
    // 造一个普通文件冒充残留 socket 路径（unlink 后进程也没了）。
    writeFileSync(pkg.socket, '');
    const probe = runHarness(
      pkg,
      `
updater_running && printf 'RUNNING=1\\n' || printf 'RUNNING=0\\n'
printf 'LINE=%s\\n' "$(updater_status_line)"
`,
    );
    assert.equal(probe.status, 0, probe.stderr);
    assert.match(probe.stdout, /RUNNING=0/, '只看 socket 判活 ⇒ 进程死后会永远报运行中');
  } finally {
    pkg.cleanup();
  }
});

test('什么都没跑时 stop_updater 是幂等的（rc=0，不报错）', () => {
  const pkg = makePkg();
  try {
    const stop = runHarness(pkg, 'stop_updater');
    assert.equal(stop.status, 0, `空跑 stop_updater 不应失败：${stop.stderr}`);
    assert.match(stop.stdout, /升级助手已停止/);
  } finally {
    pkg.cleanup();
  }
});
