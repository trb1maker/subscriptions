#!/bin/sh
set -eu

psql "$1" -v ON_ERROR_STOP=1 -f /bootstrap/01-create-auth.sql
