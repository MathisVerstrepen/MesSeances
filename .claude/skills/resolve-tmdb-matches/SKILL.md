---
name: resolve-tmdb-matches
description: Resolve MesSeances production TMDB correspondences (admin "Correspondances TMDB" page) through the token-authenticated admin API with scripts/admin-api. Use when the user asks to resolve, review, approve, reject or correct TMDB matches, or to act on other production admin pages via the API.
---

# Resolve TMDB Matches in Production

You act on **production** (`https://messeances.fr`) through `scripts/admin-api`. Every approve, reject and correct is immediately visible on the public site. Run this skill only when the user explicitly asks for it.

## Preconditions

1. Run `scripts/admin-api session`. Continue only on `{"authenticated":true}`.
2. On a missing or rejected token, stop and tell the user how to set it up (below). Never read, print, copy or regenerate the token yourself, and never fall back to `ADMIN_PASSWORD`, SSH or SQL.

Token setup (user-side, once):

```sh
# On the VPS: add ADMIN_API_TOKEN=$(openssl rand -hex 32) to deploy/.env.production, then recreate the api service.
# Locally: store the same value, mode 600.
install -m 600 /dev/null ~/.config/messeances/admin-token && $EDITOR ~/.config/messeances/admin-token
```

## Commands

```sh
scripts/admin-api matches [unresolved|rejected|matched] [limit<=100] [offset]
scripts/admin-api search "<titre>"          # TMDB search, fr-FR, max 20 results
scripts/admin-api tmdb <tmdb_id>            # runtime, release date, original title/language, overview, imdb_id
scripts/admin-api approve <provider> <source_id> <tmdb_id>
scripts/admin-api reject <provider> <source_id>
scripts/admin-api correct <provider> <source_id> <tmdb_id> <updated_at>
scripts/admin-api get <path> | post|put|patch|delete <path> [json]   # other /api/v1/admin routes
```

Pipe JSON through `jq` to keep context small, e.g. `scripts/admin-api matches unresolved 100 | jq -c '.items[] | {source_provider, source_movie_id, source_title, source_runtime_minutes, status, candidates: [.candidates[] | {id, title, original_title, runtime_minutes, score}]}'`.

## Workflow

1. List `unresolved` items (statuses `review_required` and `unmatched`), paginating with `offset` until a page returns fewer than `limit` items.
2. For each item, decide from evidence, not from the score alone:
   - Compare `source_title` with candidate `title`/`original_title` (accents, articles, subtitles, sequel numbers).
   - Compare `source_runtime_minutes` with TMDB runtime (`tmdb <id>`); within ~10 min is consistent, a larger gap needs another signal.
   - Check release year plausibility: films in French cinemas now are usually recent releases or known re-releases (classics, ciné-club, festivals).
   - If no candidate fits, `search` with title variants (without subtitle, original title, without accents), then confirm with `tmdb <id>`.
3. Act only when confident:
   - **approve**: one TMDB film clearly matches. Works with a listed candidate or with a manual TMDB ID found via `search`.
   - **reject**: the source is not a film in TMDB (events, opera/ballet broadcasts, concert, marathon/bundle sessions, avant-premières surprise, short-film programs, conferences).
   - **leave unresolved and report** anything ambiguous: two plausible films, same-title remakes, missing runtime and no other signal, unclear event vs film.
4. Use `correct` only for `matched` items the user asked to fix. Pass the item's `updated_at` verbatim; a `409 review_conflict` means it changed or is locally merged: re-read, never retry blindly.
5. Do not batch more than ~25 mutations without a progress line to the user. Stop on repeated `5xx`/`502 tmdb_failed`/`503`.

## Error codes

- `401 unauthorized`: token missing/invalid/not configured on server — stop.
- `404 not_found`: source no longer in the current schedule snapshot — skip.
- `409 review_conflict`: state changed, locally merged, or candidate not allowed — re-read the item, then decide.
- `502 review_failed` / `tmdb_failed`, `503 review_unavailable` / `tmdb_unavailable`: TMDB side issue — retry later, do not loop.

## Final report

End with a compact table: approved (source → TMDB id + title + year), rejected (reason), left unresolved (why, best candidates), errors. Include counts.

## Safety

- Never print or log the token, cookies, or raw provider error bodies.
- Never call sync, schedule, source-toggle, or geocoding mutations unless the user explicitly asks for that specific action.
- Production SSH/SQL is not an alternative path for review decisions: it bypasses `ReviewService` validation.
