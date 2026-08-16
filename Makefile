.PHONY: dev

DATABASE_URL ?= postgres://movieflow:movieflow@localhost:5432/movieflow?sslmode=disable
export DATABASE_URL

dev:
	@set -eu; \
	api_pid=; \
	web_pid=; \
	cleanup() { \
		trap - INT TERM EXIT; \
		[ -z "$$api_pid" ] || kill "$$api_pid" 2>/dev/null || true; \
		[ -z "$$web_pid" ] || kill "$$web_pid" 2>/dev/null || true; \
		[ -z "$$api_pid" ] || wait "$$api_pid" 2>/dev/null || true; \
		[ -z "$$web_pid" ] || wait "$$web_pid" 2>/dev/null || true; \
	}; \
	trap cleanup INT TERM EXIT; \
	( cd api && exec go run ./cmd/api ) & api_pid=$$!; \
	( exec npm --prefix web run dev ) & web_pid=$$!; \
	while kill -0 "$$api_pid" 2>/dev/null && kill -0 "$$web_pid" 2>/dev/null; do \
		sleep 1; \
	done; \
	status=0; \
	if ! kill -0 "$$api_pid" 2>/dev/null; then \
		wait "$$api_pid" || status=$$?; \
	else \
		wait "$$web_pid" || status=$$?; \
	fi; \
	exit "$$status"
