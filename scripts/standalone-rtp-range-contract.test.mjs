import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';

const script = readFileSync(new URL('../deploy/standalone/uvp-gb28181-ctl.sh', import.meta.url), 'utf8');
const fixture = '[rtp_proxy]\r\nport=10000\r\nport_range=30000-35000\r\n[rtp]\r\naudioMtuSize=600\r\n[rtc]\r\nenableTurn=1\r\n[rtsp]\r\nport=554\r\n';

// 位置参数与 uvp-gb28181-ctl.sh 里那行 heredoc 调用**必须逐位对应**（顺序错了不会报错，
// 只会把端口写到别的键上 —— 所以这里也当契约来锁）。
const PORTS_ARGS = ['51004', '51007', '51005', '51006', '51008', '51014',
  '51014-51063', '51009', '51010', '51011', '51012', '51013'];
// TURN 开关不是端口，但和 [rtc] 的端口一起同步（2026-10-10 起固定关，见 uvp-gb28181-ctl.sh）。
const CURRENT_ARGS = [...PORTS_ARGS, '0'];

function sync(source, iniText, args = CURRENT_ARGS) {
  const code = source.match(/<<'ZLM_INI_PY'\n([\s\S]*?)\nZLM_INI_PY/)[1];
  const directory = mkdtempSync(join(tmpdir(), 'uvp-rtp-range-'));
  try {
    const ini = join(directory, 'config.ini');
    writeFileSync(ini, iniText);
    const result = spawnSync('python3', ['-', ini, ...args], { input: code, encoding: 'utf8' });
    return { ...result, ini: readFileSync(ini, 'utf8') };
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
}

test('deployed package regression: old synchronizer leaves the RTP proxy range unchanged', () => {
  const old = execFileSync('git', ['show', 'a86e5782:deploy/standalone/uvp-ctl.sh'], { encoding: 'utf8' });
  // 老版同步器只接 12 个端口参数，不认识 TURN 开关。
  const result = sync(old, fixture, PORTS_ARGS);
  assert.equal(result.status, 0);
  assert.match(result.ini, /\[rtp_proxy\]\r\nport=51014\r\nport_range=30000-35000/);
});

test('synchronizer writes the actual RTP proxy range and preserves unrelated RTP settings', () => {
  const result = sync(script, fixture);
  assert.equal(result.status, 0, result.stderr);
  assert.match(result.ini, /\[rtp_proxy\]\r\nport=51014\r\nport_range=51014-51063/);
  assert.match(result.ini, /\[rtp\]\r\naudioMtuSize=600\r\n/);
});

test('synchronizer turns TURN off so the dead relay stops holding the ephemeral port range', () => {
  const result = sync(script, fixture);
  assert.equal(result.status, 0, result.stderr);
  assert.match(result.ini, /\[rtc\]\r\nenableTurn=0\r\n/);
});

test('missing RTP proxy range blocks startup instead of silently keeping ZLM defaults', () => {
  const result = sync(script, fixture.replace('port_range=30000-35000\r\n', ''));
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /rtp_proxy/);
});

test('missing enableTurn warns but does not block startup (a dead switch must not stop the platform)', () => {
  const result = sync(script, fixture.replace('[rtc]\r\nenableTurn=1\r\n', ''));
  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stderr, /enableTurn/);
});

// ---------------------------------------------------------------------------
// [rtc] tcpPort —— WebRTC over TCP 的媒体端口
//
// ⛔ 真实 config.ini 里 [rtc] 有**两个**媒体键：port(UDP) 与 tcpPort(TCP)。
//   同步器长期只改 port ⇒ tcpPort 一直留在 ZLM 出厂默认 **8000**（规划段外）。
//   8000 是极常见的冲突端口，目标机上被占住时 ZLM 启动即退出
//   （`Start server failed: Listen on :: 8000 failed: address already in use`），
//   而原因行又会被退出时那几十行 `~EventPoller` 析构日志挤出 tail 窗口 ——
//   客户 2026-10-12 就是这么误判成"缺依赖"的。
// ---------------------------------------------------------------------------
// 防呆网：拿包内 vendor/zlm/config.ini 的**出厂端口**当夹具（含 CRLF 行尾、
// 含 [rtc] 的 port/tcpPort 两个键），同步一遍后**每一个**监听端口键都必须落在
// 规划段 51000-51064 内（0 = 关，放行）。以后再漏任何一个键，这条会直接变红。
const FACTORY_PORTS_FIXTURE = [
  '[api]', 'apiDebug=1', // 非端口键，用来确认不会被误伤
  '[http]', 'port=80', 'sslport=443',
  '[rtmp]', 'port=1935', 'sslport=0',
  '[rtp_proxy]', 'port=10000', 'port_range=30000-35000',
  '[rtc]', 'signalingPort=3000', 'signalingSslPort=3001',
  'icePort=3478', 'iceTcpPort=3478', 'enableTurn=1',
  'port=8000', 'tcpPort=8000', 'port_range=49152-65535',
  '[srt]', 'port=9000',
  '[rtsp]', 'port=554', 'sslport=0',
  '[onvif]', 'port=3702',
  '[shell]', 'port=0',
].join('\r\n') + '\r\n';

test('[rtc] tcpPort is rewritten into the planned range (it used to stay at ZLM default 8000)', () => {
  const result = sync(script, FACTORY_PORTS_FIXTURE);
  assert.equal(result.status, 0, result.stderr);
  assert.match(result.ini, /^port=51008$/m);
  assert.match(result.ini, /^tcpPort=51008$/m);
  assert.doesNotMatch(result.ini, /=\s*8000\s*$/m, '还有键值停在 8000（ZLM 出厂默认）上');
});

test('old synchronizer leaves [rtc] tcpPort at 8000 — the port that collides on customer Linux', () => {
  const old = execFileSync('git', ['show', 'a86e5782:deploy/standalone/uvp-ctl.sh'], { encoding: 'utf8' });
  const result = sync(old, FACTORY_PORTS_FIXTURE, PORTS_ARGS);
  assert.equal(result.status, 0);
  assert.match(result.ini, /^tcpPort=8000$/m);
});

test('every ZLM listen port lands inside the planned 51000-51064 range', () => {
  const result = sync(script, FACTORY_PORTS_FIXTURE);
  assert.equal(result.status, 0, result.stderr);

  const offenders = [];
  let section = '';
  for (const line of result.ini.split(/\r?\n/)) {
    const head = line.match(/^\[(\w+)\]$/);
    if (head) { section = head[1]; continue; }
    const kv = line.match(/^(\w+)\s*=\s*(.+?)\s*$/);
    if (!kv) continue;
    const [, key, value] = kv;
    if (!/port/i.test(key) || /range/i.test(key)) continue; // port_range 是区间，另算
    if (value === '0') continue;                            // 0 = 该协议不启用
    const n = Number(value);
    if (!Number.isInteger(n)) continue;
    if (n < 51000 || n > 51064) offenders.push(`${section}.${key}=${value}`);
  }
  assert.deepEqual(offenders, [], `这些端口跑到规划段外了：${offenders.join(', ')}`);
});
