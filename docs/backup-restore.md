# Ручной backup и restore Jasta

Команды ниже рассчитаны на Debian/Ubuntu пакетную установку с путями по умолчанию. Архив содержит SQLite, immutable releases, конфигурацию, master key, сертификаты и приватные ключи. Храните его только на зашифрованном и ограниченном по доступу носителе, желательно отдельно от сервера. Режим файла backup — `0600`.

## Что сохранять

- `/var/lib/jasta/` — `/var/lib/jasta/admin.sqlite` вместе с возможными SQLite WAL/SHM файлами, все releases и config snapshots в БД, TLS сертификаты/ключи, ACME account key `certificates/acme-account-key.pem` и account URI.
- `/etc/jasta/` — `config.yaml`, `admin.yaml`, конфиги сайтов, root-owned `/etc/jasta/master.key` и сгенерированный `/etc/jasta/nginx/jasta.conf`.
- `/etc/nginx/conf.d/jasta.conf` — root-owned Nginx shim, подключающий управляемый Jasta include.

Постоянный JWT signing key выводится из `master.key`, поэтому отдельного JWT-файла нет. SQLite содержит hash пароля владельца, зашифрованный TOTP secret, hash recovery codes и refresh sessions. Открытые recovery codes показываются только один раз и в БД не сохраняются: держите выданную при bootstrap копию отдельно. Если коды потеряны, их нельзя восстановить из backup.

Если в `admin.yaml` изменены `sqlite.file`, `releases_root`, `certificates_root`, `master_key_path`, `nginx.config_path` или `admin_private_key`, добавьте фактические файлы и каталоги в backup и перенесите соответствующие команды восстановления на эти пути. Аналогично включите custom ACME/TLS файлы, если они хранятся за пределами `certificates_root`.

## Создать согласованный backup

Сначала убедитесь, что backup destination смонтирован как зашифрованный носитель. Остановка обоих сервисов даёт согласованный снимок БД и файлов и временно прекращает раздачу сайтов. Скрипт перезапускает сервисы даже при ошибке tar.

```bash
set -Eeuo pipefail
umask 077

backup_dir=/mnt/encrypted-backups/jasta
backup="$backup_dir/jasta-$(date -u +%Y%m%dT%H%M%SZ)-$$.tar.gz"
partial="$backup.partial"

sudo install -d -m 0700 "$backup_dir"
restart_jasta() {
  sudo systemctl start jasta-web.service jasta-admin.service
}
cleanup() {
  sudo rm -f "$partial"
  restart_jasta
}
trap cleanup EXIT

sudo systemctl stop jasta-admin.service jasta-web.service
sudo tar --create --gzip --file="$partial" --directory=/ \
  --numeric-owner --xattrs --acls \
  etc/jasta etc/nginx/conf.d/jasta.conf var/lib/jasta
sudo tar --list --gzip --file="$partial" >/dev/null
sudo chmod 0600 "$partial"
sudo mv "$partial" "$backup"
sudo sh -c 'cd "$1" && sha256sum "$(basename "$2")" > "$(basename "$2").sha256"' sh "$backup_dir" "$backup"
sudo chmod 0600 "$backup.sha256"

restart_jasta
trap - EXIT
```

Скопируйте архив и checksum на отдельное защищённое хранилище. Не удаляйте все предыдущие копии после успешного запуска новой.

## Восстановить на чистом или повреждённом хосте

1. Установите ту же или более новую совместимую версию пакета Jasta и его зависимости. Установка создаст системные users/groups и каталоги.
2. Проверьте checksum и список путей в архиве до распаковки:

```bash
backup=/mnt/encrypted-backups/jasta/jasta-<timestamp>.tar.gz
sudo sh -c 'cd "$(dirname "$1")" && sha256sum -c "$(basename "$1").sha256"' sh "$backup"
sudo tar --list --gzip --file="$backup"
```

3. Остановите сервисы, распакуйте backup от root и восстановите владельцев/группы. На новом сервере numeric UID/GID могут отличаться, поэтому назначения групп выполняются по именам пакета:

```bash
sudo systemctl stop jasta-admin.service jasta-web.service
sudo tar --extract --gzip --file="$backup" --directory=/ --numeric-owner --same-owner

sudo chown root:jasta-admin /etc/jasta/master.key
sudo chmod 0640 /etc/jasta/master.key
sudo chown -R jasta-admin:jasta-admin /etc/jasta/nginx
sudo chmod 0750 /etc/jasta/nginx
sudo chmod 0640 /etc/jasta/nginx/jasta.conf
sudo chown root:root /etc/nginx/conf.d/jasta.conf
sudo chmod 0644 /etc/nginx/conf.d/jasta.conf

sudo chown -R jasta-admin:jasta-admin /var/lib/jasta
sudo chown jasta-admin:jasta-content /var/lib/jasta
sudo chown jasta-admin:jasta-content /var/lib/jasta/releases
sudo chgrp -R jasta-content /var/lib/jasta/releases
sudo chown jasta-admin:jasta-admin /var/lib/jasta/certificates
sudo chown jasta-admin:jasta-challenge /var/lib/jasta/acme-challenges
sudo chmod 2750 /var/lib/jasta/acme-challenges
sudo find /var/lib/jasta/acme-challenges -mindepth 1 -delete
```

Эти `chown` не меняют modes файлов releases и сертификатов; они возвращают имена владельцев и группы, нужные двум службам. Не делайте все файлы releases writable: публикация считает их неизменяемыми.

4. Если установлен `sqlite3`, проверьте целостность БД до запуска admin:

```bash
sudo -u jasta-admin sqlite3 /var/lib/jasta/admin.sqlite 'PRAGMA integrity_check;'
```

Ожидаемый результат — `ok`. Затем запустите web, admin, проверьте Nginx и примените восстановленный конфиг:

```bash
sudo systemctl start jasta-web.service
sudo systemctl start jasta-admin.service
sudo nginx -t
sudo systemctl reload nginx
sudo systemctl --no-pager --full status jasta-web.service jasta-admin.service nginx
```

Admin при старте читает активные release/config пары из SQLite и отправляет snapshot запущенному web. Проверьте сайт и его домены, несколько URL/статических файлов, rollback, вход владельца с TOTP, сертификатные домены и расшифровку секрета registry при тестовой загрузке. Не запускайте `owner-bootstrap`, если owner уже восстановлен.

Если SQLite/master key отсутствуют, повреждены или не совпадают, сохранённые TOTP и registry secrets нельзя расшифровать. Если владелец утрачен, остановите admin и выполните `jasta admin owner-reset`, затем `jasta admin owner-bootstrap`; сброс удаляет старые сессии и owner recovery state, его нельзя использовать как обычный шаг restore.

## Границы процедуры

Команды не выполняют автоматическую ротацию, шифрование или отправку копии в удалённое хранилище. Выбранное хранилище и процесс хранения ключа шифрования носителя — ответственность оператора. Перед production restore проверьте этот порядок на изолированном staging host и сохраните текущие данные хоста отдельным backup.
