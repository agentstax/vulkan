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