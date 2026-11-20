# F07 — Поставка и восстановление

**Зависимости:** F01–F06 по функциям и правам, которые входят в пакет.

- [ ] F07-T01 — Разделить systemd units и ограничить права сервисов
  1. Создать отдельные users/groups, директории состояния и systemd units для web/admin.
  2. Ограничить доступ к SQLite, release content, master key, TLS private keys, Nginx includes и reload action.
  **Приёмка:** web не открывает SQLite или admin secrets; admin не работает от root и может менять только назначенные ресурсы.

- [ ] F07-T02 — Выпускать Debian/Ubuntu `.deb` для amd64 и arm64
  1. Обновить `.deb.yaml` на два бинарника/сервиса, frontend assets и необходимые каталоги/права.
  2. Учитывать CGO toolchain и порядок post-install/restart/upgrade/remove.
  **Приёмка:** оба пакета имеют правильную архитектуру и зависимости; upgrade сохраняет БД, конфиги, сертификаты и все релизы.

- [ ] F07-T03 — Документировать ручной backup/restore
  1. Добавить команды согласованного backup SQLite, configs, releases, JWT/encryption keys, owner recovery state, ACME key и TLS private keys.
  2. Описать восстановление, права файлов, проверку целостности и последующий reload/restart.
  **Приёмка:** по документации восстанавливается тестовая копия на чистом хосте; после восстановления доступны сайты, вход и registry secrets.
