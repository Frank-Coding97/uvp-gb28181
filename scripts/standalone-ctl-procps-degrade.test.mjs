import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import net from 'node:net';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import test from 'node:test';

// ---------------------------------------------------------------------------
// 契约：绿色包在「没有 pgrep（未装 procps）」的机器上必须能正常启动。
//
// ⛔⛔ 真实事故（2026-10-12，客户容器现场）：zlm_healthy / nginx_healthy 的最后
//   一步是 `pgrep -f <绝对路径>` 判活，而最小化容器（python:*-slim / alpine）
//   默认不带 procps ⇒ 命令不存在、退出码 127、输出为空 ⇒ 判活**恒为"没起来"**。
//   症状与事实矛盾：`ss -lntp` 显示 51004~51014 全在监听，脚本却报
//   「ZLM 未能在预期时间内监听端口 51004」。
//
// 修法 = 探测 pgrep 是否存在，缺则「只看端口 + 提示一次」降级。
// 本文件用**真跑**（不是静态 grep）把这条钉住：
//   · 缺 pgrep + 端口通 ⇒ 放行（不得再误判）
//   · 缺 pgrep + 端口不通 ⇒ 仍拒绝（降级 ≠ 无条件放行）
//   · 有 pgrep + 端口通但进程不存在 ⇒ 仍拒绝（不得把进程判定整个掏空）
//   · 提示只打一次（轮询里被反复调用，不能刷屏）
// ---------------------------------------------------------------------------

const script = readFileSync(new URL('../deploy/standalone/uvp-gb28181-ctl.sh', import.meta.url), 'utf8');
const BASH = '/bin/bash';

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
  script.match(/^_no_pgrep_warned=""\n(?:warn_no_pgrep_once\(\) \{[\s\S]*?^\})/m)?.[0],
].join('\n');
assert.ok(HELPERS.includes('warn_no_pgrep_once'), 'ctl.sh 里找不到 _no_pgrep_warned/warn_no_pgrep_once');

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

// emptyPathDir 只作 PATH 用：让 `command -v pgrep` 一定失败。
const EMPTY_PATH = join(tmpdir(), `uvp-nopgrep-${process.pid}`);

const PGREP = spawnSync(BASH, ['-c', 'command -v pgrep'], { encoding: 'utf8' }).stdout.trim();
const REAL_PGREP_PATH = PGREP ? dirname(PGREP) : null;

function runCtlFunctions({ path, zlmPort, nginxPort }) {
  const body = `
set -uo pipefail
log()  { printf '[log] %s\\n' "$*"; }
ok()   { :; }
det()  { :; }
fail() { printf '[fail] %s\\n' "$*" >&2; return 1; }
ZLM_HTTP_PORT=${zlmPort}
ZLM_BIN=/nonexistent-pkg/vendor/zlm/MediaServer
NGINX_HTTPS_PORT=${nginxPort}
NGINX_BIN=/nonexistent-pkg/vendor/nginx/sbin/nginx
${HELPERS}
${ZLM}
${NGINX}
zlm_healthy;   printf 'ZLM_RC=%s\\n' "$?"
nginx_healthy; printf 'NGINX_RC=%s\\n' "$?"
`;
  return spawnSync(BASH, ['-c', body], {
    encoding: 'utf8',
    env: { PATH: path, HOME: process.env.HOME ?? '/tmp' },
  });
}

const rc = (stdout, key) => Number(stdout.match(new RegExp(`${key}_RC=(\\d+)`))?.[1]);

test('没有 pgrep 时：端口通 ⇒ 判为健康（不再误报"进程没起来"）', async () => {
  const { server, port } = await openPort();
  try {
    const r = runCtlFunctions({ path: EMPTY_PATH, zlmPort: port, nginxPort: port });
    assert.equal(r.status, 0, r.stderr);
    assert.equal(rc(r.stdout, 'ZLM'), 0, `ZLM 应判健康，实际输出：\n${r.stdout}`);
    assert.equal(rc(r.stdout, 'NGINX'), 0, `nginx 应判健康，实际输出：\n${r.stdout}`);
  } finally {
    server.close();
  }
});

test('没有 pgrep 时：提示只打一次（两个函数都降级也只提示 1 次）', async () => {
  const { server, port } = await openPort();
  try {
    const r = runCtlFunctions({ path: EMPTY_PATH, zlmPort: port, nginxPort: port });
    const warnings = r.stdout.match(/没有 pgrep/g) ?? [];
    assert.equal(warnings.length, 1, `降级提示应只出现 1 次，实际 ${warnings.length} 次：\n${r.stdout}`);
    assert.match(r.stdout, /procps/, '提示里应告诉用户装 procps');
  } finally {
    server.close();
  }
});

test('没有 pgrep 时：端口不通 ⇒ 仍然判不健康（降级不等于无条件放行）', async () => {
  const port = await closedPort();
  const r = runCtlFunctions({ path: EMPTY_PATH, zlmPort: port, nginxPort: port });
  assert.equal(r.status, 0, r.stderr);
  assert.equal(rc(r.stdout, 'ZLM'), 1, `端口不通时必须仍为 1，实际输出：\n${r.stdout}`);
  assert.equal(rc(r.stdout, 'NGINX'), 1, `端口不通时必须仍为 1，实际输出：\n${r.stdout}`);
});

test('有 pgrep 时：端口通但本包进程不存在 ⇒ 仍判不健康（进程判定没被掏空）', { skip: !REAL_PGREP_PATH }, async () => {
  const { server, port } = await openPort();
  try {
    const r = runCtlFunctions({ path: REAL_PGREP_PATH, zlmPort: port, nginxPort: port });
    assert.equal(r.status, 0, r.stderr);
    assert.equal(rc(r.stdout, 'ZLM'), 1, `有 pgrep 时必须按进程判活，实际输出：\n${r.stdout}`);
    assert.equal(rc(r.stdout, 'NGINX'), 1, `有 pgrep 时必须按进程判活，实际输出：\n${r.stdout}`);
    assert.doesNotMatch(r.stdout, /没有 pgrep/, '有 pgrep 时不应出现降级提示');
  } finally {
    server.close();
  }
});

test('降级逻辑本身在代码里（防以后被顺手删掉）', () => {
  for (const [name, body] of [['zlm_healthy', ZLM], ['nginx_healthy', NGINX]]) {
    assert.match(body, /has_pgrep/, `${name} 缺少 has_pgrep 探测`);
    assert.match(body, /warn_no_pgrep_once/, `${name} 缺少降级提示`);
  }
  assert.match(HELPERS, /command -v pgrep/, 'has_pgrep 必须真的去探测 pgrep 命令');
});
