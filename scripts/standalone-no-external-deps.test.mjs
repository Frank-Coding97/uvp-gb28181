import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { createPrivateKey, X509Certificate } from 'node:crypto';
import { copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, statSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

// ============================================================================
// 契约：绿色安装包在客户机上**不需要任何解释器或外部命令**就能装完。
//
// 真实事故（2026-10-19，客户 CentOS 7）：
//   ./uvp-gb28181-ctl.sh start
//   [uvp][ERROR] 本机 Python 版本过旧（/usr/bin/python = 2.7），本包需要 Python 3.5 及以上
//   —— CentOS 7 自带 python2.7，装 python3 又要拖一堆依赖；而且这个错误是**拦在
//   "开始装"之前**的，客户连试都试不了。
//
// 同一天还有一个更隐蔽的：openssl 也不是必装件（最小化安装没有），而证书是
//   "安装时现场生成"——生成失败只写在日志里，现场表现就是"https 打不开"。
//
// ⇒ 现在的做法：
//   ① 一切"需要解释器/外部命令"的活都搬进包内的 Go 二进制 bin/uvp-setup
//      （CGO_ENABLED=0 静态编译，连 libc 都不依赖）；
//   ② 自签证书**随包内置**，安装时只做拷贝，不再现场生成。
//
// 本文件锁的就是这两条：**交付脚本里一旦重新出现 python / openssl，立刻变红**。
// 顺带把内置证书本身的关键属性钉住（有效期到 2099、SAN 覆盖 localhost/127.0.0.1、
// 私钥与证书配对）—— 这些坏了都不会报错，只会让客户某天突然打不开页面。
// ============================================================================

const LINUX_DIR = fileURLToPath(new URL('../deploy/standalone/', import.meta.url));
const WIN_DIR = fileURLToPath(new URL('../deploy/standalone-windows/', import.meta.url));

// 交付到客户机、且**在客户机上执行**的脚本（构建脚本不在此列：构建机有 python3/openssl）。
const SHIPPED_LINUX = [
  'uvp-gb28181-ctl.sh',
  'uvp-gb28181-service-install.sh',
  'uvp-gb28181-service-uninstall.sh',
];
const SHIPPED_WIN = ['uvp-gb28181-ctl.ps1'];

/**
 * 去掉注释行（只保留真正会执行的行）。
 *
 * ⛔ 必须剥注释：脚本里大量注释在**解释历史原因**（"以前用 openssl 生成""CentOS 7 只有
 *   python2.7"），那些字符串恰恰是本契约存在的理由，不能因此变红。
 * ⛔ 只剥**整行**注释，不剥行尾的：行尾 `#` 前面可能是引号里的内容，剥错会掩盖真实调用。
 */
function executableLines(text, { ps = false } = {}) {
  let body = text;
  if (ps) {
    // PowerShell 块注释 <# … #>（文件头那段说明就是这么写的）
    body = body.replace(/<#[\s\S]*?#>/g, '');
  }
  return body
    .split('\n')
    .map((line, i) => ({ line, no: i + 1 }))
    .filter(({ line }) => !/^\s*#/.test(line));
}

function findCalls(file, { ps = false } = {}) {
  const banned = /\b(?:python[0-9.]*|openssl|py_bin)\b/i;
  const hits = [];
  for (const { line, no } of executableLines(readFileSync(file, 'utf8'), { ps })) {
    if (banned.test(line)) hits.push(`${file}:${no}: ${line.trim()}`);
  }
  return hits;
}

test('Linux 交付脚本不再调用 python / openssl / $PY_BIN', () => {
  const hits = SHIPPED_LINUX.flatMap((name) => findCalls(join(LINUX_DIR, name)));
  assert.deepEqual(
    hits,
    [],
    '交付脚本里又出现了 python / openssl —— 这两样在客户机上都不保证存在\n' +
      '（CentOS 7 只有 python2.7，最小化安装没有 openssl）。\n' +
      '这类活请加进包内的 bin/uvp-setup（纯 Go 静态二进制），不要在脚本里现调。\n' +
      '命中：\n' + hits.join('\n'),
  );
});

test('Windows 交付脚本不再调用 python / openssl', () => {
  const hits = SHIPPED_WIN.flatMap((name) => findCalls(join(WIN_DIR, name), { ps: true }));
  assert.deepEqual(hits, [], 'Windows ctl 里又出现了 python / openssl：\n' + hits.join('\n'));
});

test('脚本里不再有内嵌 Python heredoc（老实现的残留）', () => {
  // 老版 ctl.sh 把 7~8 段 Python 代码用 `<<'DB_INIT_PY'` 之类的 heredoc 喂给解释器。
  // 那些 heredoc 是"必须自己维护一份解释器兼容性"的根源，删干净了才算真去掉依赖。
  const offenders = [];
  for (const [dir, names, ps] of [
    [LINUX_DIR, SHIPPED_LINUX, false],
    [WIN_DIR, SHIPPED_WIN, true],
  ]) {
    for (const name of names) {
      for (const { line, no } of executableLines(readFileSync(join(dir, name), 'utf8'), { ps })) {
        if (/<<-?\s*'?[A-Za-z_][A-Za-z0-9_]*_PY'?/.test(line)) {
          offenders.push(`${name}:${no}: ${line.trim()}`);
        }
      }
    }
  }
  assert.deepEqual(offenders, [],
    '还留着 *_PY heredoc —— 说明又有逻辑回到"趁解释器还在，塞段 Python"的老路上：\n' +
      offenders.join('\n'));
});

// ---------------------------------------------------------------------------
// 内置自签证书
// ---------------------------------------------------------------------------

const CERT_CRT = join(LINUX_DIR, 'assets/cert/uvp.crt');
const CERT_KEY = join(LINUX_DIR, 'assets/cert/uvp.key');

test('内置自签证书随包资产存在，且私钥与证书是一对', () => {
  const cert = new X509Certificate(readFileSync(CERT_CRT));
  const key = createPrivateKey(readFileSync(CERT_KEY));
  assert.equal(cert.checkPrivateKey(key), true,
    'resource/cert 里的证书与私钥不配对 —— 装到客户机上 nginx 会起不来（报 key values mismatch）');
});

test('内置证书有效期：起点在 2020 年（现场时间不准也不判"尚未生效"），到 2099 年', () => {
  const cert = new X509Certificate(readFileSync(CERT_CRT));
  const from = new Date(cert.validFrom);
  const to = new Date(cert.validTo);

  // ⛔ 起点必须**明显在过去**：内网机器的系统时间经常是出厂时间或手工填的，
  //   一旦证书 notBefore 是"签发时刻"，那台机器就判"证书尚未生效" ⇒ 浏览器直接拒。
  assert.ok(from.getUTCFullYear() <= 2020,
    `notBefore 应在 2020 年或更早，实际 ${cert.validFrom}`);
  assert.ok(to.getUTCFullYear() >= 2099,
    `notAfter 至少要到 2099 年，实际 ${cert.validTo} —— 有效期短的证书会在某天突然全部失效`);
});

test('内置证书 SAN 覆盖 localhost / 127.0.0.1 / ::1（浏览器不再弹"不安全"的前提）', () => {
  const cert = new X509Certificate(readFileSync(CERT_CRT));
  const san = cert.subjectAltName || '';
  for (const want of ['DNS:localhost', '127.0.0.1', '0:0:0:0:0:0:0:1']) {
    assert.ok(san.includes(want), `SAN 里缺少 ${want}（当前：${san}）`);
  }
});

test('两端 ctl 都**优先拷贝**包内证书，certgen 只作兜底', () => {
  const sh = readFileSync(join(LINUX_DIR, 'uvp-gb28181-ctl.sh'), 'utf8');
  // 正常路径：resource/cert 存在就只 cp（不生成）
  assert.match(sh, /BUNDLED_CRT="\$RESOURCE\/cert\/uvp\.crt"/,
    'Linux ctl 里找不到内置证书路径常量 —— 兜底顺序可能被改回去了');
  const bundledBranch = sh.slice(sh.indexOf('BUNDLED_CRT'), sh.indexOf('if [ -x "$SETUP_BIN" ]'));
  assert.match(bundledBranch, /cp -f "\$BUNDLED_CRT"/, '内置证书存在时必须走拷贝分支');
  assert.doesNotMatch(bundledBranch, /certgen/,
    '拷贝分支里不该再调 certgen（那等于"有内置证书还现场生成"）');

  const ps = readFileSync(join(WIN_DIR, 'uvp-gb28181-ctl.ps1'), 'utf8');
  const winFn = ps.slice(ps.indexOf('function Invoke-TlsMaterial'), ps.indexOf('function Invoke-NginxConfRender'));
  assert.match(winFn, /resource\\cert\\uvp\.crt/, 'Windows ctl 里找不到内置证书路径');
  assert.ok(winFn.indexOf('Copy-Item') < winFn.indexOf('certgen'),
    'Windows 侧必须先拷贝内置证书、再调 certgen（顺序反了会先现场签一张盖掉内置的）');
});

test('两端构建脚本都把 assets/cert 打进 resource/cert（私钥 0600）', () => {
  const shBuild = readFileSync(join(LINUX_DIR, 'build-standalone.sh'), 'utf8');
  assert.match(shBuild, /CERT_SRC="\$DEPLOY_DIR\/assets\/cert"/,
    'Linux 出包脚本里找不到内置证书源目录');
  assert.match(shBuild, /install -m 0600 "\$CERT_SRC\/uvp\.key"/,
    '内置私钥必须以 0600 装进包里（否则解压后是所有人可读）');
  assert.match(shBuild, /缺少内置证书/,
    '缺资产时必须直接 fail —— 否则会出一个"没有证书"的包，到客户机上才暴露');

  const psBuild = readFileSync(join(WIN_DIR, 'build-standalone.ps1'), 'utf8');
  assert.match(psBuild, /assets\\cert/, 'Windows 出包脚本里找不到内置证书源目录');
  assert.match(psBuild, /resource\\cert\\uvp\.key/, 'Windows 包的 resource\\cert\\uvp.key 没进关键文件清单');
});

test('内置私钥在仓库里也是 0600（拷贝到客户机之前的最后一道）', () => {
  const mode = statSync(CERT_KEY).mode & 0o777;
  assert.equal(mode, 0o600, `deploy/standalone/assets/cert/uvp.key 的权限是 ${mode.toString(8)}，期望 600`);
});

// ---------------------------------------------------------------------------
// ffmpeg 内置契约（2026-10-20 客户现场实锤后立的）：
//   客户机没装 ffmpeg ⇒ ZLM getSnap 每次都 execv 失败、返回 1024x512 占位 PNG，
//   平台只报「未返回 JPEG」——通道抓拍从装机起静默不可用、零告警。
//   与"客户机不该预装 python/openssl"是同一类契约：**ffmpeg 也是预装件，必须随包**。
// ---------------------------------------------------------------------------
test('两端构建脚本都把 ffmpeg 打进 vendor/zlm（缺资产必须 fail）', () => {
  const shBuild = readFileSync(join(LINUX_DIR, 'build-standalone.sh'), 'utf8');
  assert.match(shBuild, /缺少内置 ffmpeg/,
    'Linux 出包脚本缺资产时必须直接 fail —— 否则又出一个"装完抓拍不可用"的包');
  assert.match(shBuild, /"\$PKG\/vendor\/zlm\/bin\/ffmpeg"/,
    'Linux 包必须把 ffmpeg 装到 vendor/zlm/bin/ffmpeg');
  // ini 重写进同一个 python 块；相对路径 ./bin/ffmpeg 由 ZLM 按 exeDir 解析。
  assert.match(shBuild, /section == "ffmpeg" and key == "bin"/,
    '出包时必须把 config.ini 的 [ffmpeg] bin 重写到包内路径');
  assert.match(shBuild, /"\.\/bin\/ffmpeg"/, '[ffmpeg] bin 的目标值必须是 ./bin/ffmpeg');

  const psBuild = readFileSync(join(WIN_DIR, 'build-standalone.ps1'), 'utf8');
  assert.match(psBuild, /zlm\\ffmpeg\.exe/,
    'Windows 出包脚本里找不到 ffmpeg.exe 的断言/拷贝（应随包打到 vendor\\zlm\\ffmpeg.exe）');
});

test('两端 ctl 启动时必须自检 ZLM 配置的 ffmpeg（把静默失效点破成启动警告）', () => {
  const sh = readFileSync(join(LINUX_DIR, 'uvp-gb28181-ctl.sh'), 'utf8');
  assert.match(sh, /zlm_ffmpeg_check\(\)/, 'Linux ctl 里找不到 zlm_ffmpeg_check 函数');
  assert.match(sh, /ZLM ffmpeg 自检通过/,
    '自检通过的文案要保持这个固定句式（排障时好 grep）');
  // 函数必须在 start_zlm 的成功路径上被调用（定义了不调=白写）。
  const startZlm = sh.slice(sh.indexOf('start_zlm() {'), sh.indexOf('stop_zlm() {'));
  assert.ok(/zlm_ffmpeg_check/.test(startZlm), 'start_zlm 的成功路径必须调用 zlm_ffmpeg_check');

  const ps = readFileSync(join(WIN_DIR, 'uvp-gb28181-ctl.ps1'), 'utf8');
  const startZlmFn = ps.slice(ps.indexOf('function Start-Zlm'), ps.indexOf('function Start-Nginx'));
  assert.match(startZlmFn, /ffmpeg 自检通过/,
    'Windows Start-Zlm 的成功路径必须做 ffmpeg 自检');
});

// ---------------------------------------------------------------------------
// 行为用例：把 ensure_self_signed_cert 从脚本里抠出来真跑一遍。
//
// 为什么光有静态断言不够：「优先级写反了」和「兜底时静默返回 0」这两种错法都能通过
// 静态检查，但前者会让内置证书永远用不上、后者会让"没证书"装成"成功了"——
// 到客户机上就是 https 打不开而脚本全程不报错。
// ---------------------------------------------------------------------------

function ensureCertHarness({ withBundled, setupBin }) {
  const sh = readFileSync(join(LINUX_DIR, 'uvp-gb28181-ctl.sh'), 'utf8');
  const body = sh.match(/^ensure_self_signed_cert\(\) \{[\s\S]*?^\}/m);
  assert.ok(body, 'ctl.sh 里找不到 ensure_self_signed_cert()');
  const vars = sh.match(/^BUNDLED_CRT=.*$/m)?.[0];
  const varsKey = sh.match(/^BUNDLED_KEY=.*$/m)?.[0];
  assert.ok(vars && varsKey, 'ctl.sh 里找不到 BUNDLED_CRT / BUNDLED_KEY 常量');

  const dir = mkdtempSync(join(tmpdir(), 'uvp-cert-'));
  if (withBundled) {
    mkdirSync(join(dir, 'resource/cert'), { recursive: true });
    copyFileSync(CERT_CRT, join(dir, 'resource/cert/uvp.crt'));
    copyFileSync(CERT_KEY, join(dir, 'resource/cert/uvp.key'));
  }
  const harness = [
    `RESOURCE="${dir}/resource"`,
    `NGINX_CRT="${dir}/vendor/nginx/conf/uvp.crt"`,
    `NGINX_KEY="${dir}/vendor/nginx/conf/uvp.key"`,
    `NGINX_LOG="${dir}/logs/nginx.log"`,
    `SETUP_BIN="${setupBin}"`,
    'log()  { printf "[log] %s\\n" "$*"; }',
    vars,
    varsKey,
    body[0],
    'ensure_self_signed_cert; printf "RC=%s\\n" "$?"',
  ].join('\n');

  const r = spawnSync('/bin/bash', ['-c', harness], { encoding: 'utf8' });
  const stat = (p) => {
    try {
      return statSync(join(dir, p));
    } catch {
      return null;
    }
  };
  return { r, dir, nginxCrt: stat('vendor/nginx/conf/uvp.crt'), nginxKey: stat('vendor/nginx/conf/uvp.key') };
}

test('行为：有内置证书时只做拷贝（不调 certgen），私钥落盘 600', () => {
  const { r, dir, nginxCrt, nginxKey } = ensureCertHarness({ withBundled: true, setupBin: '/nonexistent/uvp-setup' });
  try {
    assert.equal(r.status, 0, r.stderr);
    assert.match(r.stdout, /RC=0/, '有内置证书时必须成功：' + r.stdout + r.stderr);
    assert.ok(nginxCrt && nginxKey, '证书没被拷到 nginx 的 conf 目录');
    assert.equal(nginxCrt.size, statSync(CERT_CRT).size, '拷过去的证书大小不对（被改写过？）');
    assert.equal(nginxKey.mode & 0o777, 0o600, '私钥落盘权限不是 600');
  // SETUP_BIN 指向不存在的路径 ⇒ 一旦走了 certgen 分支就会报"既没有证书也没有…"
    assert.doesNotMatch(r.stdout, /certgen|既没有证书/, '有内置证书时不该走到 certgen 兜底');
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test('行为：既没有内置证书、也没有 uvp-setup 时必须报错，不能静默当成功', () => {
  const { r, dir } = ensureCertHarness({ withBundled: false, setupBin: '/nonexistent/uvp-setup' });
  try {
    assert.match(r.stdout, /RC=1/, '两条路都没有时必须返回 1（否则启动会当成"证书就绪"）');
    assert.match(r.stdout, /既没有证书/, '必须明确说清缺什么：' + r.stdout);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});
