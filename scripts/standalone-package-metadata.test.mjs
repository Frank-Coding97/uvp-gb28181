import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
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
