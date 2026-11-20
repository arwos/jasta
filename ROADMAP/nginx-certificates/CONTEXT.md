# Контекст F05 — Nginx и сертификаты

## Согласованные решения

- TLS завершается в Nginx. Admin создаёт Nginx-конфиги и делает reload.
- `jasta admin` — отдельный systemd-сервис с узким доступом к каталогу конфигов, сертификатам и reload Nginx.
- Admin доступен только localhost; отдельный admin-домен публикуется через Nginx по HTTPS.
- Let’s Encrypt — обычные домены через HTTP-01; DNS-01 и wildcard не входят в scope.
- Self-signed сертификаты нужны локальным доменам и временному fallback; публичные домены используют Let’s Encrypt.

## Текущее состояние

- Legacy-команда `jasta nginx`, записывавшая конфиг в `/etc/nginx/sites-available/default`, удалена. F05-T01 добавляет renderer, atomic apply и защищённый `nginx.apply`; output по умолчанию — `/etc/nginx/conf.d/jasta.conf`.
- `.deb.yaml` зависит от nginx, а текущий `jasta.service` одинарный и работает от `www-data`. F07-T01 должен разделить сервисы и выдать admin Unix-права только на include-файл, challenge root, `nginx -t` и `systemctl reload nginx`.
- Генератор создаёт HTTP public blocks; admin HTTP блок обслуживает ACME challenge и перенаправляет остальной трафик на HTTPS. Если парные `admin_certificate`/`admin_private_key` заданы в `admin.nginx`, создаётся TLS server block с proxy на localhost admin listener и WebSocket upgrade только на `/ws`; F05-T02/T03 настраивают пути выпущенных сертификатов.
- `nginx.apply` — защищённый owner JSON-RPC метод с пустыми params. Он группирует `site_domains` из admin SQLite, берёт admin host/listeners/output/cert paths только из root-managed config и не принимает пути, executable или shell strings от клиента.
- Перед первым apply оператор задаёт `admin.nginx.admin_domain` в `/etc/jasta/admin.yaml`; сертификатные пути остаются пустыми до выпуска сертификата. Из-за этого generated HTTP admin block пока перенаправляет на HTTPS, а TLS proxy block появится после F05-T02/T03.

## Проверенные ограничения и источники

- [Let’s Encrypt HTTP-01](https://letsencrypt.org/docs/challenge-types/) выполняется через порт 80 и не подходит для wildcard-сертификатов.
- В проекте уже есть indirect `golang.org/x/crypto` v0.53.0. [Пакет Go ACME](https://pkg.go.dev/golang.org/x/crypto/acme) предоставляет ACME client и HTTP-01 path/response helpers; точную версию и API проверить против закреплённого Go toolchain.
- Nginx [`-t` и `-s reload`](https://nginx.org/en/docs/switches.html): `-t` проверяет синтаксис и referenced files; reload запускает workers с новой конфигурацией и gracefully завершает старые. Jasta сначала проверяет isolated candidate и полную конфигурацию, rollback-ит include при ошибке и только после проверки вызывает `systemctl reload nginx`.

## Ограничения внедрения

- Не выдавать admin универсальные root-права. Установочный пакет должен создавать директории/группы и задавать узкие ACL или systemd/sudo policy.
- Challenge-location имеет приоритет перед редиректом HTTP→HTTPS и redirect site rules.
- Private keys и ACME account key должны входить в ручной backup и не попадать в статусы или логи.
