#!/bin/sh
set -eu

: "${REDIS_PASSWORD:?}"
: "${REDIS_MASTER:?}"
: "${SENTINEL_ANNOUNCE:?}"

conf=/data/sentinel.conf
if [ ! -f "$conf" ]; then
  cat > "$conf" <<EOF
port 26379
bind 0.0.0.0
protected-mode no
dir /data
sentinel resolve-hostnames yes
sentinel announce-hostnames yes
sentinel announce-ip ${SENTINEL_ANNOUNCE}
sentinel announce-port 26379
sentinel monitor usage ${REDIS_MASTER} 6379 2
sentinel down-after-milliseconds usage 5000
sentinel failover-timeout usage 20000
sentinel parallel-syncs usage 1
sentinel auth-pass usage ${REDIS_PASSWORD}
EOF
fi

exec redis-sentinel "$conf"
