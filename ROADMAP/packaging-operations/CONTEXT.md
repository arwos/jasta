# Контекст F07 — Поставка и восстановление

## Согласованные решения

- Цель поставки: Debian/Ubuntu `.deb`, amd64 и arm64; CGO разрешён.
- Сервисы раздельные: `jasta web` и `jasta admin`, каждому отдельная systemd unit и минимальные filesystem/network permissions.
- Архивные релизы хранятся без автоматической очистки.
- Backup/restore — документированные ручные согласованные операции, не встроенное расписание и не внешний storage integration.
- Восстанавливаемый набор включает SQLite, конфиги, releases, master encryption key, постоянные JWT signing keys, owner/TOTP recovery data, ACME account key и TLS certificate/private keys.

## Текущее состояние

- F07-T01 заменяет общий unit на `jasta-web.service` и `jasta-admin.service`; sysusers/tmpfiles задают раздельных пользователей, группы, каталоги и фиксированные права.
- `jasta-web` не имеет права записи в state; `ProtectSystem=strict`, Unix-права и `InaccessiblePaths` закрывают SQLite, master key, сертификаты и ACME account key. Доступ к content releases идёт через отдельную read-only группу `jasta-content`.
- Admin работает от `jasta-admin`, владеет SQLite, release tree и приватными файлами. Content tree меняет группу на `jasta-content` при публикации; сертификатные ключи остаются в группе `jasta-admin`.
- Web создаёт `/run/jasta/web-reload.sock` внутри runtime directory `0770`; socket mode `0660` и группа `jasta-content` позволяют admin отправлять активный snapshot. При рестарте web связанный admin перезапускается и повторно загружает из SQLite только активные пары; тот же snapshot revision принимается идемпотентно.
- Nginx включает root-owned shim из `/etc/nginx/conf.d/jasta.conf`, который подключает выделенный `/etc/jasta/nginx/jasta.conf`. Admin может менять только эту отдельную директорию. Sudoers разрешает `jasta-admin` только `/usr/bin/systemctl reload nginx`; `nginx -t` запускается без привилегий. В admin unit `NoNewPrivileges` выключен только для выполнения этой точечной sudoers-команды; `jasta-admin` не запускает сервис от root.
- `make install` вызывает `go install .../goppy@latest` и `goppy setup-lib`, поэтому использует подвижный tool version и может обновлять локальную tooling state; эта команда не входит в проверку F07-T01.
- `.deb.yaml` выпускает amd64 и arm64 из общего CGO-бинарника `jasta`; systemd разделяет режимы `jasta web` и `jasta admin`. `scripts/build-deb.sh` пересобирает UI через pnpm/Vite перед Go и выбирает native GCC либо заданный `JASTA_CC_AMD64`/`JASTA_CC_ARM64` cross compiler.
- `postinst` создаёт отдельные системные users/groups и каталоги, генерирует root-owned `/etc/jasta/master.key` только при его отсутствии, затем запускает web/admin и проверяет/restarts Nginx, чтобы уже запущенный master унаследовал новые supplementary groups для ACME challenge. Runtime-изменения Nginx по-прежнему применяются reload. Upgrade и обычный remove сохраняют SQLite, releases, certificates, конфиги и master key; явный `purge` удаляет `/var/lib/jasta` и master key.

## Ограничения и источники

- [mattn/go-sqlite3](https://github.com/mattn/go-sqlite3): CGO_ENABLED=1 и C toolchain необходимы сборке SQLite connector.
- Текущие package lifecycle scripts лежат в `scripts/`; перед изменением проверить postinst/prerm поведение и не удалять данные при обновлении/удалении `.deb`.
- Не обещать, что текущий install/CI target проходит: он не выполнялся при подготовке roadmap.
