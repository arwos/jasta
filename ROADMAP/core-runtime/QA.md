# QA F01 — Runtime и управляющее хранилище

F01-T01–F01-T04 выполнены. Полный тестовый набор и lint не запускались.

## Сценарии

- Запускать web и admin отдельно; проверять, что каждый слушает только назначенный localhost listener.
- Проверять миграцию пустой SQLite и повторный запуск миграций.
- Проверять, что web отвечает при остановленной или недоступной SQLite.
- Отправлять валидный снимок, затем некорректный; web должен сохранить предыдущую активную маршрутизацию.
- Импортировать валидные и устаревшие YAML; сверять отчёт, отсутствие изменений исходных файлов и отсутствие дубликатов при повторном импорте.
- Проверять права процессов: admin имеет доступ к DB, web — нет; web читает только выбранные релизные файлы.

## Запуски

- `gofmt -w cmd/jasta/main.go internal/api/p.go` — успешно.
- `go build -o /tmp/jasta-f01-t01 ./cmd/jasta` — успешно; Go вывел предупреждение, что sandbox не разрешил создать stat cache под GVM cache.
- `/tmp/jasta-f01-t01 web --help` и `/tmp/jasta-f01-t01 admin --help` — оба вызова завершились с кодом 0.
- Runtime smoke с `config/config.dev.yaml` и `config/admin.dev.yaml`: `web` на `127.0.0.1:8080` вернул 200 для Host localhost; `admin` на `127.0.0.1:8090` вернул 404 (публичный handler не зарегистрирован). Первый запуск без разрешения завершился `operation not permitted`; повторный smoke с разрешённым localhost bind прошёл.
- `go build -o /tmp/jasta-f01-t02 ./cmd/jasta` — успешно; Go вывел предупреждение о read-only GVM stat cache.
- Runtime migration smoke: `jasta admin` дважды запущен с одной временной SQLite-базой. Оба запуска дошли до HTTP listener; первый создал `0001_initial.sql`, второй пропустил её как применённую. SQLite inspection подтвердил 12 доменных таблиц, `__migrations__` и одну запись миграции.
- `gofmt -w cmd/jasta/main.go internal/api/admin/reload.go internal/api/public/api.go internal/api/public/config.go internal/api/public/p.go internal/pkg/config/config.go internal/snapshot/snapshot.go internal/snapshot/socket.go` — успешно.
- `go build -o /tmp/jasta-f01-t03 ./cmd/jasta` — успешно; Go вывел предупреждение о read-only GVM stat cache.
- Runtime reload smoke: `jasta web` запущен на `127.0.0.1:8080`; `jasta admin reload` отправил снимок revision 1 через `/tmp/jasta-web-reload.sock`, команда подтвердила активацию, запрос с Host `localhost` вернул HTTP 200. Снимок revision 2 с неизвестным типом был отклонён с ошибкой, повторный запрос с Host `localhost` вернул HTTP 200. Проверка требовала разрешения на локальный listener и Unix-сокет; процесс остановлен.
- `gofmt -w cmd/jasta/main.go internal/api/admin/import_configs.go internal/siteimport/import.go internal/storage/plugin.go` и `go build -o /tmp/jasta-f01-t04 ./cmd/jasta` — успешно; Go вывел предупреждение о read-only GVM stat cache.
- CLI smoke в изолированной SQLite `/tmp/jasta-f01-t04-t2_vujc0/admin.sqlite`: `/tmp/jasta-f01-t04 admin import-configs --config=/tmp/jasta-f01-t04-t2_vujc0/admin.yaml --dir=/tmp/jasta-f01-t04-t2_vujc0/configs --database=/tmp/jasta-f01-t04-t2_vujc0/admin.sqlite` импортировал 3 файла (современные SPA/MPA и legacy SPA), сообщил о 2 пропусках (неполный SPA и неподдерживаемый MDA). Повторный вызов сообщил `imported=0 already-present=3 skipped=2`.
- SQLite inspection подтвердил ровно 3 строки в `sites`, 3 домена и 3 config snapshots; legacy YAML преобразован в `app_type=spa`. Исходные fixture YAML/контент не изменялись; команды читали их, а Git diff не включает исходные каталоги.
- Первый smoke-вызов через `DIResolver.Resolve` показал завершённый импорт, но не завершил CLI до timeout из-за ожидания Goppy ticker lifecycle. Команда переведена на отдельное прямое подключение Goppy ORM, явное применение общей миграции и закрытие ORM; последующие два вызова завершились с кодом 0.
