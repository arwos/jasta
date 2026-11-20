# QA F04 — Владелец, API и прогресс

F04-T01 и F04-T02 реализованы как CLI и auth primitives. F04-T03 подключает JSON-RPC admin-команды и CRUD registry sources. Операции deploy через multipart и управление сертификатами ещё не реализованы; WebSocket и multipart относятся к F04-T04/T05, сертификатные actions — к F05.

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
- [ ] Config deployment через multipart и certificate issuance остаются за границей F04-T03 и проверяются в F04-T05/F05.
- WebSocket: разрешённая сессия, неверный Origin, истёкшая сессия, reconnect, job success/failure/cancel.
- Multipart: файл в пределах лимита, превышение, повреждённый multipart body, отключение клиента посреди загрузки.
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
- `go test ./internal/api/admin ./internal/pkg/archive` — PASS; registry source round-trip checked AES-GCM at rest, signed query removal from DB URL, list secrecy, update, and delete (2026-11-20).
- `go test -race ./internal/api/admin ./internal/pkg/archive ./internal/auth/session` — PASS (2026-11-20).
- `go vet ./internal/api/admin ./internal/pkg/archive ./internal/auth/session` — PASS (2026-11-20).
