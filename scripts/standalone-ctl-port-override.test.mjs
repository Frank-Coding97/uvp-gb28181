import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';

// ---------------------------------------------------------------------------
// 契约：**所有端口**（4 个主端口 + ZLM 的 12 个）都能在 config/config.env 里改，
//       且升级换包时不会把用户改过的值冲掉，也不会让老实例停在旧端口规划上。
//
// 三层判据（缺一不可）：
//   ① 有 `UVP_USER_SET_<KEY>=1` 标记          ⇒ 用户设置（历史机制，永久生效）
//   ② 文件值 ≠ config/.port-baseline 里记的值  ⇒ 用户手改（2026-10-20 新增）
//   ③ 两者都不成立                            ⇒ 按本包默认值（升级跟随新规划）
//
// ⛔⛔ 为什么要有 ②：老实现只认 ①，客户改一个端口得写**两行**（值 + 标记）。
//   漏写标记 → 静默失效 → 下次启动还被 ports-sync 用默认值覆盖掉，连痕迹都没有。
//   老板 2026-10-20 明确要求「所有端口都能改，尤其是 ZLM 那些」⇒ 改成程序记基线、
//   自动补标记，客户只改值。
//
// ⛔ 为什么 ② 不会破坏"升级跟随新规划"：老包**根本没有** .port-baseline，
//   ② 恒不成立 ⇒ 与既有语义逐字一致。契约由本文件第 1 个用例钉住。
// ---------------------------------------------------------------------------

const script = readFileSync(new URL('../deploy/standalone/uvp-gb28181-ctl.sh', import.meta.url), 'utf8');
const BASH = '/bin/bash';

function fn(name) {
  const multiLine = script.match(new RegExp(`^${name}\\(\\) \\{[\\s\\S]*?^\\}`, 'm'));
  assert.ok(multiLine, `ctl.sh 里找不到函数 ${name}()`);
  return multiLine[0];
}

const PORT_FUNCS = ['port_baseline_get', 'port_is_user_set', 'read_port_env'].map(fn).join('\n');

const ZLM_KEYS = [
  'UVP_ZLM_HTTP_PORT',
  'UVP_ZLM_SSL_PORT',
  'UVP_ZLM_RTSP_PORT',
  'UVP_ZLM_RTMP_PORT',
  'UVP_ZLM_RTC_PORT',
  'UVP_ZLM_RTP_PROXY_PORT',
  'UVP_ZLM_RTP_RANGE',
  'UVP_ZLM_SIGNALING_PORT',
  'UVP_ZLM_SIGNALING_SSL_PORT',
  'UVP_ZLM_SRT_PORT',
  'UVP_ZLM_ONVIF_PORT',
  'UVP_ZLM_ICE_PORT',
];

/** 在临时包里跑一次 read_port_env，返回 stdout（去掉首行空串）。 */
function readPort(key, envBody, baselineBody) {
  const dir = mkdtempSync(join(tmpdir(), 'uvp-port-'));
  mkdirSync(join(dir, 'config'), { recursive: true });
  writeFileSync(join(dir, 'config', 'config.env'), envBody);
  if (baselineBody !== null) {
    writeFileSync(join(dir, 'config', '.port-baseline'), baselineBody);
  }
  const harness = [
    'set -euo pipefail',
    `ENV_FILE="${dir}/config/config.env"`,
    `PORT_BASELINE_FILE="${dir}/config/.port-baseline"`,
    PORT_FUNCS,
    `read_port_env ${key}`,
  ].join('\n');
  const r = spawnSync(BASH, ['-c', harness], { encoding: 'utf8' });
  assert.equal(r.status, 0, `harness 失败：${r.stderr}`);
  return r.stdout;
}

test('老包（无基线、无标记）：端口一律按本包默认值 —— 升级后跟随新端口规划', () => {
  // 老包 config.env 里留着上一版的旧值，也绝不能当成"用户设置"。
  const env = ['# 旧包留下的行', 'UVP_ZLM_HTTP_PORT=30004', 'UVP_HTTP_PORT=8280', ''].join('\n');
  assert.equal(readPort('UVP_ZLM_HTTP_PORT', env, null), '');
  assert.equal(readPort('UVP_HTTP_PORT', env, null), '');
});

test('用户手改（值与基线不同）：被识别为用户设置 —— 无需手写标记', () => {
  const env = 'UVP_ZLM_HTTP_PORT=52004\n';
  const baseline = 'UVP_ZLM_HTTP_PORT=51004\n';
  assert.equal(readPort('UVP_ZLM_HTTP_PORT', env, baseline).trim(), '52004');
});

test('值没动过（与基线一致）：不算用户设置 —— 换包后跟着新默认值走', () => {
  const env = 'UVP_ZLM_HTTP_PORT=51004\n';
  const baseline = 'UVP_ZLM_HTTP_PORT=51004\n';
  assert.equal(readPort('UVP_ZLM_HTTP_PORT', env, baseline), '');
});

test('有标记（程序补的或历史手写的）：即使没有基线也永久生效', () => {
  const env = ['UVP_ZLM_SRT_PORT=53011', 'UVP_USER_SET_UVP_ZLM_SRT_PORT=1', ''].join('\n');
  assert.equal(readPort('UVP_ZLM_SRT_PORT', env, null).trim(), '53011');
});

test('RTP 段（字符串值）同样走这套判据', () => {
  const env = 'UVP_ZLM_RTP_RANGE=52014-52063\n';
  const baseline = 'UVP_ZLM_RTP_RANGE=51014-51063\n';
  assert.equal(readPort('UVP_ZLM_RTP_RANGE', env, baseline).trim(), '52014-52063');
});

test('静态：12 个 ZLM 端口键必须走 read_port_env，不能退回"看到就用"', () => {
  const legacy = script.match(/read_env_file UVP_ZLM_/g) || [];
  assert.equal(legacy.length, 0, `仍有 ${legacy.length} 处用 read_env_file 读 ZLM 端口（会绕过标记/基线判据）`);
  for (const key of ZLM_KEYS) {
    assert.match(
      script,
      new RegExp(`read_port_env ${key}\\)`),
      `${key} 没有走 read_port_env —— 客户改了它在升级时会被默认值顶掉`,
    );
  }
});

test('静态：ports-sync 的"用户设置键"候选表必须覆盖全部 16 个端口键', () => {
  const start = script.indexOf('local user_set="" port_key');
  assert.ok(start > 0, '找不到 sync_ports_into_config 的候选键表');
  const end = script.indexOf('\n  done', start);
  const block = script.slice(start, end > 0 ? end : undefined);
  for (const key of [...['UVP_HTTP_PORT', 'UVP_REDIS_PORT', 'UVP_HTTPS_PORT', 'UVP_NGINX_HTTP_PORT'], ...ZLM_KEYS]) {
    assert.ok(block.includes(key), `sync_ports_into_config 的候选键表漏了 ${key}`);
  }
});

test('静态：ports-sync 必须落盘基线文件，否则"手改识别"永远不生效', () => {
  assert.match(script, /--baseline "\$PORT_BASELINE_FILE"/, 'ctl 没有把 --baseline 传给 uvp-setup ports-sync');
});

test('静态：RTP 段与 rtp_proxy 端口必须做一致性处理', () => {
  assert.match(script, /resolve_rtp_pair\(\) \{/, '缺少 resolve_rtp_pair()：只改段/只改 proxy 会静默脱节');
  assert.match(script, /^resolve_rtp_pair$/m, 'resolve_rtp_pair 定义了但没被调用');
});

test('静态：SIP 默认位由 RTP 段尾派生，整段平移后不会漏放行', () => {
  assert.match(script, /SIP_PORT_DEFAULT=\$\(\( \$\{ZLM_RTP_RANGE#\*-\} \+ 1 \)\)/,
    'SIP 默认位仍写死 51064：整段平移后防火墙放行会漏掉 SIP');
});

test('静态：防火墙放行范围按实际端口 min/max 算，不再拼"倒挂区间"', () => {
  const plan = fn('print_port_plan');
  assert.match(plan, /port_range_bounds/, 'print_port_plan 必须用 port_range_bounds 取范围');
  assert.doesNotMatch(plan, /fw_to="\$to"/, '仍在用 "RTP 段尾" 当范围末尾（客户改高 nginx 端口就会倒挂）');
  const preflight = fn('check_ports_free');
  assert.doesNotMatch(preflight, /端口预检通过（\$\{NGINX_HTTPS_PORT\}/,
    '预检通过提示仍拼写死区间，客户改端口后会输出 52001-51063 这类倒挂范围');
});
