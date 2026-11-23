# F05 — Nginx и сертификаты

**Зависимости:** F01-T01, F01-T03, F04-T03.

- [x] F05-T01 — Генерировать и безопасно применять Nginx-конфигурацию
  1. Генерировать отдельные server blocks для сайтов и admin-домена в принадлежащей Jasta include-зоне.
  2. Поддержать проксирование в localhost listeners и путь HTTP-01 challenge.
  3. Перед reload выполнять `nginx -t`; ограничить операцию одним include-файлом и фиксированными командами проверки/reload. Unix-права для systemd admin на include, challenge root и reload задаёт F07-T01.
  **Приёмка:** неверная конфигурация не активируется и не прерывает текущие worker-процессы; валидная применяет изменения штатным reload. Admin не принимает путь или команду из API request.

- [x] F05-T02 — Выпускать локальные self-signed сертификаты
  1. Защищённый owner JSON-RPC `certificates.issue_self_signed` создаёт ECDSA P-256 сертификат на 90 дней с SAN для локального DNS-имени или loopback/private/link-local IP; публичные DNS/IP и wildcard отклоняются.
  2. Сертификат и PKCS#8 ключ сохраняются отдельным UUID-каталогом под `admin.certificates_root` (по умолчанию `/var/lib/jasta/certificates`), вне релизов. Каталог имеет режим 0750, сертификат 0644, ключ 0640. SQLite хранит метаданные и пути; API не возвращает пути или ключ.
  3. Повторный выпуск для уже зарегистрированного домена отклоняется без перезаписи существующего сертификата. Для включения admin TLS оператор запускает `nginx.apply` после выпуска сертификата для настроенного `admin_domain`.
  **Приёмка:** сертификат проходит локальную проверку подписи и hostname SAN; публичный домен не принимается и сертификат не объявляется публично доверенным. Общая Unix-группа admin/Nginx настраивается в F07-T01.

- [x] F05-T03 — Получать и продлевать Let’s Encrypt сертификаты через HTTP-01
  1. Защищённый owner JSON-RPC `certificates.issue_letsencrypt` запускает progress job для настроенного публичного домена. Оператор явно принимает Let’s Encrypt ToS в root-managed `admin.acme.accept_terms`; ACME account key и account URI сохраняются в `admin.certificates_root`.
  2. HTTP-01 token временно публикуется под настроенным `admin.nginx.challenge_root/.well-known/acme-challenge`; Nginx challenge-location включён до проверки CA и не перехватывается redirect.
  3. Повторный вызов для домена обновляет метаданные сертификата с сохранением предыдущей записи для rollback. Новые файлы хранятся отдельным UUID-каталогом под certificates root. Nginx создаёт HTTPS server block для домена и переводит обычный HTTP на HTTPS; challenge URL остаётся доступен по HTTP.
  **Приёмка:** успешный выпуск приводит к проверенному `nginx -t` и reload; при challenge или apply failure прежний Nginx config/certificate продолжает обслуживаться; wildcard, локальные имена и IP отклоняются. Выпуск/продление запускается владельцем повторным RPC-вызовом.
