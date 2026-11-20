# QA F07 — Поставка и восстановление

F07-T01 добавил отдельные web/admin systemd units, sysusers/tmpfiles, выделенную Nginx include directory и одноточечное sudoers правило. Реальная установка units, service restart, DAC и Nginx reload не выполнялись в этом окружении.

## Сценарии

- Устанавливать пакет на чистые Debian/Ubuntu amd64 и arm64; проверять `dpkg-deb --info` и содержимое архива.
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
- `systemd-analyze verify` на временных копиях двух units — PASS; в копиях `ExecStart` заменён на `/usr/bin/true`, а межсервисные зависимости убраны. Установленные units и реальный запуск сервисов не проверялись.
- `systemd-sysusers --dry-run init/jasta.sysusers` и `systemd-tmpfiles --create --dry-run --root=... init/jasta.tmpfiles` разобрали декларации. Fake-root sysusers создал ожидаемые memberships. Применение tmpfiles-владельцев к fake root упёрлось в ограничения uid mapping.
- `visudo -cf init/jasta-admin.sudoers` — правило разобрано; команда также вывела предупреждение, что `/etc/sudo.conf` в test host принадлежит uid 65534.
