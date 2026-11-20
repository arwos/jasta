# QA F06 — React-админка

Проверки запланированы; frontend ещё не создан, во время планирования тесты не запускались.

## Сценарии

- После bootstrap проверить `pnpm install --frozen-lockfile`, lint и production build.
- Responsive layout на узком и широком viewport; keyboard navigation, видимый focus, labels, ошибки форм.
- Login: пароль/TOTP, некорректный код, expired JWT, refresh success/failure, logout.
- Проверить экраны всех четырёх application types, server validation, неизвестный host и конфликт домена.
- Multipart upload с ошибкой лимита, URL source auth, real-time job progress/reconnect, публикация и rollback.
- Проверить, что secrets, token, recovery codes и signed query URL отсутствуют в DOM после завершения настройки и в клиентских логах.
- Проверить выдачу fallback SPA админки только на admin-домене; API пути не перехватываются client-side router.

## Запуски

Frontend-команды появятся в F06-T01. Фактических команд и результатов пока нет.
