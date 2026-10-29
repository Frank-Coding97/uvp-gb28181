# Process authority deployment prerequisite

Normal API startup uses a private process-authority directory. An omitted
`processauthority.state_dir` selects and creates the current account's platform
default. An explicit value remains an advanced deployment override. The directory
anchors the single API process to one database across restarts. Missing,
relative, replaced, symlinked or unsafe state fails startup; turning OpenAPI
off does not disable this protection. Migration-only commands do not register.

## Bare process

The checked-in `uvp-backend.service` runs as root and supplies the persistent
location through its environment. The deployer creates it with mode `0700` on
first install. If the directory already exists, it is validated but never
recreated or repaired during upgrade.

An advanced deployment may override the service environment in YAML:

```yaml
processauthority:
  state_dir: /opt/uvp-gb28181/data/process-authority
```

The deploy script checks the directory before changing releases. The application
also validates the actual lock/domain objects; the shell check is not a substitute
for those checks. If the service account changes, provision the state for that
account and update the unit/deploy prerequisite together.

## Container

The deployer creates the private host directory on first install, and Compose
binds it to `/var/lib/uvp/process-authority`. No manual host-directory or YAML
setup is required through the supported deployment flow. Advanced deployments
may still configure the **container** path explicitly:

```yaml
processauthority:
  state_dir: /var/lib/uvp/process-authority
```

The checked-in backend image runs as root. User namespace remapping or a custom
container user requires an explicitly reviewed ownership/mount arrangement.
Do not run bare and container API instances for the same database concurrently.

## Upgrades, shutdown and rollback

- Keep this directory, its lock file and domain identity outside release trees,
  log rotation, uploads, cleanup jobs and temporary storage.
- Never unlink or replace the lock/domain to recover a failed deployment.
- SIP reload borrows the current generation; it neither registers nor unlocks.
- On API shutdown, HTTP/scheduler, maintenance and all GB owners must finish
  before the root seals its authority and releases the lock. A timeout is not
  evidence that an owner exited.
- Retain the authority ledger migrations on rollback. Do not restart a version
  that lacks the authority checks against active protected operations; that is
  not a safe online downgrade.
- Copying a VM/volume, changing hosts, using network storage or losing domain
  state is not an ordinary restart. Automatic takeover is not supported without
  a reviewed shutdown/migration procedure and evidence that old owners exited.

These are deployment prerequisites, not evidence that any live host has been
provisioned or that media end-to-end acceptance has passed.
