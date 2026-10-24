import assert from 'node:assert/strict';
import { spawn, spawnSync } from 'node:child_process';
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { homedir, tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';

// ---------------------------------------------------------------------------
// 契约：升级助手（uvp-updater）的「生命周期」必须同时满足两件互相拉扯的事。
//
// ① **业务 stop 不许停它**（设计契约，不能翻）：
//    升级流程本身就是「先停业务 → 助手替换文件 → 再起业务」，助手被一起杀掉
//    等于升级断在半路。所以 `stop` 只停业务四件套。
//
// ② **但它不能被"看不见"**（2026-10-20 在 220 上真踩到）：
//    `ctl.sh stop` 之后 51000-51064 全释放、`status` 四行全是「未运行」，
//    可 ps 里 `uvp-updater serve` 还在跑（ppid=1 常驻）。用户以为整套都停了。
//    ⇒ 补维度：status 必须显示它、stop 必须提示它、并给一个显式的 stop-updater。
//
// 本文件钉住四条：
//   · `stop)` 分支**不**调 stop_updater（语义没被翻）；
//   · `stop-updater)` / `stop-all)` 存在且真的会停；
//   · `status` 会输出「升级助手」这一行；
//   · 判活不能只看 socket（残留 socket 在进程死后依然存在）；
//   · systemd 托管时走 systemctl 而不是 kill（否则 Restart=on-failure 会拉回来）。
// ---------------------------------------------------------------------------

const script = readFileSync(new URL('../deploy/standalone/uvp-gb28181-ctl.sh', import.meta.url), 'utf8');
const installScript = readFileSync(
  new URL('../deploy/standalone/uvp-gb28181-service-install.sh', import.meta.url),
  'utf8',
);
const BASH = '/bin/bash';

// fn(name) 从 ctl.sh 里原样抽出函数定义（单行 `f() { ... }` 与多行两种形态）。
function fn(name) {
  const oneLine = script.match(new RegExp(`^${name}\\(\\) \\{[^\\n]*\\}$`, 'm'));
  const multiLine = script.match(new RegExp(`^${name}\\(\\) \\{[\\s\\S]*?^\\}`, 'm'));
  const hit = oneLine || multiLine;
  assert.ok(hit, `ctl.sh 里找不到函数 ${name}()`);
  return hit[0];
}

// branches() 把 case 分支按 `  name)` 切开，返回 { 分支名: 分支正文 }。
// ⛔ 不能用简单的行正则：分支正文里嵌着 if/then/else，靠缩进猜边界必错。
//   按「两个字母间距的分支标签行」切，其余整段归上一个分支。
function branches() {
  const lines = script.split('\n');
  let start = -1;
  for (let i = lines.length - 1; i >= 0; i -= 1) {
    if (/^case .+ in$/.test(lines[i])) {
      start = i;
      break;
    }
  }
  assert.ok(start > 0, 'ctl.sh 里找不到顶层 case 分派');
  const body = lines.slice(start + 1);
  const marks = [];
  body.forEach((line, i) => {
    const m = line.match(/^ {2}([a-z][a-z-]*)\)$/);
    if (m) marks.push({ name: m[1], line: i });
  });
  assert.ok(marks.length > 5, `分支解析失败，只认出 ${marks.length} 个分支`);
  const out = {};
  for (let i = 0; i < marks.length; i += 1) {
    const end = i + 1 < marks.length ? marks[i + 1].line : body.length;
    out[marks[i].name] = body.slice(marks[i].line + 1, end).join('\n');
  }
  return out;
}

// ---------------------------------------------------------------- 静态契约 ----

test('stop 只停业务四件套，不许停升级助手（设计语义未被翻转）', () => {
  const b = branches();
  assert.ok(b.stop, '找不到 stop 分支');
  // ⛔ 只看**非注释行**：分支里的注释会提到 stop_updater（"契约见 stop_updater 上方"），
  //   拿整段正文匹配会把注释当成调用，假红。
  const calls = b.stop
    .split('\n')
    .filter((line) => !line.trim().startsWith('#'))
    .join('\n');
  assert.doesNotMatch(calls, /stop_updater/, 'stop 分支调用了 stop_updater —— 升级会在半路被打断');
  for (const needed of ['stop_nginx', 'stop_backend', 'stop_zlm', 'stop_redis']) {
    assert.match(calls, new RegExp(needed), `stop 分支漏了 ${needed}`);
  }
});

test('stop 分支在助手还活着时必须明说，不能只打「已停止」', () => {
  const b = branches();
  assert.match(b.stop, /updater_running/, 'stop 分支没有判断助手是否还在跑');
  assert.match(b.stop, /stop-updater/, 'stop 分支没有告诉用户怎么一并停掉助手');
});

test('提供了 stop-updater 与 stop-all 两个显式入口', () => {
  const b = branches();
  assert.ok(b['stop-updater'], '缺少 stop-updater 分支');
  assert.match(b['stop-updater'], /stop_updater/);
  assert.ok(b['stop-all'], '缺少 stop-all 分支');
  for (const needed of ['stop_nginx', 'stop_backend', 'stop_zlm', 'stop_redis', 'stop_updater']) {
    assert.match(b['stop-all'], new RegExp(needed), `stop-all 分支漏了 ${needed}`);
  }
});

test('status 会输出升级助手一行（不让常驻进程隐形）', () => {
  const b = branches();
  assert.match(b.status, /升级助手/, 'status 没有显示升级助手');
  assert.match(b.status, /updater_status_line/, 'status 应复用 updater_status_line');
});

test('systemd 托管时走 systemctl stop，不能直接 kill', () => {
  const body = fn('stop_updater');
  assert.match(body, /updater_systemd_managed/, 'stop_updater 没有区分 systemd 托管');
  assert.match(body, /systemctl stop/, 'systemd 托管时必须走 systemctl stop');
  // kill 只允许出现在 else（非托管）分支里。
  const sysIdx = body.indexOf('systemctl stop');
  const killIdx = body.indexOf('stop_by_pidfile');
  assert.ok(sysIdx > 0 && killIdx > sysIdx, 'stop_by_pidfile 应在 systemctl 分支之后（作为 else 兜底）');
});

test('单元名在 ctl.sh 与 service-install.sh 里逐字一致', () => {
  const a = script.match(/^UPDATER_UNIT_NAME="([^"]+)"/m)?.[1];
  const b = installScript.match(/^UPDATER_UNIT_NAME="([^"]+)"/m)?.[1];
  assert.ok(a, 'ctl.sh 里没有 UPDATER_UNIT_NAME');
  assert.equal(a, b, 'ctl.sh 与 service-install.sh 的升级助手单元名不一致');
});

test('用法提示把两个新入口列出来了', () => {
  assert.match(script, /stop-all\|restart/, '用法行里没有 stop-all');
  assert.match(script, /stop-updater\|serve-updater/, '用法行里没有 stop-updater');
});

// ------------------------------------------------- 目录信任（2026-10-20 现场）----
// 现场症状：装到「阶段 3/3」才报
//   open trusted updater registration: updater registration parent must not be
//   group/world writable
// —— 114 张表已经建完，报错是英文、不说哪一层、不说怎么改。
// 根因：包解压在 umask 002 建出来的 775 目录里。助手要求「从 / 到 config/
// 每一层都不能对同组/其他人可写」，这是它的安全设计（要以 root 身份替换
// 程序文件），不能放宽 ⇒ 只能做到：**在阶段 1 检查、就地修好、说清楚**。

test('阶段 1 就检查目录信任（不能等建完 114 张表才炸）', () => {
  const b = branches();
  const at = b.start.indexOf('check_updater_trusted_paths');
  assert.ok(at > 0, 'start 分支没有调用 check_updater_trusted_paths');
  const phase2 = b.start.indexOf('阶段 2/3');
  assert.ok(phase2 > 0, '找不到阶段 2/3 的分界');
  assert.ok(at < phase2, '目录信任检查必须在建库建表之前');
});

test('prepare_updater 也要查（systemd / start-updater 会绕过阶段检查）', () => {
  assert.match(fn('prepare_updater'), /check_updater_trusted_paths/);
});

test('修权限只做 go-w，绝不放宽到人人可写', () => {
  const body = fn('check_updater_trusted_paths');
  assert.match(body, /chmod go-w/, '应当是 go-w（去掉组/其他写），不是 chmod 777');
  // ⛔ 只看非注释行：注释里会举例说明"现场为什么是 775"，拿整篇匹配会假红。
  const commands = script
    .split('\n')
    .filter((line) => !line.trim().startsWith('#'))
    .join('\n');
  assert.doesNotMatch(commands, /chmod\s+(-R\s+)?7{3}/, 'ctl.sh 里不许出现 chmod 777');
});

// ⛔⛔ 第二类现场（2026-10-20 客户机 CentOS 7）：权限位全是 755，只有**属主**是
//   1000（包被普通用户解压，tar 默认还原归档里的属主）。助手以 root 跑时只认
//   root 属主的路径 ⇒ 阶段 3/3 报
//     `... parent "/opt/uvp/uvp-gb28181-linux-amd64-1.1.11" owner UID 1000 is not trusted`
//   属主比"组可写"更松：owner 能 chmod 自己拥有的目录，等于能换掉整棵树。

test('阶段 1 也查属主：root 运行时把安装根整棵收回 root', () => {
  const body = fn('check_updater_trusted_paths');
  assert.match(body, /stat -c '%u'/, '没有读属主 ⇒ 只查权限位查不出「属主是普通用户」');
  assert.match(body, /chown -R 0:0/, '安装根必须连子树一起收回（程序文件也能被 owner 替换）');
  assert.match(body, /install_root_child/, '祖先层不能无条件 chown —— 会误伤 /home/<某人>');
  assert.match(body, /属主 UID|不是 root/, '报错里要指出属主，否则现场还是看不懂');
});

test('install_root_child 只认系统安装根的直接子目录（绝不碰别人的家目录）', () => {
  const pkg = makePkg();
  try {
    const probe = runHarness(
      pkg,
      `for p in /opt/uvp /usr/local/uvp /srv/uvp /var/lib/uvp /home/alice /home/alice/uvp /root/uvp; do
  if install_root_child "$p"; then printf 'YES %s\\n' "$p"; else printf 'NO %s\\n' "$p"; fi
done`,
    );
    assert.equal(probe.status, 0, probe.stderr);
    for (const p of ['/opt/uvp', '/usr/local/uvp', '/srv/uvp', '/var/lib/uvp']) {
      assert.match(probe.stdout, new RegExp(`^YES ${p}$`, 'm'), `${p} 应当可以自动收回 root`);
    }
    // ⛔ 安全底线：把别人的家目录 chown 成 root 比"装不上"严重得多。
    assert.match(probe.stdout, /^NO \/home\/alice$/m);
    assert.match(probe.stdout, /^NO \/home\/alice\/uvp$/m);
    assert.match(probe.stdout, /^NO \/root\/uvp$/m);
  } finally {
    pkg.cleanup();
  }
});

// ---------------------------------------------------------------- 行为契约 ----

const HELPERS = [
  fn('has_pgrep'),
  fn('proc_pids_by_exe'),
  fn('proc_alive_by_exe'),
  '_no_pgrep_noted=""',
  fn('note_no_pgrep_once'),
  fn('stop_by_pidfile'),
  fn('updater_pid'),
  fn('updater_running'),
  fn('updater_systemd_managed'),
  fn('updater_status_line'),
  fn('stop_updater'),
  fn('install_root_child'),
  fn('check_updater_trusted_paths'),
].join('\n');

// makePkg 造一个最小安装目录：真的可执行文件（停进程时兜底要求 -x）+ run/ 目录。
function makePkg() {
  const root = mkdtempSync(join(tmpdir(), 'uvp-updater-life-'));
  const bin = join(root, 'updater', 'uvp-updater');
  mkdirSync(join(root, 'updater'), { recursive: true });
  mkdirSync(join(root, 'run'), { recursive: true });
  // 桩程序：只能被 exec，不需要真的实现协议。
  writeFileSync(bin, '#!/bin/sh\nexit 0\n');
  chmodSync(bin, 0o755);
  return {
    root,
    bin,
    pidFile: join(root, 'run', 'updater.pid'),
    socket: join(root, 'run', 'updater.sock'),
    cleanup: () => rmSync(root, { recursive: true, force: true }),
  };
}

// spawnFakeUpdater 起一个 cmdline 里带 $bin 绝对路径的进程（冒充助手常驻）。
// 用 `exec -a` 伪造 argv[0]，正是 proc_pids_by_exe 要找的东西。
// ⛔ 必须等 exec 真正生效再返回：pgrep -f 是在 exec 之后才能看到新 argv 的，
//   不等就是「进程刚 fork、argv 还是 bash -c …」的假红。
async function spawnFakeUpdater(bin) {
  const child = spawn(BASH, ['-c', `exec -a ${JSON.stringify(bin)} sleep 30`], { stdio: 'ignore' });
  for (let i = 0; i < 40; i += 1) {
    const hit = spawnSync(BASH, ['-c', `pgrep -f ${JSON.stringify(bin)} || true`], { encoding: 'utf8' });
    if (hit.stdout.includes(String(child.pid))) return child;
    await new Promise((r) => setTimeout(r, 25));
  }
  return child;
}

// runHarness 在抽出来的函数上下文里跑一段动作脚本。
// ⛔ 目标路径一律走环境变量：写进正文会出现在 `bash -c` 的 argv 里，
//   而 proc_pids_by_exe 是「cmdline 含该路径」即命中 ⇒ 会把自己当目标（假红/假绿）。
function runHarness(pkg, action) {
  const body = `
set -uo pipefail
log()  { printf '[log] %s\\n' "$*"; }
ok()   { printf '[ok] %s\\n' "$*"; }
det()  { printf '[det] %s\\n' "$*"; }
fail() { printf '[fail] %s\\n' "$*" >&2; exit 1; }
UPDATER_UNIT_NAME="uvp-gb28181-updater.service"
UPDATER_PID_FILE="$UVP_T_PIDFILE"
UPDATER_SOCKET="$UVP_T_SOCKET"
UPDATER_BIN="$UVP_T_BIN"
ROOT="$UVP_T_ROOT"
${HELPERS}
${action}
`;
  return spawnSync(BASH, ['-c', body], {
    encoding: 'utf8',
    env: {
      PATH: process.env.PATH ?? '/usr/bin:/bin',
      HOME: process.env.HOME ?? '/tmp',
      UVP_T_PIDFILE: pkg.pidFile,
      UVP_T_SOCKET: pkg.socket,
      UVP_T_BIN: pkg.bin,
      UVP_T_ROOT: pkg.root,
    },
  });
}

const alive = (pid) => {
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
};

test('pid 文件有效时：判活为真、status 带 PID、stop_updater 真能停掉', async () => {
  const pkg = makePkg();
  const fake = await spawnFakeUpdater(pkg.bin);
  try {
    writeFileSync(pkg.pidFile, `${fake.pid}\n`);
    const probe = runHarness(
      pkg,
      `
updater_running && printf 'RUNNING=1\\n' || printf 'RUNNING=0\\n'
printf 'LINE=%s\\n' "$(updater_status_line)"
`,
    );
    assert.equal(probe.status, 0, probe.stderr);
    assert.match(probe.stdout, /RUNNING=1/, '助手在跑却判成未运行');
    assert.match(probe.stdout, new RegExp(`LINE=运行中（PID ${fake.pid}）`));

    const stop = runHarness(pkg, 'stop_updater');
    assert.equal(stop.status, 0, stop.stderr);
    assert.match(stop.stdout, /升级助手已停止/);
    assert.equal(existsSync(pkg.pidFile), false, 'stop_updater 后 pid 文件应被清掉');
    await new Promise((r) => setTimeout(r, 200));
    assert.equal(alive(fake.pid), false, 'stop_updater 后进程应该真的没了');
  } finally {
    pkg.cleanup();
    try {
      fake.kill('SIGKILL');
    } catch {
      /* 已经退了 */
    }
  }
});

test('pid 文件丢了但进程还在：仍判运行中，且兜底能停掉（不能"什么都没停"）', async () => {
  const pkg = makePkg();
  const fake = await spawnFakeUpdater(pkg.bin);
  try {
    // 故意不写 pid 文件 —— 对应「rm -rf run/ 后重新解压」的现场。
    const probe = runHarness(
      pkg,
      `updater_running && printf 'RUNNING=1\\n' || printf 'RUNNING=0\\n'`,
    );
    assert.equal(probe.status, 0, probe.stderr);
    assert.match(probe.stdout, /RUNNING=1/, 'pid 文件缺失时按可执行路径应能找到助手');

    const stop = runHarness(pkg, 'stop_updater');
    assert.equal(stop.status, 0, stop.stderr);
    await new Promise((r) => setTimeout(r, 200));
    assert.equal(alive(fake.pid), false, '兜底路径没能停掉助手 —— 会留下看不见的常驻进程');
  } finally {
    pkg.cleanup();
    try {
      fake.kill('SIGKILL');
    } catch {
      /* 已经退了 */
    }
  }
});

test('只剩残留 socket、进程已死：必须判「未运行」（不能永远报运行中）', () => {
  const pkg = makePkg();
  try {
    // 造一个普通文件冒充残留 socket 路径（unlink 后进程也没了）。
    writeFileSync(pkg.socket, '');
    const probe = runHarness(
      pkg,
      `
updater_running && printf 'RUNNING=1\\n' || printf 'RUNNING=0\\n'
printf 'LINE=%s\\n' "$(updater_status_line)"
`,
    );
    assert.equal(probe.status, 0, probe.stderr);
    assert.match(probe.stdout, /RUNNING=0/, '只看 socket 判活 ⇒ 进程死后会永远报运行中');
  } finally {
    pkg.cleanup();
  }
});

test('什么都没跑时 stop_updater 是幂等的（rc=0，不报错）', () => {
  const pkg = makePkg();
  try {
    const stop = runHarness(pkg, 'stop_updater');
    assert.equal(stop.status, 0, `空跑 stop_updater 不应失败：${stop.stderr}`);
    assert.match(stop.stdout, /升级助手已停止/);
  } finally {
    pkg.cleanup();
  }
});

// ⛔ 这个用例必须建在 $HOME 下，不能用 os.tmpdir()：/tmp 是 1777（root 所有），
//   逐层往上走时会被判成"不可信且改不动"，那测的就不是我们要测的东西了。
test('安装目录是 775 时自动收成 755（不再让助手起不来）', () => {
  const base = mkdtempSync(join(homedir(), '.uvp-perm-'));
  const root = join(base, 'uvp-gb28181');
  mkdirSync(join(root, 'updater'), { recursive: true });
  const bin = join(root, 'updater', 'uvp-updater');
  writeFileSync(bin, '#!/bin/sh\nexit 0\n');
  chmodSync(bin, 0o755);
  chmodSync(root, 0o775);
  const pkg = {
    root,
    bin,
    pidFile: join(root, 'run', 'updater.pid'),
    socket: join(root, 'run', 'updater.sock'),
  };
  try {
    const r = runHarness(
      pkg,
      `check_updater_trusted_paths
printf 'MODE=%s\\n' "$(stat -c '%a' "$ROOT" 2>/dev/null || stat -f '%Lp' "$ROOT")"`,
    );
    assert.equal(r.status, 0, `775 的安装目录应当被就地修好，而不是直接失败：${r.stderr}`);
    assert.match(r.stdout, /已收紧目录权限/, '修权限必须留日志，不能悄悄改');
    assert.match(r.stdout, /MODE=755/, '目录应被收成 755');
  } finally {
    chmodSync(base, 0o700);
    rmSync(base, { recursive: true, force: true });
  }
});
