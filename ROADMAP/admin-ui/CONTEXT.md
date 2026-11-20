# Контекст F06 — React-админка

## Согласованные решения

- Frontend: React, TypeScript, Tailwind CSS, `@tailwindcss/vite`, Vite, Oxlint и pnpm.
- API-контракт: Goppy JSON-RPC для команд, WebSocket для прогресса, multipart HTTP для архива.
- Admin слушает localhost и публикуется через отдельный HTTPS-домен Nginx.
- UI включает владельца, сайты/конфиги, источники архивов, деплои/релизы, rollback, домены/сертификаты и прогресс.
- Низкорисковое предположение: первая версия интерфейса на русском языке, адаптивная и доступная с клавиатуры; отдельный визуальный референс не задан.

## Состояние и источники

- В репозитории пока нет React frontend, `package.json`, pnpm lockfile или Vite config.
- [Vite guide](https://vite.dev/guide/) на дату исследования: Node.js 20.19+ или 22.12+; планировать Node 22.12+.
- [Tailwind Vite integration](https://tailwindcss.com/docs/installation/using-vite) использует `@tailwindcss/vite` и CSS `@import "tailwindcss"`.
- [Oxlint quickstart](https://oxc.rs/docs/guide/usage/linter/quickstart.html) показывает установку и запуск через pnpm.
- [pnpm installation](https://pnpm.io/installation); после bootstrap закрепить pnpm через `packageManager` и проверять с frozen lockfile.

## Контракт раздачи UI

- Vite build входит в бинарник `jasta admin`, чтобы `.deb` поставлял backend и админку согласованными версиями.
- RPC клиент добавляет access credential; refresh и TOTP остаются внутри auth flow.
- WebSocket проверяет admin origin и пользовательскую сессию.
- Статусы и сообщения показывать человеческим языком; технические подробности доступны в job detail без секретов.
