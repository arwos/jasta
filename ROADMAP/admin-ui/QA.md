# QA F06 — React-админка

F06-T01 build/lint/embed serving and Go regressions are checked. F06-T02 login has browser rendering coverage; real backend session rotation remains deferred. F06-T03 has mocked-JSON-RPC browser coverage and API-level Go tests.

## Сценарии

- [x] `pnpm install --frozen-lockfile`, lint and production build.
- [x] Embedded frontend returns index on admin host and SPA routes; rejects other hosts and does not shadow API paths.
- [x] Desktop and 390px viewport render the owner login without horizontal overflow; password/TOTP inputs have labels and visible-focus styles.
- Verify successful/failed password+TOTP login, access expiry refresh/rotation, refresh expiry, and logout revocation against a running admin service.
- [x] Source/build review confirms access and refresh tokens exist only in the `AdminApi` instance, with no browser storage or console logging. Access refreshes proactively and after 401; logout calls backend revocation.
- Owner bootstrap/recovery codes remain one-time CLI output; UI does not receive or re-display them. Local CLI reset is the recovery path.
- [x] Create all four application types; config update saves a draft revision, rejects domain conflicts without partial save or activation, and config read removes absolute release paths.
- [x] Domain/TLS table renders registered site domains and certificate issuer/status/expiry; missing certificates have an explicit state.
- [x] Playwright with mocked JSON-RPC covers login, server validation error display, config draft save, create form selection for all four app types, redirect creation, TLS table, and 390px/1440px widths without overflow or console errors.
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
