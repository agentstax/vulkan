set dotenv-required := true

### DATABASE ###

database-up:
  docker-compose -f ./scripts/database/docker-compose.yaml up

database-down:
  docker-compose -f ./scripts/database/docker-compose.yaml down

database-delete:
  docker-compose -f ./scripts/database/docker-compose.yaml down -v

migrate-up:
  migrate -source file:./migrations -database postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@localhost:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable up

migrate-down:
  migrate -source file:./migrations -database postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@localhost:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable down

### TESTING ###

# EX: just consume
consume group="learning.v1" processorsleep="0.1" shutdownsleep="1.0" failrate="0.0" crashafter="-1":
  go run examples/phase_1/consumer/main.go -group={{ group }} -processor-sleep={{ processorsleep }} -shutdown-sleep={{ shutdownsleep }} -fail-rate={{ failrate }} -crash-after={{ crashafter }}

# EX: just produce 3
produce count="1":
  go run examples/phase_1/producer/main.go -count={{ count }}

# Phase 6.5b lab: crash mid-range, recover. Deterministic, self-verifying --
# needs >=20 rows in message_log (run `just produce 20` first if empty).
reclaim-lab:
  go run examples/phase_1/reclaimlab/main.go

# Phase 6.5c lab: waterline pins on a failing message, jumps past it once resolved.
# Deterministic, self-verifying -- needs >=20 rows in message_log.
exception-lab:
  go run examples/phase_1/exceptionlab/main.go

# routing lab: bindings gate what a group receives, not what gets claimed.
# Deterministic, self-verifying, self-seeding -- publishes its own messages.
routing-lab:
  go run examples/phase_1/routinglab/main.go

# Phase 8a lab (a): id-range partitioning prunes claim reads to 1-2 partitions.
# Self-contained -- swaps message_log to a lab-scale partition width for the
# run and restores migration 001's shape after, but this WIPES message_log's
# existing rows permanently (no way to restore data, only schema).
partition-lab:
  go run examples/phase_1/partitionlab/main.go

# Phase 8a lab (b): a dropped partition is a hole a lagging cursor walks over
# empty, not a stall; the drop floor refuses the drop until committed past it
# or waived. Same schema swap + data-wipe caveat as partition-lab.
drop-floor-lab:
  go run examples/phase_1/dropfloorlab/main.go

# Phase 8a lab (c): the low-volume tail -- a partition too small to ever earn
# a whole-partition drop still sheds its expired prefix via the sweep.
# No schema swap, stays inside message_log_0 at its real migration width.
sweep-lab:
  go run examples/phase_1/sweeplab/main.go

peek:
  psql "postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@localhost:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable" \
    -c "SELECT * FROM message_log ORDER BY id;"

peek-users:
  psql "postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@localhost:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable" \
    -c "SELECT * FROM users ORDER BY id;"

# Phase 5 health metric: per-group lag = log head − cursor position.
# Run two groups, slow one with -processorsleep, watch their lags diverge.
lag:
  psql "postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@localhost:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable" \
    -c "SELECT c.consumer_group, c.position, COALESCE((SELECT max(id) FROM message_log), 0) AS head, COALESCE((SELECT max(id) FROM message_log), 0) - c.position AS lag FROM cursors c ORDER BY lag DESC;"

### DOC SITE (https://vulkan-5ss.pages.dev) ###

site-dev:
  cd website && npm run dev

site-deploy:
  cd website && npm run build && ./node_modules/.bin/wrangler pages deploy dist --project-name vulkan --branch main