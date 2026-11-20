# Контекст F05 — Nginx и сертификаты

## Согласованные решения

- TLS завершается в Nginx. Admin создаёт Nginx-конфиги и делает reload.
- `jasta admin` — отдельный systemd-сервис с узким доступом к каталогу конфигов, сертификатам и reload Nginx.
- Admin доступен только localhost; отдельный admin-домен публикуется через Nginx по HTTPS.
- Let’s Encrypt — обычные домены через HTTP-01; DNS-01 и wildcard не входят в scope.
- Self-signed сертификаты нужны локальным доменам и временному fallback; публичные домены используют Let’s Encrypt.

## Текущее состояние

- Legacy-команда `jasta nginx`, записывавшая конфиг в `/etc/nginx/sites-available/default`, удалена. F05-T01 добавляет renderer, atomic apply и защищённый `nginx.apply`; F07-T01 меняет generated include на `/etc/jasta/nginx/jasta.conf`. Root-owned shim `/etc/nginx/conf.d/jasta.conf` подключает этот отдельный файл.
- `.deb.yaml` пока содержит прежний `jasta.service`; F07-T02 должен переключить пакет на новые web/admin units и установить shim, sudoers policy, sysusers и tmpfiles файлы F07-T01.
- Генератор создаёт HTTP public blocks; admin HTTP блок обслуживает ACME challenge и перенаправляет остальной трафик на HTTPS. Если парные `admin_certificate`/`admin_private_key` заданы в `admin.nginx`, создаётся TLS server block с proxy на localhost admin listener и WebSocket upgrade только на `/ws`; F05-T02/T03 настраивают пути выпущенных сертификатов.
- `nginx.apply` — защищённый owner JSON-RPC метод с пустыми params. Он группирует `site_domains` из admin SQLite, берёт admin host/listeners/output/cert paths только из root-managed config и не принимает пути, executable или shell strings от клиента.
- Перед первым apply оператор задаёт `admin.nginx.admin_domain` в `/etc/jasta/admin.yaml`; сертификатные пути остаются пустыми до выпуска сертификата. Из-за этого generated HTTP admin block пока перенаправляет на HTTPS, а TLS proxy block появится после F05-T02/T03.
- F05-T02 добавляет защищённый `certificates.issue_self_signed` для одиночных локальных имён, специальных локальных suffixes и loopback/private/link-local IP. Публичные имена/IP и wildcard отклоняются. Выпускается ECDSA P-256 server certificate с DNS/IP SAN на 90 дней; private key хранится в PKCS#8. Сертификатный каталог (`admin.certificates_root`, default `/var/lib/jasta/certificates`) отделён от релизов. API выдаёт только метаданные, SQLite хранит пути; повторная выдача на домен запрещена. Если выпущен сертификат для admin domain, следующий `nginx.apply` выбирает его из metadata и генерирует TLS server block. Каталог/key modes: 0750/0640, public cert: 0644; создание общей Unix-группы и права systemd выполняются в F07-T01.
- F05-T03 добавляет `certificates.issue_letsencrypt`: owner запускает асинхронный progress job для домена, уже назначенного сайту или admin. Публичный DNS hostname обязателен; IP, локальные suffixes и wildcard не принимаются. Terms acceptance задаётся оператором в root-managed `admin.acme.accept_terms`; directory URL по умолчанию указывает на production ACME, а staging можно задать в конфиге. Account ECDSA key (`0600`) и URI сохраняются под certificates root; HTTP-01 response временно появляется в challenge root и удаляется после попытки.
- После успешного issuance сохраняется новая версия chain/key и активируется Nginx HTTPS-блок для public site или admin domain. HTTP-01 location предшествует redirect, а обычные HTTP запросы для certified host получают 308 на HTTPS. Обновление выполняется повторным owner RPC-вызовом. Метаданные заменяются до Nginx apply; если apply проваливается, Nginx Apply возвращает предыдущий include, а admin восстанавливает прежнюю DB-запись и удаляет candidate files. Предыдущие сохранённые cert files не очищаются автоматически.

## Проверенные ограничения и источники

- [Let’s Encrypt HTTP-01](https://letsencrypt.org/docs/challenge-types/) выполняется через порт 80 и не подходит для wildcard-сертификатов.
- В проекте уже есть indirect `golang.org/x/crypto` v0.53.0. [Пакет Go ACME](https://pkg.go.dev/golang.org/x/crypto/acme) предоставляет ACME client и HTTP-01 path/response helpers; точную версию и API проверить против закреплённого Go toolchain.
- Nginx [`-t` и `-s reload`](https://nginx.org/en/docs/switches.html): `-t` проверяет синтаксис и referenced files; reload запускает workers с новой конфигурацией и gracefully завершает старые. Jasta сначала проверяет isolated candidate и полную конфигурацию, rollback-ит include при ошибке и только после проверки вызывает `systemctl reload nginx`.

## Ограничения внедрения

- `jasta-admin` остаётся непривилегированным пользователем; sudoers разрешает только точную команду `/usr/bin/systemctl reload nginx`. Admin пишет generated include только в `/etc/jasta/nginx`, а Nginx проверяет его через `nginx -t` до reload.
- Отдельная группа `jasta-admin` даёт `nginx -t` чтение TLS private keys; `jasta-web` в эту группу не входит. Группа `jasta-challenge` открывает HTTP-01 файлы только для admin и Nginx worker.
- Challenge-location имеет приоритет перед редиректом HTTP→HTTPS и redirect site rules.
- Private keys и ACME account key должны входить в ручной backup и не попадать в статусы или логи.
