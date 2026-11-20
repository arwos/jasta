# Контекст F04 — Владелец, API и прогресс

## Согласованные решения

- Один локальный владелец; bootstrap — одноразовая CLI-команда.
- TOTP обязателен при начальной настройке. Recovery-коды показываются при настройке, локальная CLI-команда остаётся для сброса.
- Access JWT ориентировочно 15 минут; refresh-сессия обновляемая и отзываемая при logout, с целевым максимумом 7 дней.
- JSON-RPC для операций, WebSocket для прогресса, отдельные multipart HTTP-маршруты для архивов.
- Admin API слушает localhost; Nginx публикует admin-домен по HTTPS.

## Состояние и проверенные API ограничения

- В текущем Goppy v3.2.3 есть JWT token service/middleware и JSON-RPC/WS transports в source tree.
- Goppy JWT config default генерирует десять EdDSA key pairs в памяти. Jasta не использует этот default: `internal/auth/session` задаёт один Ed25519 key, детерминированно выведенный HKDF-SHA256 из `/etc/jasta/master.key` с отдельными salt/info.
- JWT issuer и audience фиксированы как `jasta-admin`; verifier дополнительно проверяет EdDSA, issuer, audience, время, `sub=owner`, UUID session ID и верхнюю границу lifetime.
- Refresh-токен — случайные 256 бит; SQLite хранит только domain-separated SHA-256 hash. Rotation атомарно помечает старую строку отозванной и связывает её с новой, сохраняя исходный абсолютный срок 7 дней.
- Login требует подтверждённого TOTP и принимает TOTP либо recovery code. Recovery code потребляется в той же транзакции, что и создание сессии.
- Logout/revoke отзывает session и все её ротационные descendants, запрещая дальнейший refresh немедленно; выданный access JWT остаётся криптографически валиден до своей короткой 15-минутной границы, если вызывающий endpoint не проверяет session ID в БД.
- SQLite driver и миграции доступны через Goppy ORM; база используется только admin.

## Безопасность

- Password hashes, TOTP secrets, recovery-code hashes и JWT private key не возвращать через API.
- TOTP секрет показывать только при enrollment; recovery codes показывать только один раз и хранить в виде проверяемых хэшей.
- Локальный TOTP-секрет шифруется AES-256-GCM; ключ берётся из `/etc/jasta/master.key` (raw 32 bytes или 64 hex characters), файл должен принадлежать root и иметь режим `0600`/`0640`.
- Bootstrap: `jasta admin owner-bootstrap` (интерактивный скрытый ввод пароля, затем подтверждение TOTP до записи владельца). Сброс: `jasta admin owner-reset` с явным вводом `RESET`. Команды требуют интерактивный терминал.
- Пароль хранится как Argon2id (64 MiB, 3 итерации, 2 потока); recovery-коды — случайные 128-битные значения с SHA-256 хэшами и одноразовым атомарным потреблением.
- Учитывать CSRF и cookie flags, если токены кладутся в cookie; при Bearer-токене ограничить CORS admin origin и проверять WebSocket Origin.
- Логировать тип и ID операции, исключая пароли, TOTP, tokens, source credentials и signed URLs.

## Выполнено

- F04-T01: CLI bootstrap/reset и owner primitives добавлены.
- F04-T02: owner login, stable EdDSA JWT, persistent refresh rotation, logout и session revoke реализованы как auth primitives; HTTP/JSON-RPC wiring входит в F04-T03.

## Источники

- [Goppy docs](https://goppy.osspkg.com/) и локальный исходный код закреплённой версии v3.2.3.
- [Goppy JWT source tree](https://github.com/osspkg/goppy/tree/v3.2.3/plugins/auth/token) — API и конфигурацию сверять с закреплённым тегом.
- TOTP реализован через `github.com/pquerna/otp` v1.5.0 с параметрами 30 секунд, SHA-1 и шестизначным кодом; эти параметры должны оставаться согласованы с authenticator URI.
