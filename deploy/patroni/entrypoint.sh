#!/bin/bash
set -euo pipefail

if [ "$(id -u)" = "0" ]; then
  mkdir -p /var/lib/postgresql/data /var/run/postgresql
  chown -R postgres:postgres /var/lib/postgresql/data /var/run/postgresql
  exec gosu postgres "$0"
fi

envsubst '${PATRONI_NAME} ${POSTGRES_USER} ${POSTGRES_PASSWORD}' \
  < /etc/patroni.yml.template > /tmp/patroni.yml

exec patroni /tmp/patroni.yml
