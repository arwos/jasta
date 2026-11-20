# QA F07 — Поставка и восстановление

Проверки запланированы; реализация не начата. Во время планирования тесты не запускались.

## Сценарии

- Устанавливать пакет на чистые Debian/Ubuntu amd64 и arm64; проверять `dpkg-deb --info` и содержимое архива.
- Проверить, что оба systemd units стартуют от разных ограниченных users и слушают только localhost.
- Проверить permissions на DB, releases, staging, master key, JWT keys, ACME key, TLS certs и Nginx include directory.
- Upgrade существующей установки сохраняет SQLite, sites, config snapshots, сертификаты и все releases.
- Remove пакет сохраняет пользовательские данные; действия по удалению данных требуют отдельной явной операции.
- Backup/restore в чистый staging host: восстановить SQLite и файлы/ключи, запустить оба сервиса, проверить сайт, rollback, owner login и расшифровку registry secret.
- Проверить сборку amd64/arm64 с CGO toolchain и frontend assets; не считать текущий amd64-only `make build` доказательством обоих пакетов.

## Запуски

Фактических команд и результатов пока нет.
