# Gitee Domestic Deployment

This deployment path builds on the domestic server after a verified GitHub CI
run mirrors `develop` to Gitee. It does not run database migrations
automatically.

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
Back up and apply the reviewed migration manually, verify it, then run the
first or database-changing release explicitly:

```bash
sudo /usr/local/sbin/uvp-gitee-deploy-local \
  --allow-database-changes <40-character-commit-sha>
```

Before the first migration-assisted release, make sure
`/opt/uvp-gb28181/current-release` is a regular file containing the last
successful 40-character release SHA. If it is a directory or malformed, back
it up and repair it deliberately; the automatic worker will refuse to proceed.

## Test-Environment Acceptance

For a test push, confirm `202 Accepted` from the hook, inspect the queue and
`journalctl -u uvp-gitee-deploy.service`, then verify the release marker,
container health, public health endpoint, and recent application logs. A hook
response alone is not deployment acceptance.
