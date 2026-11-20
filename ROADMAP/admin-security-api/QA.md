# QA F04 — Владелец, API и прогресс

F04-T01 реализован. Проверки ниже выполнены для owner/auth package; сетевые API, JWT, WebSocket и multipart ещё не реализованы.

## Сценарии

- [x] Bootstrap проходит один раз; второй вызов не заменяет владельца; неверный TOTP не создаёт владельца.
- [x] TOTP-секрет сохраняется зашифрованным; проверка пароля работает; recovery code хранится только хэшем и потребляется один раз.
- [x] Reset удаляет владельца, recovery codes и связанные refresh-сессии.
- [ ] CLI key-file permissions и интерактивный ввод требуют ручной проверки на установленном Debian/Ubuntu сервисе.
- Access JWT истекает через 15 минут; restart сохраняет верификацию; refresh rotation, logout, отзыв и срок до 7 дней работают.
- JSON-RPC: валидные/невалидные payload, неизвестная команда, unauthenticated запрос к protected методу, ошибки валидации.
- WebSocket: разрешённая сессия, неверный Origin, истёкшая сессия, reconnect, job success/failure/cancel.
- Multipart: файл в пределах лимита, превышение, повреждённый multipart body, отключение клиента посреди загрузки.
- В логах отсутствуют пароль, TOTP, recovery codes, JWT, registry credentials и подписанный URL.

## Запуски

- `go test ./internal/auth/owner ./internal/api/admin ./cmd/jasta` — PASS (2026-11-20).
- `go test ./...` — PASS (2026-11-20).
- `go build -o /tmp/jasta-f04-t01 ./cmd/jasta` — PASS; `jasta admin --help` lists `owner-bootstrap` and `owner-reset` (2026-11-20).
- `golangci-lint run --new ./internal/auth/owner ./internal/api/admin` — 0 new issues (2026-11-20).
- `make lint` — FAIL: четыре ранее существовавших замечания в `internal/api/admin/reload.go` и `internal/snapshot/socket.go`; добавленный `owner.go` ошибок lint не оставляет.
