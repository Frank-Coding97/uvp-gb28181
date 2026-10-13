import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

// ============================================================================
// 契约：随包分发的**预编译二进制**不得要求"和构建机同款"的系统库。
//
// 真实事故（2026-10-10，客户银河麒麟 V10 SP2 实机）：
//   阶段 3/3 启动服务 → ZLM 起来了 → nginx 报
//     nginx: error while loading shared libraries: libssl.so.3
//   根因：包里的 nginx 是在较新的 Linux 上直接编的，ELF 里写着要
//     libssl.so.3 / libcrypto.so.3（OpenSSL 3.x） + GLIBC_2.34
//   而麒麟 V10 SP2 只有 OpenSSL 1.1.1f + glibc 2.28。
//   ⛔ 补库救不了：下一关是 `version 'GLIBC_2.34' not found`，glibc 不能随包发。
//   ⛔ 而且**不止 nginx**：同批审计发现 redis-server / redis-cli 是同一个毛病，
//      而启动顺序是 ZLM → nginx → redis → 后端 ⇒ 只修 nginx 会立刻在 redis 再炸一次。
//
// 所以判据必须落在"产物对系统库提了什么要求"上，且**逐二进制**核，
// 不能再用"包级 glibc 基线 2.17"这种笼统说法（它正是漏掉 nginx 的原因）。
//
// 这里测的是门禁脚本 deploy/standalone/check-portable-elf.sh 的**判定逻辑**：
//   ① 动态依赖 OpenSSL（libssl.so* / libcrypto.so*） ⇒ 判红
//   ② 要求的 GLIBC_x.y 高于上限（默认 2.17）      ⇒ 判红
//   ③ 其余非 glibc 库只提示、不判错（它们可能是 dlopen 的候选）
//   ④ 静态链 OpenSSL 的二进制里仍有 OPENSSL_x.y.z 字符串 ⇒ **不判错**（关键）
// 另附接线断言：出包/构建流程必须真的调用门禁（防止有人把接线删掉）。
// ============================================================================

const DEPLOY_DIR = fileURLToPath(new URL('../deploy/standalone/', import.meta.url));
const GATE = join(DEPLOY_DIR, 'check-portable-elf.sh');

// ---------------------------------------------------------------------------
// 夹具：合成一个"像 ELF 的"文件 —— ELF magic + NUL 分隔的字符串表。
// 门禁只认 magic + 字符串（它就是这么设计的：要在 macOS / CentOS 7 / 国产机上
// 都能跑，不能依赖 readelf），所以这样造出来的夹具走的是**完全真实的代码路径**。
// 下面每组 token 都是从真实二进制里 `strings` 出来的指纹，不是编的。
// ---------------------------------------------------------------------------
function makeElf(dir, name, tokens) {
  const buf = Buffer.concat([
    Buffer.from([0x7f, 0x45, 0x4c, 0x46]), // \x7f E L F
    Buffer.alloc(16, 0),
    Buffer.from(tokens.join('\0') + '\0', 'utf8'),
  ]);
  const p = join(dir, name);
  writeFileSync(p, buf);
  return p;
}

// 真实指纹：包内 nginx 的动态依赖 + glibc 版本
const FINGERPRINT = {
  // deploy/standalone/bin/nginx/sbin/nginx
  nginx: ['libssl.so.3', 'libcrypto.so.3', 'libcrypt.so.1', 'libpcre2-8.so.0', 'libz.so.1',
          'GLIBC_2.17', 'GLIBC_2.28', 'GLIBC_2.32', 'GLIBC_2.34'],
  // deploy/standalone/bin/redis-server（redis 7.4.9）
  redis: ['libssl.so.3', 'libcrypto.so.3', 'libm.so.6', 'GLIBC_2.17', 'GLIBC_2.28', 'GLIBC_2.34'],
  // deploy/standalone/bin/zlm/MediaServer（走 glibc217 底座，**合格的那个**）
  zlm: ['libc.so.6', 'libdl.so.2', 'libm.so.6', 'libpthread.so.0',
        'libavcodec.so.60', 'libavfilter.so.9', 'libavformat.so.60',
        'libnvcuvid.so.1', 'GLIBC_2.12', 'GLIBC_2.14', 'GLIBC_2.17'],
  // 纯静态 Go 后端（uvp-server：statically linked，连 GLIBC_ 串都没有）
  goStatic: ['runtime.gcdata', 'go1.25.6', 'internal/abi.KindDirectIface'],
};

function runGate(args) {
  return spawnSync('bash', [GATE, ...args], { encoding: 'utf8' });
}

function withTmpDir(fn) {
  const dir = mkdtempSync(join(tmpdir(), 'uvp-portable-elf-'));
  try {
    return fn(dir);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

// ---------------------------------------------------------------------------

test('真实指纹：nginx 应被判红（要 OpenSSL 3 且 GLIBC 2.34）', () => {
  withTmpDir((dir) => {
    const f = makeElf(dir, 'nginx', FINGERPRINT.nginx);
    const r = runGate([f]);
    assert.equal(r.status, 1, '这份 nginx 必须被判红，否则门禁等于没装');
    assert.match(r.stdout, /libssl\.so\.3/, '要指出缺的是 libssl.so.3');
    assert.match(r.stdout, /GLIBC_2\.34/, '要指出 glibc 要求过高');
  });
});

test('真实指纹：redis 也是同一个毛病（不能只修 nginx）', () => {
  withTmpDir((dir) => {
    const f = makeElf(dir, 'redis-server', FINGERPRINT.redis);
    const r = runGate([f]);
    assert.equal(r.status, 1);
    assert.match(r.stdout, /libcrypto\.so\.3/);
    assert.match(r.stdout, /GLIBC_2\.34/);
  });
});

test('真实指纹：ZLM 合格（glibc ≤ 2.17、无动态 OpenSSL）', () => {
  withTmpDir((dir) => {
    const f = makeElf(dir, 'MediaServer', FINGERPRINT.zlm);
    const r = runGate([f]);
    assert.equal(r.status, 0, r.stdout + r.stderr);
    assert.match(r.stdout, /\[ok\]/);
    // libnvcuvid.so.1 未在 --provided-dir 里 ⇒ 应作为"需目标机自带"提示出来
    assert.match(r.stdout, /libnvcuvid\.so\.1/);
  });
});

test('--provided-dir：包内自带的库不再提示"需目标机自带"', () => {
  withTmpDir((dir) => {
    const libdir = join(dir, 'lib');
    mkdirSync(libdir);
    for (const n of ['libavcodec.so.60', 'libavfilter.so.9']) writeFileSync(join(libdir, n), '');
    const f = makeElf(dir, 'MediaServer', FINGERPRINT.zlm);
    const r = runGate([f, '--provided-dir', libdir]);
    assert.equal(r.status, 0, r.stdout + r.stderr);
    assert.ok(!/libavcodec\.so\.60/.test(r.stdout),
      '包内已带 libavcodec.so.60，不该再提示目标机自带');
  });
});

test('纯静态 Go 二进制：无 GLIBC 串也应判绿', () => {
  withTmpDir((dir) => {
    const f = makeElf(dir, 'uvp-server', FINGERPRINT.goStatic);
    const r = runGate([f]);
    assert.equal(r.status, 0, r.stdout + r.stderr);
  });
});

test('边界：GLIBC_2.17 判绿，GLIBC_2.18 判红（判据是"高于上限"而非"存在"）', () => {
  withTmpDir((dir) => {
    const ok = makeElf(dir, 'ok.so', ['libc.so.6', 'GLIBC_2.17']);
    assert.equal(runGate([ok]).status, 0, '恰好等于上限应当通过');

    const bad = makeElf(dir, 'bad.so', ['libc.so.6', 'GLIBC_2.18']);
    assert.equal(runGate([bad]).status, 1, '比上限高一位就该红');
  });
});

test('--glibc-max 生效（放宽到 2.28 后，只要 2.28 的产物应通过）', () => {
  withTmpDir((dir) => {
    const f = makeElf(dir, 'mid.so', ['libc.so.6', 'GLIBC_2.28']);
    assert.equal(runGate([f]).status, 1, '默认上限 2.17 时 2.28 应红');
    assert.equal(runGate(['--glibc-max', '2.28', f]).status, 0, '放宽后应绿');
  });
});

test('⛔ 静态链 OpenSSL 的正常形态：有 OPENSSL_3.0.0 符号串但无 libssl 动态依赖 ⇒ 判绿', () => {
  // 这是本门禁最容易写错的地方：静态链了 OpenSSL 的二进制里**照样**会有
  // OPENSSL_3.0.0 这类字符串。若按"出现 OPENSSL_ 就判错"，修好之后反而会被判红，
  // 逼着人把门禁删掉。判据必须是**动态依赖**，不是符号串。
  withTmpDir((dir) => {
    const f = makeElf(dir, 'nginx-static', ['libc.so.6', 'libcrypt.so.1', 'GLIBC_2.17', 'OPENSSL_3.0.0']);
    const r = runGate([f]);
    assert.equal(r.status, 0, '不应因为符号串里有 OPENSSL_3.0.0 就判红：' + r.stdout);
    assert.match(r.stdout, /OPENSSL_3\.0\.0/, '但应当在 [i] 里把这个符号串展示出来');
  });
});

test('文件不存在 ⇒ 判红（不能因为"没文件"就静默通过）', () => {
  withTmpDir((dir) => {
    const r = runGate([join(dir, 'not-there')]);
    assert.equal(r.status, 1);
    assert.match(r.stdout, /文件不存在/);
  });
});

test('非 ELF 文件 ⇒ skip 且不判错（脚本/配置不应被当成二进制审计）', () => {
  withTmpDir((dir) => {
    const f = join(dir, 'x.sh');
    writeFileSync(f, '#!/bin/sh\necho hi\n');
    const r = runGate([f]);
    assert.equal(r.status, 0);
    assert.match(r.stdout, /\[skip\]/);
  });
});

test('用法错误 ⇒ 退出码 2（与"审计不通过"的 1 区分开）', () => {
  const r = runGate([]);
  assert.equal(r.status, 2);
});

// ---------------------------------------------------------------------------
// 接线断言：门禁写好了但没人调用 = 零价值。这几条防的是"以后有人把接线删掉"。
//
// ⛔⛔ 断言必须落在**代码**上，不能匹配到注释 —— 这是变异验证实测抓出来的：
//   本次改动在脚本里写了大量解释性注释（例如 build-redis.sh 的注释里就有
//   `BUILD_TLS=no` 字样）。最初直接对全文做正则，结果**把真正的 make 参数删掉后
//   测试仍然全绿**（假绿）。⇒ 先剥掉整行注释，再断言。
// ---------------------------------------------------------------------------
function codeOnly(text) {
  return text
    .split('\n')
    .filter((line) => !/^\s*#/.test(line))
    .join('\n');
}

test('出包流程必须审计全部关键二进制（nginx / redis×2 / ZLM / 各自带 .so）', () => {
  const s = codeOnly(readFileSync(join(DEPLOY_DIR, 'build-standalone.sh'), 'utf8'));
  assert.match(s, /check-portable-elf\.sh/, '出包脚本没有调用可移植性门禁');
  assert.match(s, /--glibc-max/, '出包时没传 glibc 上限');
  for (const needle of [
    'vendor/nginx/sbin/nginx',
    'vendor/redis/redis-server',
    'vendor/redis/redis-cli',
    'vendor/zlm/MediaServer',
    'vendor/zlm/lib/*.so*',
  ]) {
    assert.ok(s.includes(needle), `出包门禁没有覆盖 ${needle}`);
  }
});

test('nginx 构建脚本必须强制「老 glibc 底座 + 静态 OpenSSL」', () => {
  const s = codeOnly(readFileSync(join(DEPLOY_DIR, 'build-nginx.sh'), 'utf8'));
  assert.match(s, /UVP_INSIDE_BUILDER/, '缺少"必须进底座容器"的守卫');
  assert.match(s, /--with-openssl=/, '缺少静态 OpenSSL 的源码路径');
  assert.match(s, /--with-openssl-opt='no-shared/, '没有要求 OpenSSL 出静态库');
  assert.match(s, /--with-pcre=/, '缺少静态 PCRE2 的源码路径');
  assert.match(s, /--with-zlib=/, '缺少静态 zlib 的源码路径');
  assert.match(s, /check-portable-elf\.sh/, '构建脚本没有自检可移植性');
  // ⛔ nginx 把 --with-pcre-opt 当 CFLAGS 用，传 --disable-shared 会编不过。
  assert.ok(!/--with-pcre-opt=/.test(s),
    '--with-pcre-opt 会被当成 CFLAGS，不能用它传 configure 参数');
});

test('redis 构建脚本必须 BUILD_TLS=no（从根上去掉 OpenSSL 依赖）', () => {
  const s = codeOnly(readFileSync(join(DEPLOY_DIR, 'build-redis.sh'), 'utf8'));
  assert.match(s, /UVP_INSIDE_BUILDER/, '缺少"必须进底座容器"的守卫');
  // ⛔ 必须锚定**真正的 make 调用行**：脚本里有 `log "make（BUILD_TLS=no…）"` 与
  //   buildinfo 里的 `BUILD_TLS=no` 两处"长得像"的地方。变异验证实测：
  //   用宽松的 /make.*BUILD_TLS=no/ 去匹配，把真正的 make 参数删掉后仍然全绿（假绿）。
  assert.match(s, /^\s*make\b[^\n]*BUILD_TLS=no/m,
    'redis 的 make 调用必须带 BUILD_TLS=no，否则又回到动态链 OpenSSL 的老坑');
  assert.match(s, /check-portable-elf\.sh/, '构建脚本没有自检可移植性');
});

test('构建底座 Dockerfile 自证 glibc 必须是 2.17', () => {
  const s = codeOnly(readFileSync(join(DEPLOY_DIR, 'Dockerfile.linux-builder'), 'utf8'));
  assert.match(s, /FROM centos:7/, '底座必须是 CentOS 7（glibc 2.17）');
  assert.ok(s.includes("'2\\.17'"), 'Dockerfile 里应有 glibc 版本自检（ldd --version | grep 2.17）');
});
