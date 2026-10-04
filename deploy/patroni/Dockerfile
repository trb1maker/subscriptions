FROM postgres:18.6

USER root

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        python3 \
        python3-venv \
        python3-pip \
        curl \
        ca-certificates \
        gettext-base \
    && python3 -m venv /opt/patroni \
    && /opt/patroni/bin/pip install --no-cache-dir "patroni[psycopg3,etcd3]==4.1.5" \
    && apt-get purge -y python3-pip \
    && apt-get autoremove -y \
    && rm -rf /var/lib/apt/lists/*

ENV PATH="/opt/patroni/bin:/usr/lib/postgresql/18/bin:${PATH}"

COPY patroni/entrypoint.sh /usr/local/bin/patroni-entrypoint.sh
COPY patroni/patroni.yml.template /etc/patroni.yml.template
COPY patroni/post-bootstrap.sh /bootstrap/post-bootstrap.sh
COPY postgres/01-create-auth.sql /bootstrap/01-create-auth.sql

RUN chmod 755 /usr/local/bin/patroni-entrypoint.sh /bootstrap/post-bootstrap.sh \
    && chown -R postgres:postgres /bootstrap

ENTRYPOINT ["/usr/local/bin/patroni-entrypoint.sh"]
