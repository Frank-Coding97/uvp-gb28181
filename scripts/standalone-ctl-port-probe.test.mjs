import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';

// ---------------------------------------------------------------------------
// 契约：端口预检在「没有 ss（未装 iproute2）」时**必须明说自己跳过了**，
//       不能把"查不到"当成"端口空闲"。
//
// ⛔⛔ 真实风险（2026-10-19 代码复查时发现）：port_holder 原来直接
//   `ss -tln | awk …`，而 ss 缺失时这条管道**失败得很安静** ——
//   `command not found` 走 stderr（被 2>/dev/null 吞掉），awk 拿到空输入 ⇒
//   found=0 ⇒ 返回"空闲"。于是整个端口预检变成"全部通过"，而真实情况是
//   **一个端口都没检查**。后果不是"启动失败"，而是"启动了、但 nginx 连到了
//   别人的服务上，status 还报运行中"（见 check_ports_free 顶部那段事故）。
//
// ⇒ 现在的分工：
//   · port_holder 缺 ss 时返回 **2（无法判断）**，不再假装"空闲"；
//   · check_ports_free 在**自己的 shell 里**打一次提示，并把结论改成"已跳过"
//     （绝不能只打提示、然后照样报"预检通过"）。
//
// ⛔⛔ 为什么提示不能写在 port_holder 里（这条是实测出来的，不是理论）：
//   所有调用点都是 `holder="$(port_holder …)"` —— 命令替换 fork 子 shell，
//   子 shell 里设的"已提示过"标志**传不回父 shell** ⇒ RTP 段那 50 个端口 × 2 协议
//   会把同一段提示打一百多遍。第一版就是这么错的，本文件的"只提示一次"抓住了它。
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
  'C_TAG=""',
  'C_RST=""',
  // port_holder 的兜底分支会拿端口跟这几个变量比对，先给上假值（`set -u` 才不会炸）
  'HTTP_PORT=51002',
  'REDIS_PORT=51003',
  'ZLM_HTTP_PORT=51004',
  'NGINX_HTTPS_PORT=51001',
  fn('has_ss'),
  '_no_ss_noted=""',
  fn('note_no_ss_once'),
  fn('port_holder'),
].join('\n');

const EMPTY_PATH = join(tmpdir(), `uvp-no-ss-${process.pid}`);
const HAS_SS = spawnSync(BASH, ['-c', 'command -v ss'], { encoding: 'utf8' }).stdout.trim() !== '';

// ⛔ 不能用 `for i in $(seq …)`：harness 故意把 PATH 设空（让 command -v ss 失败），
//   而 seq 是**外部命令** ⇒ 循环一次都不跑，输出全空、看起来像"断言失败"。
//   一律用 bash 内建（while / [ ] / $(( ))）。
function runHarness(path, scriptBody) {
  return spawnSync(BASH, ['-c', `set -o pipefail\n${HELPERS}\n${scriptBody}`], {
    encoding: 'utf8',
    env: { PATH: path, HOME: process.env.HOME ?? '/tmp' },
  });
}

/** 在**子 shell**里调 port_holder（与真实调用点同形态），返回占用者/返回码。 */
function probe(path, { port = 51002, proto = 'tcp', times = 1 } = {}) {
  return runHarness(path, `
i=0
while [ "$i" -lt ${times} ]; do
  holder="$(port_holder "${port}" "${proto}")"; rc=$?
  printf 'RC=%s STDOUT=[%s]\\n' "$rc" "$holder"
  i=$(( i + 1 ))
done
`);
}

test('缺 ss 时：port_holder 返回「无法判断」(2)，而不是「空闲」(1)', () => {
  const r = probe(EMPTY_PATH);
  assert.equal(r.status, 0, r.stderr);
  assert.match(r.stdout, /RC=2 STDOUT=\[\]/,
    '缺 ss 时必须返回 2（无法判断）。返回 1 会让端口预检假装全绿：\n' + r.stdout);
});

test('缺 ss 时：port_holder 自己不打提示（提示归 check_ports_free，否则会被子 shell 重复打）', () => {
  const r = probe(EMPTY_PATH, { times: 3 });
  assert.doesNotMatch(r.stderr, /没有 ss/,
    'port_holder 里不该打提示：它在 $( ) 里跑，子 shell 的"已提示"标志传不回去，会刷屏');
});

test('提示函数在同一 shell 里只生效一次', () => {
  const r = runHarness(EMPTY_PATH, 'note_no_ss_once; note_no_ss_once; note_no_ss_once');
  assert.equal(r.status, 0, r.stderr);
  const hits = (r.stderr.match(/没有 ss/g) || []).length;
  assert.equal(hits, 1, `提示重复了 ${hits} 次（应只一次）:\n${r.stderr}`);
  assert.match(r.stderr, /端口预检已跳过/, '提示没说清后果：' + r.stderr);
  assert.match(r.stderr, /iproute2/, '提示里应给出安装 iproute2 的办法：' + r.stderr);
  assert.equal(r.stdout, '', '提示不能走 stdout（stdout 会被调用方当成"明细"收走）');
});

test('有 ss 时提示函数什么都不说', { skip: !HAS_SS }, () => {
  const which = spawnSync(BASH, ['-c', 'command -v ss'], { encoding: 'utf8' }).stdout.trim();
  const r = runHarness(join(which, '..'), 'note_no_ss_once');
  assert.equal(r.status, 0, r.stderr);
  assert.equal(r.stderr, '', '有 ss 时不该提示缺 ss：' + r.stderr);
});

test('静态：port_holder 先探测 ss 再调用 ss，且缺 ss 时返回 2', () => {
  const body = fn('port_holder');
  assert.match(fn('has_ss'), /command -v ss/, 'has_ss 必须真的去探测 ss 命令');
  assert.ok(body.indexOf('has_ss') < body.indexOf('ss "$flag"'),
    'has_ss 探测必须在第一条 ss 调用之前');
  assert.match(body, /has_ss \|\| return 2/, '缺 ss 时必须显式返回 2（无法判断）');
});

test('静态：check_ports_free 先提示、且缺 ss 时不许报"预检通过"', () => {
  const body = fn('check_ports_free');
  assert.match(body, /note_no_ss_once/, 'check_ports_free 必须提示"预检已跳过"');
  assert.ok(body.indexOf('note_no_ss_once') < body.indexOf('_check_one 51000'),
    '提示必须在逐个检查之前打出来');
  assert.match(body, /cannot_judge/, '缺 ss 时必须有专门的结论分支');
  assert.match(body, /端口预检：已跳过/,
    '缺 ss 时不能说"通过"——把"没检查"说成"没问题"比不检查更危险');
});

// ---------------------------------------------------------------------------
// Windows 版 Test-PortsFree 的静态门禁（本机无 pwsh，只能钉代码形态）。
//
// ⛔⛔ 2026-11-10 现场反馈「端口检测也不准」，复查发现 11-09 那次修复
//   （e69365a9「识别出站连接占端口」）**只改了 Linux 的 check_ports_free**，
//   Windows 的端口预检没同步，还额外留了两个洞：
//     · 真绑定探测只认 reserved，探到 busy（出站连接 / TIME_WAIT 占本地端口）
//       直接丢掉 ⇒ 「预检通过 ✓」紧接着服务 bind 失败；
//     · 两条 Get-Net* 命令任一失败就 return，连不依赖它们的真绑定探测都不跑。
//   Linux 版「缺 ss 不许报通过」的硬测试只覆盖了 Linux，这里把 Windows 补上。
// ---------------------------------------------------------------------------
const WIN_CTL = readFileSync(
  new URL('../deploy/standalone-windows/uvp-gb28181-ctl.ps1', import.meta.url), 'utf8');
const winPreflight = (() => {
  const start = WIN_CTL.indexOf('function Test-PortsFree');
  assert.ok(start >= 0, '找不到 Windows 版 Test-PortsFree');
  const end = WIN_CTL.indexOf('\nfunction ', start + 1);
  return WIN_CTL.slice(start, end > start ? end : undefined);
})();

test('静态：Windows 真绑定探到 busy 必须报出来，不许只认 reserved', () => {
  assert.match(winPreflight, /\$bindBusy\s*=\s*@\(\)/, '缺少 busy 结果收集');
  assert.match(winPreflight, /\$state -eq 'busy'\)\s*\{\s*\$bindBusy \+= "UDP /,
    'UDP 探测的 busy 分支');
  assert.match(winPreflight, /\$state -eq 'busy'\)\s*\{\s*\$bindBusy \+= "TCP /,
    'TCP 探测的 busy 分支');
  assert.match(winPreflight, /if \(\$bindBusy\.Count -gt 0\)/, 'busy 收集后必须有失败分支');
  assert.match(winPreflight, /没有进程可停/, 'busy 必须说明这类占用停不掉');
});

test('静态：Windows 占用扫描失败时不能跳过整个预检', () => {
  const catchStart = winPreflight.indexOf('} catch {');
  const catchEnd = winPreflight.indexOf('$conflicts = @()');
  assert.ok(catchStart >= 0 && catchEnd > catchStart, '找不到占用扫描的 catch 块');
  const catchBlock = winPreflight.slice(catchStart, catchEnd);
  assert.doesNotMatch(catchBlock, /\breturn\b/,
    '扫描失败直接 return 会把不依赖扫描的真绑定探测一起跳过（等于一个端口都没查）');
  assert.match(catchBlock, /\$scanOk = \$false/, '扫描失败必须留下"没扫过"的状态');
  assert.match(winPreflight, /if \(\$scanOk\)/, '结论必须按扫描是否成功分叉');
});

test('静态：Windows 预检结论不许把 SIP 端口当范围终点，也不许在没扫描时报"通过"', () => {
  const tail = winPreflight.slice(winPreflight.indexOf('# ⛔ 结论不许把 SIP'));
  assert.ok(tail.length > 0, '找不到预检结论段');
  assert.doesNotMatch(tail, /端口预检通过[^\r\n]*\$SipPort/,
    'SIP 明确不在预检内，拿它当范围终点是误导');
  const elseStart = tail.indexOf('} else {');
  assert.ok(elseStart >= 0, '缺少扫描失败时的结论分支');
  const elseBlock = tail.slice(elseStart);
  assert.doesNotMatch(elseBlock, /端口预检通过/,
    '扫描没做成时不能说"预检通过"——把"没检查"说成"没问题"比不检查更危险');
  assert.match(elseBlock, /端口占用扫描不可用/, '必须明说扫了什么、没扫什么');
});
