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

const PORT_FUNCS = ['port_baseline_get', '_is_fresh_install', 'port_is_user_set', 'read_port_env'].map(fn).join('\n');

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

/** 在临时包里跑一次 read_port_env，返回 stdout（去掉首行空串）。
 *
 *  `root` 传入时相当于「安装根存在」：data/uvp.db 与 logs/backend.log 都**不建**，
 *  用来模拟"全新解压、一次都没启动过"的包；传 `started: true` 则补上这两个产物，
 *  模拟"跑过一次的老安装"。不传 root 时 ROOT 未定义 ⇒ _is_fresh_install 恒不成立，
 *  等价于老语义（与第 1 个用例一致）。
 */
function readPort(key, envBody, baselineBody, { root = false, started = false } = {}) {
  const dir = mkdtempSync(join(tmpdir(), 'uvp-port-'));
  mkdirSync(join(dir, 'config'), { recursive: true });
  writeFileSync(join(dir, 'config', 'config.env'), envBody);
  if (baselineBody !== null) {
    writeFileSync(join(dir, 'config', '.port-baseline'), baselineBody);
  }
  if (started) {
    mkdirSync(join(dir, 'data'), { recursive: true });
    writeFileSync(join(dir, 'data', 'uvp.db'), 'sqlite');
    mkdirSync(join(dir, 'logs'), { recursive: true });
    writeFileSync(join(dir, 'logs', 'backend.log'), 'started\n');
  }
  const harness = [
    'set -euo pipefail',
    ...(root || started ? [`ROOT="${dir}"`] : []),
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

test('全新安装（包从没启动过）：装机前改的端口必须生效 —— 不能被当成老包吞掉', () => {
  // ⛔⛔ 2026-11-09 在 220 上实测：全新包没有 .port-baseline，判据 ② 恒不成立
  //   ⇒ 把整段改成 53000 后 start 仍报 51008，**全程零提示**。
  //   判据 ③（_is_fresh_install）专门堵这个洞：config.env 里就是现场要的值。
  const env = 'UVP_ZLM_HTTP_PORT=53004\nUVP_HTTP_PORT=53002\n';
  assert.equal(readPort('UVP_ZLM_HTTP_PORT', env, null, { root: true }).trim(), '53004');
  assert.equal(readPort('UVP_HTTP_PORT', env, null, { root: true }).trim(), '53002');
});

test('全新安装的判定必须缓存：中途建完库再问一次，不能改口', () => {
  // ⛔⛔ 2026-11-09 真机踩到（比第 2 个用例更隐蔽）：read_port_env 在脚本**加载时**
  //   采纳了 24000 段；启动到阶段 2 时 ensure_database 建出了 data/uvp.db；
  //   随后 sync_ports_into_config 再问一次「是不是全新安装」⇒ 那次判成「不是」
  //   ⇒ 一个 UVP_USER_SET_ 标记都不打。
  //   后果不在当次启动（端口已经生效），而在**下次**：值 == 基线、判据 ② 不成立
  //   ⇒ 端口自己退回出厂 51000 段 —— 客户会看到"重启一次端口就变了"。
  //   ⇒ _is_fresh_install 必须把首次结论缓存住。
  const dir = mkdtempSync(join(tmpdir(), 'uvp-fresh-'));
  mkdirSync(join(dir, 'config'), { recursive: true });
  writeFileSync(join(dir, 'config', 'config.env'), 'UVP_HTTP_PORT=24002\n');
  const harness = [
    'set -euo pipefail',
    `ROOT="${dir}"`,
    `ENV_FILE="${dir}/config/config.env"`,
    `PORT_BASELINE_FILE="${dir}/config/.port-baseline"`,
    PORT_FUNCS,
    // 复刻 ctl.sh 顶层的预计算：read_port_env 在真实脚本里跑在子 shell 中，
    // 父 shell 必须先算好 _FRESH_INSTALL，子 shell 才继承得到（见脚本里的注释）。
    '_is_fresh_install || true',
    'echo "first=$(read_port_env UVP_HTTP_PORT)"',
    '# 模拟启动中途 ensure_database 建库',
    'mkdir -p "$ROOT/data"; : > "$ROOT/data/uvp.db"',
    'echo "second=$(read_port_env UVP_HTTP_PORT)"',
  ].join('\n');
  const r = spawnSync(BASH, ['-c', harness], { encoding: 'utf8' });
  assert.equal(r.status, 0, `harness 失败：${r.stderr}`);
  assert.match(r.stdout, /first=24002/, '首次（加载时）没采纳用户改的端口');
  assert.match(r.stdout, /second=24002/, '建库后再问就改口了 —— 缓存没做，标记会漏打、重启端口会漂回默认');
});

test('跑过一次的安装（有 uvp.db）：仍按老语义 —— 无基线就跟随本包默认值', () => {
  // ⛔ 这是判据 ③ 的**边界**，别把它当成"又反悔了"：
  //   升级上来的老包 config.env 里全是上一版的旧默认值，绝不能因为"没有基线"
  //   就被当用户设置 —— 否则端口永远停在旧规划上（第 1 个用例钉的就是这个）。
  const env = 'UVP_ZLM_HTTP_PORT=30004\n';
  assert.equal(readPort('UVP_ZLM_HTTP_PORT', env, null, { started: true }), '');
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

// ---------------------------------------------------------------------------
// 2026-11-09 在 220 上照官网教程实机演练暴露的两个坑（都已修，这里钉住）：
//   ① 端口被**出站连接**当源端口占着 —— `ss -lnt` 里看不到监听，预检全绿，
//      ZLM 却 bind 失败，重试无效。默认段 51000-51064 正好落在系统临时端口范围
//      （32768-60999）里，所以这不是理论风险。
//   ② 中文报错消息里用反引号包命令示例 ⇒ 双引号内被当命令替换执行，
//      打出「端口: No such file or directory」，把真正的错误冲掉。
// ---------------------------------------------------------------------------

test('静态：端口预检要能看出「被出站连接占着」—— 只看 LISTEN 会漏判', () => {
  const preflight = fn('check_ports_free');
  assert.match(preflight, /nonlisten_local_ports/,
    'check_ports_free 没调 nonlisten_local_ports：出站连接占用会被判成「空闲」');
  assert.match(preflight, /ip_local_port_range/,
    'check_ports_free 没查临时端口范围重叠：偶发 bind 失败现场查不出来');
  assert.match(script, /^nonlisten_local_ports\(\) \{/m, '缺少 nonlisten_local_ports()');
  assert.match(script, /^ip_local_port_range\(\) \{/m, '缺少 ip_local_port_range()');

  const startZlm = fn('start_zlm');
  assert.match(startZlm, /nonlisten_local_ports/,
    'ZLM 起不来时没有区分「出站连接占用」：现场会去找一个根本不存在的进程');
});

test('静态：中文报错消息里不得出现反引号（会被 shell 当命令替换执行）', () => {
  // 实测输出：「端口: No such file or directory」+「error: command not found」，
  // 真正的「Listen on :: 53004 failed」提示反而被冲掉。
  // ⛔ 只看**代码行**：注释里的反引号不会被 shell 解析，排掉才不误伤。
  const code = fn('start_zlm')
    .split('\n')
    .filter((line) => !line.trim().startsWith('#'))
    .join('\n');
  assert.doesNotMatch(code, /`/,
    'start_zlm 的消息含反引号：它在双引号里，会被当命令替换执行');
});

test('静态：端口预检里 port_holder 的退出码必须用 `|| rc=$?` 兜住', () => {
  // ⛔⛔ 2026-11-09 真机踩到：写成 `holder="$(port_holder …)"; rc=$?` 时，
  //   端口**空闲**（port_holder 返回 1）会让赋值语句返回 1 ⇒ `set -e` 直接把整个
  //   脚本干掉。症状是 start 停在「阶段 1/3 环境检查」，**一行错误都没有**。
  const preflight = fn('check_ports_free');
  // ⛔ 只看代码行：注释里正好在讲这个反例，不能把它算成违规。
  const code = preflight
    .split('\n')
    .filter((line) => !line.trim().startsWith('#'))
    .join('\n');
  const bad = code.match(/;\s*rc=\$\?/g) || [];
  assert.equal(bad.length, 0,
    `check_ports_free 里有 ${bad.length} 处 \`; rc=$?\`：端口空闲时会静默退出，改成 \`|| rc=$?\``);
  assert.match(code, /\|\| rc=\$\?/, '看不到 `|| rc=$?`：port_holder 的退出码没被兜住');
});

test('静态：Windows 版必须有同一套「全新安装」判据（两版语义不能分叉）', () => {
  const ps1 = readFileSync(new URL('../deploy/standalone-windows/uvp-gb28181-ctl.ps1', import.meta.url), 'utf8');
  assert.match(ps1, /function Test-FreshInstall/, 'Windows 版缺少 Test-FreshInstall');
  const calls = ps1.match(/Test-FreshInstall\)/g) || [];
  assert.ok(calls.length >= 2,
    `Windows 版只有 ${calls.length} 处调用 Test-FreshInstall：Resolve-Port 与 Resolve-RtpRange 都要有`);
  assert.equal(ps1.charCodeAt(0), 0xfeff, 'ps1 必须保留 UTF-8 BOM（缺了中文会乱码甚至语法错）');
});
