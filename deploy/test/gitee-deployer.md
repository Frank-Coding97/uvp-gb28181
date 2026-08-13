# Gitee Domestic Deployment

This deployment path builds on the domestic server after a verified GitHub CI
run mirrors `develop` to Gitee. Database migrations are embedded in the
backend binary and applied automatically at startup (see
"Database Changes with Embedded Migrations").

## First Installation

1. Install Go, Node.js, pnpm, Docker Compose, Nginx, Python 3, and the tools
   used by the existing deployment script (`git`, `curl`, `flock`, `tar`, and
   `sha256sum`) on the server.
2. Create `/etc/uvp-gitee-webhook.env` from
   `deploy/test/uvp-gitee-webhook.env.example`. Set a long random
   `UVP_GITEE_WEBHOOK_TOKEN`, restrict the file to root, and do not commit it.
3. Run `sudo deploy/test/install-gitee-deployer.sh /opt/uvp-gb28181/source`.
   The source repository is cloned from `UVP_GITEE_REPO_URL` when absent.
4. Add `deploy/test/gitee-webhook.nginx.conf` to the HTTPS virtual host. The
   recommended endpoint is `https://<host>/hooks/gitee`; keep the Go receiver
   bound to loopback.
5. In Gitee, create a repository WebHook for the HTTPS endpoint, select only
   the push event, and configure the same token.
6. In GitHub Actions secrets, set `GITEE_PUSH_URL` to a credentialed Gitee
   remote URL that is allowed to fast-forward `develop`. The CI mirror step
   safely skips when this secret is unset.

## Database Changes and First Release

The automatic worker refuses a release when it has no valid 40-character
release baseline or when `server/resource/database` differs from that baseline.
Back up the database, then run the first or database-changing release
explicitly; the backend applies pending embedded migrations at startup:

```bash
sudo /usr/local/sbin/uvp-gitee-deploy-local \
  --allow-database-changes <40-character-commit-sha>
```

Before the first migration-assisted release, make sure
`/opt/uvp-gb28181/current-release` is a regular file containing the last
successful 40-character release SHA. If it is a directory or malformed, back
it up and repair it deliberately; the automatic worker will refuse to proceed.

## Database Changes with Embedded Migrations

The backend binary embeds `server/resource/database/gb28181/migrations/*.sql`
and applies pending ones automatically at startup — after `initDB` and before
business initialization. A dialect lock (`GET_LOCK` / `pg_advisory_lock` /
`sp_getapplock`) prevents concurrent instances from migrating at the same
time. A migration failure aborts startup with `log.Fatal`; the error includes
the file name and the offending SQL.

### First roll-out order (baseline once)

The runner treats an empty `gb_schema_migrations` table as an existing
environment: it marks every shipped migration as applied without executing
any SQL, because existing schemas already match the full snapshot
`server/resource/database/uvp-gb28181.sql`.

1. Deploy and start the runner version first (no schema-changing feature in
   the same release). Check the startup log for the migrations package.
2. Verify `gb_schema_migrations` exists and lists all shipped migration file
   names. Existing environments keep their current schema untouched.
3. Only then ship the first feature release that carries a new migration.
   New empty environments initialize from the full snapshot first, so the
   runner baselines the incremental files there as well.

### Writing a new migration

Add files under `server/resource/database/gb28181/migrations/` named
`YYYY-MM-DD-<desc>.sql`:

- `YYYY-MM-DD-<desc>.sql` — MySQL (default dialect)
- `YYYY-MM-DD-<desc>-postgresql.sql` — PostgreSQL
- `YYYY-MM-DD-<desc>-sqlserver.sql` — SQL Server
- `YYYY-MM-DD-<desc>-down.sql` — rollback (plus `-postgresql-down.sql` and
  `-sqlserver-down.sql` for the other dialects)

Files dated 2026-08-14 or later are checked by the contract test
`server/app/gb28181/migration/migration_contract_test.go`: all three dialects
and the down file must exist. The deployer's `--allow-database-changes` gate
still applies — keep the release baseline updated as before.

### Manual rollback (down)

Down files never run automatically. To roll back one applied migration on the
primary database (MySQL > PostgreSQL > SQL Server precedence):

```bash
sudo -u <app-user> /opt/uvp-gb28181/<run-path>/uvp-gb28181 \
  -migrate-down=YYYY-MM-DD-<desc>.sql
```

The process executes the down SQL, deletes the version row, and exits. The
next startup re-applies that migration, so treat this as a test-environment
operational tool, not a downgrade mechanism.

### Checklist before the first migration-assisted release

- [ ] Runner version deployed and baselined (see order above)
- [ ] New migration files: three dialects + down, contract test green
- [ ] `--allow-database-changes` used for database-changing releases
- [ ] Database backed up before the release

## Test-Environment Acceptance

For a test push, confirm `202 Accepted` from the hook, inspect the queue and
`journalctl -u uvp-gitee-deploy.service`, then verify the release marker,
container health, public health endpoint, and recent application logs. A hook
response alone is not deployment acceptance.
