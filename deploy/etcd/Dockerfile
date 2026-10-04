FROM alpine:3.22

ARG ETCD_VERSION=3.5.21

RUN apk add --no-cache ca-certificates curl \
    && arch="$(uname -m)" \
    && case "$arch" in \
        aarch64) etcd_arch=arm64 ;; \
        x86_64) etcd_arch=amd64 ;; \
        *) echo "unsupported architecture: $arch" >&2; exit 1 ;; \
    esac \
    && curl -fsSL -o /tmp/etcd.tar.gz \
        "https://github.com/etcd-io/etcd/releases/download/v${ETCD_VERSION}/etcd-v${ETCD_VERSION}-linux-${etcd_arch}.tar.gz" \
    && tar -xzf /tmp/etcd.tar.gz -C /tmp \
    && mv "/tmp/etcd-v${ETCD_VERSION}-linux-${etcd_arch}/etcd" /usr/local/bin/etcd \
    && mv "/tmp/etcd-v${ETCD_VERSION}-linux-${etcd_arch}/etcdctl" /usr/local/bin/etcdctl \
    && rm -rf /tmp/etcd* \
    && apk del curl

COPY entrypoint.sh /entrypoint.sh
RUN chmod 755 /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
