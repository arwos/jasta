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
- Ручные процедуры согласованного backup/restore и remap permissions после миграции UID/GID описаны в `docs/backup-restore.md`; фактический staging restore в этом окружении не выполнялся.

## F07-T04 — Alpine и Compose

- Одна многоэтапная сборка компилирует UI и Go-бинарник с CGO на Alpine; Compose запускает `web` и `admin` отдельными сервисами из одного образа.
- В репозитории `deploy/data` и `deploy/runtime` — локальные пути разработки. На конечном сервере Compose-файлы лежат прямо в `SVC_PATH`; bind-mounted state — `SVC_PATH/data`, reload socket — `SVC_PATH/runtime`. Web получает state только для чтения; admin владеет SQLite, релизами и ключом.
- Master key хранится как root-owned `root:jasta-admin` с режимом `0640`: admin остаётся непривилегированным, web не состоит в группе `jasta-admin`, а текущий валидатор требует root ownership и разрешает `0600`/`0640`.
- Nginx остаётся на хосте и направляет запросы на loopback-порты 15432/15433. В контейнерной конфигурации `nginx.apply` отключён через `/bin/false`; host Nginx и ACME reload не управляются Compose.
- Admin UI compares the request `Host` with `admin.nginx.admin_domain` and returns 404 `file does not exist` on mismatch. Keep that hostname aligned with the `Host` forwarded by host Nginx.
- Контейнеры используют UID 10001 (`jasta-web`) и 10002 (`jasta-admin`); admin дополнительно состоит в `jasta-content`, чтобы подключаться к web reload socket.
- Compose перезапускает admin после явного Compose-managed restart/update web, чтобы восстановить active snapshot. Автоматический restart контейнера после неожиданного падения web не перезапускает зависимый admin; оператор перезапускает admin после восстановления web.
- `deploy/Makefile` builds `jasta:local` with `podman compose`, saves it in `docker-archive` format through a unique temporary file, and bundles only `compose.yaml`, `config.yaml`, `admin.yaml`, and `websites/` from `deploy/` into the archive root. It atomically replaces the saved image archive only after a successful save. `install` requires no existing Compose deployment; `update` requires one; `deploy` detects the mode. Each mode copies archives and SHA-256 checksums over SSH, loads and inspects the local image on the target, then runs Compose with `--no-build`. Both services use `pull_policy: never`; podman-compose 1.3.0 translates it to Podman's `--pull never` run option. The command-level `podman compose up --pull never` is unsupported because that provider treats `--pull` as a boolean option.
- The server has no `deploy/` directory: Compose/config files, persistent state, and runtime socket live under `SVC_PATH`. Updates preserve `SVC_PATH/data` and `SVC_PATH/runtime`. When upgrading from the earlier nested layout, the Makefile stops any old stack containers, moves legacy `data/` and `websites/` into the flat layout, removes the transient legacy runtime directory, and deletes the legacy deployment directory after flattening the updated files. It aborts on old/flat data or websites path collisions rather than merging user state.
- `SVC_USER`, `SVC_HOST`, and absolute normalized `SVC_PATH` identify the target SSH account and install directory. The remote account must own/be able to write that directory and run Podman; the Makefile checks target architecture against the loaded image before startup.
- `make -C deploy owner-bootstrap` requires the Compose stack to be running and an interactive local terminal. It forces an SSH TTY, then runs the command in the `admin` service as `jasta-admin`, explicitly pointing at `/var/lib/jasta/master.key` because the container stores the generated key under the data mount rather than the CLI's non-container default path.

## Ограничения и источники

- [mattn/go-sqlite3](https://github.com/mattn/go-sqlite3): CGO_ENABLED=1 и C toolchain необходимы сборке SQLite connector.
- Текущие package lifecycle scripts лежат в `scripts/`; перед изменением проверить postinst/prerm поведение и не удалять данные при обновлении/удалении `.deb`.
- Не обещать, что текущий install/CI target проходит: он не выполнялся при подготовке roadmap.
