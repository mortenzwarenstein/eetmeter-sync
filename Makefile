# eetmeter-sync — common tasks. Run `make help` for the list.

# --env-file /dev/null: the compose file has no ${vars}; stop compose from
# reading ./.env (and warning about $-signs in the app's real secrets).
DC ?= docker compose --env-file /dev/null
DEV_DB_URL := postgres://eetmeter:eetmeter@localhost:5432/eetmeter?sslmode=disable

.DEFAULT_GOAL := help

## help: show this list
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## /  /'

## build: compile both binaries into ./bin
build:
	go build -o bin/eetmeter-sync ./cmd/eetmeter-sync
	go build -o bin/fake-eetmeter ./cmd/fake-eetmeter

## test: run unit tests (no database needed)
test:
	go test ./...

## test-all: run every test, including store + engine integration tests
## test-all: -p 1 because the integration packages share one database
test-all: db-wait
	TEST_DATABASE_URL="$(DEV_DB_URL)" go test -p 1 -count=1 ./...

## check: gofmt + go vet
check:
	gofmt -l . | (! grep .) || (echo "run: gofmt -w ." && exit 1)
	go vet ./...

## db: start the local PostgreSQL container
db:
	$(DC) up -d db

## db-wait: start the DB and block until it accepts connections
db-wait: db
	@until $(DC) exec -T db pg_isready -U eetmeter >/dev/null 2>&1; do sleep 0.5; done
	@echo "postgres ready on localhost:5432"

## db-stop: stop the DB container (keeps the volume)
db-stop:
	$(DC) stop db

## db-reset: drop and recreate the local database
db-reset: db-wait
	$(DC) exec -T db psql -U eetmeter -d eetmeter -c 'DROP SCHEMA public CASCADE; CREATE SCHEMA public;'
	@echo "database wiped; it will re-migrate on next start"

## dev: full local stack against the FAKE Eetmeter API with seeded recipes.
## dev: starts DB + fake API + the app. Ctrl-C stops everything. No .env needed.
dev: db-wait
	@echo "starting fake-eetmeter on :8090 and eetmeter-sync on :8080 ..."
	@trap 'kill 0' EXIT; \
	go run ./cmd/fake-eetmeter -addr :8090 -seed & \
	sleep 1; \
	DATABASE_URL="$(DEV_DB_URL)" \
	EETMETER_API_BASE_URL="http://localhost:8090/api" \
	EETMETER_ACCOUNT_A_EMAIL="you@example.com"     EETMETER_ACCOUNT_A_PASSWORD="dev" EETMETER_ACCOUNT_A_LABEL="You" \
	EETMETER_ACCOUNT_B_EMAIL="partner@example.com" EETMETER_ACCOUNT_B_PASSWORD="dev" EETMETER_ACCOUNT_B_LABEL="Partner" \
	EETMETER_ACCOUNT_A_TOKEN="" EETMETER_ACCOUNT_A_DEVICE_ID="" EETMETER_ACCOUNT_B_TOKEN="" EETMETER_ACCOUNT_B_DEVICE_ID="" \
	SYNC_API_TOKEN="" SYNC_ON_STARTUP="true" LOG_LEVEL="debug" \
	go run ./cmd/eetmeter-sync

## run: run the app on the host against your real accounts (reads .env directly)
run: db-wait .env
	go run ./cmd/eetmeter-sync

## sync: trigger a sync on a running instance and print the result
sync:
	curl -fsS -X POST localhost:8080/sync | sed 's/.*/triggered: &/'
	@sleep 1
	curl -fsS localhost:8080/sync/last

.env:
	cp .env.example .env
	@echo "created .env from .env.example — edit it, then re-run"
	@exit 1

## docker-build: build the production container image
docker-build:
	docker build -t eetmeter-sync:dev .

.PHONY: help build test test-all check db db-wait db-stop db-reset dev run sync docker-build
