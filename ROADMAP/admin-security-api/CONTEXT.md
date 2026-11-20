# Контекст F04 — Владелец, API и прогресс

## Согласованные решения

- Один локальный владелец; bootstrap — одноразовая CLI-команда.
- TOTP обязателен при начальной настройке. Recovery-коды показываются при настройке, локальная CLI-команда остаётся для сброса.
- Access JWT ориентировочно 15 минут; refresh-сессия обновляемая и отзываемая при logout, с целевым максимумом 7 дней.
- JSON-RPC для операций, WebSocket для прогресса, отдельные multipart HTTP-маршруты для архивов.
- Admin API слушает localhost; Nginx публикует admin-домен по HTTPS.

## Состояние и проверенные API ограничения

- В текущем Goppy v3.2.3 есть JWT token service/middleware и JSON-RPC/WS transports в source tree.
- Goppy JWT config default генерирует десять EdDSA key pairs в памяти. Для стабильных JWT и восстановления после рестарта настроить ключ явно; не использовать runtime defaults.
- JWT middleware проверяет подпись/время, но приложение отдельно хранит refresh-сессии и обеспечивает отзыв через SQLite.
- SQLite driver и миграции доступны через Goppy ORM; база используется только admin.

## Безопасность

- Password hashes, TOTP secrets, recovery-code hashes и JWT private key не возвращать через API.
- TOTP секрет показывать только при enrollment; recovery codes показывать только один раз и хранить в виде проверяемых хэшей.
- Учитывать CSRF и cookie flags, если токены кладутся в cookie; при Bearer-токене ограничить CORS admin origin и проверять WebSocket Origin.
- Логировать тип и ID операции, исключая пароли, TOTP, tokens, source credentials и signed URLs.

## Источники

- [Goppy docs](https://goppy.osspkg.com/) и локальный исходный код закреплённой версии v3.2.3.
- [Goppy JWT source tree](https://github.com/osspkg/goppy/tree/v3.2.3/plugins/auth/token) — API и конфигурацию сверять с закреплённым тегом.
- Новую TOTP-зависимость не зафиксировали; до её выбора сверить существующие OSSPkg пакеты и официальный API. Контракт входа/TOTP при этом уже определён.
