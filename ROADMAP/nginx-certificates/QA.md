# QA F05 — Nginx и сертификаты

F05-T01 реализован как renderer, защищённая JSON-RPC команда и transactional file apply. F05-T02 добавляет self-signed TLS для локальных доменов/IP. F05-T03 добавляет HTTP-01 issuance/reissuance с асинхронными jobs; фактический CA и systemd/Nginx deployment в этом окружении не проверялись.

## Сценарии

- [x] Renderer создаёт public server block per site и admin HTTP block; проверяет loopback targets, host syntax и domain collisions; HTTP-01 location идёт до SPA proxy/admin HTTPS redirect. Парные admin certificate/key paths включают TLS proxy block и WebSocket upgrade location для `/ws`.
- [x] Apply выполняет isolated candidate test, устанавливает include атомарно, запускает full `nginx -t`, затем `systemctl reload nginx`.
- [x] Ошибка isolated/full test не делает reload; full test/reload failure восстанавливает предыдущий include.
- [x] `nginx.apply` защищён owner session, не принимает путь/команду из RPC request и возвращает только публичные сведения.
- Unix-права admin service на include/challenge и systemd reload policy проверяются в F07-T01 на Debian/Ubuntu.
- Live проверки системным `nginx -t` и reload требуют установленного Nginx сервиса и выполняются на Debian/Ubuntu deployment.
- HTTP-01 challenge доступен на порту 80 под ожидаемым path; общий redirect и RPA не перехватывают challenge.
- [x] Создать self-signed сертификат с SAN, проверить hostname, подпись, non-CA свойства и permission modes; проверить отдельный каталог и запись metadata.
- [x] Отклонить публичные домены/IP и wildcard; повторный выпуск домена не оставляет orphan files.
- [x] Проверить, что сертификат admin-домена включается в следующую генерируемую Nginx конфигурацию, а protected RPC не раскрывает пути.
- Unix-группа admin/Nginx для доступа к key mode 0640 проверяется вместе с установкой в F07-T01.
- [x] Fake ACME flow проверяет публикацию ответа в точном URL path Nginx root, HTTP-01 validation, leaf SAN/public key/validity и удаление временного challenge.
- [x] Fake ACME flow с `UMask=0077` проверяет, что challenge-каталоги сохраняют group traversal, а token-файл доступен Nginx worker.
- [x] Let’s Encrypt rejects wildcard, local domains/IPs; terms acceptance is required; renewed metadata can restore the previous row.
- [x] Certified public domains get HTTPS server blocks and HTTP→HTTPS redirects while keeping the HTTP-01 location ahead of redirect.
- Verify actual issuance, staging/production CA interoperability, network failure, rate limit, reload failure and renewal on Debian/Ubuntu with reachable public DNS and port 80.
- Wildcard and DNS-01 are not supported; HTTP-01 only.

## Запуски

- `GOCACHE=/tmp/jasta-go-cache go test -count=1 ./internal/certificates` — PASS; fake ACME test использует `UMask=0077` как admin systemd unit.
- `GOCACHE=/tmp/jasta-go-cache go test -count=1 ./internal/pkg/nginx ./internal/api/admin ./cmd/jasta` — PASS (2026-11-20).
- `GOCACHE=/tmp/jasta-go-cache go test -count=1 ./...` — PASS (2026-11-20).
- `GOCACHE=/tmp/jasta-go-cache go test -race -count=1 ./internal/pkg/nginx ./internal/api/admin` — PASS (2026-11-20).
- `GOCACHE=/tmp/jasta-go-cache go vet ./internal/pkg/nginx ./internal/api/admin ./cmd/jasta` — PASS (2026-11-20).
- `GOLANGCI_LINT_CACHE=/tmp/jasta-lint-cache golangci-lint run --new ./internal/pkg/nginx ./internal/api/admin ./cmd/jasta` — 0 new issues (2026-11-20).
- `GOCACHE=/tmp/jasta-go-cache go build -o /tmp/jasta-f05-t01 ./cmd/jasta` — PASS, exit 0; GVM emitted a read-only stat-cache write warning (2026-11-20).
- `git diff --check` — PASS (2026-11-20).
- `GOCACHE=/tmp/jasta-go-cache go test -count=1 ./internal/certificates ./internal/api/admin ./internal/pkg/nginx` — PASS (2026-11-20, F05-T02).
- `GOCACHE=/tmp/jasta-go-cache go test -count=1 ./...` — PASS (2026-11-20, F05-T02).
- `GOCACHE=/tmp/jasta-go-cache go test -race -count=1 ./internal/certificates ./internal/api/admin ./internal/pkg/nginx` — PASS (2026-11-20, F05-T02).
- `GOCACHE=/tmp/jasta-go-cache go vet ./...` — PASS (2026-11-20, F05-T02).
- `GOLANGCI_LINT_CACHE=/tmp/jasta-lint-cache golangci-lint run --new ./...` — 0 new issues (2026-11-20, F05-T02).
- `GOCACHE=/tmp/jasta-go-cache go build -o /tmp/jasta-f05-t02 ./cmd/jasta` — PASS, exit 0; GVM emitted a read-only stat-cache write warning (2026-11-20).
- `git diff --check` — PASS (2026-11-20, F05-T02).
- `GOCACHE=/tmp/jasta-go-cache go test -count=1 ./internal/certificates ./internal/pkg/nginx ./internal/api/admin` — PASS (2026-11-20, F05-T03 fake ACME and renderer scenarios).
- `GOCACHE=/tmp/jasta-go-cache go test -count=1 ./...` — PASS (2026-11-20, F05-T03).
- `GOCACHE=/tmp/jasta-go-cache go test -race -count=1 ./internal/certificates ./internal/pkg/nginx ./internal/api/admin` — PASS (2026-11-20, F05-T03).
- `GOCACHE=/tmp/jasta-go-cache go vet ./...` — PASS (2026-11-20, F05-T03).
- `GOLANGCI_LINT_CACHE=/tmp/jasta-lint-cache golangci-lint run --new ./...` — 0 new issues (2026-11-20, F05-T03).
- `GOCACHE=/tmp/jasta-go-cache go build -o /tmp/jasta-f05-t03 ./cmd/jasta` — PASS, exit 0; GVM emitted a read-only stat-cache write warning (2026-11-20).
- `git diff --check` — PASS (2026-11-20, F05-T03).
- Live `nginx -t` and systemd reload were not run because the Nginx binary is not installed in this environment.
