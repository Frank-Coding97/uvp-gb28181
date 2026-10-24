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
