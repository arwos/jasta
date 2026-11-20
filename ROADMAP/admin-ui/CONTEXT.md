# Контекст F06 — React-админка

## Согласованные решения

- Frontend: React, TypeScript, Tailwind CSS, `@tailwindcss/vite`, Vite, Oxlint и pnpm.
- API-контракт: Goppy JSON-RPC для команд, WebSocket для прогресса, multipart HTTP для архива.
- Admin слушает localhost и публикуется через отдельный HTTPS-домен Nginx.
- UI включает владельца, сайты/конфиги, источники архивов, деплои/релизы, rollback, домены/сертификаты и прогресс.
- Низкорисковое предположение: первая версия интерфейса на русском языке, адаптивная и доступная с клавиатуры; отдельный визуальный референс не задан.

## Состояние и источники

- F06-T01 добавляет frontend в `ui/admin/`: React 19.2.7, React DOM 19.2.7, TypeScript 7.0.2, Vite 8.3.8, Tailwind CSS/`@tailwindcss/vite` 4.3.5, Oxlint 1.90.0 и pnpm 12.18.0. Node requirement: `>=22.12.0 <27`; versions pinned in `package.json` and `pnpm-lock.yaml`.
- Vite outputs `ui/admin/dist`; `ui/admin/adminui.go` embeds it into `jasta admin`. The admin NotFound handler serves the app and client routes only when Host matches `admin.nginx.admin_domain`, and leaves `/jsonrpc`, `/uploads`, `/ws` and `/.well-known` reserved. Vite uses content-hashed CSS/JS filenames; `index.html` is no-cache.
- The initial UI is a Russian responsive shell/empty state. Auth and data flows are implemented in following F06 tasks.
- F06-T02 keeps owner credentials and JWTs in page memory only. A short access token refreshes 30 seconds before expiry and on a protected-RPC unauthorized response; logout revokes the refresh session. A page reload requires a new login. Owner TOTP enrollment/recovery codes stay in the approved one-time local `jasta owner-bootstrap` command and are never returned by browser API.
- F06-T03 exposes site creation/config editing and the current domain/certificate inventory. Saving configuration creates an immutable draft snapshot and updates the registered domain set; the active web snapshot changes only when a release containing that config is activated. Rollback still restores the selected release/config pair. Config read normalizes a release content root back to an archive-relative path and omits the internal Markdown source directory.
- [Vite guide](https://vite.dev/guide/) requires Node.js 20.19+ or 22.12+; project pins Node 22.12+ minimum.
- [Tailwind Vite integration](https://tailwindcss.com/docs/installation/using-vite) uses `@tailwindcss/vite` and CSS `@import "tailwindcss"`.
- [Oxlint quickstart](https://oxc.rs/docs/guide/usage/linter/quickstart.html) documents installation and pnpm scripts. `pnpm lint` scopes linting to `src`, excluding generated bundle output.
- [pnpm installation](https://pnpm.io/installation); после bootstrap закрепить pnpm через `packageManager` и проверять с frozen lockfile.

## Контракт раздачи UI

- Vite build входит в бинарник `jasta admin`, чтобы `.deb` поставлял backend и админку согласованными версиями.
- RPC клиент добавляет access credential; refresh и TOTP остаются внутри auth flow.
- WebSocket проверяет admin origin и пользовательскую сессию.
- Статусы и сообщения показывать человеческим языком; технические подробности доступны в job detail без секретов.
