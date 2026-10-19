import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

async function source(path) {
  return readFile(new URL(`../${path}`, import.meta.url), 'utf8');
}

test('standalone package builders pass the configured release public keys into config generation', async () => {
  const [linux, windows, generator] = await Promise.all([
    source('deploy/standalone/build-standalone.sh'),
    source('deploy/standalone-windows/build-standalone.ps1'),
    source('deploy/standalone/make-config.py'),
  ]);

  assert.match(linux, /--upgrade-trusted-keys "\$\{UVP_UPGRADE_TRUSTED_KEYS:-\}"/);
  assert.match(windows, /\$env:UVP_UPGRADE_TRUSTED_KEYS/);
  assert.match(generator, /--upgrade-trusted-keys/);
});

test('standalone updater registration reads trusted keys from the generated application config', async () => {
  const [control, updater] = await Promise.all([
    source('deploy/standalone/uvp-gb28181-ctl.sh'),
    source('server/cmd/uvp-updater/main.go'),
  ]);

  assert.match(control, /--config-dir "\$ROOT\/config"/);
  assert.match(updater, /registration\.TrustedKeys\s*=\s*registrationTrustedKeys\(config\.Get\("upgrade\.trusted_keys"\)\)/);
});

test('Compose installation forwards public keys and explains when the config has no offline-upgrade key', async () => {
  const installer = await source('deploy/compose/install.sh');

  assert.match(installer, /--upgrade-trusted-keys "\$\{UVP_UPGRADE_TRUSTED_KEYS:-\}"/);
  assert.match(installer, /UVP_UPGRADE_TRUSTED_KEYS/);
  assert.match(installer, /离线升级不可用/);
  assert.doesNotMatch(installer, /upgrade\.release_source\s*=/);
});
