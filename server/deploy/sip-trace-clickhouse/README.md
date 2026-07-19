# SIP Trace ClickHouse

This directory deploys the optional ClickHouse dependency for the GB28181 SIP diagnostic workbench. It is independent from the UVP process and MySQL.

## Keep It Disabled By Default

The repository example remains:

```yaml
gb28181:
  trace:
    enabled: false
```

With this setting UVP does not dial ClickHouse, start a trace writer, or return the SIP Trace menu. Deploy this component only when original SIP messages are required.

## Start ClickHouse

Requirements: Docker Engine with Compose v2 and a persistent local disk.

```bash
cd server/deploy/sip-trace-clickhouse
cp .env.example .env
```

Replace both `CHANGE_ME` passwords in `.env`. Keep `CLICKHOUSE_BIND_IP=127.0.0.1` when UVP runs on the same host. For separate hosts, bind a private IP and restrict TCP 9000/8123 to the UVP server at the firewall.

```bash
docker compose up -d
docker compose ps
docker compose exec clickhouse clickhouse-client \
  --user uvp_trace --password \
  --query "SELECT currentUser(), version()"
```

The initialization script creates `uvp_sip_trace` and grants the application account only `CREATE TABLE`, `CREATE VIEW`, `SELECT`, and `INSERT` on that database. UVP creates the message table, the 30-day session summary table, and its materialized view on first successful connection.

## Enable UVP

Generate a separate 32-byte application encryption key. It is not the ClickHouse password.

```bash
openssl rand -base64 32
```

Expose the two secrets to the UVP process through its service manager or container secret mechanism:

```bash
UVP_SIP_TRACE_CLICKHOUSE_PASSWORD=<same application password as .env>
UVP_SIP_TRACE_ENCRYPTION_KEY=<generated 32-byte base64 key>
```

Then update `server/config/config.yml`:

```yaml
gb28181:
  trace:
    enabled: true
    address: "127.0.0.1:9000"
    database: "uvp_sip_trace"
    username: "uvp_trace"
    password_env: "UVP_SIP_TRACE_CLICKHOUSE_PASSWORD"
    tls: false
    queue_capacity: 8192
    batch_size: 500
    flush_interval_ms: 500
    encryption_key_env: "UVP_SIP_TRACE_ENCRYPTION_KEY"
```

Restart UVP and check `GET /api/gb28181/sip-traces/health` with a system administrator account. `ready` means writes are succeeding. `degraded` reports the last storage error and dropped-event count without interrupting SIP traffic.

## Retention And Capacity

- Encrypted original messages: 7 天, deleted by ClickHouse TTL.
- Daily Call-ID summaries: 30 天, then deleted by TTL.
- The encrypted payload is effectively incompressible. Size the disk from observed message rate and average raw bytes.

Approximation:

```text
7-day bytes = messages/second * average message bytes * 604800 * 1.30
```

At an average encrypted row size of 1.5 KB, including a 30% metadata and merge reserve:

| Average rate | Approximate 7-day disk |
|---:|---:|
| 5 messages/s | 6 GB |
| 50 messages/s | 59 GB |
| 200 messages/s | 236 GB |

Measure `system.parts.bytes_on_disk` after a normal business day and revise the estimate. Alert at 70% disk usage and treat 85% as critical. Also alert when health remains `degraded` for 60 seconds or `dropped` increases.

## Backup And Recovery (备份与恢复)

SIP Trace is diagnostic data, not the business source of truth. A single-container deployment has no high-availability guarantee.

Before host maintenance, stop ClickHouse and snapshot the `clickhouse-data` volume. For recovery:

1. Restore the volume to the same ClickHouse version and run `docker compose up -d`.
2. Verify `SELECT count() FROM uvp_sip_trace.sip_trace_message` with the restricted account.
3. Start UVP with the same encryption key; changing it makes retained payloads unreadable.
4. If the volume cannot be recovered, start with an empty volume. UVP recreates the schema and new SIP traffic continues; the workbench must report the historical gap.

To disable the feature, set `enabled: false` and restart UVP before stopping ClickHouse. Existing ClickHouse data remains until the volume is explicitly removed.
