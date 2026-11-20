# F03-T05 — Откат и история релизов

## Цель

Показывать все сохранённые релизы, текущую активную пару и занимаемое ими место; откатывать выбранный релиз вместе с его конфигурацией.

## Задачи

- [x] Добавить history API с current pair, config revision, source, status, timestamps и per-release/total disk usage → SQLite и файловые integration tests.
- [x] Добавить rollback API поверх общей атомарной activation операции → выбрать только ready release сайта, использовать его точный config snapshot.
- [x] Добавить `jasta admin history` и `jasta admin rollback` команды → проверить CLI help.
- [x] Проверить успешный rollback и отказ reload → прежняя пара остаётся активной, все каталоги релизов остаются на месте.
- [x] Обновить F03 roadmap, контекст и QA → зафиксировать команды и границы проверки.

## Готово, когда

- [x] Rollback меняет release и config snapshot вместе, без удаления старых версий.
- [x] History показывает активный pair, все версии и bytes фактически занятых файлов.
- [x] `go test ./...`, `go vet ./...` и focused lint проходят; full lint baseline указан.
