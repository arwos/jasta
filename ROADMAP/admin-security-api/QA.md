# QA F04 — Владелец, API и прогресс

F04-T01 и F04-T02 реализованы как CLI и auth primitives. HTTP/JSON-RPC wiring, WebSocket и multipart ещё не реализованы.

## Сценарии

- [x] Bootstrap проходит один раз; второй вызов не заменяет владельца; неверный TOTP не создаёт владельца.
- [x] TOTP-секрет сохраняется зашифрованным; неверный пароль/второй фактор не создаёт сессию; recovery code хранится только хэшем и работает один раз.
- [x] Reset удаляет владельца, recovery codes и связанные refresh-сессии.
- [x] Access JWT имеет lifetime 15 минут, отклоняет изменённую подпись/истечение/чужой ключ и остаётся проверяемым после пересоздания manager с тем же ключом.
- [x] Refresh-сессия переживает пересоздание manager; rotation сохраняет абсолютные 7 дней, replay/expiry/revoke/logout отклоняются.
- [ ] CLI key-file permissions и интерактивный ввод требуют ручной проверки на установленном Debian/Ubuntu сервисе.
- [x] Logout немедленно прекращает refresh. Выданный access JWT остаётся валиден до 15 минут, пока endpoint не проверяет session ID/revoke в SQLite.
- JSON-RPC: валидные/невалидные payload, неизвестная команда, unauthenticated запрос к protected методу, ошибки валидации.
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
