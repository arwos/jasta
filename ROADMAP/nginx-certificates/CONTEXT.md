# Контекст F05 — Nginx и сертификаты

## Согласованные решения

- TLS завершается в Nginx. Admin создаёт Nginx-конфиги и делает reload.
- `jasta admin` — отдельный systemd-сервис с узким доступом к каталогу конфигов, сертификатам и reload Nginx.
- Admin доступен только localhost; отдельный admin-домен публикуется через Nginx по HTTPS.
- Let’s Encrypt — обычные домены через HTTP-01; DNS-01 и wildcard не входят в scope.
- Self-signed сертификаты нужны локальным доменам и временному fallback; публичные домены используют Let’s Encrypt.

## Текущее состояние

- В репозитории есть `internal/pkg/nginx/command.go` и `config/nginx-default.conf`; текущая команда записывает default config непосредственно в `/etc/nginx/sites-available/default`.
- `.deb.yaml` зависит от nginx, а текущий `jasta.service` одинарный и работает от `www-data`. Целевая установка должна заменить эту модель на отдельные сервисы и выделенную include-зону.

## Проверенные ограничения и источники

- [Let’s Encrypt HTTP-01](https://letsencrypt.org/docs/challenge-types/) выполняется через порт 80 и не подходит для wildcard-сертификатов.
- В проекте уже есть indirect `golang.org/x/crypto` v0.53.0. [Пакет Go ACME](https://pkg.go.dev/golang.org/x/crypto/acme) предоставляет ACME client и HTTP-01 path/response helpers; точную версию и API проверить против закреплённого Go toolchain.
- Nginx [`-t` и `-s reload`](https://nginx.org/en/docs/switches.html): `-t` проверяет синтаксис и доступность файлов; reload запускает worker с новой конфигурацией и gracefully завершает старые.

## Ограничения внедрения

- Не выдавать admin универсальные root-права. Установочный пакет должен создавать директории/группы и задавать узкие ACL или systemd/sudo policy.
- Challenge-location имеет приоритет перед редиректом HTTP→HTTPS и redirect site rules.
- Private keys и ACME account key должны входить в ручной backup и не попадать в статусы или логи.
