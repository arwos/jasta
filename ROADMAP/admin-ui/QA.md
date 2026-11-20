# QA F06 — React-админка

F06-T01 build, lint, embed serving, and Go regression scenarios are checked. Browser screenshots and interactive acceptance are deferred to later UI tasks.

## Сценарии

- [x] `pnpm install --frozen-lockfile`, lint and production build.
- [x] Embedded frontend returns index on admin host and SPA routes; rejects other hosts and does not shadow API paths.
- [x] Desktop and 390px viewport render the owner login without horizontal overflow; password/TOTP inputs have labels and visible-focus styles.
- Verify successful/failed password+TOTP login, access expiry refresh/rotation, refresh expiry, and logout revocation against a running admin service.
- [x] Source/build review confirms access and refresh tokens exist only in the `AdminApi` instance, with no browser storage or console logging. Access refreshes proactively and after 401; logout calls backend revocation.
- Owner bootstrap/recovery codes remain one-time CLI output; UI does not receive or re-display them. Local CLI reset is the recovery path.
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
