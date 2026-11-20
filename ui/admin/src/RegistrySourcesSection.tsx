import { useState, type FormEvent } from 'react'
import { AdminApiError, adminApi, type RegistryAuthType, type RegistrySource, type RegistrySourceInput } from './api'

type Draft = RegistrySourceInput

const emptyDraft: Draft = { name: '', url: '', auth_type: 'none' }

function RegistrySourcesSection({ sources, onRefresh }: { sources: RegistrySource[]; onRefresh: () => Promise<void> }) {
  const [draft, setDraft] = useState<Draft>(emptyDraft)
  const [editingID, setEditingID] = useState('')
  const [deleteTarget, setDeleteTarget] = useState<RegistrySource | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')

  function edit(source: RegistrySource) {
    setEditingID(source.id)
    setDraft({ name: source.name, url: '', auth_type: source.auth_type })
    setError('')
    setNotice('')
  }

  function reset() {
    setEditingID('')
    setDraft(emptyDraft)
  }

  function updateDraft(key: keyof Draft, value: string) {
    setDraft((current) => ({ ...current, [key]: value }))
  }

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBusy(true)
    setError('')
    setNotice('')
    try {
      const source = normalizeSourceDraft(draft)
      if (editingID) await adminApi.updateSource(editingID, source)
      else await adminApi.createSource(source)
      const wasEditing = Boolean(editingID)
      reset()
      setNotice(wasEditing ? 'Источник обновлён. URL и секреты не раскрываются API.' : 'Источник создан. URL и секреты не раскрываются API.')
      try {
        await onRefresh()
      } catch {
        setError('Источник сохранён, но список не обновился. Перезагрузите страницу.')
      }
    } catch (requestError) {
      setError(sourceError(requestError))
    } finally {
      setBusy(false)
    }
  }

  async function removeSource() {
    if (!deleteTarget) return
    setBusy(true)
    setError('')
    setNotice('')
    try {
      await adminApi.deleteSource(deleteTarget.id)
      const deletedName = deleteTarget.name
      if (editingID === deleteTarget.id) reset()
      setDeleteTarget(null)
      setNotice(`Источник «${deletedName}» удалён.`)
      try {
        await onRefresh()
      } catch {
        setError('Источник удалён, но список не обновился. Обновите страницу.')
      }
    } catch (requestError) {
      setError(sourceError(requestError))
    } finally {
      setBusy(false)
    }
  }

  const authType = draft.auth_type
  return (
    <div className="mt-7 space-y-6">
      <div>
        <h2 className="text-xl font-semibold tracking-tight">Источники архивов</h2>
        <p className="mt-1 text-sm text-slate-600">URL и учётные данные хранятся на сервере в зашифрованном виде. API возвращает только название и тип доступа.</p>
      </div>
      {error ? <p aria-live="polite" className="rounded-lg border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-800" role="alert">{error}</p> : null}
      {notice ? <p aria-live="polite" className="rounded-lg border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-900" role="status">{notice}</p> : null}

      <section className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_minmax(320px,0.9fr)]">
        <form className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm sm:p-6" onSubmit={save}>
          <p className="text-sm font-semibold text-blue-700">{editingID ? 'Редактирование' : 'Новый registry'}</p>
          <h3 className="mt-2 text-lg font-semibold">{editingID ? 'Обновить источник' : 'Добавить источник архива'}</h3>
          {editingID ? <p className="mt-2 rounded-lg border border-amber-200 bg-amber-50 px-3.5 py-3 text-sm leading-5 text-amber-900">Сохранённый URL и секрет нельзя прочитать. Введите URL полностью заново; для Basic/Bearer также укажите новый пароль или токен.</p> : null}
          <div className="mt-5 space-y-4">
            <label className="block text-sm font-medium text-slate-800">
              Название
              <input autoComplete="off" className={inputClass} maxLength={128} onChange={(event) => updateDraft('name', event.target.value)} required value={draft.name} />
            </label>
            <label className="block text-sm font-medium text-slate-800">
              HTTPS URL архива
              <input autoComplete="url" className={`${inputClass} font-mono text-sm`} onChange={(event) => updateDraft('url', event.target.value)} placeholder="https://registry.example.com/builds/site.zip" required spellCheck={false} type="url" value={draft.url} />
              <span className="mt-1 block text-xs font-normal text-slate-500">Поддерживаются .zip и .tar.gz; подпись query-параметров хранится зашифрованно.</span>
            </label>
            <label className="block text-sm font-medium text-slate-800">
              Авторизация
              <select className={inputClass} onChange={(event) => setDraft((current) => ({ ...current, auth_type: event.target.value as RegistryAuthType, username: '', password: '', bearer_token: '' }))} value={authType}>
                <option value="none">Публичный URL</option>
                <option value="basic">HTTP Basic</option>
                <option value="bearer">Bearer token</option>
                <option value="query">Подписанный URL (query)</option>
              </select>
            </label>
            {authType === 'basic' ? (
              <div className="grid gap-4 sm:grid-cols-2">
                <label className="block text-sm font-medium text-slate-800">Имя пользователя<input autoComplete="off" className={inputClass} onChange={(event) => updateDraft('username', event.target.value)} required value={draft.username || ''} /></label>
                <label className="block text-sm font-medium text-slate-800">Пароль<input autoComplete="new-password" className={inputClass} onChange={(event) => updateDraft('password', event.target.value)} required type="password" value={draft.password || ''} /></label>
              </div>
            ) : null}
            {authType === 'bearer' ? <label className="block text-sm font-medium text-slate-800">Bearer token<input autoComplete="new-password" className={`${inputClass} font-mono text-sm`} onChange={(event) => updateDraft('bearer_token', event.target.value)} required type="password" value={draft.bearer_token || ''} /></label> : null}
          </div>
          <div className="mt-6 flex flex-wrap gap-2">
            <button className="cursor-pointer rounded-lg bg-blue-700 px-4 py-2.5 text-sm font-semibold text-white hover:bg-blue-800 disabled:cursor-wait disabled:bg-blue-400" disabled={busy} type="submit">{busy ? 'Сохраняем…' : editingID ? 'Сохранить изменения' : 'Добавить источник'}</button>
            {editingID ? <button className="cursor-pointer rounded-lg border border-slate-300 px-4 py-2.5 text-sm font-semibold text-slate-700 hover:bg-slate-50" disabled={busy} onClick={reset} type="button">Отмена</button> : null}
          </div>
        </form>

        <section className="overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-sm">
          <div className="border-b border-slate-200 px-5 py-5 sm:px-6">
            <h3 className="text-lg font-semibold tracking-tight">Сохранённые источники</h3>
            <p className="mt-1 text-sm text-slate-500">{sources.length} источников настроено</p>
          </div>
          {sources.length === 0 ? <p className="p-6 text-sm leading-6 text-slate-600">Добавьте публичный URL, приватный registry с Basic/Bearer или URL с подписью в query.</p> : (
            <ul className="divide-y divide-slate-100">
              {sources.map((source) => (
                <li className="flex flex-wrap items-center justify-between gap-3 px-5 py-4 sm:px-6" key={source.id}>
                  <div className="min-w-0">
                    <p className="truncate text-sm font-semibold text-slate-900">{source.name}</p>
                    <p className="mt-1 text-xs text-slate-500">{authLabel(source.auth_type)} · изменён {new Date(source.updated_at).toLocaleDateString('ru-RU')}</p>
                  </div>
                  <div className="flex shrink-0 gap-3">
                    <button className="cursor-pointer text-sm font-semibold text-blue-700 hover:text-blue-900 disabled:opacity-50" disabled={busy} onClick={() => edit(source)} type="button">Изменить</button>
                    <button className="cursor-pointer text-sm font-semibold text-rose-700 hover:text-rose-900 disabled:opacity-50" disabled={busy} onClick={() => setDeleteTarget(source)} type="button">Удалить</button>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </section>
      </section>

      {deleteTarget ? <DeleteDialog source={deleteTarget} busy={busy} onCancel={() => setDeleteTarget(null)} onConfirm={() => void removeSource()} /> : null}
    </div>
  )
}

function normalizeSourceDraft(value: Draft): Draft {
  const result: Draft = { name: value.name.trim(), url: value.url.trim(), auth_type: value.auth_type }
  if (value.auth_type === 'basic') {
    result.username = value.username?.trim()
    result.password = value.password
  }
  if (value.auth_type === 'bearer') result.bearer_token = value.bearer_token
  return result
}

function DeleteDialog({ source, busy, onCancel, onConfirm }: { source: RegistrySource; busy: boolean; onCancel: () => void; onConfirm: () => void }) {
  return (
    <div className="fixed inset-0 z-20 grid place-items-center bg-slate-950/40 p-4" role="presentation">
      <section aria-labelledby="source-delete-title" aria-modal="true" className="w-full max-w-md rounded-2xl bg-white p-6 shadow-xl" role="dialog">
        <p className="text-sm font-semibold text-rose-700">Удаление источника</p>
        <h3 className="mt-2 text-xl font-semibold tracking-tight" id="source-delete-title">Удалить «{source.name}»?</h3>
        <p className="mt-3 text-sm leading-6 text-slate-600">Секреты и URL будут удалены. Сохранённые релизы сайта не изменятся.</p>
        <div className="mt-6 flex justify-end gap-2">
          <button className="cursor-pointer rounded-lg border border-slate-300 px-4 py-2.5 text-sm font-semibold text-slate-700 hover:bg-slate-50" disabled={busy} onClick={onCancel} type="button">Отмена</button>
          <button className="cursor-pointer rounded-lg bg-rose-700 px-4 py-2.5 text-sm font-semibold text-white hover:bg-rose-800 disabled:cursor-wait disabled:bg-rose-400" disabled={busy} onClick={onConfirm} type="button">{busy ? 'Удаляем…' : 'Удалить источник'}</button>
        </div>
      </section>
    </div>
  )
}

function sourceError(error: unknown): string {
  if (error instanceof AdminApiError && error.code === -32009) return 'Источник с таким названием уже есть или данные не прошли проверку.'
  if (error instanceof AdminApiError && error.code === -32004) return 'Источник уже удалён. Обновите список.'
  if (error instanceof AdminApiError && error.code === -32001) return 'Сессия завершена. Войдите снова.'
  if (error instanceof AdminApiError && error.code === -32602) return 'Проверьте HTTPS URL, название и данные авторизации.'
  return 'Не удалось сохранить источник. Проверьте данные и повторите попытку.'
}

function authLabel(value: RegistryAuthType): string {
  const names: Record<RegistryAuthType, string> = { none: 'Публичный URL', basic: 'HTTP Basic', bearer: 'Bearer', query: 'Подписанный URL' }
  return names[value]
}

const inputClass = 'mt-2 w-full rounded-lg border border-slate-300 bg-white px-3 py-2.5 text-base font-normal shadow-sm outline-none focus:border-blue-600 focus:ring-4 focus:ring-blue-600/10'

export default RegistrySourcesSection
