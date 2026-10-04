#!/bin/sh
set -eu

: "${REDIS_PASSWORD:?}"
: "${REDIS_ANNOUNCE:?}"

conf=/data/redis.conf
if [ ! -f "$conf" ]; then
  cat > "$conf" <<EOF
bind 0.0.0.0
protected-mode yes
port 6379
requirepass ${REDIS_PASSWORD}
masterauth ${REDIS_PASSWORD}
appendonly yes
appendfsync everysec
dir /data
min-replicas-to-write 1
min-replicas-max-lag 10
replica-announce-ip ${REDIS_ANNOUNCE}
replica-announce-port 6379
EOF
  if [ "${REDIS_ROLE:-master}" = "replica" ]; then
    echo "replicaof ${REDIS_MASTER:?} 6379" >> "$conf"
  fi
fi

exec redis-server "$conf"
