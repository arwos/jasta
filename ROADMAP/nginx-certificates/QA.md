# QA F05 — Nginx и сертификаты

F05-T01 реализован как renderer, защищённая JSON-RPC команда и transactional file apply. Self-signed/ACME TLS пока не реализованы.

## Сценарии

- [x] Renderer создаёт public server block per site и admin HTTP block; проверяет loopback targets, host syntax и domain collisions; HTTP-01 location идёт до SPA proxy/admin HTTPS redirect. Парные admin certificate/key paths включают TLS proxy block и WebSocket upgrade location для `/ws`.
- [x] Apply выполняет isolated candidate test, устанавливает include атомарно, запускает full `nginx -t`, затем `systemctl reload nginx`.
- [x] Ошибка isolated/full test не делает reload; full test/reload failure восстанавливает предыдущий include.
- [x] `nginx.apply` защищён owner session, не принимает путь/команду из RPC request и возвращает только публичные сведения.
- Unix-права admin service на include/challenge и systemd reload policy проверяются в F07-T01 на Debian/Ubuntu.
- Live проверки системным `nginx -t` и reload требуют установленного Nginx сервиса и выполняются на Debian/Ubuntu deployment.
- HTTP-01 challenge доступен на порту 80 под ожидаемым path; общий redirect и RPA не перехватывают challenge.
- Создать self-signed сертификат с SAN, проверить mismatched hostname и permission modes.
- Успешный ACME issuance, renewal до срока, сеть недоступна, CA rate limit/challenge error; существующий валидный сертификат продолжает обслуживаться при отказе.
- Проверить, что wildcard и DNS-01 настройки отклоняются явно.

## Запуски

- `GOCACHE=/tmp/jasta-go-cache go test -count=1 ./internal/pkg/nginx ./internal/api/admin ./cmd/jasta` — PASS (2026-11-20).
- `GOCACHE=/tmp/jasta-go-cache go test -count=1 ./...` — PASS (2026-11-20).
- `GOCACHE=/tmp/jasta-go-cache go test -race -count=1 ./internal/pkg/nginx ./internal/api/admin` — PASS (2026-11-20).
- `GOCACHE=/tmp/jasta-go-cache go vet ./internal/pkg/nginx ./internal/api/admin ./cmd/jasta` — PASS (2026-11-20).
- `GOLANGCI_LINT_CACHE=/tmp/jasta-lint-cache golangci-lint run --new ./internal/pkg/nginx ./internal/api/admin ./cmd/jasta` — 0 new issues (2026-11-20).
- `GOCACHE=/tmp/jasta-go-cache go build -o /tmp/jasta-f05-t01 ./cmd/jasta` — PASS, exit 0; GVM emitted a read-only stat-cache write warning (2026-11-20).
- `git diff --check` — PASS (2026-11-20).
- Live `nginx -t` and systemd reload were not run because the Nginx binary is not installed in this environment.
