# QA F07 — Поставка и восстановление

F07-T01 добавил отдельные web/admin systemd units, sysusers/tmpfiles, выделенную Nginx include directory и одноточечное sudoers правило. Реальная установка units, service restart, DAC и Nginx reload не выполнялись в этом окружении.

## Сценарии

- [x] Собрать `.deb` amd64 с CGO и проверить `dpkg-deb --info`/`--contents`: указаны amd64, `systemd`, `ca-certificates`, `nginx`, `libc6`; архив содержит бинарник, оба config-файла, два systemd units, sysusers/tmpfiles, sudoers и Nginx shim с безопасными режимами файлов.
- [ ] Собрать и проверить `.deb` arm64 на нативном arm64 builder либо с `aarch64-linux-gnu-gcc`; текущая среда amd64 не имеет этого cross compiler.
- Для package builder нужны Go/Goppy, Make, Node.js/pnpm и CGO C toolchain целевой архитектуры; pnpm dependencies ставятся из lockfile перед `make deb`.
- Проверить установку/upgrade/remove на чистых Debian/Ubuntu amd64 и arm64; upgrade сохраняет конфиги, SQLite, сертификаты и все releases.
- [x] Systemd parser принял unit directives временных копий; ExecStart был заменён на `/usr/bin/true`, а связи между двумя units пропущены, поскольку реальный `/usr/bin/jasta` не установлен в test host.
- [x] `systemd-sysusers --dry-run` разобрал users/groups; временный sandbox подтвердил memberships admin→content/challenge и Nginx worker→challenge.
- [x] `systemd-tmpfiles --dry-run` разобрал каталоги и режимы; применение владельцев в fake root блокируется uid mapping окружения.
- [x] `visudo -cf` разобрал точное reload-only sudoers правило; команда вывела предупреждение о владельце системного `/etc/sudo.conf` в этом контейнере.
- Проверить live, что оба systemd units стартуют от разных ограниченных users и слушают только localhost.
- Проверить runtime permissions на DB, releases, staging, master key, JWT keys, ACME key, TLS certs и Nginx include.
- Upgrade существующей установки сохраняет SQLite, sites, config snapshots, сертификаты и все releases.
- Remove пакет сохраняет пользовательские данные; действия по удалению данных требуют отдельной явной операции.
- Backup/restore в чистый staging host: восстановить SQLite и файлы/ключи, запустить оба сервиса, проверить сайт, rollback, owner login и расшифровку registry secret.
- [x] Добавить пошаговый backup/restore с остановкой сервисов, SQLite sidecars, releases/config snapshots, master/JWT key material, ACME/TLS keys, checksum, owner/group remap и post-restore checks.
- Проверить сборку amd64/arm64 с CGO toolchain и frontend assets; не считать текущий amd64-only `make build` доказательством обоих пакетов.

## Запуски F07-T04 — Alpine и Compose

- `sh -n deploy/entrypoint.sh` — PASS.
- `git diff --check` — PASS.
- `python3 -c 'import yaml; print(yaml.safe_load(open("deploy/compose.yaml")))'` — PASS: YAML разобран; проверка не подтверждает Compose schema.
- `python3` с `yaml.safe_load` для `deploy/compose.yaml`, `deploy/config.yaml`, `deploy/admin.yaml`, `deploy/websites/localhost.yaml` — PASS: все YAML разобраны.
- `docker compose -f deploy/compose.yaml config` — BLOCKED: команда завершилась `docker: command not found`.
- `podman-compose -f deploy/compose.yaml config` — BLOCKED: wrapper сообщает, что Podman не установлен; установленный `/usr/bin/podman` не может инициализировать runtime, потому что `chmod /run/user/1000/libpod` блокируется read-only filesystem.
- Docker CLI отсутствует. Container build, Compose startup, live HTTP/UI, key persistence и reload socket acceptance не запускались.
- `make -C deploy help` — PASS.
- `make -C deploy check-vars SVC_USER=jasta SVC_HOST=server.example.com SVC_PATH=/opt/jasta` — PASS.
- `SVC_USER=jasta SVC_HOST=server.example.com SVC_PATH=/opt/jasta make -C deploy check-vars` — PASS: параметры приняты из окружения.
- `make -C deploy check-vars SVC_USER=-option SVC_HOST=server.example.com SVC_PATH=/opt/jasta` — PASS: SSH-пользователь с начальным дефисом отклонён с exit code 2.
- `SVC_USER=jasta SVC_HOST=server.example.com SVC_PATH=/opt/jasta make -C deploy -n deploy` — PASS: dry-run shows build, package, SSH transfer, checksum validation, Podman load and Compose launch in order; no Podman/SSH actions were executed.
- `SVC_USER=jasta SVC_HOST=server.example.com SVC_PATH=/opt/jasta make -C deploy -n install` — PASS: dry-run показывает build, передачу, `podman load` и `podman compose up`; SSH/Podman фактически не запускались.
- `SVC_USER=jasta SVC_HOST=server.example.com SVC_PATH=/opt/jasta make -C deploy -n update` — PASS: dry-run показывает build, передачу, `podman load` и `podman compose up`; SSH/Podman фактически не запускались.
- `make -C deploy check-vars DEPLOY_MODE=invalid SVC_USER=jasta SVC_HOST=server.example.com SVC_PATH=/opt/jasta` — PASS: неизвестный режим отклонён с exit code 2.
- Пользователь сообщил о неудачном запуске `make install`: `podman save` отказался перезаписать непустой `jasta-image.tar` в формате `docker-archive`; исправление — сохранять во временный уникальный файл и заменять архив только после успеха.
- `make -C deploy -n package` — PASS: dry-run сохраняет образ во временный файл и заменяет архив после успешного `podman save`; Podman не запускался.
- Пользователь сообщил о неудачном запуске `podman compose -f compose.yaml up -d --no-build --pull never` с `podman-compose==1.3.0`: provider трактовал `never` как сервис (`KeyError`). Аргумент удалён из команды; оба сервиса используют `pull_policy: never`, а перед Compose Makefile проверяет загруженный image.
- `podman-compose up --help` — PASS: установленный provider показывает `--pull` как флаг без аргумента и `--pull-always` отдельно.
- `SVC_USER=jasta SVC_HOST=server.example.com SVC_PATH=/opt/jasta make -C deploy -n deploy` — PASS: archive содержит только Compose/config/websites под корнем; удалённый сценарий переносит legacy state, распаковывает файлы прямо в `SVC_PATH` и запускает Compose без `--pull never`; SSH/Podman фактически не выполнялись.
- `sh -n /tmp/jasta-remote-deploy.sh` — PASS: синтаксис удалённого сценария из dry-run разобран shell.
- `tar -czf /tmp/jasta-flat-deploy.tar.gz -C deploy compose.yaml config.yaml admin.yaml websites` и `tar -tzf /tmp/jasta-flat-deploy.tar.gz` — PASS: список архива начинается с `compose.yaml`, `config.yaml`, `admin.yaml`, `websites/`; верхнего каталога `deploy/` и state-каталогов нет.
- `python3 -c 'import yaml; print(yaml.safe_load(open("deploy/compose.yaml")))'` — PASS: обновлённый Compose YAML разобран; provider schema/runtime не проверялись.
- `make -C deploy check-vars SVC_USER=jasta SVC_HOST=server.example.com SVC_PATH='/opt/jasta;touch /tmp/nope'` — PASS: rejected unsafe path with exit code 2 before any transfer.
- `podman-compose exec --help` — PASS: installed provider supports selecting the exec user and allocates a pseudo-TTY by default.
- `make -C deploy -n owner-bootstrap SVC_USER=jasta SVC_HOST=server.example.com SVC_PATH=/opt/jasta` — PASS: dry-run includes forced SSH TTY and runs `owner-bootstrap` in the Compose `admin` service as `jasta-admin` with the mounted database/key paths; no remote command was executed.
- `git diff --check` — PASS.
- Actual `make -C deploy deploy` was not run: this environment's Podman runtime cannot initialize because its runtime directory is read-only. Image save/load and remote deployment remain unverified.
- F07-T04 остаётся частично проверенной до запуска Compose schema/build/runtime checks на рабочем Docker или Podman host.

## Запуски

- `GOCACHE=/tmp/jasta-go-cache go test -count=1 ./...` — PASS.
- `GOCACHE=/tmp/jasta-go-cache go test -race -count=1 ./internal/api/admin ./internal/api/public ./internal/deployment/activation ./internal/deployment/release ./internal/pkg/nginx ./internal/snapshot` — PASS.
- `GOCACHE=/tmp/jasta-go-cache go vet ./...` — PASS.
- `GOLANGCI_LINT_CACHE=/tmp/jasta-lint-cache golangci-lint run --new ./...` — PASS, 0 issues.
- `GOCACHE=/tmp/jasta-go-cache go build -o /tmp/jasta-f07-t01 ./cmd/jasta` — PASS; GVM сообщил о недоступном для записи stat cache, на результат сборки это не повлияло.
- `git diff --check` — PASS.
- `bash -n scripts/jasta_preinst.sh scripts/jasta_postinst.sh scripts/jasta_prerm.sh scripts/jasta_postrm.sh scripts/build-deb.sh` — PASS.
- `cd ui/admin && pnpm install --frozen-lockfile --store-dir=/tmp/jasta-pnpm-store` и `make build-admin` — PASS; package build требует предварительно установленные lockfile-зависимости, затем пересобирает Vite UI.
- `scripts/build-deb.sh amd64` — PASS: собирает UI, затем `jasta` с `CGO_ENABLED=1`.
- `scripts/build-deb.sh arm64` — корректно остановилась до Go сборки с сообщением о недостающем `aarch64-linux-gnu-gcc`; прямой вызов goppy без cross compiler ошибочно передавал ARM assembly системному x86 gcc.
- `deb-builder build` с временной amd64-only копией `.deb.yaml` — PASS; `dpkg-deb --info`, `--field`, `--contents` и control archive подтвердили архитектуру, зависимости, maintainer scripts, список conffiles, пути и modes. Реальная установка/удаление не выполнялись.
- `systemd-sysusers --dry-run init/jasta.sysusers` и `systemd-tmpfiles --create --dry-run --root=... init/jasta.tmpfiles` — PASS, включая создаваемый Nginx managed include.
- Fake ACME permission test под `UMask=0077` подтвердил режимы traversal для HTTP-01 пути; Nginx group membership на реальном хосте не проверялась.
- `systemd-analyze verify` на временных копиях двух units — PASS; в копиях `ExecStart` заменён на `/usr/bin/true`, а межсервисные зависимости убраны. Установленные units и реальный запуск сервисов не проверялись.
- `systemd-sysusers --dry-run init/jasta.sysusers` и `systemd-tmpfiles --create --dry-run --root=... init/jasta.tmpfiles` разобрали декларации. Fake-root sysusers создал ожидаемые memberships. Применение tmpfiles-владельцев к fake root упёрлось в ограничения uid mapping.
- `visudo -cf init/jasta-admin.sudoers` — правило разобрано; команда также вывела предупреждение, что `/etc/sudo.conf` в test host принадлежит uid 65534.
- Проверка примеров backup/restore на чистом staging host не выполнялась; документация не является свидетельством live restore.
- Backup/restore bash snippets прошли `bash -n`; tar create/list/extract с fixture деревом сохранили тестовый release файл. Это проверка формата/путей, не сервисный restore.
