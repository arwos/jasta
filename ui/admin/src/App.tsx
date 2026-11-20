import { useEffect, useState, type FormEvent } from 'react'
import { AdminApiError, adminApi } from './api'
import Dashboard from './Dashboard'

function App() {
  const [authenticated, setAuthenticated] = useState(false)
  const [sessionExpired, setSessionExpired] = useState(false)

  useEffect(() => {
    const handleExpiredSession = () => {
      setAuthenticated(false)
      setSessionExpired(true)
    }
    window.addEventListener('jasta:session-expired', handleExpiredSession)
    return () => window.removeEventListener('jasta:session-expired', handleExpiredSession)
  }, [])

  if (authenticated) {
    return <Welcome onLogout={async () => {
      await adminApi.logout().catch(() => undefined)
      setAuthenticated(false)
    }} />
  }

  return <LoginForm sessionExpired={sessionExpired} onAuthenticated={() => {
    setSessionExpired(false)
    setAuthenticated(true)
  }} />
}

function LoginForm({ onAuthenticated, sessionExpired }: { onAuthenticated: () => void; sessionExpired: boolean }) {
  const [password, setPassword] = useState('')
  const [secondFactor, setSecondFactor] = useState('')
  const [error, setError] = useState('')
  const [pending, setPending] = useState(false)

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError('')
    setPending(true)

    try {
      await adminApi.login(password, secondFactor)
      setPassword('')
      setSecondFactor('')
      onAuthenticated()
    } catch (requestError) {
      setPassword('')
      setSecondFactor('')
      setError(loginErrorMessage(requestError))
    } finally {
      setPending(false)
    }
  }

  return (
    <main className="min-h-screen bg-[#f3f5f8] p-4 text-slate-950 sm:grid sm:place-items-center sm:p-8">
      <div className="mx-auto grid w-full max-w-5xl overflow-hidden rounded-3xl border border-slate-200 bg-white shadow-[0_24px_80px_-36px_rgba(15,23,42,0.35)] lg:min-h-[620px] lg:grid-cols-[1.02fr_0.98fr]">
        <section className="relative hidden overflow-hidden bg-[#101b34] p-10 text-white lg:flex lg:flex-col lg:justify-between xl:p-14">
          <div aria-hidden="true" className="absolute -right-28 -top-24 size-96 rounded-full border border-white/10" />
          <div aria-hidden="true" className="absolute -right-12 -top-8 size-64 rounded-full border border-white/10" />
          <div aria-hidden="true" className="absolute -bottom-32 -left-20 size-80 rounded-full bg-blue-600/20 blur-3xl" />
          <a className="relative flex w-fit items-center gap-3 rounded-lg focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-white" href="#login" aria-label="Jasta">
            <span className="grid size-11 place-items-center rounded-xl bg-blue-600 text-xl font-bold">J</span>
            <span>
              <span className="block text-lg font-semibold tracking-tight">jasta</span>
              <span className="block text-xs text-slate-400">control plane</span>
            </span>
          </a>

          <div className="relative max-w-md py-12">
            <p className="text-sm font-semibold uppercase tracking-[0.18em] text-blue-300">Панель владельца</p>
            <h1 className="mt-5 text-4xl font-semibold leading-tight tracking-[-0.04em] xl:text-5xl">
              Ваш контент. Ваши правила.
            </h1>
            <p className="mt-5 max-w-sm text-base leading-7 text-slate-300">
              Управляйте статическими сайтами, публикациями и доменами из одного безопасного места.
            </p>
            <div className="mt-10 flex items-center gap-3 text-sm text-slate-300">
              <span className="grid size-8 place-items-center rounded-full border border-white/15 text-blue-200" aria-hidden="true">✓</span>
              Доступ защищён паролем и TOTP
            </div>
          </div>

          <p className="relative text-xs text-slate-500">Локальная панель управления Jasta</p>
        </section>

        <section className="flex flex-col justify-center px-6 py-10 sm:px-12 lg:px-12 xl:px-16" id="login">
          <div className="mx-auto w-full max-w-sm">
            <a className="flex w-fit items-center gap-2 rounded-lg text-sm font-semibold text-slate-700 focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-blue-700 lg:hidden" href="#login">
              <span className="grid size-8 place-items-center rounded-lg bg-blue-700 text-sm text-white">J</span>
              jasta
            </a>
            <p className="mt-8 text-sm font-semibold text-blue-700 lg:mt-0">Добро пожаловать</p>
            <h2 className="mt-2 text-3xl font-semibold tracking-tight">Вход владельца</h2>
            <p className="mt-3 text-sm leading-6 text-slate-600">
              Введите пароль и одноразовый код из приложения-аутентификатора.
            </p>

            {sessionExpired ? (
              <p aria-live="polite" className="mt-5 rounded-lg border border-blue-100 bg-blue-50 px-3.5 py-3 text-sm text-blue-900" role="status">
                Сессия завершена. Войдите снова.
              </p>
            ) : null}

            <form className="mt-8 space-y-5" onSubmit={submit}>
              <div>
                <label className="mb-2 block text-sm font-medium text-slate-800" htmlFor="owner-password">Пароль</label>
                <input
                  autoComplete="current-password"
                  className="w-full rounded-xl border border-slate-300 bg-white px-3.5 py-3 text-base shadow-sm outline-none transition placeholder:text-slate-400 focus:border-blue-600 focus:ring-4 focus:ring-blue-600/10 disabled:bg-slate-50"
                  disabled={pending}
                  id="owner-password"
                  name="password"
                  onChange={(event) => setPassword(event.target.value)}
                  placeholder="Пароль владельца"
                  required
                  type="password"
                  value={password}
                />
              </div>

              <div>
                <label className="mb-2 block text-sm font-medium text-slate-800" htmlFor="owner-totp">Код аутентификатора</label>
                <input
                  autoComplete="one-time-code"
                  className="w-full rounded-xl border border-slate-300 bg-white px-3.5 py-3 font-mono text-base tracking-[0.28em] shadow-sm outline-none transition placeholder:font-sans placeholder:text-sm placeholder:tracking-normal placeholder:text-slate-400 focus:border-blue-600 focus:ring-4 focus:ring-blue-600/10 disabled:bg-slate-50"
                  disabled={pending}
                  id="owner-totp"
                  inputMode="numeric"
                  maxLength={6}
                  name="second_factor"
                  onChange={(event) => setSecondFactor(event.target.value.replace(/\D/g, '').slice(0, 6))}
                  pattern="[0-9]{6}"
                  placeholder="Шесть цифр"
                  required
                  type="text"
                  value={secondFactor}
                />
                <p className="mt-2 text-xs leading-5 text-slate-500">Код обновляется каждые 30 секунд.</p>
              </div>

              {error ? (
                <p aria-live="polite" className="rounded-lg border border-rose-200 bg-rose-50 px-3.5 py-3 text-sm text-rose-800" role="alert">{error}</p>
              ) : null}

              <button
                className="flex w-full cursor-pointer items-center justify-center gap-2 rounded-xl bg-blue-700 px-4 py-3 text-sm font-semibold text-white shadow-sm transition hover:bg-blue-800 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-700 disabled:cursor-wait disabled:bg-blue-400"
                disabled={pending || password.length === 0 || secondFactor.length !== 6}
                type="submit"
              >
                {pending ? 'Проверяем данные…' : 'Войти в панель'}
                {!pending ? <span aria-hidden="true">→</span> : null}
              </button>
            </form>

            <div className="mt-8 border-t border-slate-200 pt-5">
              <p className="text-xs leading-5 text-slate-500">
                Первичная настройка TOTP и recovery codes выполняется локальной командой <code className="rounded bg-slate-100 px-1 py-0.5 font-mono text-[11px] text-slate-700">jasta owner-bootstrap</code>.
              </p>
            </div>
          </div>
        </section>
      </div>
    </main>
  )
}

function Welcome({ onLogout }: { onLogout: () => void }) {
  return <Dashboard onLogout={onLogout} />
}

function loginErrorMessage(error: unknown): string {
  if (error instanceof AdminApiError && error.code === -32001) {
    return 'Неверный пароль или код приложения-аутентификатора.'
  }
  if (error instanceof AdminApiError) {
    return error.message
  }
  return 'Не удалось выполнить вход. Попробуйте ещё раз.'
}

export default App
