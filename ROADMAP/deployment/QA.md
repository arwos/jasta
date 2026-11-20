# QA F03 — Импорт архивов и релизы

## F03-T01 — архивы

Проверено:

- ZIP и TAR.GZ извлекают файлы и вложенные каталоги.
- Входной лимит, распакованный лимит и число записей отклоняются на малых тестовых значениях; failed extraction удаляет staging.
- ZIP-slip, абсолютный путь, escaping symlink/hardlink, дублирующий путь, повреждённые форматы отклоняются без записи за пределы staging.
- Внутренний symlink разрешается после извлечения его целевого файла.
- Малые тестовые лимиты проверяют те же потоковые ветки; физические архивы размером 100 MiB/1 GiB отдельно не создавались.

Команды и результаты:

- `go test ./internal/pkg/archive ./internal/api/admin` — пройдено.
- `go test ./...` — пройдено.
- `go vet ./internal/pkg/archive ./internal/api/admin` — пройдено.
- `GOLANGCI_LINT_CACHE=/tmp/jasta-golangci-cache golangci-lint run ./internal/pkg/archive` — 0 issues.
- `GOLANGCI_LINT_CACHE=/tmp/jasta-golangci-cache golangci-lint run ./...` — блокируют 4 существующих finding вне archive: unchecked Close в `internal/api/admin/reload.go` и `internal/snapshot/socket.go`, а также G302 для socket permissions в `internal/snapshot/socket.go`.

## Сценарии

- ZIP и TAR.GZ на 100 МБ и граничных значениях; распаковка ровно 1 ГБ и превышение; пустой и повреждённый архив.
- Zip-slip (`../`), абсолютный путь, escaping symlink/hardlink, слишком много записей, nested archive и компрессионная бомба.
- Fetch URL с Basic/Bearer/query signature; auth не попадает в обычные логи.
- SSRF: localhost, private IPv4/IPv6, link-local, reserved ranges, DNS rebinding и redirect в запрещённую сеть.
- Ошибка валидации/build не затрагивает активный релиз; атомарное переключение; rollback возвращает точный config snapshot.
- После публикации и rollback проверять, что web продолжает работать без рестарта, а старые релизы остаются на диске.

## Запуски

Остальные сценарии ниже относятся к ещё не реализованным задачам F03-T02—F03-T05.
