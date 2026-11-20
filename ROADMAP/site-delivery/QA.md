# QA F02 — Обслуживание типов сайтов

F02-T01 выполнена. F02-T02–F02-T04 и оставшиеся сценарии запланированы; тестовые наборы не запускались.

## Сценарии

- Два сайта на разных host; host с портом/регистром; домен назначен двум сайтам; неизвестный host.
- SPA: найденный asset, extensionless route, index, missing asset и path traversal.
- MPA: корневой и вложенный `index.html`, trailing slash, отсутствующая страница, custom 404 и path traversal.
- Redirect: 301 и 308, сохранение query и разных path, некорректный target, цикл правил.
- Markdown: валидный slug, главная `/`, повторный и некорректный slug, отсутствующий slug, UTF-8 и специальные HTML-значения в front matter.
- Markdown renderer: HTML из Markdown остаётся экранированным; input/depth/token limits дают ошибку сборки; сгенерированный HTML и copied assets доступны по ожидаемым URL.

## Запуски

- `gofmt -w internal/snapshot/host.go internal/snapshot/snapshot.go internal/api/public/api.go` — успешно.
- `go build -o /tmp/jasta-f02-t01 ./cmd/jasta` — успешно; Go вывел предупреждение о read-only GVM stat cache.
- Runtime smoke: web на `127.0.0.1:8080` принял снимок с доменом `LoCaLhOsT.`; запрос `Host: LOCALHOST.:8088` вернул HTTP 200 и SPA content. Запрос `Host: unknown.example.test` вернул HTTP 523.
- Runtime conflict smoke: снимок с доменами `dup.test` и `DUP.TEST.` был отклонён как конфликт после нормализации; после отказа прежний домен `localhost` продолжил возвращать HTTP 200. Тестовый web-процесс остановлен.
- Полный тестовый набор и lint не запускались.
