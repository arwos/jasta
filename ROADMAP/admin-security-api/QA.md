# QA F04 — Владелец, API и прогресс

F04-T01/T02 реализуют owner CLI/auth primitives; F04-T03 подключает защищённые JSON-RPC команды и registry CRUD; F04-T04/T05 реализуют durable progress WebSocket и multipart upload. Сертификатные действия остаются в F05.

## Сценарии

- [x] Bootstrap проходит один раз; второй вызов не заменяет владельца; неверный TOTP не создаёт владельца.
- [x] TOTP-секрет сохраняется зашифрованным; неверный пароль/второй фактор не создаёт сессию; recovery code хранится только хэшем и работает один раз.
- [x] Reset удаляет владельца, recovery codes и связанные refresh-сессии.
- [x] Access JWT имеет lifetime 15 минут, отклоняет изменённую подпись/истечение/чужой ключ и остаётся проверяемым после пересоздания manager с тем же ключом.
- [x] Refresh-сессия переживает пересоздание manager; rotation сохраняет абсолютные 7 дней, replay/expiry/revoke/logout отклоняются.
- [ ] CLI key-file permissions и интерактивный ввод требуют ручной проверки на установленном Debian/Ubuntu сервисе.
- [x] Logout немедленно прекращает refresh. Выданный access JWT остаётся валиден до 15 минут, пока endpoint не проверяет session ID/revoke в SQLite.
- [x] JSON-RPC protected handler отклоняет запрос без bearer token и после logout; session rotation/logout сразу блокируют прежний JWT через проверку SQLite.
- [x] API contracts разделяют публичные auth.login/auth.refresh и protected owner методы; URL источников, секреты, certificate paths и private keys отсутствуют в read responses.
- [x] Registry create/update/delete проверяют URL/auth credentials; query и Basic/Bearer поля сохраняются только как AES-GCM ciphertext. Внутренний fetch accessor восстанавливает значения, API list их не показывает.
- [ ] Полный HTTP JSON-RPC transport: валидные/невалидные payload, неизвестная команда и запуск admin listener на Debian сервисе требуют ручного integration прогона.
- [x] Multipart upload создаёт готовую версию с отдельным config snapshot; activation/reload остаются отдельным действием.
- [ ] Certificate issuance остаётся в F05.
- [x] WebSocket integration: неверный JWT отклонён; активная сессия получает queued/download/build/publish/succeeded; reconnect получает durable replay; независимая login session не видит replay/live; logout запрещает повторную WS авторизацию.
- [x] Origin unit cases: same-origin forwarded HTTPS разрешён; host/scheme mismatch, missing Origin и неоднозначный forwarded proto отклонены.
- [x] Job manager unit cases: sequence, replay, terminal transition, live subscriber, session-family access и foreign-session isolation.
- [x] Upgrade старой SQLite схемы с существующей job строкой добавляет owner session и `job_events`, сохраняя существующую запись.
- F04-T04 тестирует синтетические job события; реальные стадии archive deploy входят в F04-T05, сертификатные операции — в F05.
- [x] Multipart upload: активная сессия создаёт готовый unactivated release с config snapshot и job; WebSocket job завершается succeeded.
- [x] Multipart upload отклоняет неверный JWT и отозванную сессию.
- [x] Multipart upload отклоняет повреждённое тело, архив больше лимита и повреждённый ZIP; невалидный архив не создаёт release.
- Multipart client disconnect during body transfer requires runtime verification through the installed admin service.
- В логах отсутствуют пароль, TOTP, recovery codes, JWT, registry credentials и подписанный URL.

## Запуски

- `go test ./...` — PASS (2026-11-20).
- `go test ./internal/auth/session ./internal/auth/owner` — PASS (2026-11-20).
- `go test -race ./internal/auth/session ./internal/auth/owner` — PASS (2026-11-20).
- `go build -o /tmp/jasta-f04-t02 ./cmd/jasta` — PASS (2026-11-20).
- `/tmp/jasta-f04-t02 admin --help` — shows `owner-bootstrap` and `owner-reset` (2026-11-20).
- `golangci-lint run --new ./internal/auth/owner ./internal/auth/session` — 0 new issues (2026-11-20).
- `make lint` — FAIL: четыре ранее существовавших замечания в `internal/api/admin/reload.go` и `internal/snapshot/socket.go`; новая auth/session реализация ошибок lint не добавила.
- `go test ./...` — PASS (2026-11-20); включает `TestProtectedJSONRPCRequiresActiveOwnerSession` и проверку отзыва старой access session при refresh/logout.
- `go build -o /tmp/jasta-f04-t03 ./cmd/jasta` — PASS, exit 0; Go сообщил, что не может обновить stat cache в read-only module cache (2026-11-20).
- `golangci-lint run --new ./internal/api/admin ./internal/api ./internal/auth/session` (`GOLANGCI_LINT_CACHE=/tmp/jasta-lint-cache`) — 0 issues (2026-11-20).
- `git diff --check` — PASS (2026-11-20).
- `GOCACHE=/tmp/jasta-go-cache go test -count=1 ./internal/api/admin ./internal/deployment/release ./internal/pkg/archive` — PASS; successful upload release/job, invalid archive without release, archive size limit, malformed multipart and revoked-session rejection (2026-11-20).
- `GOCACHE=/tmp/jasta-go-cache go test -count=1 ./...` — PASS (2026-11-20).
- `GOCACHE=/tmp/jasta-go-cache go test -race -count=1 ./internal/api/admin ./internal/jobs ./internal/deployment/release ./internal/pkg/archive` — PASS (2026-11-20).
- `GOCACHE=/tmp/jasta-go-cache go vet ./internal/api/admin ./internal/jobs ./internal/deployment/release ./internal/pkg/archive` — PASS (2026-11-20).
- `GOLANGCI_LINT_CACHE=/tmp/jasta-lint-cache golangci-lint run --new ./internal/api/admin ./internal/jobs ./internal/deployment/release ./internal/pkg/archive` — 0 new issues after checking deferred multipart file close (2026-11-20).
- `GOCACHE=/tmp/jasta-go-cache go build -o /tmp/jasta-f04-t05 ./cmd/jasta` — PASS, exit 0; GVM emitted a read-only stat-cache write warning (2026-11-20).
- `git diff --check` — PASS (2026-11-20).
- `go test ./internal/api/admin ./internal/pkg/archive` — PASS; registry source round-trip checked AES-GCM at rest, signed query removal from DB URL, list secrecy, update, and delete (2026-11-20).
- `go test -race ./internal/api/admin ./internal/pkg/archive ./internal/auth/session` — PASS (2026-11-20).
- `go vet ./internal/api/admin ./internal/pkg/archive ./internal/auth/session` — PASS (2026-11-20).
- `GOCACHE=/tmp/jasta-go-cache go test -count=1 ./...` — PASS (2026-11-20).
- `GOCACHE=/tmp/jasta-go-cache go test -count=1 ./internal/jobs ./internal/api/admin ./internal/storage ./internal/api ./internal/auth/session` — PASS (2026-11-20).
- `GOCACHE=/tmp/jasta-go-cache go test -race -count=1 ./internal/jobs ./internal/api/admin ./internal/storage ./internal/auth/session` — PASS (2026-11-20).
- `GOCACHE=/tmp/jasta-go-cache go test -count=1 -run '^TestProtectedJSONRPCRequiresActiveOwnerSession$' ./internal/api/admin` — PASS with loopback integration enabled (2026-11-20); the default sandbox run skips this test because it disallows binding a loopback listener.
- `GOCACHE=/tmp/jasta-go-cache go vet ./internal/jobs ./internal/api/admin ./internal/storage ./internal/api` — PASS (2026-11-20).
- `GOLANGCI_LINT_CACHE=/tmp/jasta-lint-cache golangci-lint run --new ./internal/jobs ./internal/api/admin ./internal/storage ./internal/api` — 0 new issues (2026-11-20).
- `GOCACHE=/tmp/jasta-go-cache go build -o /tmp/jasta-f04-t04 ./cmd/jasta` — PASS, exit 0; GVM emitted a read-only stat-cache write warning (2026-11-20).
- `git diff --check` — PASS (2026-11-20).
