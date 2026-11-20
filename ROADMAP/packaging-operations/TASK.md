# F07 — Поставка и восстановление

**Зависимости:** F01–F06 по функциям и правам, которые входят в пакет.

- [x] F07-T01 — Разделить systemd units и ограничить права сервисов
  1. Создать `jasta-web`/`jasta-admin`, content/challenge groups, state directories и отдельные hardened units.
  2. Закрыть SQLite, master key, сертификаты и ACME account key от web; дать web read-only доступ к release content и reload socket.
  3. Оставить admin непривилегированным; ограничить reload через точное sudoers правило, Nginx include — выделенным каталогом.
  4. При запуске admin восстанавливать только активный snapshot; одинаковая ревизия повторно принимается как idempotent retry.
  **Приёмка:** Unix-группы и systemd sandbox разделяют web/admin доступ; admin не запускает основной сервис от root и может менять только назначенные ресурсы.

- [x] F07-T02 — Выпускать Debian/Ubuntu `.deb` для amd64 и arm64
  1. Обновить `.deb.yaml` на сервисы web/admin, встроенные frontend assets и необходимые каталоги/права.
  2. Учитывать CGO toolchain (native либо настроенный cross compiler) и порядок post-install/restart/upgrade/remove.
  **Приёмка:** оба пакета имеют правильную архитектуру и зависимости; upgrade сохраняет БД, конфиги, сертификаты и все релизы.

- [ ] F07-T03 — Документировать ручной backup/restore
  1. Добавить команды согласованного backup SQLite, configs, releases, JWT/encryption keys, owner recovery state, ACME key и TLS private keys.
  2. Описать восстановление, права файлов, проверку целостности и последующий reload/restart.
  **Приёмка:** по документации восстанавливается тестовая копия на чистом хосте; после восстановления доступны сайты, вход и registry secrets.
