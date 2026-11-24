import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

// ---------------------------------------------------------------------------
// 契约：Windows 单机版「流媒体服务（ZLM）启动失败」，2026-11-24 在客户现场
//       （1.1.26，D:\uvp28181）定位，这里把修复钉死。
//
// 现象：start.cmd 走完环境检查/建库/端口预检，卡在
//       「✗ 流媒体服务（ZLM）启动失败，详见 logs\zlm.log」。
//       zlm.log 里两条证据：
//         config.cpp:52 loadIniConfig | dump ini file to: ..\config\zlm.ini
//         main.cpp:315 start_main | The api.secret is invalid, modified it to: <随机串>
//       端口全是出厂默认（554 / 1935 / 80 / 443 / 10000 / 8000 / 3478）。
//
// 根因：`Start-Zlm` 传给 MediaServer 的 `-c` 是**手写常量** `..\config\zlm.ini`，
//       而进程工作目录是 `<根>\vendor\zlm` —— 一个 `..` 只上到 `<根>\vendor`，
//       于是去找 `<根>\vendor\config\zlm.ini`（包内根本没有这个目录）。
//       ⇒ `ifstream` 打开失败 ⇒ ZLM 走 catch：打 `dump ini file to:` 并**静默退回
//       出厂默认端口**；随后的 `dumpFile` 也写不进不存在的目录（ofstream 静默失败），
//       所以磁盘上那份 `config\zlm.ini` 毫发无损 —— 现场看起来就是「文件明明在、
//       就是读不到」，极易被误判成杀软抢锁 / 文件内容写坏。
//       Linux 版同一逻辑传的是绝对路径 `$ROOT/config/zlm.ini`，所以从未暴露。
//
// ⛔⛔ 这条为什么必须静态守：它**只在 Windows 真机上**才炸，而且症状（等不到 15504）
//    与「端口被系统保留」完全一样，常规验收会一路查错方向。2026-11-20 的改动正是
//    「只做了代码 review、没在真机跑过」，1.1.26 第一次上线就命中。
// ---------------------------------------------------------------------------

const url = (p) => new URL(p, import.meta.url);
const ctlBuf = readFileSync(url('../deploy/standalone-windows/uvp-gb28181-ctl.ps1'));
const ctl = ctlBuf.toString('utf8').replace(/^\uFEFF/, '');

/** 从 ctl.ps1 里原样抽出一个 PowerShell 函数体（结束的 `}` 顶格）。 */
function psFunction(name) {
  const hit = ctl.match(new RegExp(`^function ${name}\\s*\\{[\\s\\S]*?^\\}`, 'm'));
  assert.ok(hit, `ctl.ps1 里找不到 function ${name}`);
  return hit[0];
}

/** 去掉整行注释 —— 注释里**允许**写事故原文（说明为什么不能那样写），断言只看代码。 */
const stripComments = (s) => s.split('\n').filter((line) => !/^\s*#/.test(line)).join('\n');

const resolveArg = psFunction('Resolve-ZlmIniArg');
const readFailure = psFunction('Test-ZlmIniReadFailure');
const startZlm = psFunction('Start-Zlm');
const startZlmCode = stripComments(startZlm);

// ------------------------------------------------------------- 编码门禁 ----

test('ctl.ps1 仍带 UTF-8 BOM（PS 5.1 否则按 GBK 读中文，可能语法错误）', () => {
  assert.deepEqual([...ctlBuf.subarray(0, 3)], [0xef, 0xbb, 0xbf], 'BOM 丢了，中文注释会乱码');
});

// ------------------------------------------------ ① 路径不许再手写常量 ----

test('Start-Zlm 不许再出现手写的相对路径常量 ..\\config\\zlm.ini', () => {
  // 这条就是事故原文：一个 `..` 只到 vendor\，够不着根下的 config\。
  assert.ok(
    !startZlmCode.includes("'..\\config\\zlm.ini'"),
    '又写回手写常量了：工作目录是 <根>\\vendor\\zlm，至少要两层 ..（改用 Resolve-ZlmIniArg）',
  );
});

test('Start-Zlm 的 -c 必须由 Resolve-ZlmIniArg 计算，且缺副本时退回包内模板', () => {
  assert.match(startZlmCode, /\$iniArg\s*=\s*Resolve-ZlmIniArg/, 'iniArg 没走 Resolve-ZlmIniArg');
  assert.match(startZlmCode, /if\s*\(-not \$iniArg\)\s*\{\s*\$iniArg\s*=\s*'config\.ini'\s*\}/,
    '缺少「没有持久副本就退回包内 config.ini」的兜底');
});

test('Resolve-ZlmIniArg 按目录深度算 ..，不许再出现魔数层数', () => {
  assert.match(resolveArg, /\$sub\s*=\s*\$ZlmDir\.Substring\(\$Root\.Length\)/,
    '没有从 $ZlmDir 相对 $Root 的位置推导深度');
  assert.match(resolveArg, /\$sub\s+-split\s+'\\\\'/, '没有按反斜杠切分目录层级');
  assert.match(resolveArg, /'\.\.\\'\s*\*\s*\$depth/, "没有用 '..\\' * $depth 拼上级层数");
  assert.ok(!/'\.\.\\\.\.\\'/.test(resolveArg), '深度被写死成两层了（应跟着布局算）');
});

test('Resolve-ZlmIniArg 必须用 Test-Path 反查拼出来的路径真实存在', () => {
  assert.match(resolveArg, /\$probe\s*=\s*Join-Path \$ZlmDir \$arg/, '没有把相对路径拼回去校验');
  assert.match(resolveArg, /if\s*\(-not \(Test-Path -LiteralPath \$probe\)\)/, '校验缺失或写反了');
  assert.match(resolveArg, /return \$null/, '校验失败必须返回 $null（交给调用方退回模板），不能照样传给 ZLM');
});

// ------------------------------------------- ② 日志证词：打不开 ≠ 绑不上 ----

test('必须用 dump ini file to 作「打不开配置文件」的判据', () => {
  assert.match(readFailure, /'dump ini file to'/, '判据字符串变了：那是 ZLM loadIniConfig 的 catch 分支唯一输出');
  assert.match(readFailure, /-SimpleMatch/, '必须 SimpleMatch（正则下这句里的空格/冒号含义会漂）');
  assert.match(readFailure, /-Quiet/, '不需要把整行打出来，只要布尔');
});

test('ZLM 启动失败时必须先分诊「打不开配置」再报错', () => {
  const failIndex = startZlm.indexOf('流媒体服务（ZLM）启动失败');
  assert.ok(failIndex > 0, '找不到启动失败的报错行');
  const before = startZlm.slice(0, failIndex);
  assert.match(before, /Test-ZlmIniReadFailure -LogPath \$log/,
    '报错前没有判「是不是打不开配置文件」——现场会把 15504 等不到误判成端口被保留');
  // 提示里必须点明"不是内容写错"，否则下一次还会有人去查 ini 格式
  assert.match(startZlm, /不是内容写错/, '缺少「这不是配置内容问题」的说明');
});

// --------------------------------------------------- ③ 布局前提没被改掉 ----

test('持久副本仍在 <根>\\config\\zlm.ini，模板仍在 vendor\\zlm\\config.ini', () => {
  assert.match(ctl, /^\$ConfDir\s*=\s*Join-Path \$Root 'config'$/m, '$ConfDir 不再是 <根>\\config');
  assert.match(ctl, /^\$ZlmIni\s*=\s*Join-Path \$ConfDir 'zlm\.ini'$/m, '$ZlmIni 不再是 config\\zlm.ini');
  assert.match(ctl, /^\$ZlmDir\s*=\s*Join-Path \$VendorDir 'zlm'$/m, '$ZlmDir 不再是 vendor\\zlm');
  assert.match(ctl, /^\$ZlmIniPkg\s*=\s*Join-Path \$ZlmDir 'config\.ini'$/m, '$ZlmIniPkg 不再是 vendor\\zlm\\config.ini');
});

test('ZLM 的工作目录必须还是 $ZlmDir（相对路径的正确性全靠它）', () => {
  assert.match(startZlm, /Start-Child -Name 'zlm'[^\n]*-WorkDir \$ZlmDir/,
    'Start-Child 的 -WorkDir 变了，相对路径会整片失效');
});
