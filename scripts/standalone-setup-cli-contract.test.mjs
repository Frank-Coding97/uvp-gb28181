import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

// ============================================================================
// 契约：安装脚本与 bin/uvp-setup 之间的「命令行接口」不许漂移。
//
// 背景：2026-10-19 起，Linux 绿色包把所有"需要解释器"的活都搬进了包内的 Go 二进制
//   bin/uvp-setup（子命令 db-init / certgen / ports-sync / secret-sync /
//   https-state / config-get / reset-admin）。于是多出一条**跨语言**的接缝：
//   shell 里写 "`"$SETUP_BIN" ports-sync --zlm-rtp-range ...`"，Go 那边必须真的
//   注册了这个名字。
//
// 这条接缝坏掉时**两边各自都完全正常**：shell 语法没错、Go 编译也过 ——
//   要等到客户机上跑 start 才会报
//     flag provided but not defined: -zlm-rtp-range
//   或者干脆静默把值写错键。所以这里用静态解析把两边钉在一起：
//   ① 脚本里出现的每个子命令，必须在 main.go 的 switch 里有 case；
//   ② 脚本里出现的每个 --flag，必须在某个 uvp-setup/*.go 里注册过。
//
// ⛔ 为什么不做成"跑一次 --help 抓输出"：那要求跑测试的机器能 `go build`，
//   而且 --help 只列 usage 文案、**不列 flag**（我们的 usage 是手写文本，
//   和 flag 注册表本来就可能不一致）。静态解析 Go 源码反而是最准的。
// ============================================================================

const ROOT = fileURLToPath(new URL('../', import.meta.url));
const SETUP_DIR = join(ROOT, 'server/cmd/uvp-setup');
const LINUX_CTL = join(ROOT, 'deploy/standalone/uvp-gb28181-ctl.sh');
const WIN_CTL = join(ROOT, 'deploy/standalone-windows/uvp-gb28181-ctl.ps1');

// ---- Go 侧：子命令白名单 + 已注册的 flag 集合 -------------------------------

function dispatchedSubcommands() {
  const main = readFileSync(join(SETUP_DIR, 'main.go'), 'utf8');
  // 只认 `case "xxx":` 这种纯小写命令名；`case "-h", "--help", "help":` 不会命中。
  return new Set([...main.matchAll(/case "([a-z][a-z0-9-]*)":/g)].map((m) => m[1]));
}

function registeredFlags() {
  const flags = new Set();
  for (const name of readdirSync(SETUP_DIR).filter((f) => f.endsWith('.go') && !f.endsWith('_test.go'))) {
    const body = readFileSync(join(SETUP_DIR, name), 'utf8');
    // fs.String / fs.Int / fs.Bool —— 名字是第一个参数。
    for (const m of body.matchAll(/fs\.(?:String|Int|Bool)\("([a-z0-9-]+)"/g)) flags.add(m[1]);
    // ⛔ fs.Var(&x, "name", …) 是"自定义类型"的注册法（--ip 走的就是这条）：
    //   第一参数是接收器、第二个才是名字，光收 String/Int/Bool 会漏掉它。
    for (const m of body.matchAll(/fs\.Var\([^,]+,\s*"([a-z0-9-]+)"/g)) flags.add(m[1]);
  }
  return flags;
}

// ---- shell 侧：剥掉注释，收集 `$SETUP_BIN <sub>` 与 `args=(<sub> ...` ------

function collectFlags(chunk, into) {
  // ⛔ 必须容忍前导空白/引号：`--env` 是裸词，`"--env"` 也可能出现。
  for (const m of chunk.matchAll(/(?:^|[\s"']);?--([a-z][a-z0-9-]*)/gm)) into.add(m[1]);
}

function shellCalls(text) {
  // 整行注释会解释历史原因（里面也有 --xxx 字样），必须先剥掉。
  const body = text
    .split('\n')
    .filter((line) => !/^\s*#/.test(line))
    .join('\n');

  const subs = new Set();
  const flags = new Set();

  // ① 数组式：`local -a args=(ports-sync … --user-set "$user_set")`
  //   ⛔⛔ 这个数组**没有反斜杠续行**，参数按行往下排 —— 第一版只按"含 $SETUP_BIN
  //     或含 args=(" 的**单行**"收集，于是后面十几行 flag 全被漏掉（而且测试照样绿：
  //     sync_zlm_ports_ini 那串带反斜杠的参数把 zlm-* 补齐了，看起来像是都收到了）。
  //   ⇒ 必须先按括号取出整个数组体，再在体内收 flag。
  //   ⛔ 用 `[^()]*` 而不是贪婪：这些数组值里没有嵌套括号，遇到嵌套宁可跳过也不错杀。
  for (const m of body.matchAll(/\bargs=\(([^()]*)\)/g)) {
    const inner = m[1];
    const first = inner.match(/^\s*([a-z][a-z0-9-]*)/);
    if (first) subs.add(first[1]);
    collectFlags(inner, flags);
  }

  // ② 直接调用（参数用反斜杠续行，先拼成逻辑行）
  const joined = body.replace(/\\\n/g, ' ');
  for (const line of joined.split('\n')) {
    if (!line.includes('SETUP_BIN')) continue;
    for (const m of line.matchAll(/\$SETUP_BIN"?\s+([a-z][a-z0-9-]*)/g)) subs.add(m[1]);
    collectFlags(line, flags);
  }

  return { subs, flags };
}

/** PowerShell 同样处理：`& $SetupExe db-init …` 与 `@('ports-sync', '--env', …)`。 */
function psCalls(text) {
  const body = text.replace(/<#[\s\S]*?#>/g, '');
  const joined = body
    .split('\n')
    .filter((line) => !/^\s*#/.test(line))
    .join('\n');

  const subs = new Set();
  const flags = new Set();

  // ① 直接调用：`& $SetupExe db-init --baseline …`
  for (const m of joined.matchAll(/&\s*\$SetupExe\s+([a-z][a-z0-9-]*)/g)) subs.add(m[1]);

  // ② 参数数组：`$syncArgs = @('ports-sync', '--env', …)`
  //    ⛔ 只认「数组里含 '--flag'」的 —— 这个文件还有一堆 `@('nginx-client-body', …)`
  //      之类的目录清单，按 "@(' 开头"盲收会把它们当成子命令（第一版就是这么错的）。
  //    ⛔ 匹配 `[^()]*`：这些数组里没有嵌套括号，遇到嵌套就跳过，宁漏不误报。
  for (const m of joined.matchAll(/@\(([^()]*)\)/g)) {
    const inner = m[1];
    if (!/'--/.test(inner)) continue;
    const first = inner.match(/'([a-z][a-z0-9-]*)'/);
    if (first) subs.add(first[1]);
  }

  // flag 有加引号的（`'--env'`）也有不加的（`--root $Root`）—— 两种都要收。
  for (const m of joined.matchAll(/(?:'|\s)--([a-z][a-z0-9-]*)/g)) flags.add(m[1]);

  return { subs, flags };
}

// ⛔ 只对「真的会打到 uvp-setup 的那几个变量」收 flag：`--` 开头的字符串在脚本里
//   还可能出现在别处（比如给用户的提示文本）。上面两个收集器已经按调用点限定了范围。

function diff(used, known) {
  return [...used].filter((x) => !known.has(x)).sort();
}

test('Linux ctl.sh 用到的 uvp-setup 子命令都在 Go 的 dispatch 里', () => {
  const known = dispatchedSubcommands();
  assert.ok(known.size >= 7, `只解析出 ${known.size} 个子命令，main.go 的 switch 结构变了？`);

  const { subs } = shellCalls(readFileSync(LINUX_CTL, 'utf8'));
  assert.ok(subs.size >= 5, `只从 ctl.sh 解析出 ${subs.size} 个子命令，解析规则可能失效了`);
  assert.deepEqual(diff(subs, known), [],
    'ctl.sh 调了 uvp-setup 不认识的子命令 —— 客户机上会直接报「未知子命令」并中断安装');
});

test('Windows ctl.ps1 用到的 uvp-setup 子命令都在 Go 的 dispatch 里', () => {
  const known = dispatchedSubcommands();
  const { subs } = psCalls(readFileSync(WIN_CTL, 'utf8'));
  assert.ok(subs.size >= 3, `只从 ctl.ps1 解析出 ${subs.size} 个子命令，解析规则可能失效了`);
  assert.deepEqual(diff(subs, known), [],
    'ctl.ps1 调了 uvp-setup 不认识的子命令');
});

test('两个平台传给 uvp-setup 的 --flag 都真的注册过', () => {
  const known = registeredFlags();
  assert.ok(known.size >= 20, `只解析出 ${known.size} 个 flag，注册方式变了（换成 fs.StringVar？）`);

  // 解析器自检：`--user-set` **只**出现在 ctl.sh 的多行参数数组里，
  // 收不到它就说明"数组体没被扫"——那样这个用例会大片假绿（第一版就是这么错的）。
  const linux = shellCalls(readFileSync(LINUX_CTL, 'utf8'));
  assert.ok(linux.flags.has('user-set'),
    '没从 ctl.sh 的参数数组里收到 --user-set —— 数组体扫描失效了');
  assert.ok(linux.flags.size >= 15, `只收到 ${linux.flags.size} 个 flag，明显偏少`);

  for (const [label, file] of [['Linux ctl.sh', LINUX_CTL], ['Windows ctl.ps1', WIN_CTL]]) {
    const text = readFileSync(file, 'utf8');
    const { flags } = file.endsWith('.ps1') ? psCalls(text) : shellCalls(text);
    assert.ok(flags.size > 0, `${label}: 一个 --flag 都没解析出来`);
    assert.deepEqual(diff(flags, known), [],
      `${label} 传了 uvp-setup 没注册的 flag —— Go 的 flag 包会抛\n` +
      '`flag provided but not defined: -xxx` 并以 2 退出，安装当场中断。\n' +
      '（改 flag 名时两边要一起改；只改 Go 或只改脚本都会在这里被抓住。）');
  }
});

test('两个平台的 ZLM 端口组参数一致（同一套 --zlm-* 键，不能一边多一边少）', () => {
  // ⛔ 端口同步是"三处配置同源"的唯一入口：Linux 少传一个 --zlm-xxx，那份配置就
  //   停在 ZLM 出厂默认上，而 Windows 却正常 —— 同一个包在两个平台表现不同，
  //   排查成本极高。这里比对的是**端口键集合**，不比对值。
  //
  // ⛔ 只挑 ports-sync 那一组：`--zlm-pem` 是 certgen 的参数（ZLM 单文件 PEM 的输出
  //   路径），按 "zlm-" 前缀盲筛会把它算进来，然后两侧"不一致"——其实是两回事。
  const PORT_KEYS = new Set([
    'zlm-http', 'zlm-ssl', 'zlm-rtsp', 'zlm-rtmp', 'zlm-rtc',
    'zlm-signaling', 'zlm-signaling-ssl', 'zlm-srt', 'zlm-onvif', 'zlm-ice',
    'zlm-rtp-proxy', 'zlm-rtp-range', 'zlm-ffmpeg',
  ]);
  const zlmKeys = (flags) => [...flags].filter((f) => PORT_KEYS.has(f)).sort();

  const linuxFlags = shellCalls(readFileSync(LINUX_CTL, 'utf8')).flags;
  const winFlags = psCalls(readFileSync(WIN_CTL, 'utf8')).flags;

  const linux = zlmKeys(linuxFlags);
  const win = zlmKeys(winFlags);
  assert.ok(linux.length >= 11, `Linux 侧只解析到 ${linux.length} 个 --zlm-* 参数`);
  assert.deepEqual(linux, win,
    '两个平台传给 ports-sync 的 --zlm-* 键不一致（左边 Linux / 右边 Windows）：\n' +
    `  Linux 独有: ${linux.filter((k) => !win.includes(k)).join(', ') || '（无）'}\n` +
    `  Windows 独有: ${win.filter((k) => !linux.includes(k)).join(', ') || '（无）'}`);
});
