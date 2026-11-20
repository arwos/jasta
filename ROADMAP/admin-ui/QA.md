# QA F06 — React-админка

F06-T01 build/lint/embed serving and Go regressions are checked. F06-T02 login has browser rendering coverage; real backend session rotation remains deferred. F06-T03 has mocked-JSON-RPC browser coverage and API-level Go tests. F06-T04 has API-level registry/config-only release coverage and mocked-browser multipart upload/history/rollback coverage; a running admin with a real registry and live WebSocket remains unverified. F06-T05 has mocked browser coverage for source CRUD, secret omission/edit reset, self-signed and Let’s Encrypt actions, progress, and safe ACME failure display; real Nginx/ACME issuance is not exercised. F06-T06 implements cookie-backed session restore and passes frontend/build checks; end-to-end cookie flags, login/refresh rotation, reload restoration, and logout revocation remain unverified. F06-T07 central creation form layout passes frontend lint/build; live browser interaction was not repeated.

## Сценарии

- [x] `pnpm install --frozen-lockfile`, lint and production build.
- [x] Embedded frontend returns index on admin host and SPA routes; rejects other hosts and does not shadow API paths.
- [x] Desktop and 390px viewport render the owner login without horizontal overflow; password/TOTP inputs have labels and visible-focus styles.
- Verify successful/failed password+TOTP login, access expiry refresh/rotation, refresh expiry, and logout revocation against a running admin service.
- [x] F06-T02 source/build review confirmed the initial access/refresh tokens existed only in the `AdminApi` instance, with no browser storage or console logging.
- [ ] F06-T06 verify login/refresh set a host-only HttpOnly SameSite=Strict cookie with Path `/jsonrpc`, Secure on HTTPS, no-store responses, and no refresh token in JSON; verify reload restores, expiry/revocation returns to login, and logout clears/revokes the cookie session.
- Owner bootstrap/recovery codes remain one-time CLI output; UI does not receive or re-display them. Local CLI reset is the recovery path.
- [x] Create all four application types; config update saves a draft revision, rejects domain conflicts without partial save or activation, and config read removes absolute release paths.
- [x] Domain/TLS table renders registered site domains and certificate issuer/status/expiry; missing certificates have an explicit state.
- [x] Playwright with mocked JSON-RPC covers login, server validation error display, config draft save, create form selection for all four app types, redirect creation, TLS table, and 390px/1440px widths without overflow or console errors.
- [x] API test covers owner-protected registry release creation using an injected fetcher, configured archive limits, release history metadata, Redirect config-only release, and a stable source-fetch error without leaking details.
- [x] Playwright with mocked JSON-RPC/multipart covers archive upload, URL-source selection and request payload, success notice, refreshed release history, active release display, explicit rollback confirmation, and 390px/1440px document widths without horizontal overflow. In this run `/ws` was unavailable: the UI continued after its handshake timeout and displayed the progress-unavailable notice; the mocked run does not verify live progress/reconnect.
- [x] Playwright with mocked JSON-RPC covers source create/update/delete, source URL and Bearer secret absent from rendered UI, blank URL/secret fields when editing, and confirmation before delete.
- [x] Playwright with mocked WebSocket covers local self-signed issuance, public Let’s Encrypt job success and failure, certificate refresh, safe `acme_failed` guidance, and 390px layout without horizontal overflow or secret disclosure.
- Live browser flow against a running `jasta admin` with SQLite, owner bootstrap, and real authenticated JSON-RPC remains a deployment integration check.
- Responsive layout на узком и широком viewport; keyboard navigation, видимый focus, labels, ошибки форм.
- Login: пароль/TOTP, некорректный код, expired JWT, refresh success/failure, logout.
- Проверить экраны всех четырёх application types, server validation, неизвестный host и конфликт домена.
- Multipart upload с ошибкой лимита, URL source auth, real-time job progress/reconnect, публикация и rollback.
- Проверить, что secrets, token, recovery codes и signed query URL отсутствуют в DOM после завершения настройки и в клиентских логах.
- Проверить выдачу fallback SPA админки только на admin-домене; API пути не перехватываются client-side router.

## Запуски

- `pnpm install --frozen-lockfile --store-dir /tmp/jasta-pnpm-store` — PASS (2026-11-20; pnpm 12.18.0).
- `pnpm lint` — PASS, 0 warnings (2026-11-20; Oxlint 1.90.0).
- `pnpm build` — PASS; TypeScript check and Vite 8.3.8 build (2026-11-20).
- `GOCACHE=/tmp/jasta-go-cache go test -count=1 ./internal/api/admin ./ui/admin` — PASS (2026-11-20).
- `GOCACHE=/tmp/jasta-go-cache go test -count=1 ./...` — PASS (2026-11-20).
- `GOCACHE=/tmp/jasta-go-cache go test -race -count=1 ./internal/api/admin ./ui/admin` — PASS (2026-11-20).
- `GOCACHE=/tmp/jasta-go-cache go vet ./...` — PASS (2026-11-20).
- `GOLANGCI_LINT_CACHE=/tmp/jasta-lint-cache golangci-lint run --new ./...` — 0 new issues (2026-11-20).
- `GOCACHE=/tmp/jasta-go-cache go build -o /tmp/jasta-f06-t01 ./cmd/jasta` — PASS, exit 0; GVM emitted a read-only stat-cache warning (2026-11-20).
- `git diff --check` — PASS (2026-11-20).
- `pnpm lint` — PASS, 0 warnings (2026-11-20; F06-T02).
- `pnpm build` — PASS, TypeScript and Vite production build (2026-11-20; F06-T02).
- Playwright browser inspection at desktop and 390px viewport — page rendered without browser errors; mobile document width matched viewport (2026-11-20).
- `GOCACHE=/tmp/jasta-go-cache go test -count=1 ./...` — PASS (2026-11-20; F06-T02).
- `GOCACHE=/tmp/jasta-go-cache go vet ./...` — PASS (2026-11-20; F06-T02).
- `GOLANGCI_LINT_CACHE=/tmp/jasta-lint-cache golangci-lint run --new ./...` — 0 new issues (2026-11-20; F06-T02).
- `GOCACHE=/tmp/jasta-go-cache go build -o /tmp/jasta-f06-t02 ./cmd/jasta` — PASS, with a non-fatal GVM stat-cache warning because its cache is read-only (2026-11-20; F06-T02).
- `git diff --check` — PASS (2026-11-20; F06-T02).
- `GOCACHE=/tmp/jasta-go-cache go test -count=1 ./internal/api/admin` — PASS; covers protected site creation, redirect-domain de-duplication, draft config revision, domain normalization/conflict rollback, and no activation (2026-11-20; F06-T03).
- `pnpm lint` — PASS, 0 warnings (2026-11-20; F06-T03).
- `pnpm build` — PASS, TypeScript and Vite production build (2026-11-20; F06-T03).
- `GOCACHE=/tmp/jasta-go-cache go test -count=1 ./...` — PASS (2026-11-20; F06-T03).
- `GOCACHE=/tmp/jasta-go-cache go test -race -count=1 ./internal/api/admin ./ui/admin` — PASS (2026-11-20; F06-T03).
- `GOCACHE=/tmp/jasta-go-cache go vet ./...` — PASS (2026-11-20; F06-T03).
- `GOLANGCI_LINT_CACHE=/tmp/jasta-lint-cache golangci-lint run --new ./...` — 0 new issues (2026-11-20; F06-T03).
- `GOCACHE=/tmp/jasta-go-cache go build -o /tmp/jasta-f06-t03 ./cmd/jasta` — PASS, with a non-fatal GVM stat-cache warning because its cache is read-only (2026-11-20; F06-T03).
- Playwright mock-JSON-RPC scenario — PASS: validation error shown, draft save, all four type forms, redirect create, and TLS state at 390px and 1440px; no overflow or console errors (2026-11-20; F06-T03).
- `git diff --check` — PASS (2026-11-20; F06-T03).
- `GOCACHE=/tmp/jasta-go-cache go test -count=1 ./internal/api/admin` — PASS; includes registry archive source, limit propagation, Redirect config-only release, and safe fetch failure (2026-11-20; F06-T04).
- `pnpm lint` — PASS, 0 warnings (2026-11-20; F06-T04).
- `pnpm build` — PASS; TypeScript and Vite production build (2026-11-20; F06-T04).
- Playwright with mocked JSON-RPC and multipart — PASS for archive upload, URL-source request payload, history refresh, explicit rollback confirmation, and 390px/1440px widths; `/ws` was unavailable and the fallback state was displayed. Browser reported the expected dev-server `/ws` connection warning and an unrelated missing `/favicon.ico` request (2026-11-20; F06-T04).
- `pnpm lint` — PASS, 0 warnings (2026-11-20; F06-T05).
- `pnpm build` — PASS; TypeScript and Vite production build (2026-11-20; F06-T05).
- `GOCACHE=/tmp/jasta-go-cache go test -count=1 ./ui/admin` — PASS; embedded frontend package compiles with the new bundle (2026-11-20; F06-T05).
- `GOCACHE=/tmp/jasta-go-cache go build -o /tmp/jasta-f06-t05 ./cmd/jasta` — PASS; GVM printed a non-fatal warning because its stat cache is read-only (2026-11-20; F06-T05).
- Playwright with mocked JSON-RPC/WebSocket — PASS: source create/update/delete, secret not rendered and edit fields reset; self-signed issuance; Let’s Encrypt job completion and mapped `acme_failed` error; 390px document width equals viewport. No live Nginx or ACME CA was contacted (2026-11-20; F06-T05).
- `pnpm lint` — PASS, 0 warnings (2026-11-20; F06-T06).
- `pnpm build` — PASS; TypeScript and Vite production build (2026-11-20; F06-T06).
- `GOCACHE=/tmp/jasta-go-cache go build -mod=readonly -o /tmp/jasta-auth-cookie-check ./cmd/jasta` — PASS, exit 0; GVM printed a non-fatal warning because its stat cache is read-only (2026-11-20; F06-T06).
- `git diff --check` — PASS (2026-11-20; F06-T06).
- `pnpm lint` — PASS, 0 warnings (2026-11-20; F06-T07).
- `pnpm build` — PASS; TypeScript and Vite production build (2026-11-20; F06-T07).
- `git diff --check` — PASS (2026-11-20; F06-T07).
