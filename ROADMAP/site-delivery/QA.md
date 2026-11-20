# QA F02 — Обслуживание типов сайтов

F02-T01–F02-T04 выполнены. Остальные feature-группы остаются в работе.

## Сценарии

- Два сайта на разных host; host с портом/регистром; домен назначен двум сайтам; неизвестный host.
- SPA: найденный asset, extensionless route, index, missing asset и path traversal.
- MPA: корневой и вложенный `index.html`, trailing slash, отсутствующая страница, custom 404 и path traversal.
- Redirect: 301 и 308, сохранение path/query, host normalization и path-prefix граница, некорректный target, циклы внутри одной конфигурации и между сайтами.
- Markdown: валидный slug, главная `/`, повторный и некорректный slug, отсутствующий slug, UTF-8 и специальные HTML-значения в front matter; выбор альтернативного template.
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
- `gofmt -w internal/pkg/host/normalize.go internal/pkg/application/rpa.go internal/pkg/application/rpa_test.go internal/pkg/config/config.go internal/snapshot/snapshot.go internal/snapshot/redirect_cycle_test.go internal/api/public/api.go` — успешно.
- `go test ./internal/pkg/application ./internal/snapshot ./internal/pkg/config` — успешно; покрыты 301/308, path/query, нормализация host, prefix boundary, ошибочные redirect rules и межсайтовый цикл.
- `go build -o /tmp/jasta-f02-t03 ./cmd/jasta` и `git diff --check` — успешно; Go вывел предупреждение о read-only GVM stat cache.
- `go get go.osspkg.com/markdown@v0.1.0` — успешно; Go добавил выбранный Markdown-пакет и обновил минимальные версии `golang.org/x/{crypto,net,sys}`, требуемые его графом зависимостей.
- `gofmt -w internal/pkg/application/mda.go internal/pkg/application/mda_builder.go internal/pkg/application/mda_test.go internal/pkg/config/parse.go internal/pkg/config/parse_test.go` и `go mod tidy` — успешно.
- `go test ./internal/pkg/application ./internal/pkg/config ./internal/snapshot` — успешно; проверены сборка и выдача главной/вложенной Markdown-страниц, copied assets, HTML escaping, raw HTML escaping, slug normalization, отсутствующий/некорректный/повторный slug, malformed/неизвестные front matter поля, сохранение предыдущего root при неудачной или повторной сборке, read-only web setup без `source` и разрешение относительных `source`/`root` от YAML-конфига.
- `go vet ./internal/pkg/application ./internal/pkg/config ./internal/snapshot`, `go build -o /tmp/jasta-f02-t04 ./cmd/jasta` и `git diff --check` — успешно; Go вывел предупреждение о read-only GVM stat cache.
- `golangci-lint run ./internal/pkg/application ./internal/pkg/config` — успешно, 0 issues.
- `make lint` во время работы нашёл замечания в новом Markdown-сборщике и четыре ошибки в несвязанных исходных файлах. Замечания сборщика исправлены; повторный `golangci-lint run ./...` сообщает только baseline-ошибки `internal/api/admin/reload.go:23` (deferred `file.Close`) и `internal/snapshot/socket.go:36,71,101` (permissions/deferred `conn.Close`).
- Полный Go test suite не запускался; полный lint запускался и остаётся с четырьмя baseline-ошибками, перечисленными выше.
