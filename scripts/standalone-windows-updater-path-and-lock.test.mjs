import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

// ---------------------------------------------------------------------------
// 契约：Windows 单机版「服务升级」页报 503 的两个真 bug，2026-11-10 在
//       192.168.10.222（1.1.21 全量包）上现场定位，这里把修复钉死。
//
// ① **socket 路径长度**：AF_UNIX 的 sun_path 共 108 字节（含结尾 NUL），
//    可用 107 字节。包内 run\updater.sock 随解压目录变长；现场解压套了两层
//    同名目录 ⇒ 正好 108 字节 ⇒ 助手 `bind: invalid argument`、后端
//    `connect: invalid argument`，页面统一显示「升级助手未运行」。
//    修复：启动脚本超限自动改用固定短目录；Go 两侧（助手启动 / 后端拨号）
//    提前校验并给出可操作的错误，不再只丢一句 invalid argument。
//
// ② **icacls /T 破坏进程授权锁**：收紧目录权限时，带 (OI)(CI) 的 ACE 递归到
//    普通文件不生效 ⇒ 已存在的 api-authority.lock 被剥光 DACL（空 = 拒绝所有人）
//    ⇒ 后端 phase=process_authority 拒绝启动。**只在第二次启动触发**：全新安装
//    时锁文件还不存在，所以常规验收根本发现不了。
//    修复：收紧目录后单独恢复锁文件的继承授权，且不删锁文件（删了会换 domainID）。
//
// ⛔ 本文件只做静态契约（mac/Linux 上跑不了 PowerShell），行为测试在 Go 侧：
//    server/internal/upgrade/socket_path_test.go。
// ---------------------------------------------------------------------------

const url = (p) => new URL(p, import.meta.url);
const ctlBuf = readFileSync(url('../deploy/standalone-windows/uvp-gb28181-ctl.ps1'));
const ctl = ctlBuf.toString('utf8').replace(/^\uFEFF/, '');
const goSocket = readFileSync(url('../server/internal/upgrade/socket_path.go'), 'utf8');
const goDaemon = readFileSync(url('../server/internal/upgrade/daemon.go'), 'utf8');
const goMain = readFileSync(url('../server/cmd/uvp-updater/main.go'), 'utf8');
const goLock = readFileSync(url('../server/app/openapi/processauthority/local_lock.go'), 'utf8');
const readme = readFileSync(url('../deploy/standalone-windows/README.md'), 'utf8');

/** 从 ctl.ps1 里原样抽出一个 PowerShell 函数体（结束的 `}` 顶格）。 */
function psFunction(name) {
  const hit = ctl.match(new RegExp(`^function ${name}\\s*\\{[\\s\\S]*?^\\}`, 'm'));
  assert.ok(hit, `ctl.ps1 里找不到 function ${name}`);
  return hit[0];
}

const goLimit = Number(goSocket.match(/MaxSocketPathBytes\s*=\s*(\d+)/)?.[1]);
const resolve = psFunction('Resolve-UpdaterSocketPath');
const authority = psFunction('Initialize-ProcessAuthorityDir');
const startUpdater = psFunction('Start-Updater');

// ---------------------------------------------------------------- 编码门禁 ----

test('ctl.ps1 仍是 UTF-8 with BOM + 纯 CRLF（PS 5.1 会按 GBK 读无 BOM 的中文）', () => {
  assert.deepEqual([...ctlBuf.subarray(0, 3)], [0xef, 0xbb, 0xbf], 'BOM 丢了，中文注释会乱码');
  const text = ctlBuf.toString('utf8');
  const lf = (text.match(/\n/g) || []).length;
  const crlf = (text.match(/\r\n/g) || []).length;
  assert.equal(lf, crlf, `出现 ${lf - crlf} 个裸 LF`);
});

// ------------------------------------------------------- ① socket 路径长度 ----

test('Go 与 ctl.ps1 的 socket 上限必须是同一个 107 字节', () => {
  assert.equal(goLimit, 107, 'Go 侧上限变成了别的值，请同步检查 sun_path 口径');
  assert.equal(Number(resolve.match(/\$limit\s*=\s*(\d+)/)?.[1]), goLimit,
    'PS 与 Go 各写了一份上限 —— 分家后深路径会重新静默失败');
});

test('长度按 UTF-8 字节数判，不能按字符数（中文一个字 3 字节）', () => {
  assert.match(resolve, /Encoding\]::UTF8\.GetByteCount\(\$Preferred\)/);
  assert.doesNotMatch(resolve, /\$Preferred\.Length/, '.Length 是字符数，中文目录会漏判');
  assert.match(goSocket, /len\(path\)\s*>\s*MaxSocketPathBytes/, 'Go 侧也要按字节判');
});

test('超限自动回退到固定短目录，且与安装根解耦、多实例隔离', () => {
  assert.match(ctl, /\$UpdaterSocket\s*=\s*Resolve-UpdaterSocketPath/,
    '$UpdaterSocket 必须走解析函数，不能直接 Join-Path $RunDir');
  assert.doesNotMatch(ctl, /\$UpdaterSocket\s*=\s*Join-Path \$RunDir/);
  assert.match(resolve, /\$env:ProgramData/, '短路径不能挂在安装根下面');
  assert.match(resolve, /'C:\\ProgramData'/, '非交互会话取不到 ProgramData 时要有兜底');
  assert.match(resolve, /'uvp-updater'/);
  // 安装根指纹：就地升级（替换包内文件、根不变）前后必须算出同一个 socket，
  // 否则助手还在旧 socket 上监听，业务重启后就再也连不上它。
  assert.match(resolve, /SHA1/);
  assert.match(resolve, /ToUpperInvariant/, 'Windows 路径大小写不敏感，指纹前须归一化');
});

test('回退路径自己也要再预检一次，且报错必须给可操作的指引', () => {
  assert.match(resolve, /GetByteCount\(\$fallback\)\s*-gt\s*\$limit/);
  assert.match(resolve, /更短的目录/);
  assert.match(resolve, /同名文件夹层/, '现场正是套了两层同名目录，提示要点破');
});

test('助手与后端必须读同一个 $UpdaterSocket（一处真源）', () => {
  const envAssignments = ctl.match(/\$env:UVP_UPDATER_SOCKET = \$UpdaterSocket/g) || [];
  assert.equal(envAssignments.length, 2, 'Start-Backend 与 Start-Updater 各要设一次');
  assert.match(ctl, /'--socket', \(Quote-NativeArg \$UpdaterSocket\)/);
});

test('深路径回退时启动输出必须明说，别让现场以为包内 run 目录出了事', () => {
  assert.match(startUpdater, /\$script:UpdaterSocketFallback/);
  assert.match(startUpdater, /Write-UvpWarn/);
  assert.match(startUpdater, /短路径/);
});

test('Go 两侧（助手启动 / 后端拨号）都要提前校验，不能只信部署层', () => {
  assert.match(goMain, /upgrade\.ValidateSocketPath\(socketPath\)/);
  assert.match(goDaemon, /ValidateSocketPath\(socketPath\)/);
});

test('现场复现：108 字节被 107 上限拦下，改名 app 后 79 字节放行', () => {
  const nested = String.raw`C:\Users\lnsoft\Downloads\uvp-gb28181-windows-amd64-1.1.21\uvp-gb28181-windows-amd64-1.1.21\run\updater.sock`;
  const renamed = String.raw`C:\Users\lnsoft\Downloads\uvp-gb28181-windows-amd64-1.1.21\app\run\updater.sock`;
  assert.equal(Buffer.byteLength(nested, 'utf8'), 108);
  assert.equal(Buffer.byteLength(renamed, 'utf8'), 79);
  assert.ok(Buffer.byteLength(nested) > goLimit);
  assert.ok(Buffer.byteLength(renamed) <= goLimit);
  const fixedShort = String.raw`C:\ProgramData\uvp-updater\u-0123456789ab.sock`;
  assert.ok(Buffer.byteLength(fixedShort) <= goLimit, '固定短路径自己不能超限');
});

test('安装说明要提醒路径长度，别只写「不含中文和空格」', () => {
  assert.match(readme, /107 字节/);
  assert.match(readme, /同名文件夹/);
});

// ------------------------------------------------- ② icacls /T 破坏锁文件 ----

test('已存在的 api-authority.lock 必须显式授权：不许 /reset（父目录没 ACE 时会留空 DACL）', () => {
  // /reset 会沿用父目录当时可继承的 ACE；父目录一旦没给出 ACE（收紧命令因
  // 「一次 icacls 多个 /grant:r ⇒ 退出码 87，整条不生效」而失败时就是这样），
  // 结果就是「受保护 + 零条 ACE」的死锁文件 —— 2026-11-26 在 222 上实测复现。
  // 注释里写着「别用 /reset」是正常的，只检查真正执行的行。
  const authCode = authority.split('\n').filter((l) => !l.trim().startsWith('#')).join('\n');
  assert.doesNotMatch(authCode, /\/reset/, '不许再用 /reset 恢复锁文件授权');
  assert.match(authority, /Join-Path \$Path 'api-authority\.lock'/);
  assert.match(authority, /Test-Path -LiteralPath \$lockFile/,
    '首次启动锁文件还不存在，不能无条件动它的 ACL');

  // 三条受信身份（当前用户 / SYSTEM / Administrators）都要显式落到文件上。
  const grants = authority.match(/Set-ProcessAuthorityAce -Path \$lockFile[^\n]*/g) || [];
  assert.equal(grants.length, 3, '锁文件必须逐条授权三项受信身份');
  assert.ok(grants.some((g) => /\$sid/.test(g)), '必须含当前用户 SID（提权创建后普通令牌才读得动）');
  assert.ok(grants.some((g) => g.includes('S-1-5-18')), '必须含 SYSTEM');
  assert.ok(grants.some((g) => g.includes('S-1-5-32-544')), '必须含 Administrators');
});

test('一次 icacls 只能带一个 /grant:r（多个即退出码 87，一个 ACL 都不改）', () => {
  // ⛔ 2026-11-26 踩过：把三条 /grant:r 塞进一次调用，icacls 直接「无效参数」
  //   返回 87，而 stdout 是本地化文本，`Failed processing` 那种英文正则匹配不到，
  //   于是脚本安静地以为权限已收紧。⇒ 现在只允许 Set-ProcessAuthorityAce 里出现
  //   唯一一处 '/grant:r'（每条 ACE 一次调用），且必须看 $LASTEXITCODE。
  const occurrences = (ctl.match(/'\/grant:r'/g) || []).length;
  assert.equal(occurrences, 1, `ctl.ps1 里 '/grant:r' 应只在 Set-ProcessAuthorityAce 出现一次，实际 ${occurrences} 次`);
  const helper = psFunction('Set-ProcessAuthorityAce');
  assert.match(helper, /\/grant:r/, '辅助函数负责授权');
  assert.match(helper, /\$LASTEXITCODE/, '必须用退出码判断成败，别只看输出文本');
});

test('锁文件名必须与 Go 侧 lockName 常量一致（跨语言，别一处改一处不改）', () => {
  const name = goLock.match(/const lockName = "([^"]+)"/)?.[1];
  assert.ok(name, 'Go 侧找不到 lockName 常量');
  assert.ok(authority.includes(`'${name}'`), `PS 侧没有用 ${name}`);
});

test('反向契约：不许删锁文件救急（会换新 domainID ⇒ database_domain_mismatch）', () => {
  const code = authority.split('\n').filter((l) => !l.trim().startsWith('#')).join('\n');
  assert.doesNotMatch(code, /Remove-Item[^\n]*api-authority\.lock/i);
});

// ------------------------------------------- ③ 停止/退出要看得见反馈 ----
// 老板 2026-11-26：「停止退出的时候能不能也弹出 cmd 弹窗，最起码用户能看到反馈，
// 托盘上是有悬浮提示但是用户不一定悬浮去看」。
// 于是停止和启动一样走可见控制台窗口，托盘靠一个回报文件判断窗口里那一轮停完没有。
// ⛔ 别退回「托盘静默跑 ctl.ps1 stop + 消息框」：那十几秒里用户什么也看不到，
//    222 现场就是把它反馈成「点退出没反应 / 退不掉」。

const trayGo = readFileSync(url('../server/cmd/uvp-tray/main.go'), 'utf8');
const startCmd = readFileSync(url('../deploy/standalone-windows/start.cmd'), 'utf8');
const stopCmd = readFileSync(url('../deploy/standalone-windows/stop.cmd'), 'utf8');

test('托盘窗口标题必须与脚本里的 title 一致（改名会让托盘找不到窗口、只能空等超时）', () => {
  for (const [goConst, cmdText, cmdName] of [
    ['startWindowTitle', startCmd, 'start.cmd'],
    ['stopWindowTitle', stopCmd, 'stop.cmd'],
  ]) {
    const want = trayGo.match(new RegExp(`const ${goConst} = "([^"]+)"`))?.[1];
    assert.ok(want, `main.go 里找不到常量 ${goConst}`);
    const escaped = want.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    assert.match(cmdText, new RegExp(`^title ${escaped}[ \\t]*$`, 'm'),
      `${cmdName} 的 title 必须与 ${goConst}（${want}）逐字一致`);
  }
});

test('stop.cmd 必须在 pause 之前回报退出码（窗口会停在 pause 上，之后才写就永远等不到）', () => {
  const envName = trayGo.match(/const stopMarkerEnv = "([^"]+)"/)?.[1];
  assert.ok(envName, 'main.go 里找不到 stopMarkerEnv');
  assert.ok(stopCmd.includes(`%${envName}%`), `stop.cmd 没有用 %${envName}% 回报退出码`);

  const markerIdx = stopCmd.indexOf(`%${envName}%`);
  const pauseIdx = stopCmd.search(/^pause[ \t]*$/m);
  assert.ok(pauseIdx > 0, 'stop.cmd 里找不到 pause');
  assert.ok(markerIdx < pauseIdx, '回报退出码必须写在 pause 之前');
});

test('停止必须走可见窗口，托盘不许再静默跑 stop', () => {
  assert.doesNotMatch(trayGo, /runAction\("stop"/,
    'stop 一律走 handleStop（可见窗口）；静默调用就是「点了没反应」的老问题');
  assert.match(trayGo, /go handleStop\(false\)/, '菜单「停止服务」要开窗口');
  assert.match(trayGo, /handleStop\(true\)/, '「退出」要先在窗口里停完再退托盘');

  const spawn = trayGo.match(/func spawnStopWindow\(marker string\) error \{[\s\S]*?\n\}/)?.[0];
  assert.ok(spawn, 'main.go 里找不到 spawnStopWindow');
  // ⛔ 不能自己 exec cmd /c stop.cmd：os/exec 在 Stdout/Stderr 为 nil 时把子进程
  //    输出接到 NUL，窗口会是一片空白（启动窗口同样踩过这个坑）。
  assert.match(spawn, /"\/c", "start", ""/, '必须让 cmd 的 start 去建控制台');
});

test('服务就绪后要自动打开管理页面（别让人再去托盘菜单里翻）', () => {
  // 老板 2026-11-26：「启动的时候能自动打开浏览器自动打开管理页面吗」
  const watch = trayGo.match(/func watchStartup\(rawURL string\) \{[\s\S]*?\n\}/)?.[0];
  assert.ok(watch, 'main.go 里找不到 watchStartup');
  const ready = watch.match(/if portOpen\(addr\) \{[\s\S]*?\n\t\t\}/)?.[0];
  assert.ok(ready, 'watchStartup 里找不到就绪分支');
  assert.match(ready, /openURL\(rawURL\)/, '探到就绪必须自动开一次管理页面');
});

// ---------------------------------------------------------------- 门禁接线 ----

test('本契约测试已接入质量门禁，别让它变成没人跑的死文件', () => {
  const gate = readFileSync(url('./quality-gate.sh'), 'utf8');
  assert.match(gate, /standalone-windows-updater-path-and-lock\.test\.mjs/);
});
