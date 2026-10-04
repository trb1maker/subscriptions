#!/bin/bash
set -euo pipefail

query() {
  local host="$1"
  shift
  clickhouse-client --host "$host" --user "$CLICKHOUSE_USER" --password "$CLICKHOUSE_PASSWORD" "$@"
}

for host in clickhouse-1 clickhouse-2; do
  ready=0
  for _ in {1..30}; do
    if query "$host" --query "SELECT 1" >/dev/null 2>&1; then
      ready=1
      break
    fi
    sleep 2
  done
  if [ "$ready" -ne 1 ]; then
    echo "clickhouse ${host} is not ready" >&2
    exit 1
  fi
done

query clickhouse-1 --multiquery --query "
CREATE DATABASE IF NOT EXISTS usage ON CLUSTER subscriptions
ENGINE = Replicated('/clickhouse/databases/usage', '{shard}', '{replica}');
"

query clickhouse-2 --query "SELECT name FROM system.databases WHERE name = 'usage'" | grep -q usage
