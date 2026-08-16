.PHONY: dev sync

SHELL := /bin/bash

DATABASE_URL ?= postgres://movieflow:movieflow@localhost:5432/movieflow?sslmode=disable
export DATABASE_URL

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
	docker compose up -d --wait postgres; \
	mkdir -p api/bin; \
	setsid bash -c 'cd api && exec go run github.com/air-verse/air@v1.61.7 -c .air.toml' & api_pid=$$!; \
	setsid npm --prefix web run dev & web_pid=$$!; \
	set +e; \
	wait -n -p first_pid "$$api_pid" "$$web_pid"; \
	status=$$?; \
	set -e; \
	exit "$$status"

sync:
	@if [ -z "$${PROXY_FILE:-}" ]; then \
		printf '%s\n' 'Usage: make sync PROXY_FILE=/path/to/proxies.txt' >&2; \
		exit 2; \
	fi
	@docker compose up -d --wait postgres
	@cd api && go run ./cmd/sync-ugc -proxy-file "$$PROXY_FILE"
