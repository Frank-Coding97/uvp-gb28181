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

test('收紧目录权限后必须恢复 api-authority.lock 的授权，且顺序在 /T 之后', () => {
  const tighten = authority.indexOf("'/T'");
  const reset = authority.indexOf("'/reset'");
  assert.ok(tighten > 0, '找不到 icacls /T 收紧动作');
  assert.ok(reset > tighten, '/reset 必须排在 /T 之后，否则会被 /T 再次剥光');
  assert.match(authority, /Join-Path \$Path 'api-authority\.lock'/);
  assert.match(authority, /Test-Path -LiteralPath \$lockFile/,
    '首次启动锁文件还不存在，不能无条件 reset');
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

// ---------------------------------------------------------------- 门禁接线 ----

test('本契约测试已接入质量门禁，别让它变成没人跑的死文件', () => {
  const gate = readFileSync(url('./quality-gate.sh'), 'utf8');
  assert.match(gate, /standalone-windows-updater-path-and-lock\.test\.mjs/);
});
