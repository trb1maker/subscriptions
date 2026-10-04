#!/bin/sh
set -eu

data_dir="${ETCD_DATA_DIR:-/etcd-data}"
mkdir -p "$data_dir"

if [ -d "${data_dir}/member" ]; then
  export ETCD_INITIAL_CLUSTER_STATE=existing
else
  export ETCD_INITIAL_CLUSTER_STATE=new
fi

exec etcd
