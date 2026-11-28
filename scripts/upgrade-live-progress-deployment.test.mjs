import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { chmodSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';

const read = path => readFileSync(new URL(`../${path}`, import.meta.url), 'utf8').replace(/^\uFEFF/, '').replace(/\r\n/g, '\n');
const linux = read('deploy/standalone/uvp-gb28181-ctl.sh');
const windows = read('deploy/standalone-windows/uvp-gb28181-ctl.ps1');
const fn = (source, name, ps = false) => {
  const expression = ps ? `^function ${name} \\{[\\s\\S]*?^\\}` : `^${name}\\(\\) \\{[\\s\\S]*?^\\}`;
  const match = source.match(new RegExp(expression, 'm'));
  assert.ok(match, `missing ${name}`);
  return match[0];
};

const configs = [
  ['deploy/standalone/nginx.conf.template', '@ROOT@/updater-progress/', 2],
  ['deploy/standalone-windows/nginx.conf.template', '@ROOT@/updater-progress/', 2],
  ['deploy/compose/nginx/nginx.conf.template', '/opt/uvp/updater-progress/', 2],
  ['deploy/test/nginx.conf', '/opt/uvp/updater-progress/', 1],
  ['deploy/test/uvp-frontend.nginx.conf', '/opt/uvp-gb28181/updater-progress/', 1]
];
for (const [path, root, count] of configs) {
  test(`${path}: progress is bounded, read-only and never cached or logged`, () => {
    const source = read(path);
    const blocks = [...source.matchAll(/location ~ "\^\/api\/system\/upgrade\/progress\/[^\n]+\{\n([\s\S]*?)^\s{4,8}\}/gm)].map(match => match[1]);
    assert.equal(blocks.length, count);
    for (const block of blocks) {
      assert.ok(block.includes(`alias "${root}$uvp_upgrade_progress_token.json";`));
      assert.match(block, /limit_except GET HEAD \{ deny all; \}/);
      assert.match(block, /default_type application\/json/);
      assert.match(block, /Cache-Control "no-store" always/);
      assert.match(block, /access_log off/);
      assert.match(block, /error_log (?:\/dev\/null|nul) crit/);
      assert.match(block, /autoindex off/);
      assert.doesNotMatch(block, /proxy_pass/);
    }
    assert.equal((source.match(/location = \/api\/system\/upgrade\/progress \{ return 404; \}/g) || []).length, count);
    assert.equal((source.match(/location \/api\/system\/upgrade\/progress\/ \{ return 404; \}/g) || []).length, count);
    assert.equal((source.match(/\(\?<uvp_upgrade_progress_token>\[0-9a-f\]\{64\}\)\\\.json\$/g) || []).length, count);
  });
}

test('Linux upgrade stop keeps the frontend and helper while stopping business components', () => {
  const match = linux.match(/^  stop-for-upgrade\)\n([\s\S]*?)^    ;;/m);
  assert.ok(match, 'missing stop-for-upgrade');
  const probe = spawnSync('/bin/bash', ['-c', `set -e\n${['backend', 'zlm', 'redis', 'firewall_agent', 'nginx', 'updater'].map(name => `stop_${name}() { echo ${name}; }`).join('\n')}\nlog() { :; }\nredis_healthy() { return 1; }\nzlm_healthy() { return 1; }\nbackend_healthy() { return 1; }\n${match[1]}`], { encoding: 'utf8' });
  assert.equal(probe.status, 0, probe.stderr);
  assert.deepEqual(probe.stdout.trim().split('\n'), ['backend', 'zlm', 'redis', 'firewall_agent']);
});

test('Linux reuses only this installation nginx and reloads the switched frontend root', () => {
  assert.match(fn(linux, 'nginx_healthy'), /nginx_running_exe/);
  assert.match(fn(linux, 'nginx_running_exe'), /NGINX_PID_FILE/);
  assert.match(fn(linux, 'nginx_running_exe'), /releases\/\*\/vendor\/nginx\/sbin\/nginx/);
  const start = fn(linux, 'start_nginx');
  assert.match(start, /-s reload/);
  assert.ok(start.indexOf('-t') < start.indexOf('-s reload'), 'config validation must precede reload');
  assert.match(fn(linux, 'check_ports_free'), /nginx_healthy/);
});

test('Windows upgrade stop preserves frontend and helper; frontend stop is separate', () => {
  const stop = fn(windows, 'Invoke-Stop', true);
  assert.match(stop, /param\(\[switch\]\$ForUpgrade\)/);
  assert.match(stop, /if \(-not \$ForUpgrade\) \{ Invoke-StopFrontend \}/);
  assert.match(stop, /if \(-not \$ForUpgrade\) \{ Stop-Updater \}/);
  assert.match(windows, /'stop-for-upgrade'\s+\{ Invoke-Stop -ForUpgrade/);
  assert.match(windows, /'stop-frontend'\s+\{[^\n]*Invoke-StopFrontend/);
  const frontend = fn(windows, 'Invoke-StopFrontend', true);
  assert.match(frontend, /Get-PidByExe -ExePath \$NginxExe/);
  assert.doesNotMatch(frontend, /Stop-Updater|BackendPidFile|ZlmPidFile|RedisPidFile/);
  const bytes = readFileSync(new URL('../deploy/standalone-windows/uvp-gb28181-ctl.ps1', import.meta.url));
  assert.deepEqual([...bytes.subarray(0, 3)], [0xef, 0xbb, 0xbf]);
  const raw = bytes.toString('utf8');
  assert.equal((raw.match(/\n/g) || []).length, (raw.match(/\r\n/g) || []).length);
});

for (const path of ['deploy/compose/compose.yml', 'deploy/test/compose.yml']) {
  test(`${path}: frontend mounts only progress snapshots read-only`, () => {
    const frontend = read(path).split(/^  frontend:/m)[1];
    assert.ok(frontend);
    assert.match(frontend, /updater-progress:\/opt\/uvp\/updater-progress:ro/);
  });
}


test('Linux nginx reuse accepts the prior release executable but rejects another installation', () => {
  const directory = mkdtempSync(join(tmpdir(), 'uvp-nginx-owner-'));
  try {
    const body = `set -eu
ROOT="$UVP_TEST_ROOT"
NGINX_PID_FILE="$ROOT/nginx.pid"
NGINX_BIN="$ROOT/releases/new/vendor/nginx/sbin/nginx"
printf '%s' "$$" > "$NGINX_PID_FILE"
readlink() { printf '%s\\n' "$UVP_TEST_EXE"; }
proc_alive_by_exe() { return 1; }
${fn(linux, 'nginx_running_exe')}
nginx_running_exe`;
    for (const [exe, expected] of [
      [`${directory}/releases/old/vendor/nginx/sbin/nginx`, 0],
      [`${directory}/releases/old/vendor/nginx/sbin/nginx (deleted)`, 0],
      ['/other-install/vendor/nginx/sbin/nginx', 1],
      [`${directory}/bin/unrelated`, 1]
    ]) {
      const probe = spawnSync('/bin/bash', ['-c', body], {
        encoding: 'utf8', env: { ...process.env, UVP_TEST_ROOT: directory, UVP_TEST_EXE: exe }
      });
      assert.equal(probe.status, expected, `${exe}: ${probe.stderr}`);
    }
  } finally { rmSync(directory, { recursive: true, force: true }); }
});

test('Linux retained nginx validates the new configuration then reloads without another master', () => {
  const directory = mkdtempSync(join(tmpdir(), 'uvp-nginx-reload-'));
  try {
    const binary = join(directory, 'nginx');
    const calls = join(directory, 'calls');
    writeFileSync(binary, '#!/bin/bash\nprintf \'%s\\n\' "$*" >> "$UVP_TEST_CALLS"\n');
    chmodSync(binary, 0o755);
    const body = `set -eu
ROOT="$UVP_TEST_ROOT"
NGINX_BIN="$ROOT/nginx"
NGINX_CONF="$ROOT/nginx.conf"
NGINX_LOG="$ROOT/nginx.log"
NGINX_HTTPS_PORT=51001
render_nginx_conf() { :; }
ensure_self_signed_cert() { :; }
nginx_healthy() { return 0; }
log() { :; }
fail() { exit 1; }
${fn(linux, 'start_nginx')}
start_nginx`;
    const probe = spawnSync('/bin/bash', ['-c', body], {
      encoding: 'utf8', env: { ...process.env, UVP_TEST_ROOT: directory, UVP_TEST_CALLS: calls }
    });
    assert.equal(probe.status, 0, probe.stderr);
    const commands = readFileSync(calls, 'utf8').trim().split('\n');
    assert.equal(commands.length, 2);
    assert.match(commands[0], / -t$/);
    assert.match(commands[1], / -s reload$/);
  } finally { rmSync(directory, { recursive: true, force: true }); }
});


test('Linux upgrade stop refuses to continue if the backend remains alive', () => {
  const body = linux.match(/^  stop-for-upgrade\)\n([\s\S]*?)^    ;;/m)[1];
  const probe = spawnSync('/bin/bash', ['-c', `set -e
stop_backend() { :; }
stop_zlm() { :; }
stop_redis() { :; }
stop_firewall_agent() { :; }
redis_healthy() { return 1; }
zlm_healthy() { return 1; }
backend_healthy() { return 0; }
log() { :; }
fail() { echo "$*" >&2; exit 1; }
${body}`], { encoding: 'utf8' });
  assert.equal(probe.status, 1);
  assert.match(probe.stderr, /拒绝继续备份和迁移/);
});

for (const trusted of [true, false]) {
  test(`Linux failed nginx quit targets the verified prior release only (trusted=${trusted})`, () => {
    const directory = mkdtempSync(join(tmpdir(), 'uvp-nginx-stop-'));
    try {
      const body = `set -eu
ROOT="$UVP_TEST_ROOT"
NGINX_BIN="$ROOT/releases/new/vendor/nginx/sbin/nginx"
NGINX_CONF="$ROOT/nginx.conf"
NGINX_PID_FILE="$ROOT/nginx.pid"
printf '%s' "$$" > "$NGINX_PID_FILE"
readlink() { printf '%s\\n' "$UVP_TEST_EXE"; }
proc_alive_by_exe() { return 1; }
nginx_healthy() { return 0; }
stop_by_pidfile() { printf '%s|%s|%s\\n' "$1" "$2" "$3"; }
${fn(linux, 'nginx_running_exe')}
${fn(linux, 'stop_nginx')}
stop_nginx`;
      const probe = spawnSync('/bin/bash', ['-c', body], {
        encoding: 'utf8', env: {
          ...process.env, UVP_TEST_ROOT: directory,
          UVP_TEST_EXE: trusted ? `${directory}/releases/old/vendor/nginx/sbin/nginx` : '/other-install/vendor/nginx/sbin/nginx'
        }
      });
      assert.equal(probe.status, 0, probe.stderr);
      assert.equal(probe.stdout.trim(), trusted ? `|${directory}/releases/old/vendor/nginx/sbin/nginx|nginx` : '');
    } finally { rmSync(directory, { recursive: true, force: true }); }
  });
}
