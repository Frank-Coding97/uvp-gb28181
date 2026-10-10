import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';

const script = readFileSync(new URL('../deploy/standalone/uvp-ctl.sh', import.meta.url), 'utf8');
const fixture = '[rtp_proxy]\r\nport=10000\r\nport_range=30000-35000\r\n[rtp]\r\naudioMtuSize=600\r\n[rtsp]\r\nport=554\r\n';

// 位置参数与 uvp-ctl.sh 里那行 heredoc 调用**必须逐位对应**（顺序错了不会报错，
// 只会把端口写到别的键上 —— 所以这里也当契约来锁）。
const PORTS_ARGS = ['51004', '51007', '51005', '51006', '51008', '51014',
  '51014-51063', '51009', '51010', '51011', '51012', '51013'];

function sync(source, iniText, args = PORTS_ARGS) {
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
  const result = sync(old, fixture);
  assert.equal(result.status, 0);
  assert.match(result.ini, /\[rtp_proxy\]\r\nport=51014\r\nport_range=30000-35000/);
});

test('synchronizer writes the actual RTP proxy range and preserves unrelated RTP settings', () => {
  const result = sync(script, fixture);
  assert.equal(result.status, 0, result.stderr);
  assert.match(result.ini, /\[rtp_proxy\]\r\nport=51014\r\nport_range=51014-51063/);
  assert.match(result.ini, /\[rtp\]\r\naudioMtuSize=600\r\n/);
});

test('missing RTP proxy range blocks startup instead of silently keeping ZLM defaults', () => {
  const result = sync(script, fixture.replace('port_range=30000-35000\r\n', ''));
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /rtp_proxy/);
});
