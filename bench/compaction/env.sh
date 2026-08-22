#!/usr/bin/env bash
# Shared connection for the compaction hot-key serialization benchmark.
# Targets the THROWAWAY container on :5433 (NOT the dev DB on :5432).
export PGHOST="${PGHOST:-localhost}"
export PGPORT="${PGPORT:-5433}"
export PGUSER="${PGUSER:-bench}"
export PGDATABASE="${PGDATABASE:-bench}"
export PGPASSWORD="${PGPASSWORD:-bench}"

psql_q()   { psql -v ON_ERROR_STOP=1 -tAc "$1"; }
psql_run() { psql -v ON_ERROR_STOP=1 -q -c "$1"; }
