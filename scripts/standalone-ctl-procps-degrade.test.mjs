import assert from 'node:assert/strict';
import { spawn, spawnSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import net from 'node:net';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import test from 'node:test';

// ---------------------------------------------------------------------------
// 契约：绿色包在「没有 pgrep（未装 procps）」的机器上，判活与停止都必须照常工作。
//
// ⛔⛔ 真实事故（2026-10-12，python:3.11-slim 容器现场）：
//   · zlm_healthy / nginx_healthy 最后一步是 `pgrep -f <绝对路径>`，容器没 procps
//     ⇒ 命令不存在、输出为空 ⇒ 判活恒为「没起来」。而 ZLM 明明在 51004 监听
//     （ss -lntp 可见），脚本却报「ZLM 未能在预期时间内监听端口 51004」。
//   · 紧接着 stop 报「已停止」而 ZLM 还在跑 —— ZLM daemon 化后 pidfile 失效，
//     按路径兜底同样只有 pgrep 一条路 ⇒ 兜底失效 ⇒ 下次 start 被端口预检拦下。
//
// 修法 = pgrep 只作择优路径，缺了就改读 /proc/<pid>/cmdline（内核提供、零外部命令）。
// 本文件把这条钉住：
//   · 缺 pgrep + 端口通 + 进程在 ⇒ 判为健康
//   · 缺 pgrep + 端口通 + 进程不在 ⇒ 判不健康（不是无条件放行）
//   · 缺 pgrep + 端口不通 ⇒ 判不健康
//   · 有 pgrep + 进程不在 ⇒ 判不健康（进程判定没被掏空）
//   · 缺 pgrep 时 /proc 扫描能真的找到进程（stop 兜底的前提）
// ---------------------------------------------------------------------------

const script = readFileSync(new URL('../deploy/standalone/uvp-gb28181-ctl.sh', import.meta.url), 'utf8');
const BASH = '/bin/bash';
const isLinux = process.platform === 'linux';

// fn(name) 从 ctl.sh 里原样抽出函数定义（单行 `f() { ... }` 与多行两种形态）。
function fn(name) {
  const oneLine = script.match(new RegExp(`^${name}\\(\\) \\{[^\\n]*\\}$`, 'm'));
  const multiLine = script.match(new RegExp(`^${name}\\(\\) \\{[\\s\\S]*?^\\}`, 'm'));
  const hit = oneLine || multiLine;
  assert.ok(hit, `ctl.sh 里找不到函数 ${name}()`);
  return hit[0];
}

const HELPERS = [
  fn('has_pgrep'),
  fn('proc_pids_by_exe'),
  fn('proc_alive_by_exe'),
  '_no_pgrep_noted=""',
  fn('note_no_pgrep_once'),
].join('\n');

const ZLM = fn('zlm_healthy');
const NGINX = fn('nginx_healthy');

// openPort 返回一个**真的在监听**的 127.0.0.1 端口（保持打开）。
function openPort() {
  return new Promise((resolve, reject) => {
    const server = net.createServer();
    server.once('error', reject);
    server.listen(0, '127.0.0.1', () => resolve({ server, port: server.address().port }));
  });
}

// closedPort 拿一个内核刚分配又立即释放的端口号（当下没人听）。
function closedPort() {
  return new Promise((resolve, reject) => {
    const server = net.createServer();
    server.once('error', reject);
    server.listen(0, '127.0.0.1', () => {
      const port = server.address().port;
      server.close(() => resolve(port));
    });
  });
}

// EMPTY_PATH 只作 PATH 用：让 `command -v pgrep` 一定失败。
const EMPTY_PATH = join(tmpdir(), `uvp-nopgrep-${process.pid}`);

const PGREP = spawnSync(BASH, ['-c', 'command -v pgrep'], { encoding: 'utf8' }).stdout.trim();
const REAL_PGREP_PATH = PGREP ? dirname(PGREP) : null;

// spawnFake 起一个 cmdline 里带指定绝对路径的进程（冒充「本包的 MediaServer」）。
// 用 `exec -a` 伪造 argv[0]，这样 /proc/<pid>/cmdline 里就会出现该路径 ——
// 正是 proc_pids_by_exe 要找的东西。
function spawnFake(exePath) {
  return spawn(BASH, ['-c', `exec -a ${JSON.stringify(exePath)} sleep 30`], { stdio: 'ignore' });
}

const FAKE_ZLM = '/fake-pkg/vendor/zlm/MediaServer';

// ⛔ 目标路径必须走**环境变量**传进 harness，不能写进 harness 正文。
//   否则 `bash -c '<正文>'` 的 argv 里就等于写着这个路径，而 proc_pids_by_exe
//   是「cmdline 包含该路径」即命中 —— 它会把自己（以及命令替换/进程替换 fork
//   出来的临时 shell）当成目标进程，用例就会假红。真实调用不存在这个形态
//   （ctl.sh 是从文件执行的，正文不在 argv 里）。
function runCtl({ path, zlmPort, nginxPort, zlmBin = '/nonexistent-pkg/vendor/zlm/MediaServer', nginxBin = '/nonexistent-pkg/vendor/nginx/sbin/nginx' }) {
  const body = `
set -uo pipefail
log()  { printf '[log] %s\\n' "$*"; }
ok()   { :; }
det()  { :; }
fail() { printf '[fail] %s\\n' "$*" >&2; return 1; }
ZLM_HTTP_PORT=${zlmPort}
ZLM_BIN="$UVP_T_ZLM_BIN"
NGINX_HTTPS_PORT=${nginxPort}
# ⛔ nginx_healthy 会按 NGINX_HTTPS_ENABLED 选探测端口（关掉 HTTPS 时探明文口）。
#   以前这个变量由 ctl.sh 里的 https 状态探测块赋值，harness 抄不到 ⇒
#   set -u 下直接 unbound variable（127），看起来像"断言失败"、实则是 harness 缺变量。
#   2026-10-19 起该值改由 bin/uvp-setup https-state 给出，这里显式钉成"开着 HTTPS"。
NGINX_HTTPS_ENABLED=1
NGINX_HTTP_PORT=${nginxPort}
NGINX_BIN="$UVP_T_NGINX_BIN"
${HELPERS}
${ZLM}
${NGINX}
printf 'PIDS=%s\\n' "$(proc_pids_by_exe "$ZLM_BIN")"
zlm_healthy;   printf 'ZLM_RC=%s\\n' "$?"
nginx_healthy; printf 'NGINX_RC=%s\\n' "$?"
`;
  return spawnSync(BASH, ['-c', body], {
    encoding: 'utf8',
    env: {
      PATH: path,
      HOME: process.env.HOME ?? '/tmp',
      UVP_T_ZLM_BIN: zlmBin,
      UVP_T_NGINX_BIN: nginxBin,
    },
  });
}

const rc = (stdout, key) => Number(stdout.match(new RegExp(`${key}_RC=(\\d+)`))?.[1]);

test('没有 pgrep 时：端口通 + 进程在 ⇒ 判为健康（不再误报"进程没起来"）', { skip: !isLinux }, async () => {
  const { server, port } = await openPort();
  const fake = spawnFake(FAKE_ZLM);
  try {
    const r = runCtl({ path: EMPTY_PATH, zlmPort: port, nginxPort: port, zlmBin: FAKE_ZLM });
    assert.equal(r.status, 0, r.stderr);
    assert.equal(rc(r.stdout, 'ZLM'), 0, `ZLM 应判健康，实际输出：\n${r.stdout}`);
    assert.match(r.stdout, /没有 pgrep/, '应告知用户本机缺 pgrep（走 /proc 慢路径）');
    assert.match(r.stdout, /procps/, '提示里应给出安装 procps 的建议');
  } finally {
    fake.kill('SIGKILL');
    server.close();
  }
});

test('没有 pgrep 时：端口通但进程不在 ⇒ 仍判不健康（不是无条件放行）', { skip: !isLinux }, async () => {
  const { server, port } = await openPort();
  try {
    const r = runCtl({ path: EMPTY_PATH, zlmPort: port, nginxPort: port });
    assert.equal(r.status, 0, r.stderr);
    assert.equal(rc(r.stdout, 'ZLM'), 1, `进程不存在时必须为 1，实际输出：\n${r.stdout}`);
  } finally {
    server.close();
  }
});

test('没有 pgrep 时：端口不通 ⇒ 判不健康（进程在也一样）', { skip: !isLinux }, async () => {
  const port = await closedPort();
  const fake = spawnFake(FAKE_ZLM);
  try {
    const r = runCtl({ path: EMPTY_PATH, zlmPort: port, nginxPort: port, zlmBin: FAKE_ZLM });
    assert.equal(r.status, 0, r.stderr);
    assert.equal(rc(r.stdout, 'ZLM'), 1, `端口不通时必须为 1，实际输出：\n${r.stdout}`);
  } finally {
    fake.kill('SIGKILL');
  }
});

test('没有 pgrep 时：/proc 扫描真的能找到进程（stop 兜底的前提）', { skip: !isLinux }, async () => {
  const fake = spawnFake(FAKE_ZLM);
  try {
    const r = runCtl({ path: EMPTY_PATH, zlmPort: 1, nginxPort: 1, zlmBin: FAKE_ZLM });
    const pids = r.stdout.match(/PIDS=(\d+)/)?.[1];
    assert.equal(pids, String(fake.pid), `应找到伪造进程 ${fake.pid}，实际输出：\n${r.stdout}`);
  } finally {
    fake.kill('SIGKILL');
  }
});

test('有 pgrep 时：端口通但本包进程不存在 ⇒ 仍判不健康（进程判定没被掏空）', { skip: !REAL_PGREP_PATH }, async () => {
  const { server, port } = await openPort();
  try {
    const r = runCtl({ path: REAL_PGREP_PATH, zlmPort: port, nginxPort: port });
    assert.equal(r.status, 0, r.stderr);
    assert.equal(rc(r.stdout, 'ZLM'), 1, `有 pgrep 时必须按进程判活，实际输出：\n${r.stdout}`);
    assert.equal(rc(r.stdout, 'NGINX'), 1, `有 pgrep 时必须按进程判活，实际输出：\n${r.stdout}`);
    assert.doesNotMatch(r.stdout, /没有 pgrep/, '有 pgrep 时不应出现慢路径提示');
  } finally {
    server.close();
  }
});

test('/proc 兜底与 pgrep 择优路径都在代码里（防以后被顺手删掉）', () => {
  assert.match(fn('proc_pids_by_exe'), /\/proc\/\[0-9\]\*/, 'proc_pids_by_exe 必须遍历 /proc');
  assert.match(fn('proc_pids_by_exe'), /cmdline/, 'proc_pids_by_exe 必须读 cmdline');
  assert.match(fn('proc_pids_by_exe'), /if has_pgrep; then/, 'proc_pids_by_exe 应优先用 pgrep');
  assert.match(fn('has_pgrep'), /command -v pgrep/, 'has_pgrep 必须真的去探测 pgrep 命令');
  for (const [name, body] of [['zlm_healthy', ZLM], ['nginx_healthy', NGINX]]) {
    assert.match(body, /proc_alive_by_exe/, `${name} 必须用 proc_alive_by_exe（而非裸 pgrep）`);
    assert.doesNotMatch(body, /if ! has_pgrep/, `${name} 不应再有「缺 pgrep 就放行」的分支`);
  }
  assert.match(fn('stop_by_pidfile'), /proc_pids_by_exe/, 'stop 的按路径兜底必须用 proc_pids_by_exe');
  assert.doesNotMatch(fn('stop_by_pidfile'), /if ! has_pgrep/, 'stop 不应再因缺 pgrep 直接放弃兜底');
});
