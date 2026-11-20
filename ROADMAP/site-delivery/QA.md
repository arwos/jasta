# QA F02 — Обслуживание типов сайтов

Проверки запланированы; реализация не начата. Во время планирования тесты не запускались.

## Сценарии

- Два сайта на разных host; host с портом/регистром; домен назначен двум сайтам; неизвестный host.
- SPA: найденный asset, extensionless route, index, missing asset и path traversal.
- MPA: корневой и вложенный `index.html`, trailing slash, отсутствующая страница, custom 404 и path traversal.
- Redirect: 301 и 308, сохранение query и разных path, некорректный target, цикл правил.
- Markdown: валидный slug, главная `/`, повторный и некорректный slug, отсутствующий slug, UTF-8 и специальные HTML-значения в front matter.
- Markdown renderer: HTML из Markdown остаётся экранированным; input/depth/token limits дают ошибку сборки; сгенерированный HTML и copied assets доступны по ожидаемым URL.

## Запуски

Фактических команд и результатов пока нет.
