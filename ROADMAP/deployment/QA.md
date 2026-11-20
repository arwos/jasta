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

## F03-T02 — HTTPS-источники

Проверено unit-тестами с подставными DNS-ответами, dialer и HTTP transport:

- HTTPS only, неверные порты, URL userinfo/fragment, scoped IPv6 и нестандартные IPv4 формы отклоняются.
- Loopback/private/shared/link-local/multicast/documentation/reserved IPv4/IPv6 и IPv4-mapped private destinations отклоняются; внешние IPv4/IPv6 допускаются.
- При смешанном DNS-ответе public+private отклоняется весь хост. Симуляция DNS rebinding (публичный ответ при первичной проверке, loopback при DialContext) блокирует соединение до dial.
- HTTP Basic/Bearer headers формируются для исходного registry, query URL сохраняется в запросе; после cross-host redirect Authorization удаляется. Redirect с private DNS и local hostname блокируется.
- Тело читается потоком с лимитом; ошибки не содержат подписанный query. Source не сериализуется в JSON/YAML.
- Реальные внешние registry не опрашивались; end-to-end TLS, Basic auth и redirect прошли через локальный HTTPS server на `net.Pipe` с DNS/dialer подменами.

Команды и результаты:

- `go test -race ./internal/pkg/archive` — пройдено.
- `go test ./...` — пройдено.
- `go vet ./internal/pkg/archive` — пройдено.
- `GOLANGCI_LINT_CACHE=/tmp/jasta-golangci-cache golangci-lint run ./internal/pkg/archive` — 0 issues.
- `GOLANGCI_LINT_CACHE=/tmp/jasta-golangci-cache golangci-lint run ./...` — блокируют 4 существующих finding вне archive: unchecked Close в `internal/api/admin/reload.go` и `internal/snapshot/socket.go`, а также G302 для socket permissions в `internal/snapshot/socket.go`.

## Сценарии

- ZIP и TAR.GZ на 100 МБ и граничных значениях; распаковка ровно 1 ГБ и превышение; пустой и повреждённый архив.
- Zip-slip (`../`), абсолютный путь, escaping symlink/hardlink, слишком много записей, nested archive и компрессионная бомба.
- Fetch URL с Basic/Bearer/query signature; auth не попадает в обычные логи.
- SSRF: localhost, private IPv4/IPv6, link-local, reserved ranges, DNS rebinding и redirect в запрещённую сеть.
- Ошибка валидации/build не затрагивает активный релиз; атомарное переключение; rollback возвращает точный config snapshot.
- После публикации и rollback проверять, что web продолжает работать без рестарта, а старые релизы остаются на диске.

### F03-T03 — проверенный релиз

Проверено:

- SPA релиз сохраняет отдельные config/metadata snapshots, неизменяемый контент и не создаёт активацию.
- Ошибка валидации содержимого не оставляет release directory, snapshot или DB release/activation.
- Markdown релиз собирает HTML, удаляет source/templates из runtime-релиза и сохраняет готовые файлы.
- RPA создаёт config-only релиз с `source_kind=config`.
- Миграция старой SQLite-схемы сохраняет существующий релиз и активацию, принимает `config` и идемпотентна.

Команды и результаты:

- `go test ./...` — пройдено.
- `go vet ./...` — пройдено.
- `GOLANGCI_LINT_CACHE=/tmp/jasta-golangci-cache golangci-lint run ./internal/deployment/release ./internal/storage` — 0 issues.
- `GOLANGCI_LINT_CACHE=/tmp/jasta-golangci-cache golangci-lint run ./...` — остаются 4 ранее существовавших finding вне задачи: unchecked Close в `internal/api/admin/reload.go` и `internal/snapshot/socket.go`, плюс G302 для socket permissions в `internal/snapshot/socket.go`.

## Запуски

Сценарии атомарной активации, reload web и rollback ниже относятся к F03-T04—F03-T05.
