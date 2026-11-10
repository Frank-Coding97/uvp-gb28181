import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import test from 'node:test';

// ---------------------------------------------------------------------------
// 契约：出包时写进 config/config.env 的「端口区」必须
//   ① 覆盖**全部 16 个端口**（4 个主端口 + ZLM 的 12 个）；
//   ② 值全部现读 uvp-gb28181-ctl.sh，**不在出包脚本里另抄一份字面量**。
//
// ⛔⛔ 为什么 ② 是硬要求：端口默认值一旦有第二份，就一定会分家 ——
//   客户打开 config.env 看到 51004、运行期实际是别的值，页面上完全看不出来，
//   排查时只会怀疑"改了不生效"。这里用「动态跑一次提取函数」来钉住，
//   而不是比对某个字面量（那样只是把第二份真相同步了一遍）。
// ---------------------------------------------------------------------------

const ROOT = new URL('..', import.meta.url).pathname;
const BUILD = readFileSync(join(ROOT, 'deploy/standalone/build-standalone.sh'), 'utf8');
const CTL = readFileSync(join(ROOT, 'deploy/standalone/uvp-gb28181-ctl.sh'), 'utf8');
const BASH = '/bin/bash';

const ZLM_VARS = [
  'ZLM_HTTP_PORT',
  'ZLM_SSL_PORT',
  'ZLM_RTSP_PORT',
  'ZLM_RTMP_PORT',
  'ZLM_RTC_PORT',
  'ZLM_SIGNALING_PORT',
  'ZLM_SIGNALING_SSL_PORT',
  'ZLM_SRT_PORT',
  'ZLM_ONVIF_PORT',
  'ZLM_ICE_PORT',
  'ZLM_RTP_PROXY_PORT',
  'ZLM_RTP_RANGE',
];

function fn(source, name) {
  const hit = source.match(new RegExp(`^${name}\\(\\) \\{[\\s\\S]*?^\\}`, 'm'));
  assert.ok(hit, `找不到函数 ${name}()`);
  return hit[0];
}

/** 从 ctl.sh 里取 `${VAR:-值}` 形态的兜底默认值（与被测实现同源，但独立解析一遍）。 */
function ctlDefault(name) {
  const hit = CTL.match(new RegExp(`^${name}="\\$\\{${name}:-([0-9][0-9-]*)\\}"$`, 'm'));
  return hit ? hit[1] : null;
}

test('12 个 ZLM 端口默认值都能从 ctl.sh 现场取到（且能被环境变量覆盖）', () => {
  const script = [
    'set -euo pipefail',
    `DEPLOY_DIR="${join(ROOT, 'deploy/standalone')}"`,
    fn(BUILD, 'ctl_port_default'),
    fn(BUILD, 'pkg_port_or_die'),
    ...ZLM_VARS.map((v) => `echo "${v}=$(pkg_port_or_die UVP_${v} ${v})"`),
  ].join('\n');
  const r = spawnSync(BASH, ['-c', script], { encoding: 'utf8' });
  assert.equal(r.status, 0, r.stderr);
  const got = Object.fromEntries(
    r.stdout.trim().split('\n').map((line) => line.split('=')),
  );
  for (const v of ZLM_VARS) {
    assert.ok(got[v], `${v} 取到的是空值 —— 出包会把 config.env 里的端口写成空`);
    assert.match(got[v], /^[0-9]+(-[0-9]+)?$/, `${v} 取到非法值 ${got[v]}`);
    const expected = ctlDefault(v);
    assert.equal(got[v], expected, `${v} 与 ctl.sh 的默认值不一致`);
  }
});

test('出包脚本里的端口区覆盖全部 16 个键，一个都不能漏', () => {
  const portsBlock = BUILD.slice(BUILD.indexOf('# [uvp-ports] ---- 管理页'), BUILD.indexOf("printf '\\n'", BUILD.indexOf('# [uvp-ports] ---- 流媒体')));
  assert.ok(portsBlock.length > 0, '找不到 config.env 的端口区');
  const keys = [...portsBlock.matchAll(/printf '(UVP_[A-Z0-9_]+)=%s\\n'/g)].map((m) => m[1]);
  const expected = [
    'UVP_NGINX_HTTP_PORT', 'UVP_HTTPS_PORT', 'UVP_HTTP_PORT', 'UVP_REDIS_PORT',
    'UVP_ZLM_HTTP_PORT', 'UVP_ZLM_SSL_PORT', 'UVP_ZLM_RTSP_PORT', 'UVP_ZLM_RTMP_PORT',
    'UVP_ZLM_RTC_PORT', 'UVP_ZLM_RTP_PROXY_PORT', 'UVP_ZLM_RTP_RANGE',
    'UVP_ZLM_SIGNALING_PORT', 'UVP_ZLM_SIGNALING_SSL_PORT',
    'UVP_ZLM_SRT_PORT', 'UVP_ZLM_ONVIF_PORT', 'UVP_ZLM_ICE_PORT',
  ];
  assert.deepEqual(keys.sort(), expected.sort(), '端口区漏了键（客户会以为那个端口改不了）');
});

test('出包脚本不在端口区里抄字面量（只能引用变量）', () => {
  const start = BUILD.indexOf('# [uvp-ports] ---- 管理页');
  const end = BUILD.indexOf('# ---- SIP 报文诊断加密密钥', start);
  const block = BUILD.slice(start, end);
  const literals = block.match(/printf 'UVP_[A-Z0-9_]+=[0-9]/g) || [];
  assert.deepEqual(literals, [], `端口区出现硬编码字面量：${literals.join(', ')}`);
});

// ---------------------------------------------------------------------------
// Windows 出包：端口键和值必须**写在同一行**。
//
// ⛔⛔ 2026-11-10 现场事故：1.1.16~1.1.20 的 Windows 全量包里，config.env 的
//    16 个端口全被写成「UVP_XXX_PORT=」+「下一行才是 15500」。
//    老启动脚本会忽略空值走内置默认（只是改端口不生效）；11-09 加上判据 ③
//    「全新安装以 config.env 为准」后，空值被当成现场意图 ⇒ 一装就报
//    「UVP_ZLM_RTP_RANGE 格式无效：（应形如 15514-15563）」。
//    Linux 出包有上面三条门禁，Windows 之前一条都没有 —— 这里补齐。
// ---------------------------------------------------------------------------
const WIN_BUILD = readFileSync(join(ROOT, 'deploy/standalone-windows/build-standalone.ps1'), 'utf8');
const WIN_CTL = readFileSync(join(ROOT, 'deploy/standalone-windows/uvp-gb28181-ctl.ps1'), 'utf8');

function winPortDefaults() {
  const table = WIN_CTL.match(/\$PortDefault = @\{([\s\S]*?)\r?\n\}/);
  assert.ok(table, '找不到 ctl.ps1 的 $PortDefault 表');
  const defaults = Object.fromEntries(
    [...table[1].matchAll(/^\s{4}([A-Z_]+)\s+=\s+(\d+)/gm)].map((m) => [m[1], m[2]]),
  );
  const range = WIN_CTL.match(/^\$ZlmRtpRange\s*=\s*'(\d+-\d+)'/m);
  assert.ok(range, '找不到 ctl.ps1 的 $ZlmRtpRange');
  return { defaults, range: range[1] };
}

test('Windows 出包：16 个端口键都拼成「KEY=合法值」同一行', () => {
  const { defaults, range } = winPortDefaults();
  const start = WIN_BUILD.indexOf('$portLines = @(');
  const end = WIN_BUILD.indexOf('\n)', start);
  assert.ok(start >= 0 && end > start, '找不到 build-standalone.ps1 的 $portLines 块');
  const block = WIN_BUILD.slice(start, end);

  const entries = [];
  for (const m of block.matchAll(/"(UVP_[A-Z0-9_]+)=\$\(Get-CtlPortDefault '([A-Z_]+)'\)"/g)) {
    entries.push([m[1], defaults[m[2]]]);
  }
  for (const m of block.matchAll(/"(UVP_ZLM_RTP_RANGE)=\$pkgRtpRange"/g)) {
    entries.push([m[1], range]);
  }
  assert.equal(entries.length, 16, `端口区应有 16 个键，实际解析到 ${entries.length} 个`);

  // 旧写法 `'KEY=' + (Get-CtlPortDefault …)` 在 PS 5.1 的数组字面量里会把键和值
  // 拆成两个元素 ⇒ 落盘变成「KEY=」+「下一行数字」。这里把残留写法一并钉死。
  assert.equal(
    (block.match(/'(UVP_[A-Z0-9_]+=)'\s*\+\s*\(Get-CtlPortDefault/g) || []).length,
    0,
    '端口区不许再用单引号 + 号拼接（会把键和值拆成两行）',
  );
  for (const [key, value] of entries) {
    assert.ok(value, `${key} 取到的是空值`);
    assert.match(`${key}=${value}`, /^UVP_[A-Z0-9_]+=\d+(-\d+)?$/, `${key} 的值不合法：${value}`);
  }
});

test('Windows 出包：写盘前逐行验收 + 整个数组先合并再 join', () => {
  assert.match(WIN_BUILD, /config\.env 的端口值不合法/, '缺少写盘前的端口行校验');
  assert.match(WIN_BUILD, /\$portValueCount -ne 16/, '缺少端口键数量校验');
  assert.match(WIN_BUILD, /\$envBody = \( @\(/, 'envBody 必须把整个数组用括号包住');
  assert.match(WIN_BUILD, /\) \) -join "`r`n"/, 'envBody 必须整体 join，不能只 join 尾部数组');
});

test('Windows 启动脚本：空值/非法值不采纳，回退出厂默认', () => {
  assert.match(WIN_CTL, /function Test-PortValue/, '缺少端口值合法性判断');
  assert.match(WIN_CTL, /function Test-RtpRangeValue/, '缺少 RTP 段合法性判断');
  const resolvePort = WIN_CTL.slice(
    WIN_CTL.indexOf('function Resolve-Port'),
    WIN_CTL.indexOf('function Resolve-RtpRange'),
  );
  const resolveRange = WIN_CTL.slice(
    WIN_CTL.indexOf('function Resolve-RtpRange'),
    WIN_CTL.indexOf('function Get-Config'),
  );
  assert.match(resolvePort, /Test-PortValue \(\$EnvMap\[\$envKey\]\)/, '判据 ③ 未校验端口值');
  assert.match(resolveRange, /Test-RtpRangeValue \(\$EnvMap\[\$ZlmRtpRangeKey\]\)/, '判据 ③ 未校验 RTP 段');
});
