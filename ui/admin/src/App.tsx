const sections = [
  { icon: '◫', label: 'Обзор', active: true },
  { icon: '▤', label: 'Сайты' },
  { icon: '↥', label: 'Публикации' },
  { icon: '⌁', label: 'Домены и TLS' },
  { icon: '⚙', label: 'Настройки' },
]

const capabilities = [
  { number: '01', title: 'Сайты и конфигурации', detail: 'SPA, MPA, Markdown и перенаправления' },
  { number: '02', title: 'Публикации и версии', detail: 'Загрузка сборок, история и откат' },
  { number: '03', title: 'Домены и сертификаты', detail: 'HTTP-01 и локальные сертификаты' },
]

function App() {
  return (
    <div className="min-h-screen bg-[#f4f6f8] text-slate-900 lg:grid lg:grid-cols-[250px_1fr]">
      <aside className="flex flex-col border-b border-slate-200 bg-white px-5 py-5 lg:min-h-screen lg:border-b-0 lg:border-r lg:px-6">
        <a className="flex items-center gap-3 rounded-lg focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-blue-700" href="#main" aria-label="Jasta, на главную">
          <span className="grid size-10 place-items-center rounded-xl bg-blue-700 text-lg font-bold text-white">J</span>
          <span>
            <span className="block text-base font-semibold tracking-tight">jasta</span>
            <span className="block text-xs text-slate-500">control plane</span>
          </span>
        </a>

        <nav aria-label="Главная навигация" className="mt-8 flex gap-2 overflow-x-auto lg:flex-col">
          {sections.map((section) => (
            <a
              aria-current={section.active ? 'page' : undefined}
              className={`flex shrink-0 items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-700 ${section.active ? 'bg-blue-50 text-blue-800' : 'text-slate-600 hover:bg-slate-100 hover:text-slate-950'}`}
              href="#main"
              key={section.label}
            >
              <span aria-hidden="true" className="w-5 text-center text-base">{section.icon}</span>
              {section.label}
            </a>
          ))}
        </nav>

        <div className="mt-auto hidden rounded-xl bg-slate-50 p-4 lg:block">
          <span className="text-xs font-semibold uppercase tracking-[0.14em] text-slate-500">Состояние</span>
          <p className="mt-2 flex items-center gap-2 text-sm font-medium text-slate-700">
            <span aria-hidden="true" className="size-2 rounded-full bg-emerald-500" />
            Панель готова к настройке
          </p>
        </div>
      </aside>

      <main className="mx-auto w-full max-w-7xl px-5 py-8 sm:px-8 lg:px-12 lg:py-12" id="main">
        <header className="flex flex-wrap items-start justify-between gap-5 border-b border-slate-200 pb-7">
          <div>
            <p className="text-sm font-medium text-blue-700">Панель управления</p>
            <h1 className="mt-2 text-3xl font-semibold tracking-tight sm:text-4xl">Добро пожаловать в Jasta</h1>
            <p className="mt-3 max-w-2xl text-sm leading-6 text-slate-600 sm:text-base">
              Единое место для конфигурации сайтов, публикаций и доменов.
            </p>
          </div>
          <button className="rounded-lg border border-slate-300 bg-white px-4 py-2.5 text-sm font-semibold text-slate-700 shadow-sm transition hover:border-slate-400 hover:bg-slate-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-700" type="button">
            Войти
          </button>
        </header>

        <section aria-labelledby="setup-title" className="mt-8 overflow-hidden rounded-2xl border border-blue-100 bg-white shadow-sm">
          <div className="grid gap-8 p-6 sm:p-8 lg:grid-cols-[1fr_auto] lg:items-center lg:p-10">
            <div className="max-w-2xl">
              <span className="inline-flex items-center gap-2 rounded-full bg-amber-50 px-3 py-1 text-xs font-semibold text-amber-800">
                <span aria-hidden="true" className="size-1.5 rounded-full bg-amber-500" />
                Первоначальная настройка
              </span>
              <h2 className="mt-5 text-2xl font-semibold tracking-tight" id="setup-title">Ваш контент будет здесь</h2>
              <p className="mt-3 text-sm leading-6 text-slate-600 sm:text-base">
                Создайте сайт, укажите домен и опубликуйте первый архив. Jasta подготовит версии контента и поможет настроить HTTPS.
              </p>
              <button className="mt-6 inline-flex items-center gap-2 rounded-lg bg-blue-700 px-4 py-2.5 text-sm font-semibold text-white shadow-sm transition hover:bg-blue-800 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-700" type="button">
                <span aria-hidden="true" className="text-lg leading-none">+</span>
                Создать сайт
              </button>
            </div>
            <div aria-hidden="true" className="hidden size-44 place-items-center rounded-full bg-blue-50 lg:grid">
              <div className="grid size-28 place-items-center rounded-3xl border border-blue-100 bg-white text-5xl text-blue-700 shadow-sm">⌘</div>
            </div>
          </div>
        </section>

        <section aria-labelledby="capabilities-title" className="mt-10">
          <div className="flex flex-wrap items-end justify-between gap-3">
            <div>
              <h2 className="text-lg font-semibold tracking-tight" id="capabilities-title">Возможности панели</h2>
              <p className="mt-1 text-sm text-slate-500">Все этапы публикации в одном рабочем пространстве</p>
            </div>
            <span className="text-xs font-medium uppercase tracking-[0.14em] text-slate-400">Jasta admin</span>
          </div>
          <div className="mt-4 grid gap-3 md:grid-cols-3">
            {capabilities.map((item) => (
              <article className="rounded-xl border border-slate-200 bg-white p-5 shadow-sm" key={item.number}>
                <span className="text-xs font-semibold tracking-[0.12em] text-blue-700">{item.number}</span>
                <h3 className="mt-4 font-semibold">{item.title}</h3>
                <p className="mt-2 text-sm leading-5 text-slate-600">{item.detail}</p>
              </article>
            ))}
          </div>
        </section>
      </main>
    </div>
  )
}

export default App
