.PHONY: build check dev fmt-check install lint prod screenshot sync test web-vitals

SHELL := /bin/bash

DATABASE_URL ?= postgres://movieflow:movieflow@localhost:5432/movieflow?sslmode=disable
export DATABASE_URL

DEV_COMPOSE_PROJECT_NAME ?= movieflow

URL ?= http://localhost:3000/
OUTPUT ?= /tmp/opencode/messeances-screenshot.png
WIDTH ?= 1440
HEIGHT ?= 900
WAIT_MS ?= 1000
API_URL ?= http://localhost:8080
CHROME_BIN ?= google-chrome
RUNS ?= 3

prod:
	docker compose --env-file deploy/.env.production -f deploy/compose.production.yaml up -d --wait --pull always

install:
	cd api && go mod download
	npm --prefix web install

fmt-check:
	@cd api && unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		printf '%s\n' "$$unformatted"; \
		exit 1; \
	fi

test:
	cd api && go test ./...
	npm --prefix web run test:unit

lint:
	cd api && go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.1 run

build:
	cd api && go build ./...
	npm --prefix web run build

check: fmt-check test lint build
	npm --prefix web run typecheck
	npm --prefix web run lint

dev:
	@set -eu; \
	api_pid=; \
	web_pid=; \
	group_alive() { \
		[ -n "$$1" ] && kill -0 -- "-$$1" 2>/dev/null; \
	}; \
	signal_group() { \
		[ -z "$$1" ] || kill -TERM -- "-$$1" 2>/dev/null || true; \
	}; \
	wait_group() { \
		[ -z "$$1" ] || while group_alive "$$1"; do sleep 0.1; done; \
	}; \
	cleanup() { \
		trap - INT TERM EXIT; \
		signal_group "$$api_pid"; \
		signal_group "$$web_pid"; \
		[ -z "$$api_pid" ] || wait "$$api_pid" 2>/dev/null || true; \
		[ -z "$$web_pid" ] || wait "$$web_pid" 2>/dev/null || true; \
		wait_group "$$api_pid"; \
		wait_group "$$web_pid"; \
	}; \
	trap cleanup EXIT; \
	trap 'exit 130' INT; \
	trap 'exit 143' TERM; \
	docker compose --project-name "$(DEV_COMPOSE_PROJECT_NAME)" --project-directory . --env-file deploy/.env -f deploy/compose.yaml up -d --wait postgres; \
	mkdir -p api/bin; \
	setsid bash -c 'cd api && exec go run github.com/air-verse/air@v1.61.7 -c .air.toml' & api_pid=$$!; \
	setsid npm --prefix web run dev & web_pid=$$!; \
	set +e; \
	wait -n -p first_pid "$$api_pid" "$$web_pid"; \
	status=$$?; \
	set -e; \
	exit "$$status"

screenshot: export URL := $(URL)
screenshot: export OUTPUT := $(OUTPUT)
screenshot: export WIDTH := $(WIDTH)
screenshot: export HEIGHT := $(HEIGHT)
screenshot: export WAIT_MS := $(WAIT_MS)
screenshot: export API_URL := $(API_URL)
screenshot: export CHROME_BIN := $(CHROME_BIN)
screenshot:
	@node web/tools/screenshot.mjs

web-vitals: export URL := $(URL)
web-vitals: export RUNS := $(RUNS)
web-vitals: export CHROME_BIN := $(CHROME_BIN)
web-vitals:
	npm --prefix web run web-vitals

sync:
	@printf '%s\n' '[sync] starting'
	@printf '%s\n' '[sync] validating proxy file'; \
	if [ -z "$${PROXY_FILE:-}" ]; then \
		printf '%s\n' 'Usage: make sync PROXY_FILE=/path/to/proxies.txt' >&2; \
		printf '%s\n' '[sync] failed' >&2; \
		exit 2; \
	fi
	@printf '%s\n' '[sync] starting PostgreSQL'; \
	docker compose --project-name "$(DEV_COMPOSE_PROJECT_NAME)" --project-directory . --env-file deploy/.env -f deploy/compose.yaml up -d --wait postgres || { status=$$?; printf '%s\n' '[sync] failed' >&2; exit "$$status"; }
	@printf '%s\n' '[sync] starting UGC'; \
	cd api && go run ./cmd/sync-ugc -proxy-file "$$PROXY_FILE" || { status=$$?; printf '%s\n' '[sync] failed' >&2; exit "$$status"; }
	@printf '%s\n' '[sync] UGC finished'
	@printf '%s\n' '[sync] starting Kinepolis'; \
	cd api && go run ./cmd/sync-kinepolis -proxy-file "$$PROXY_FILE" || { status=$$?; printf '%s\n' '[sync] failed' >&2; exit "$$status"; }
	@printf '%s\n' '[sync] Kinepolis finished'
	@printf '%s\n' '[sync] starting Pathé'; \
	cd api && go run ./cmd/sync-pathe -proxy-file "$$PROXY_FILE" || { status=$$?; printf '%s\n' '[sync] failed' >&2; exit "$$status"; }
	@printf '%s\n' '[sync] Pathé finished'
	@printf '%s\n' '[sync] starting CGR'; \
	cd api && go run ./cmd/sync-cgr -proxy-file "$$PROXY_FILE" || { status=$$?; printf '%s\n' '[sync] failed' >&2; exit "$$status"; }
	@printf '%s\n' '[sync] CGR finished'
	@printf '%s\n' '[sync] complete'
