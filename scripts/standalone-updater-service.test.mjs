import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const root = new URL('../', import.meta.url);

async function source(relativePath) {
  return readFile(new URL(relativePath, root), 'utf8');
}

test('systemd updater unit keeps the helper in the foreground', async () => {
  const [installer, control] = await Promise.all([
    source('deploy/standalone/uvp-gb28181-service-install.sh'),
    source('deploy/standalone/uvp-gb28181-ctl.sh'),
  ]);

  assert.match(
    installer,
    /ExecStart=\/bin\/bash "\$\{CTL\}" serve-updater/,
    'Type=simple must start the foreground helper entrypoint',
  );
  assert.doesNotMatch(
    installer,
    /ExecStart=\/bin\/bash "\$\{CTL\}" start-updater/,
    'the backgrounding entrypoint cannot be systemd\'s main process',
  );
  assert.match(control, /^serve_updater\(\) \{/m, 'ctl script needs a foreground helper entrypoint');
  assert.match(
    control,
    /printf '%s\\n' "\$\$" > "\$UPDATER_PID_FILE"\n  exec "\$UPDATER_BIN" serve --socket "\$UPDATER_SOCKET" --state "\$UPDATER_STATE" --registration "\$UPDATER_REGISTRATION"/,
    'the foreground entrypoint must leave a live PID record before exec',
  );
  assert.match(control, /^  serve-updater\)\n    serve_updater/m, 'ctl command must dispatch serve-updater');
});

test('standalone registration preserves the administrator backup limit', async () => {
  const [control, readme] = await Promise.all([
    source('deploy/standalone/uvp-gb28181-ctl.sh'),
    source('deploy/standalone/README.md'),
  ]);

  assert.match(control, /^upgrade_external_backup_max_bytes\(\) \{/m,
    'ctl needs a dedicated controlled registration value');
  assert.match(control, /read_env_file UVP_UPGRADE_EXTERNAL_BACKUP_MAX_BYTES/,
    'config.env must be able to carry the installation-level limit');
  assert.match(control, /--external-backup-max-bytes "\$max_backup_bytes"/,
    'every registration rewrite must retain the configured backup limit');
  assert.match(readme, /UVP_UPGRADE_EXTERNAL_BACKUP_MAX_BYTES/,
    'operators need the documented override and its units');
});

test('standalone updater secures its state and socket directories before registration', async () => {
  const control = await source('deploy/standalone/uvp-gb28181-ctl.sh');

  assert.match(control, /mkdir -p "\$ROOT\/config" "\$UPDATER_STATE" "\$RUN"\n  chmod 0700 "\$UPDATER_STATE" "\$RUN"/);
});
