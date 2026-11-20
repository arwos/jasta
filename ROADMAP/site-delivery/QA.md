# QA F02 — Обслуживание типов сайтов

F02-T01 и F02-T02 выполнены. F02-T03–F02-T04 и оставшиеся сценарии запланированы; тестовые наборы не запускались.

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
- `gofmt -w internal/pkg/application/common.go internal/pkg/application/spa.go internal/pkg/application/mpa.go internal/pkg/application/static_test.go` — успешно.
- `go test ./internal/pkg/application` — успешно; тесты покрывают SPA/MPA index, direct и nested files, SPA fallback, assets, custom 404 и URL traversal.
- `go build -o /tmp/jasta-f02-t02 ./cmd/jasta` и `git diff --check` — успешно; Go вывел предупреждение о read-only GVM stat cache.
- Runtime smoke на временных сайтах в `/tmp/jasta-f02-t02-dt_arwpq`: SPA root, direct file, fallback route и asset вернули 200; missing asset вернул 404 с custom page. MPA root, `/nested`, `/nested/` и nested file вернули 200; missing page и asset вернули 404 с custom page. Asset ответ содержал `Cache-Control: max-age=0`.
- Encoded traversal `/%2e%2e/%2e%2e/etc/passwd` на обоих типах сайтов вернул пустой HTTP 404. Тестовый web-процесс остановлен; fixtures размещались только в `/tmp`.
- Полный тестовый набор и lint не запускались.
