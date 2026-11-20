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
- Проверить сборку amd64/arm64 с CGO toolchain и frontend assets; не считать текущий amd64-only `make build` доказательством обоих пакетов.

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
- `systemd-analyze verify` на временных копиях двух units — PASS; в копиях `ExecStart` заменён на `/usr/bin/true`, а межсервисные зависимости убраны. Установленные units и реальный запуск сервисов не проверялись.
- `systemd-sysusers --dry-run init/jasta.sysusers` и `systemd-tmpfiles --create --dry-run --root=... init/jasta.tmpfiles` разобрали декларации. Fake-root sysusers создал ожидаемые memberships. Применение tmpfiles-владельцев к fake root упёрлось в ограничения uid mapping.
- `visudo -cf init/jasta-admin.sudoers` — правило разобрано; команда также вывела предупреждение, что `/etc/sudo.conf` в test host принадлежит uid 65534.
