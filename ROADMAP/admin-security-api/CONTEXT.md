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

## F04-T03: JSON-RPC контракт

Admin принимает JSON-RPC POST на `/jsonrpc` своего Goppy server tag. Bootstrap/reset владельца остаются только локальными CLI-командами. Токены передаются заголовком `Authorization: Bearer <access_token>`; cookies и CORS credentials не используются. `auth.login` и `auth.refresh` публичные, `auth.logout` требует активный access token, остальные методы требуют активный owner session. Каждая защищённая команда проверяет JWT и строку `auth_sessions` в SQLite: ротация refresh и logout сразу прекращают доступ старого access JWT.

Методы, параметры и ответы:

| Метод | Доступ | Параметры | Ответ |
|---|---|---|---|
| `auth.login` | публичный | `{password, second_factor}`; second_factor — TOTP или recovery code | `Tokens`: access/refresh token и сроки действия |
| `auth.refresh` | публичный | `{refresh_token}` | новая пара `Tokens`; старый refresh и его access session отзываются |
| `auth.logout` | owner bearer | `{refresh_token}` | `{ok:true}`; отзывает указанную сессию и её descendants |
| `auth.me` | owner bearer | `{}` | `{subject:"owner"}` |
| `sites.list` | owner bearer | `{}` | массив `SiteSummary`: id, name, app_type, domains, timestamps, active_release_id |
| `sites.get` | owner bearer | `{site_id}` | один `SiteSummary` |
| `sites.config.get` | owner bearer | `{site_id}` | `{site_id, app_type, revision, config}` для последнего снимка |
| `sites.config.validate` | owner bearer | `{app_type, config}` | `{valid:true}` либо ошибка `-32602` |
| `sources.list` | owner bearer | `{}` | id, name, auth_type, updated_at; URL и секреты не возвращаются |
| `sources.create` | owner bearer | `{name, url, auth_type, username?, password?, bearer_token?}` | `{id, name, auth_type}` |
| `sources.update` | owner bearer | те же поля и обязательный `id` | `{id, name, auth_type}` |
| `sources.delete` | owner bearer | `{id}` | `{ok:true}` |
| `releases.history` | owner bearer | `{site_id}` | история, активная пара и занимаемое место |
| `releases.activate` | owner bearer | `{site_id, release_id}` | `{site_id, release_id, snapshot_revision}` |
| `releases.rollback` | owner bearer | `{site_id, release_id}` | `{site_id, release_id, snapshot_revision}` |
| `certificates.list` | owner bearer | `{}` | id, domain, issuer, status, validity; файловые пути и ключи не возвращаются |

Ошибки JSON-RPC: `-32602` — отсутствующие, неизвестные или некорректные параметры; `-32001` — неверные credentials или недействительная owner session; `-32004` — запрошенный сайт/config/source отсутствует или релиз не удалось активировать; `-32009` — имя source занято либо запись source отклонена хранилищем; `-32603` — внутренняя ошибка без текста БД/файловой системы. Неизвестный метод получает стандартную Goppy ошибку unsupported method. Ошибки валидации конфигурации намеренно не возвращают внутренние пути.

Registry write methods принимают только HTTPS URL без userinfo/fragment и проверяют сочетание `auth_type`/credentials через контракт `archive.ValidateSource`. Query из URL и JSON с Basic/Bearer credentials хранятся в `url_query_ciphertext` и `credentials_ciphertext` под AES-GCM ключом `/etc/jasta/master.key`; основной URL в SQLite сохраняется без query. `LoadSource` расшифровывает значения только для использования внутри admin и возвращает несериализуемый `archive.Source`. Ответы API и список источников не содержат открытых секретов; fetch повторно выполняет DNS/IP/redirect проверки непосредственно перед сетевым запросом.

F04-T03 подключает read/validate и lifecycle методов, поддержанных текущими хранилищем и deployment-сервисами, а также полный CRUD registry metadata/credentials. Изменение config активного сайта происходит через создание релиза с config snapshot и его активацию; archive upload в multipart добавляется в F04-T05. Выпуск/продление/удаление сертификатов требуют ACME/Nginx потоков и добавляются в F05. Bootstrap по HTTP намеренно отсутствует.

## F04-T04: прогресс операций через WebSocket

Admin публикует WebSocket endpoint `GET /ws`. Handshake разрешается только при совпадающем `Origin` host и scheme; за Nginx ожидаемый scheme берётся из одиночного `X-Forwarded-Proto`. Одновременно допускается до 128 подключений. Bearer JWT не помещается в URL или handshake headers: клиент отправляет его первым event `e=1` с `{ "access_token": "..." }`, получает `{ "authenticated": true }`, затем подписывается event `e=2` с `{ "after_sequence": 0 }`. Подписка возвращает страницу сохранённого replay `{events, cursor, has_more}` и продолжает live доставку событием `e=100`. Страница ограничена 200 элементами; при `has_more=true` клиент запрашивает следующую страницу, передав последний `cursor`.

Событие содержит монотонный `sequence`, `job_id`, безопасные `kind`/`stage`, `status`, `progress`, необязательный `error_code` и `created_at`. Свободный текст ошибки не публикуется. Состояния job: queued, running, succeeded, failed, cancelled; succeeded требует 100%. История job доступна только сессии, создавшей job, и её refresh-rotation descendants/ancestors; отдельная сессия того же владельца не видит эти события. Session проверяется при auth, subscribe и live send; logout завершает доступ. События хранятся в SQLite таблице `job_events`, а миграция добавляет owner session к `jobs` без потери существующих записей.

`internal/jobs.Manager` предоставляет durable create/publish/replay/live primitives. F04-T04 пока не подключает archive upload/deploy или сертификатные операции к publisher; F04-T05 и F05 должны создавать jobs и публиковать реальные стадии через этот manager. Интеграционный сценарий F04-T04 использует синтетический deploy job.

## Источники

- [Goppy docs](https://goppy.osspkg.com/) и локальный исходный код закреплённой версии v3.2.3.
- [Goppy JWT source tree](https://github.com/osspkg/goppy/tree/v3.2.3/plugins/auth/token) — API и конфигурацию сверять с закреплённым тегом.
- TOTP реализован через `github.com/pquerna/otp` v1.5.0 с параметрами 30 секунд, SHA-1 и шестизначным кодом; эти параметры должны оставаться согласованы с authenticator URI.
