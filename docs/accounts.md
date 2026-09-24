# Accounts: setup and operations

Accounts are disabled by default. This document describes implemented behavior and required operator actions, not a deployed or approved production configuration. Registration must remain disabled until provider, proxy, privacy, backup and acceptance prerequisites below are complete. No resources or credentials are provisioned by this feature.

## Product behavior

- Email/password registration chooses the password once, then sends an SES verification link. Explicit confirmation in the original registration browser retains that password; verified users then choose a fixed unique username. No second password or registration-method choice is requested. Opening the link alone does not consume it.
- Email verification requires an independent HttpOnly registration-attempt cookie, not a session obtained by logging in with tentative credentials. A different browser must reopen the link in the original browser. If that cookie is lost, restart registration: an accepted pending-email retry replaces both tentative password and browser proof, revokes prior links and sessions, and preserves the original seven-day deadline. Verified accounts and pending Google accounts are not overwritten. Resend requires matching email-registration proof, returns generic acceptance otherwise, and never claims delivery.
- Passwords require 10-128 Unicode code points, with an internal 512-byte UTF-8 cap and common-password rejection. Passwords are not trimmed or normalized.
- Google uses `openid email profile`. Automatic email verification requires a validated token with `email_verified=true` and either an exact `@gmail.com` address or a nonblank signed Workspace `hd` claim. Other addresses, including verified third-party addresses, require SES mailbox proof and the same Google-authenticated session before username onboarding. Email equality never merges accounts or links identities; Google issuer/subject is identity, not its observed email. The optional signed picture claim can supply an avatar; names and other profile fields are not imported.
- If an unlinked Google identity has a validated verified email already used by a live account, login returns fixed `google_email_in_use` guidance: sign in with the existing method, complete onboarding if needed, then explicitly associate Google from `/compte` using recent authentication. The failed callback creates no identity or session and preserves any existing account session. Unverified-email collisions and unrelated linking/provider failures retain generic errors; no email or subject appears in the redirect.
- Username is fixed: 3-30 ASCII characters, leading letter, letters/digits/underscore, uppercase normalized. Both supplied lists plus application reservations are blocked; see [provenance](../api/internal/accounts/reserved_usernames.md). A private profile picture appears only in settings, not the header or a public profile. No favorite synchronization.
- Google signup, login and explicit linking fill an untouched empty photo after successful authorization. Missing, invalid or unavailable pictures never fail sign-in. Uploads and explicit removal are never overwritten by Google; removal remains effective across unlink/relink, while a later explicit upload is allowed. Google reauthentication does not import a photo, and unlinking retains an already imported photo.
- Sessions expire after 30 days absolute or seven days idle. Incomplete accounts become inaccessible exactly seven days after creation and are purged by bounded startup/hourly cleanup. No username claim exists before completion.
- Settings support password reset/change/addition, email change, explicit Google link/unlink, logout/all and immediate deletion. The last usable login method cannot be removed. No remove-password feature exists.
- Sensitive operations require a five-minute, single-use session/action/target-bound grant. Password-bearing accounts prove current password. Google-only accounts need a fresh subject-bound Google flow and current-mailbox challenge within its ten-minute lifetime. Losing either provider or mailbox access fails closed; no manual recovery bypass is provided.
- Email change leaves old address authoritative until new-address confirmation with a separate fresh target-bound grant, then signs out all sessions. Google-only confirmation asks for the original new-email link after fresh Google/mailbox proof, avoiding bearer-token storage across redirects.
- Deletion requires recent proof and typed `SUPPRIMER`. Account data and authority are deleted transactionally. Only username remains permanently reserved without account ID, email, subject, identity hash or deletion timestamp. Independent suppression/security data follows its retention. Browser-local cinema preferences remain unchanged.

## Configuration

`ACCOUNTS_ENABLED=false` constructs no account provider clients or workers and requires no new secrets. `GET /api/v1/auth/session` reports disabled anonymous state; other account routes return safe 503. When enabled, missing or invalid static configuration fails with generic `configuration error` rather than silently offering partial authentication.

| Variable | Requirement |
| --- | --- |
| `ACCOUNTS_ENABLED` | Explicit true only after rollout checklist. |
| `WEB_ORIGIN` | Canonical HTTPS origin without path. HTTP allowed only for explicitly configured loopback development. |
| `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` | Dedicated Google web client; callback derived from WEB_ORIGIN. |
| `AWS_REGION` | Same region as SES identity, feedback topic and queues. |
| `SES_FROM_EMAIL`, `SES_CONFIGURATION_SET`, `SES_IDENTITY_ARN` | Fixed verified sender, configuration set and identity ARN. |
| `SES_FEEDBACK_QUEUE_URL`, `SES_FEEDBACK_TOPIC_ARN` | Standard private queue/topic in expected account and region. |
| `ACCOUNT_OUTBOX_KEY_ID`, `ACCOUNT_OUTBOX_KEY` | Version label and canonical standard-base64 nonzero 32-byte AES-GCM key. |
| `ACCOUNT_ADDRESS_HMAC_KEY` | Independent, distinct nonzero 32-byte standard-base64 key for suppression/quota addresses. |
| AWS credentials | SDK default credential chain; production Compose passes `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` and optional `AWS_SESSION_TOKEN`. |

Use ignored operator configuration with restrictive permissions or appropriately mounted restricted credentials. Credential-file mounts are not provisioned by repository Compose. Never put secrets in tracked files, `NUXT_PUBLIC_*`, URLs, process arguments, chat, logs or screenshots. Do not reuse admin/internal-service secrets. Examples intentionally contain empty new credential values.

Single-key encryption supports no transparent multi-key rotation. Drain or expire queued payloads and OAuth flows before coordinated key replacement. Blind HMAC-key rotation loses local suppression/quota lookup continuity; plan migration separately. Key IDs alone do not recover old ciphertext.

## Google setup

Configure consent screen, authorized domain, privacy URL and published web client. Exact production redirect is `https://messeances.fr/api/v1/auth/google/callback`; use a separate development client with exact configured loopback origin. No arbitrary redirect/issuer endpoints are accepted. Provider calls use fixed bounded endpoints, PKCE S256, state/browser binding, nonce and maintained signature verification. Provider bearer tokens are discarded, not persisted.

Observed Google email is displayed separately and never changes account contact/login email. Google third-party email claims are not mailbox-recovery authority. Fresh SES proof is required for Google-only sensitive actions. Provider denial redirects to fixed `/connexion?error=google_failed`, without provider response details.

Provider email verification and authoritative mailbox ownership are separate claims. Only the signed ID token can establish Workspace `hd`; callback query parameters cannot. Workspace aliases need not match the hosted-domain string. This onboarding policy does not retroactively revoke existing accounts or change subject-based sign-in.

OAuth admission retains a hard limit of 10,000 stored flows. Before counting, it reclaims up to 100 expired anonymous flows under the shared admission lock, skipping locked rows. Abandoned expired flows therefore release capacity during new starts instead of waiting for hourly cleanup. Live flows and in-flight flows that have not expired are preserved. Account-bound expired flows remain subject to account-first hourly cleanup. A full table of live flows still rejects new starts; existing IP limits remain in effect.

## SES, SNS and SQS setup

1. Choose region/sender and verify SES identity. Configure DKIM, aligned SPF/custom MAIL FROM and DMARC; obtain production access and adequate quota.
2. Configure SES bounce/complaint event destination to a standard SNS topic. Disable open/click tracking. SNS subscription must keep raw message delivery disabled: consumer requires SNS envelope.
3. Create an encrypted standard SQS feedback queue and separate encrypted standard DLQ in configured AWS account/region. FIFO resources are unsupported.
4. Restrict SNS policy to `ses.amazonaws.com` publishing from exact SES configuration-set ARN `arn:aws:ses:<region>:<account>:configuration-set/<name>` and source account.
5. Restrict SQS `SendMessage` to `sns.amazonaws.com`, exact topic ARN and source account. Runtime principal must not publish, send messages, provision resources or alter queue policies.
6. Grant runtime only `ses:SendEmail` for intended identity/configuration set/sender, `sqs:ReceiveMessage` and `sqs:DeleteMessage` on main queue, `sqs:GetQueueAttributes` on queue and DLQ, and narrowly scoped KMS decryption when needed.
7. Configure main retention 60 seconds to seven days, DLQ retention at least main retention and at most seven days, redrive `maxReceiveCount` 1-5. Recommended: four days main, seven days DLQ, five receives. Monitor DLQ and queue age.

Runtime checks queue encryption, retention, ARN and redrive prerequisites before polling and every five minutes. Application validates exact SNS topic and SES source/identity/account/configuration-set fields, bounded JSON and recipients. It never follows message URLs. TLS/SigV4 and restrictive resource policies establish provenance; application field checks are not an IAM audit. Operator must audit policies before enabling sends.

Permanent bounces and complaints commit idempotent suppression before message deletion. Transient bounces do not permanently suppress. Malformed messages remain for bounded queue redrive. Keep SES account suppression enabled as additional protection; application never removes SES suppressions.

## Delivery and monitoring

Token creation and AES-GCM encrypted outbox enqueue share a database transaction. One cross-replica advisory-lock dispatcher claims jobs with 60-second leases and random fencing, then sends outside account transactions at most once per second. It rechecks expiry, account revision, token/session authority and suppression immediately before sending. SES request timeout is ten seconds with one SDK attempt. Initial attempt plus at most five retries use 1m, 5m, 15m, 1h and 4h delays; crashes consume attempts. Terminal disposition erases encrypted payload. At-least-once delivery can duplicate an accepted message after ambiguous failure, but never creates a new token. Already in-flight mail cannot be recalled; revoked links remain unusable. SES acceptance is not proof of inbox delivery.

Monitor aggregate `account_mail_dispatch` fields `accepted`, `failed`, `discarded`, `retries`, `errors`, `pending`, `oldest_seconds`; `account_mail_feedback` fields `processed`, `rejected`, `errors`; and `account_mail_unavailable`, `account_mail_feedback_unavailable`, cleanup errors/backlog and DLQ depth. Provider errors/bodies, tokens, addresses, credentials and raw messages must not enter logs or metric labels. SDK initialization and feedback failures retry after one minute. Feedback validation failure stops consumption, not outbound dispatch or public/admin traffic; alerts and operator response are mandatory.

## Browser and proxy contract

Browser account calls are relative `/api/v1/...`. Production `NUXT_PUBLIC_API_BASE` must equal canonical `WEB_ORIGIN`, without `/api` suffix; private `NUXT_API_BASE` stays internal. Development Nitro proxy preserves `/api` paths, Origin and cookies. Never proxy authentication through catalog internal-service credentials.

Operator-managed Nginx must preserve `/api/` unchanged when forwarding to Go, preserve Host/Origin/Set-Cookie, overwrite forwarding headers using verified client information, and bypass caching for `/api/v1/auth`, `/api/v1/account` and all private pages/payloads. Do not cache Set-Cookie responses. Keep existing exact admin-login rate-limit location from [admin ingress contract](nginx-admin-login-rate-limit.md). Verify actual immediate proxy peer before configuring `TRUSTED_PROXY_CIDRS`; do not broaden ranges to make tests pass. No host proxy changes happen automatically.

Disable access/error/APM request-body and query logging on OAuth callback and authentication paths; callback query contains transient code/state. Do not log Cookie/Set-Cookie or request bodies. Fragments never reach HTTP servers, but browser telemetry could expose them. Internal public/account navigation uses the SPA router, preserving the document and header; Google authorization remains external. Direct account entries strip fragments before application scripts. SPA token arrivals use a route-bound, one-shot client-memory handoff and replace the URL without retaining the token in router history, browser storage or serialized state. Confirmation still requires an explicit POST. Route departure and invalidated identity clear private operation state; late responses cannot update departed views.

Umami is loaded only for eligible public routes, with automatic tracking disabled. Public pageviews use an explicit path allowlist, sanitized titles/referrers and a guarded before-send hook. Account, admin and unknown routes produce no analytics events; query strings, fragments, account identity and credentials are excluded from payloads. Public document referrer policy is `strict-origin`; private routes use `no-referrer`. A script already loaded on a public page may remain resident during account navigation: this is event suppression, not isolation from third-party code or a guarantee that such code cannot inspect the DOM. An already dispatched sanitized public request may complete after entering an account page. Do not re-enable automatic tracking or replace the tested tracker without revalidating these boundaries.

Public analytics intentionally excludes query/hash segmentation, raw referrer paths, automatic click/performance events and superseded views before delayed script loading. Browser regression uses pinned Umami 3.3.1 with intercepted network traffic; deployed tracker version and configuration require operator verification.

Production cookie is `__Host-messeances_session`, Secure, HttpOnly, SameSite=Lax, Path=/, no Domain; only canonical loopback HTTP uses `messeances_session_dev`. Account mutations require exact Origin, JSON and custom CSRF header, reject cross-site Fetch Metadata, and do not use account CORS. Admin cookie/auth remains separate. Private SSR forwards only account cookie and generated request ID. Private HTML/payload/API responses are no-store, noindex and no-referrer. PWA excludes account/auth content and does not queue offline credential writes. Validate deployed CDN/service-worker behavior rather than assuming headers alone suffice.

## Retention and privacy gate

### Private avatar storage

Enabled accounts now require `ACCOUNT_AVATAR_DIR`, an absolute, pre-created, API-owned private directory with mode 0700. Files use mode 0600. Symlink, insecure, unwritable or already-locked roots fail startup generically; the API does not repair permissions. Prepare this directory before restarting an enabled deployment. Disabled accounts construct no avatar store, downloader or collector. Never serve the directory through Nginx, Nuxt public assets or a generic static-file route.

Production Compose mounts named volume `account_avatars` only into API at `/var/lib/messeances/avatars`, matching API UID/GID 10001 and the image's prepared directory. Existing-volume ownership still needs operator verification. Development runs the API on the host, so create a dedicated host directory and set its absolute path; development Compose does not provision an unused media volume. One writer on one host, one media-root/database pairing is supported. Stop the API before paired PostgreSQL/media backups or restoration; do not run replicas with separate roots against one database. Backup retention and restoration of deleted photos require operator policy.

Uploads accept JPEG, PNG and static WebP, at most 5 MiB, 8192 pixels per axis and 16,777,216 pixels total. Server processing applies EXIF orientation, center-crops and scales to 256×256, then emits PNG with a 512 KiB output ceiling. Missing or unusable orientation defaults to identity. Original files and metadata are not retained. Animated PNG/WebP, GIF, SVG and malformed or unsupported images are rejected. There are two non-queuing processing slots, a ten-second upload body deadline, and a 10-attempt/15-minute account write quota. Full-raster memory remains significant: a maximum 16-bit raster measured about 130 MiB per operation; provision for both slots plus API/Argon2 and runtime overhead. Decoder CPU is bounded by input limits, not hard-preempted by an I/O deadline.

Google import accepts only validated picture claims with HTTPS host `lh3.googleusercontent.com`, no redirects, query, credentials or fragment. DNS/IP checks and a separate transport reject non-public destinations and ignore environment proxies. Two seconds bounds additional import I/O/commit waits, not synchronous decoder CPU. Only source none is eligible; final account/auth/avatar revisions and linked subject are rechecked after processing. Import failures are best-effort and may be retried by a later successful login while the photo remains untouched empty. Verify production consent includes `profile`; unrecognized picture hosts skip safely rather than relaxing the allowlist.

Complete-account clients read `avatar_url` from account details. `POST /api/v1/account/avatar` accepts exactly one multipart file named `avatar`; `DELETE` accepts strict JSON `{}`. Both use normal account Origin/CSRF/session checks, with no additional step-up. GET `/api/v1/account/avatar/{revision}` serves only the current session owner's current PNG, with no-store, no-referrer, nosniff and same-origin CORP. URLs expose neither account IDs nor filesystem names. Stale revisions and anonymous requests cannot retrieve old pictures. Upload/removal conflicts return `avatar_changed`; do not automatically retry mutations. Client preview displays only the processed result and revokes its memory-only Blob URL on replacement or privacy invalidation.

Publication uses fresh opaque filenames, atomic rename and revision-checked database references. Replacements/removal/account deletion revoke access at commit, then attempt physical unlink. Ambiguous commits retain candidates for safe collection rather than risking an active image. Startup/minute collection examines at most 1000 entries and deletes at most 100 per sweep with a five-second I/O/DB budget and one-hour age grace, skipping active writes. Failed unlink, downtime and backlogs can delay physical deletion; monitor aggregate cleanup errors and disk usage. No image history is intentionally retained, but backups have their separate retention policy.

| Data | Implemented application bound |
| --- | --- |
| Incomplete accounts | Inaccessible at seven days, purged on next healthy bounded sweep. |
| Session/action/OAuth authority | Exact protocol deadlines; expired records cleaned by bounded startup/hourly worker. |
| Outbox encrypted payload | Until terminal disposition or expiry, at most 24-hour job lifetime, shorter token lifetime where applicable. |
| Terminal delivery counters | At most seven days before cleanup; no encrypted payload. |
| Local suppression HMAC/reason | 180 days after latest processed feedback; personal/security data, not anonymized. |
| Quota HMAC/window counts | At most 48-hour schema bound; expired windows cleaned. |
| Deleted username | Permanent username-only reservation to prevent impersonation. |
| Processed avatar | Until replacement, removal or account deletion; access revoked at commit, physical unlink attempted afterward. Failed unlinks/crash leftovers use bounded collection with one-hour grace; downtime/backlog and backups can extend physical retention. |

Downtime/backlog delays physical deletion, not authorization expiry. Provider/backup retention and restore procedures are not implemented or certified by application code. Owner must approve legal basis, processor/transfer details, permanent-name reservation purpose, essential-cookie notice, suppression removal policy, security contact and actual backup retention before registration opens. Privacy page describes disabled capability; it is not proof of legal approval or deployment.

## Validation

Normal regression: `make check`. Account integration requires approved disposable local PostgreSQL and `TEST_DATABASE_URL`; tests use isolated schemas, never production/public resets. From `api/`:

```sh
go test ./internal/accounts ./internal/accountmail ./internal/database ./internal/httpapi ./cmd/api -run 'Integration$' -count=1
go test -race ./internal/accounts ./internal/accountmail ./internal/httpapi
```

Ordinary suites do not call real Google/SES or cinema providers. OIDC/JWKS and SDK fakes cover protocol/error cases. Integration skips without its prerequisite are not passes.

Dependency-free Chrome acceptance uses opt-in test-only Go fixture, never production fake-provider flags. Fixture requires dedicated local test database, binds API `127.0.0.1:18089` and creates/drops isolated schema. Set `TEST_DATABASE_URL` to approved disposable fixture database before launch; exact fixture guards are in `accounts_browser_harness_test.go`.

```sh
# From api/, separate terminal:
ACCOUNT_BROWSER_HARNESS=1 go test ./internal/httpapi -run '^TestAccountBrowserHarness$' -count=1 -timeout=0 -v
# From repository root, separate terminal:
npm --prefix web run test:accounts:server
# Driver after both servers ready:
npm --prefix web run test:accounts:browser
```

Gracefully stop fixture with POST `/api/__browser/shutdown` on loopback API carrying `X-Browser-Harness: 1`, wait for schema cleanup, then restart fixture before `npm --prefix web run test:accounts:browser -- --google` to avoid production rate-limit exhaustion. Synthetic mailbox links/cookies stay in test memory; never persist raw traces. Google scenario bypasses production Google-button allowlist only in driver navigation to local test adapter. It is simulated-provider evidence, not real Google/SES acceptance. Browser tests cover development proxy, not production HTTPS or installed service worker.

DB-free SPA/privacy acceptance uses actual Nuxt pages and a pinned upstream Umami fixture against intercepted collectors. Start `npm --prefix web run test:accounts:server`, then run `npm --prefix web run test:accounts:browser -- --spa`. Restart the server with `-- --no-analytics`, then run the browser command with `-- --spa --no-analytics`. These modes own the mock API on port 18089 and must not run concurrently with the Go fixture. They verify document/header reuse, theater initialization counts, token handling, public-only analytics, lifecycle races and private response headers. Mock API behavior is frontend contract evidence, not backend or real-provider verification. Existing DB-free `--auth-focus` and `--overview --visual` modes cover focus and layout regressions; synthetic lifecycle events do not prove native BFCache or installed-PWA behavior.

## Rollout and rollback checklist

- [ ] Final aggregate, disposable-database integration/race and coordinator code/security gates pass; unresolved acceptance checks documented.
- [ ] Google production client/consent and exact callback tested in operator staging.
- [ ] SES/DNS/access/quota, SNS/SQS/DLQ, IAM audit and operational alerts verified with authorized staging delivery/feedback.
- [ ] Actual HTTPS, canonical host, immediate proxy peer, forwarding and callback log redaction verified. Public/admin/internal paths still work.
- [ ] Production Secure cookie, installed-PWA offline behavior, native focus/BFCache, accessibility and remaining browser matrix verified.
- [ ] Privacy text/retention/processors and backup deletion/restore controls approved by owner.
- [ ] Compatible build deployed with accounts disabled; only then separately authorize enablement.

API startup applies migrations even when feature disabled. After installing through 046, do not roll back to a binary embedding an earlier migration history. Use a compatible corrective build retaining migration history. Pending email registrations created before browser binding must restart registration to obtain a new bound attempt. Emergency disable must keep public/admin app available; revoke account sessions/tokens/flows before re-enabling. Never drop username reservations, rewrite migration ledger or restore old queued tokens. Deletion fences invalidate outstanding anonymous Google flows globally because their subjects are unknown before exchange; other in-flight logins may need restart.

Restoring an older backup can resurrect deleted accounts even after session clearing. Keep registration disabled unless operator-approved controls can replay intervening deletions and permanent reservations. No speculative identity audit/restore ledger was added. Deployment and recovery remain separate authorized operations.
