import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, existsSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';

test('standalone package excludes Git directories and submodule pointers while preserving runtime assets', () => {
  const source = readFileSync(new URL('../deploy/standalone/build-standalone.sh', import.meta.url), 'utf8');
  const command = source.split(String.fromCharCode(10)).find(line => line.startsWith('find "$PKG"') && line.includes('-name .git'));
  assert.ok(command, 'package builder must remove repository metadata');
  const root = mkdtempSync(join(tmpdir(), 'uvp-package-metadata-'));
  try {
    const vendor = join(root, 'vendor', 'zlm');
    const web = join(vendor, 'www', 'webassist');
    mkdirSync(web, { recursive: true });
    mkdirSync(join(vendor, '.git'));
    writeFileSync(join(vendor, '.git', 'config'), 'repository metadata');
    writeFileSync(join(web, '.git'), 'gitdir: ../../.git/modules/www/webassist');
    writeFileSync(join(web, 'index.html'), 'runtime asset');
    execFileSync('bash', ['-eu', '-c', command], { env: { ...process.env, PKG: root } });
    assert.equal(existsSync(join(vendor, '.git')), false);
    assert.equal(existsSync(join(web, '.git')), false);
    assert.equal(readFileSync(join(web, 'index.html'), 'utf8'), 'runtime asset');
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

// ---------------------------------------------------------------------------
// 归档属主必须钉成 root:root（2026-10-20 客户机 CentOS 7 现场）
//
// 症状：root 跑 ./uvp-gb28181-ctl.sh，装到「阶段 3/3」报
//   open trusted updater registration: updater registration parent
//   "/opt/uvp/uvp-gb28181-linux-amd64-1.1.11" owner UID 1000 is not trusted
// 根因：在 220 上以 superadmin 出包 ⇒ 823 个归档条目全记成 1000/1000；客户用
//   root `tar -xzf` 解压时**默认 --same-owner**，安装目录属主就变成普通用户，
//   而升级助手以 root 跑时只认 root 属主的路径（安全设计，不能放宽）。
// ⛔ 这条只能在"包里"防住：ctl.sh 的兜底要客户机跑起来才生效，而解压那一刻
//   属主就已经错了。
// ---------------------------------------------------------------------------

test('build-standalone.sh 的两条打包命令都显式改写归档属主', () => {
  const source = readFileSync(new URL('../deploy/standalone/build-standalone.sh', import.meta.url), 'utf8');
  const commands = source.split(String.fromCharCode(10)).filter((line) => line.includes('-czf "${ARCHIVE}"'));
  assert.equal(commands.length, 2, '应当只有 GNU tar / bsdtar 两条打包命令');
  for (const line of commands) {
    assert.match(line, /\$\{TAR_OWNER_FLAGS\}/, `打包命令没带属主改写：${line.trim()}`);
  }
  assert.match(source, /TAR_OWNER_FLAGS="--owner=0 --group=0"/, 'GNU tar 分支缺属主改写');
  assert.match(source, /TAR_OWNER_FLAGS="--uid 0 --gid 0"/, 'bsdtar 分支缺属主改写');
});

test('属主改写参数在真实打包器上确实生效（不是被静默忽略）', () => {
  const root = mkdtempSync(join(tmpdir(), 'uvp-tar-owner-'));
  const tarBin = process.env.UVP_TAR || 'tar';
  try {
    const pkg = join(root, 'pkg');
    mkdirSync(pkg, { recursive: true });
    writeFileSync(join(pkg, 'a.txt'), 'x');
    const archive = join(root, 'p.tar.gz');
    // 与 build-standalone.sh 同一套探测顺序。
    const flags =
      spawnSync(tarBin, ['--owner=0', '--group=0', '--version']).status === 0
        ? ['--owner=0', '--group=0']
        : ['--uid', '0', '--gid', '0'];
    execFileSync(tarBin, [...flags, '-czf', archive, '-C', root, 'pkg']);
    const first = execFileSync(tarBin, ['--numeric-owner', '-tvzf', archive], { encoding: 'utf8' })
      .split(String.fromCharCode(10))[0];
    // GNU tar 打 `0/0`，bsdtar 打 `0    0`（uid、gid 两列）。
    assert.match(first, /(^|\s)0\/0(\s|$)|(^|\s)0\s+0(\s|$)/, `归档属主不是 root: ${first}`);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});
