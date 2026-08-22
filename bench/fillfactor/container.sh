#!/usr/bin/env bash
# Start/stop the throwaway bench container (the bench/ convention:
# vulkan-bench-pg on :5433, postgres:18.4, 8 cpus / 8GB,
# shared_buffers=2.5GB, max_wal_size=4GB, pg_stat_statements,
# track_io_timing). Usage: container.sh start|stop
set -euo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"; source "$DIR/env.sh"

case "${1:?usage: container.sh start|stop}" in
start)
  docker rm -f vulkan-bench-pg >/dev/null 2>&1 || true
  docker run -d --name vulkan-bench-pg \
    --cpus=8 --memory=8g \
    -p "$PGPORT":5432 \
    -e POSTGRES_USER="$PGUSER" \
    -e POSTGRES_PASSWORD="$PGPASSWORD" \
    -e POSTGRES_DB="$PGDATABASE" \
    postgres:18.4 \
    -c shared_buffers=2560MB \
    -c max_wal_size=4GB \
    -c shared_preload_libraries=pg_stat_statements \
    -c track_io_timing=on \
    -c max_connections=200 >/dev/null

  until pg_isready -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" >/dev/null 2>&1; do sleep 0.5; done
  echo "vulkan-bench-pg ready on :$PGPORT"
  ;;
stop)
  docker rm -f vulkan-bench-pg >/dev/null
  echo "vulkan-bench-pg removed"
  ;;
*)
  echo "usage: container.sh start|stop" >&2; exit 2
  ;;
esac
