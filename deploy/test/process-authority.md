# Process authority deployment prerequisite

Normal API startup now requires `processauthority.state_dir`. The directory
anchors the single API process to one database across restarts. Missing,
relative, replaced, symlinked or unsafe state fails startup; turning OpenAPI
off does not disable this protection. Migration-only commands do not register.

## Bare process

The checked-in `uvp-backend.service` runs as root. Before deploying this version,
an operator must provision `/opt/uvp-gb28181/data/process-authority` as a real
directory on local persistent storage, owned by root, mode `0700`, with no ACL
granting access to other users. If the directory already exists, inspect it;
do not recreate it or change its identity to get past a startup failure.

Set the runtime configuration explicitly:

```yaml
processauthority:
  state_dir: /opt/uvp-gb28181/data/process-authority
```

The deploy script checks the directory before changing releases. The application
also validates the actual lock/domain objects; the shell check is not a substitute
for those checks. If the service account changes, provision the state for that
account and update the unit/deploy prerequisite together.

## Container

The compose file binds the same dedicated host directory to
`/var/lib/uvp/process-authority`. Docker is forbidden from creating a missing
host path. Configure the **container** path in the mounted configuration:

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
