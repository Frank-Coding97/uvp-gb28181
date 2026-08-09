#!/usr/bin/env bash
set -euo pipefail

database="${UVP_SIP_TRACE_DATABASE:-uvp_sip_trace}"
app_user="${UVP_SIP_TRACE_USER:-uvp_trace}"
app_password="${UVP_SIP_TRACE_CLICKHOUSE_PASSWORD:?UVP_SIP_TRACE_CLICKHOUSE_PASSWORD is required}"

identifier_pattern='^[A-Za-z_][A-Za-z0-9_]*$'
if [[ ! "${database}" =~ ${identifier_pattern} ]] || [[ ! "${app_user}" =~ ${identifier_pattern} ]]; then
  echo "invalid SIP Trace database or user identifier" >&2
  exit 1
fi
if [[ "${app_password}" == CHANGE_ME* ]]; then
  echo "replace the SIP Trace application password before startup" >&2
  exit 1
fi

clickhouse-client \
  --user "${CLICKHOUSE_USER}" \
  --password "${CLICKHOUSE_PASSWORD}" \
  --param_app_password "${app_password}" \
  --multiquery <<SQL
CREATE DATABASE IF NOT EXISTS \`${database}\`;
CREATE USER IF NOT EXISTS \`${app_user}\`
  IDENTIFIED WITH sha256_password BY {app_password:String};
ALTER USER \`${app_user}\`
  IDENTIFIED WITH sha256_password BY {app_password:String};
GRANT CREATE TABLE, CREATE VIEW, SELECT, INSERT
  ON \`${database}\`.* TO \`${app_user}\`;
GRANT DROP VIEW
  ON \`${database}\`.sip_trace_session_day_mv TO \`${app_user}\`;
SQL
