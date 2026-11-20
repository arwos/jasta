# Контекст F07 — Поставка и восстановление

## Согласованные решения

- Цель поставки: Debian/Ubuntu `.deb`, amd64 и arm64; CGO разрешён.
- Сервисы раздельные: `jasta web` и `jasta admin`, каждому отдельная systemd unit и минимальные filesystem/network permissions.
- Архивные релизы хранятся без автоматической очистки.
- Backup/restore — документированные ручные согласованные операции, не встроенное расписание и не внешний storage integration.
- Восстанавливаемый набор включает SQLite, конфиги, releases, master encryption key, постоянные JWT signing keys, owner/TOTP recovery data, ACME account key и TLS certificate/private keys.

## Текущее состояние

- `.deb.yaml` уже перечисляет amd64 и arm64, зависит от systemd, ca-certificates и nginx, устанавливает один бинарник и `init/jasta.service`.
- Текущий service работает от `www-data`; целевые service users и directory boundaries пока не описаны.
- `Makefile` объявляет `make lint`, `make build` (amd64), `make tests`, `make pre-commit` и `make ci`; команды во время планирования не запускались.
- `make install` вызывает `go install .../goppy@latest` и `goppy setup-lib`, поэтому использует подвижный tool version и может обновлять локальную tooling state.

## Ограничения и источники

- [mattn/go-sqlite3](https://github.com/mattn/go-sqlite3): CGO_ENABLED=1 и C toolchain необходимы сборке SQLite connector.
- Текущие package lifecycle scripts лежат в `scripts/`; перед изменением проверить postinst/prerm поведение и не удалять данные при обновлении/удалении `.deb`.
- Не обещать, что текущий install/CI target проходит: он не выполнялся при подготовке roadmap.
