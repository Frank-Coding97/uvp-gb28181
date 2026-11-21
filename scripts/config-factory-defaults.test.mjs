// 出厂默认值契约：把「老板 2026-10-19 定的那几个默认值」钉住。
//
// 为什么值得单独一个测试：
//   ① 这些值分散在**四处**——config.example.yml（出包模板）、
//      web/src/utils/system-footer.ts（页脚占位识别）、
//      app/logcleanup/config.go（配置缺键时的兜底）、web/index.html（首屏标题）。
//      任意一处改了、别处没跟，表现都是**静默**的：
//        · yml 改了而 footer.ts 没跟 ⇒ 页脚把 "20xxxx" 原样露给最终用户；
//        · yml 改了而 Go 没跟    ⇒ 删掉配置段以后行为又变回旧值，"默认值"名存实亡。
//   ② 它们**都是产品决策**（不是随手写的凑数值），改之前应当有人明确点头。
//      这类东西最容易被"顺手清理"掉，所以用断言把它钉成契约。
//
// ⛔ 本测试读的是**源文件字面量**，不解析 YAML 库、不 import 任何业务代码：
//   目的是让它在任何环境下都能跑（不需要装依赖），且失败信息直指"改哪个文件"。

import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const read = (rel) => readFileSync(join(ROOT, rel), 'utf8');

const EXAMPLE = 'server/config/config.example.yml';
const FOOTER = 'web/src/utils/system-footer.ts';
const LOG_CLEANUP = 'server/app/logcleanup/config.go';
const INDEX_HTML = 'web/index.html';

/**
 * 取出 YAML 里某个**顶级段落**的正文（不含段名行）。
 *
 * ⛔ 不能直接全文件正则找 key：`port` / `open` / `enabled` 这类名字在十几个段落里重名，
 *   全局搜索会把别人的值当成本段的（本项目已经为这个坑付过一次代价）。
 * ⛔ 段名行可能带行尾注释（`system: # 品牌信息…`），所以判据是「段名 + 冒号」而不是整行相等。
 */
function sectionBody(text, name) {
  const lines = text.split('\n');
  const head = new RegExp(`^(\\s*)${name}\\s*:`);
  let start = -1;
  let indent = 0;
  for (let i = 0; i < lines.length; i++) {
    const m = lines[i].match(head);
    if (m) {
      start = i;
      indent = m[1].length;
      break;
    }
  }
  assert.notEqual(start, -1, `config.example.yml 里找不到段落 ${name}:`);

  const out = [];
  for (let i = start + 1; i < lines.length; i++) {
    const line = lines[i];
    if (!line.trim()) continue;
    // 段内注释不参与缩进判定（注释可以顶格写，不该因此提前截断段落）
    if (line.trim().startsWith('#')) {
      out.push(line);
      continue;
    }
    const current = line.length - line.trimStart().length;
    if (current <= indent) break;
    out.push(line);
  }
  return out.join('\n');
}

/** 取段落内某个键的标量值：优先引号内内容，否则取冒号后到行尾注释之前的部分。 */
function scalarIn(body, section, key) {
  const line = body.split('\n').find((l) => new RegExp(`^\\s*${key}\\s*:`).test(l));
  assert.ok(line, `段落 ${section} 里找不到键 ${key}`);
  const after = line.slice(line.indexOf(':') + 1).trim();
  const quoted = after.match(/^"([^"]*)"/);
  if (quoted) return quoted[1];
  return after.split('#')[0].trim();
}

const yml = read(EXAMPLE);

test('系统名称默认是「UVP统一视频接入平台」', () => {
  const got = scalarIn(sectionBody(yml, 'system'), 'system', 'systemname');
  assert.equal(got, 'UVP统一视频接入平台');
});

test('版权信息 / 备案号的出厂占位与页脚识别常量逐字一致', () => {
  // ⛔ 这条断言的意义是**跨文件**的：footer.ts 用全等比较判断"这是不是出厂占位"，
  //   差一个空格或全角字符就会识别失败 ⇒ 占位原样显示在登录页页脚。
  const footer = read(FOOTER);
  const system = sectionBody(yml, 'system');

  const copyright = scalarIn(system, 'system', 'systemcopyright');
  const recordNo = scalarIn(system, 'system', 'systemrecordno');

  const footerCopyright = footer.match(/SYSTEM_COPYRIGHT_DEFAULT\s*=\s*"([^"]*)"/)?.[1];
  const footerRecordNo = footer.match(/SYSTEM_RECORD_NO_DEFAULT\s*=\s*"([^"]*)"/)?.[1];

  assert.ok(footerCopyright, 'system-footer.ts 里找不到 SYSTEM_COPYRIGHT_DEFAULT');
  assert.ok(footerRecordNo, 'system-footer.ts 里找不到 SYSTEM_RECORD_NO_DEFAULT');
  assert.equal(copyright, footerCopyright, `${EXAMPLE} 的 systemcopyright 与 ${FOOTER} 不一致`);
  assert.equal(recordNo, footerRecordNo, `${EXAMPLE} 的 systemrecordno 与 ${FOOTER} 不一致`);
});

test('版权 / 备案号仍带 xxxxx 占位，提醒客户替换', () => {
  // 老板明确要求"用 xxxxx 占位"。若有人把它写成看似真实的公司名/备案号，
  // 客户很可能当成已配好、直接上线 —— 所以连"占位形态"一起锁住。
  const system = sectionBody(yml, 'system');
  assert.match(scalarIn(system, 'system', 'systemcopyright'), /x{3,}/, '版权信息应保留 xxxxx 占位');
  assert.match(scalarIn(system, 'system', 'systemrecordno'), /x{3,}/, '备案号应保留 xxxxx 占位');
});

test('出厂默认开启登录验证码与操作日志', () => {
  assert.equal(scalarIn(sectionBody(yml, 'captcha'), 'captcha', 'open'), 'true', 'captcha.open 默认应为 true');
  assert.equal(scalarIn(sectionBody(yml, 'server'), 'server', 'syslog'), 'true', 'server.syslog 默认应为 true');
});

test('登录/操作日志默认保留 7 天，且 yml 与 Go 兜底值一致', () => {
  const cleanup = sectionBody(yml, 'log_cleanup');
  const ymlOperation = scalarIn(cleanup, 'log_cleanup', 'operation_retention_days');
  const ymlLogin = scalarIn(cleanup, 'log_cleanup', 'login_retention_days');
  assert.equal(ymlOperation, '7');
  assert.equal(ymlLogin, '7');

  // ⛔ 两边必须同步：config.yml 里没有该键时走 Go 的兜底值，
  //   两处不一致就会出现「新装 7 天、删掉段又变回旧值」的诡异行为。
  const go = read(LOG_CLEANUP);
  const goOperation = go.match(/defaultOperationRetentionDays\s*=\s*(\d+)/)?.[1];
  const goLogin = go.match(/defaultLoginRetentionDays\s*=\s*(\d+)/)?.[1];
  assert.ok(goOperation && goLogin, 'logcleanup/config.go 里的默认常量不见了（被改名？）');
  assert.equal(ymlOperation, goOperation, `operation：yml=${ymlOperation} 而 Go=${goOperation}`);
  assert.equal(ymlLogin, goLogin, `login：yml=${ymlLogin} 而 Go=${goLogin}`);
});

test('首屏标题与系统名称一致（不留下旧产品名）', () => {
  const title = read(INDEX_HTML).match(/<title>([^<]*)<\/title>/)?.[1]?.trim();
  assert.equal(title, 'UVP统一视频接入平台');
});

// ─────────────────────────────────────────────────────────────────────────────
// 国标服务配置出厂默认（老板 2026-10-20 交付包口径），同样分散多处：
//   config.example.yml（出包模板）/ gb28181/config/config.go（缺键兜底）/
//   serviceConfigState.ts（前端表单兜底）/ utils/http/index.ts（axios 全局超时）。
// 任意一处单方面改动都会造成「模板是 A、删掉配置行又变 B」的静默漂移。
// ─────────────────────────────────────────────────────────────────────────────
const GB_CONFIG = 'server/app/gb28181/config/config.go';
const SERVICE_CONFIG_STATE = 'web/src/views/gb28181/sip/serviceConfigState.ts';
const HTTP_INDEX = 'web/src/utils/http/index.ts';

test('国标服务配置出厂默认：报警/位置轨迹不存、固定点播地址开启', () => {
  const gb28181 = sectionBody(yml, 'gb28181');
  const alarm = sectionBody(gb28181, 'alarm');
  const position = sectionBody(gb28181, 'position_history');
  const play = sectionBody(gb28181, 'play');

  assert.equal(scalarIn(alarm, 'gb28181.alarm', 'save_messages'), 'false', 'alarm.save_messages 默认应为 false');
  assert.equal(scalarIn(position, 'gb28181.position_history', 'enabled'), 'false', 'position_history.enabled 默认应为 false');
  assert.equal(scalarIn(play, 'gb28181.play', 'fixed_address_enabled'), 'true', 'play.fixed_address_enabled 默认应为 true');
});

test('国标服务配置出厂默认：点播超时 15000ms，yml 与 Go 兜底一致', () => {
  const play = sectionBody(sectionBody(yml, 'gb28181'), 'play');
  const ymlTimeout = scalarIn(play, 'gb28181.play', 'request_timeout_ms');
  assert.equal(ymlTimeout, '15000', 'play.request_timeout_ms 默认应为 15000');

  // ⛔ 两边必须同步：config.yml 里没有该键时走 Go 的兜底常量。
  const go = read(GB_CONFIG);
  const goTimeout = go.match(/DefaultPlayRequestTimeoutMs\s*=\s*(\d+)/)?.[1];
  assert.ok(goTimeout, 'config.go 里的 DefaultPlayRequestTimeoutMs 常量不见了（被改名？）');
  assert.equal(ymlTimeout, goTimeout, `点播超时：yml=${ymlTimeout} 而 Go=${goTimeout}`);
});

test('前端表单兜底默认与 yml 出厂默认一致', () => {
  const draft = read(SERVICE_CONFIG_STATE);
  const pin = (pattern, what) => {
    assert.match(draft, pattern, `${SERVICE_CONFIG_STATE} 里 ${what} 与出厂默认不一致`);
  };
  pin(/saveMobilePositionHistory:\s*false/, 'saveMobilePositionHistory 应为 false');
  pin(/saveAlarmMessages:\s*false/, 'saveAlarmMessages 应为 false');
  pin(/fixedAddressEnabled:\s*true/, 'fixedAddressEnabled 应为 true');
  pin(/playTimeoutMs:\s*15000/, 'playTimeoutMs 应为 15000');
});

test('axios 全局请求超时默认 15 秒', () => {
  const http = read(HTTP_INDEX);
  const timeout = http.match(/timeout:\s*(\d+)/)?.[1];
  assert.ok(timeout, `${HTTP_INDEX} 里找不到全局 timeout`);
  assert.equal(timeout, '15000', '前端接口请求超时默认应为 15000ms');
});
