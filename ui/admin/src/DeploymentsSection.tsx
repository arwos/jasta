import { useState, type FormEvent } from 'react'
import {
  AdminApiError,
  adminApi,
  type ApplicationType,
  type JobProgress,
  type RegistrySource,
  type ReleaseHistory,
  type SiteConfiguration,
  type SiteSummary,
} from './api'

type JobResult = {
  job_id: string
  status?: string
  error_code?: string
  release?: { id: string; created_at: string }
}

function DeploymentsSection({ sites, selectedSite, selectedID, onSelect, configuration, sources, history, historyLoading, onRefresh, onActivate, onRollback }: {
  sites: SiteSummary[]
  selectedSite: SiteSummary | null
  selectedID: string
  onSelect: (id: string) => void
  configuration: SiteConfiguration | null
  sources: RegistrySource[]
  history: ReleaseHistory | null
  historyLoading: boolean
  onRefresh: (siteID: string) => Promise<void>
  onActivate: (siteID: string, releaseID: string) => Promise<void>
  onRollback: (siteID: string, releaseID: string) => Promise<void>
}) {
  const [file, setFile] = useState<File | null>(null)
  const [sourceID, setSourceID] = useState('')
  const [busy, setBusy] = useState(false)
  const [job, setJob] = useState<JobProgress | null>(null)
  const [jobID, setJobID] = useState('')
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [progressUnavailable, setProgressUnavailable] = useState(false)
  const [rollbackTarget, setRollbackTarget] = useState<{ id: string; siteName: string } | null>(null)
  const [rollbackBusy, setRollbackBusy] = useState(false)

  async function runJob(action: () => Promise<JobResult>) {
    if (!selectedSite || !configuration) return
    setBusy(true)
    setJob(null)
    setJobID('')
    setError('')
    setNotice('')
    setProgressUnavailable(false)
    const startedAt = Date.now()
    const events: JobProgress[] = []
    let closeProgress: (() => void) | null = null
    try {
      try {
        closeProgress = await adminApi.connectProgress((event) => {
          if ((event.kind !== 'upload' && event.kind !== 'deploy') || Date.parse(event.created_at) < startedAt - 1000) return
          events.push(event)
          setJobID(event.job_id)
          setJob(event)
        })
      } catch {
        setProgressUnavailable(true)
      }

      const result = await action()
      setJobID(result.job_id)
      const latest = events.filter((event) => event.job_id === result.job_id).at(-1)
      if (latest) setJob(latest)
      if (result.status === 'failed') {
        setError(jobErrorMessage(result.error_code, selectedSite.app_type, configuration))
      } else if (result.release) {
        setNotice(`Релиз ${result.release.id} собран и сохранён. Он начнёт обслуживаться после активации.`)
      } else {
        setNotice('Архив получен и опубликован как новый релиз. Проверьте его в истории перед активацией.')
      }
      await onRefresh(selectedSite.id).catch(() => {
        setError('Релиз создан, но историю не удалось обновить. Повторите обновление истории.')
      })
    } catch (requestError) {
      if (requestError instanceof AdminApiError && requestError.jobID) setJobID(requestError.jobID)
      const failureEvent = events.filter((event) => event.job_id === (requestError instanceof AdminApiError ? requestError.jobID : '') && event.status === 'failed').at(-1)
      setError(failureEvent
        ? jobErrorMessage(failureEvent.error_code, selectedSite.app_type, configuration)
        : errorMessage(requestError, selectedSite.app_type, configuration))
    } finally {
      closeProgress?.()
      setBusy(false)
    }
  }

  async function submitUpload(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!selectedSite || !configuration || !file) return
    await runJob(() => adminApi.uploadArchive(selectedSite.id, configuration.config, file))
  }

  async function submitSource(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!selectedSite || !configuration) return
    await runJob(() => adminApi.createRelease(selectedSite.id, sourceID, configuration.config))
  }

  async function createRedirectRelease() {
    if (!selectedSite || !configuration) return
    await runJob(() => adminApi.createRelease(selectedSite.id, '', configuration.config))
  }

  async function activate(siteID: string, releaseID: string) {
    setError('')
    setNotice('')
    try {
      await onActivate(siteID, releaseID)
    } catch (requestError) {
      setError(errorMessage(requestError))
    }
  }

  async function confirmRollback() {
    if (!rollbackTarget || !selectedSite) return
    setRollbackBusy(true)
    setError('')
    setNotice('')
    try {
      await onRollback(selectedSite.id, rollbackTarget.id)
      setRollbackTarget(null)
    } catch (requestError) {
      setError(errorMessage(requestError))
    } finally {
      setRollbackBusy(false)
    }
  }

  const activeReleaseID = history?.current_release_id || selectedSite?.active_release_id || ''
  const availableSources = sources.filter((source) => source.auth_type === 'none' || source.auth_type === 'basic' || source.auth_type === 'bearer' || source.auth_type === 'query')

  return (
    <div className="mt-7 space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h2 className="text-xl font-semibold tracking-tight">Публикации сайта</h2>
          <p className="mt-1 text-sm text-slate-600">Новые релизы сохраняются отдельно и не активируются автоматически.</p>
        </div>
        <label className="block min-w-56 text-sm font-medium text-slate-700">
          Выбранный сайт
          <select className="mt-2 w-full rounded-lg border border-slate-300 bg-white px-3 py-2.5 text-base shadow-sm outline-none focus:border-blue-600 focus:ring-4 focus:ring-blue-600/10" onChange={(event) => onSelect(event.target.value)} value={selectedID}>
            {sites.map((site) => <option key={site.id} value={site.id}>{site.name}</option>)}
          </select>
        </label>
      </div>

      {error ? <p aria-live="polite" className="rounded-lg border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-800" role="alert">{error}</p> : null}
      {notice ? <p aria-live="polite" className="rounded-lg border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-900" role="status">{notice}</p> : null}
      {progressUnavailable ? <p className="rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900" role="status">Канал прогресса пока недоступен. Дождитесь ответа сервера; результат публикации будет показан здесь.</p> : null}
      {job ? <ProgressCard event={job} jobID={jobID} /> : null}

      {!selectedSite ? <EmptyDeployments /> : (
        <>
          <section className="grid gap-4 xl:grid-cols-2">
            {selectedSite.app_type === 'rpa' ? (
              <article className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm sm:p-6">
                <p className="text-sm font-semibold text-blue-700">Redirect Page Application</p>
                <h3 className="mt-2 text-lg font-semibold">Создать конфигурационный релиз</h3>
                <p className="mt-2 text-sm leading-6 text-slate-600">Редирект-сайт не содержит архива. Jasta создаст релиз из сохранённого черновика правил перенаправления.</p>
                <ConfigRevision configuration={configuration} />
                <button className="mt-5 cursor-pointer rounded-lg bg-blue-700 px-4 py-2.5 text-sm font-semibold text-white hover:bg-blue-800 disabled:cursor-wait disabled:bg-blue-400" disabled={busy || !configuration} onClick={() => void createRedirectRelease()} type="button">{busy ? 'Создаём релиз…' : 'Создать релиз правил'}</button>
              </article>
            ) : (
              <>
                <form className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm sm:p-6" onSubmit={submitUpload}>
                  <p className="text-sm font-semibold text-blue-700">Загрузка файла</p>
                  <h3 className="mt-2 text-lg font-semibold">Загрузить ZIP или TAR.GZ</h3>
                  <p className="mt-2 text-sm leading-6 text-slate-600">Архив будет проверен и собран с сохранённой ревизией конфигурации.</p>
                  <ConfigRevision configuration={configuration} />
                  <label className="mt-5 block text-sm font-medium text-slate-800">
                    Файл сборки
                    <input accept=".zip,.tar.gz,application/zip,application/gzip" className="mt-2 block w-full cursor-pointer rounded-lg border border-slate-300 bg-white text-sm text-slate-700 file:mr-4 file:cursor-pointer file:border-0 file:bg-slate-100 file:px-4 file:py-2.5 file:text-sm file:font-semibold file:text-slate-700 hover:file:bg-slate-200" onChange={(event) => setFile(event.target.files?.[0] ?? null)} required type="file" />
                  </label>
                  {file ? <p className="mt-2 text-xs text-slate-500">{file.name} · {formatBytes(file.size)}</p> : null}
                  <button className="mt-5 cursor-pointer rounded-lg bg-blue-700 px-4 py-2.5 text-sm font-semibold text-white hover:bg-blue-800 disabled:cursor-not-allowed disabled:bg-blue-300" disabled={busy || !file || !configuration} type="submit">{busy ? 'Публикуем…' : 'Загрузить и собрать релиз'}</button>
                </form>

                <form className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm sm:p-6" onSubmit={submitSource}>
                  <p className="text-sm font-semibold text-blue-700">Registry URL</p>
                  <h3 className="mt-2 text-lg font-semibold">Скачать из источника</h3>
                  <p className="mt-2 text-sm leading-6 text-slate-600">Выберите настроенный источник. Его URL и секреты остаются на сервере.</p>
                  <ConfigRevision configuration={configuration} />
                  {availableSources.length > 0 ? (
                    <label className="mt-5 block text-sm font-medium text-slate-800">
                      Источник архива
                      <select className="mt-2 w-full rounded-lg border border-slate-300 bg-white px-3 py-2.5 text-base shadow-sm outline-none focus:border-blue-600 focus:ring-4 focus:ring-blue-600/10" onChange={(event) => setSourceID(event.target.value)} required value={sourceID}>
                        <option value="">Выберите источник</option>
                        {availableSources.map((source) => <option key={source.id} value={source.id}>{source.name} · {authName(source.auth_type)}</option>)}
                      </select>
                    </label>
                  ) : (
                    <p className="mt-5 rounded-lg border border-amber-200 bg-amber-50 px-3.5 py-3 text-sm leading-5 text-amber-900">Источников пока нет. Создайте registry в разделе «Источники»; URL должен оканчиваться на .zip или .tar.gz.</p>
                  )}
                  <button className="mt-5 cursor-pointer rounded-lg border border-blue-700 px-4 py-2.5 text-sm font-semibold text-blue-800 hover:bg-blue-50 disabled:cursor-not-allowed disabled:border-slate-300 disabled:text-slate-400" disabled={busy || !sourceID || !configuration} type="submit">{busy ? 'Скачиваем и собираем…' : 'Скачать и собрать релиз'}</button>
                </form>
              </>
            )}
          </section>

          <section className="overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-sm">
            <div className="flex flex-wrap items-end justify-between gap-4 border-b border-slate-200 px-5 py-5 sm:px-7">
              <div>
                <h3 className="text-lg font-semibold tracking-tight">История релизов</h3>
                <p className="mt-1 text-sm text-slate-500">{history ? `${history.releases.length} релизов · ${formatBytes(history.total_bytes)} на диске` : 'Загружаем историю…'}</p>
              </div>
              {activeReleaseID ? <span className="rounded-full bg-emerald-50 px-3 py-1 text-xs font-semibold text-emerald-800">Активен {shortID(activeReleaseID)}</span> : <span className="rounded-full bg-amber-50 px-3 py-1 text-xs font-semibold text-amber-800">Нет активного релиза</span>}
            </div>
            {historyLoading ? <p className="p-6 text-sm text-slate-600" role="status">Загружаем релизы…</p> : null}
            {!historyLoading && history?.releases.length === 0 ? <p className="p-6 text-sm text-slate-600">Пока нет собранных релизов. Первый релиз появится здесь после загрузки архива.</p> : null}
            {!historyLoading && history && history.releases.length > 0 ? (
              <div className="overflow-x-auto">
                <table className="w-full min-w-[800px] text-left text-sm">
                  <thead className="bg-slate-50 text-xs uppercase tracking-wide text-slate-500"><tr><th className="px-5 py-3 font-semibold sm:px-7">Релиз</th><th className="px-5 py-3 font-semibold">Источник</th><th className="px-5 py-3 font-semibold">Конфиг</th><th className="px-5 py-3 font-semibold">Размер</th><th className="px-5 py-3 font-semibold">Создан</th><th className="px-5 py-3 font-semibold">Действие</th></tr></thead>
                  <tbody className="divide-y divide-slate-100">
                    {history.releases.map((release) => (
                      <tr key={release.id}>
                        <td className="px-5 py-4 sm:px-7"><span className="block font-semibold text-slate-900">{shortID(release.id)}</span><span className="mt-1 block text-xs text-slate-500">{release.status}</span>{release.active ? <span className="mt-2 inline-flex rounded-full bg-emerald-50 px-2 py-0.5 text-xs font-semibold text-emerald-800">Активный</span> : null}</td>
                        <td className="px-5 py-4 text-slate-600">{releaseSourceName(release.source_kind, release.archive_name)}</td>
                        <td className="px-5 py-4 text-slate-600">Ревизия {release.config_revision}</td>
                        <td className="px-5 py-4 text-slate-600">{formatBytes(release.disk_bytes)}</td>
                        <td className="px-5 py-4 text-slate-600">{new Date(release.created_at).toLocaleString('ru-RU')}</td>
                        <td className="px-5 py-4">
                          {release.status === 'ready' && !release.active ? (
                            <div className="flex flex-col items-start gap-2">
                              <button className="cursor-pointer text-sm font-semibold text-blue-700 hover:text-blue-900 disabled:opacity-50" disabled={busy || rollbackBusy} onClick={() => void activate(selectedSite.id, release.id)} type="button">Активировать</button>
                              {activeReleaseID ? <button className="cursor-pointer text-sm font-semibold text-rose-700 hover:text-rose-900 disabled:opacity-50" disabled={busy || rollbackBusy} onClick={() => setRollbackTarget({ id: release.id, siteName: selectedSite.name })} type="button">Откатить на эту версию</button> : null}
                            </div>
                          ) : <span className="text-xs text-slate-400">—</span>}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : null}
          </section>
        </>
      )}

      {rollbackTarget ? <RollbackDialog target={rollbackTarget} busy={rollbackBusy} onCancel={() => setRollbackTarget(null)} onConfirm={() => void confirmRollback()} /> : null}
    </div>
  )
}

function ConfigRevision({ configuration }: { configuration: SiteConfiguration | null }) {
  if (!configuration) return <p className="mt-4 rounded-lg border border-amber-200 bg-amber-50 px-3.5 py-3 text-sm text-amber-900">Сначала дождитесь загрузки конфигурации.</p>
  return <p className="mt-4 rounded-lg bg-slate-50 px-3.5 py-3 text-xs leading-5 text-slate-600">В релиз попадёт сохранённая конфигурация, ревизия {configuration.revision}. Несохранённые изменения не включаются.</p>
}

function ProgressCard({ event, jobID }: { event: JobProgress | null; jobID: string }) {
  const stage = event?.status === 'succeeded' ? 'Релиз успешно собран' : event?.status === 'failed' ? 'Публикация завершилась с ошибкой' : event ? stageName(event.stage) : 'Ожидаем первое событие…'
  return (
    <section aria-live="polite" className="rounded-xl border border-blue-200 bg-blue-50 p-4 sm:p-5" role="status">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-sm font-semibold text-blue-950">{stage}</p>
        {event ? <span className="text-sm font-semibold text-blue-800">{event.progress}%</span> : null}
      </div>
      <progress aria-label="Прогресс публикации" className="mt-3 h-2 w-full accent-blue-700" max={100} value={event?.progress ?? 0} />
      <p className="mt-2 text-xs text-blue-900">{jobID || event?.job_id ? `Задание ${shortID(jobID || event?.job_id || '')}` : 'Подключаемся к заданию'}</p>
    </section>
  )
}

function RollbackDialog({ target, busy, onCancel, onConfirm }: { target: { id: string; siteName: string }; busy: boolean; onCancel: () => void; onConfirm: () => void }) {
  return (
    <div className="fixed inset-0 z-20 grid place-items-center bg-slate-950/40 p-4" role="presentation">
      <section aria-labelledby="rollback-title" aria-modal="true" className="w-full max-w-md rounded-2xl bg-white p-6 shadow-xl" role="dialog">
        <p className="text-sm font-semibold text-rose-700">Подтверждение отката</p>
        <h3 className="mt-2 text-xl font-semibold tracking-tight" id="rollback-title">Восстановить предыдущую версию?</h3>
        <p className="mt-3 text-sm leading-6 text-slate-600">Для «{target.siteName}» будет активирован релиз {shortID(target.id)} вместе с его снимком конфигурации. Текущая версия останется в истории.</p>
        <div className="mt-6 flex justify-end gap-2">
          <button className="cursor-pointer rounded-lg border border-slate-300 px-4 py-2.5 text-sm font-semibold text-slate-700 hover:bg-slate-50" disabled={busy} onClick={onCancel} type="button">Отмена</button>
          <button className="cursor-pointer rounded-lg bg-rose-700 px-4 py-2.5 text-sm font-semibold text-white hover:bg-rose-800 disabled:cursor-wait disabled:bg-rose-400" disabled={busy} onClick={onConfirm} type="button">{busy ? 'Восстанавливаем…' : 'Подтвердить откат'}</button>
        </div>
      </section>
    </div>
  )
}

function EmptyDeployments() {
  return <div className="rounded-xl border border-dashed border-slate-300 bg-white p-7 text-sm leading-6 text-slate-600">Создайте сайт в разделе «Сайты и конфигурация», чтобы публиковать архивы.</div>
}

function stageName(stage: string): string {
  const labels: Record<string, string> = { queued: 'В очереди', received: 'Задание принято', download: 'Скачиваем архив', validate: 'Проверяем архив', build: 'Собираем релиз', publish: 'Сохраняем релиз' }
  return labels[stage] || 'Выполняем публикацию'
}

function releaseSourceName(source: string, archiveName?: string): string {
  if (source === 'upload') return archiveName || 'Загруженный архив'
  if (source === 'url') return archiveName || 'Registry URL'
  return 'Конфигурация Redirect'
}

function authName(value: RegistrySource['auth_type']): string {
  const names: Record<RegistrySource['auth_type'], string> = { none: 'публичный', basic: 'HTTP Basic', bearer: 'Bearer', query: 'подписанный URL' }
  return names[value]
}

function formatBytes(value: number): string {
  if (value < 1024) return `${value} Б`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} КБ`
  if (value < 1024 ** 3) return `${(value / 1024 ** 2).toFixed(1)} МБ`
  return `${(value / 1024 ** 3).toFixed(2)} ГБ`
}

function shortID(value: string): string {
  return value.length > 14 ? `${value.slice(0, 8)}…${value.slice(-4)}` : value
}

function jobErrorMessage(code: string | undefined, appType?: ApplicationType, configuration?: SiteConfiguration | null): string {
  const labels: Record<string, string> = {
    source_fetch_failed: 'Не удалось скачать архив из выбранного источника.',
    invalid_archive: 'Архив не прошёл проверку. Проверьте формат ZIP/TAR.GZ, лимиты размера и отсутствие небезопасных или повторяющихся путей.',
  }
  if (code === 'build_failed') {
    const root = configString(configuration?.config.root) || '.'
    const pages = configObject(configuration?.config.pages)
    const index = configString(pages.index) || 'index.html'
    const notFound = configString(pages.not_found) || '404.html'
    if (appType === 'mda') {
      const source = configString(configuration?.config.source) || '.'
      return `Не удалось собрать Markdown-сайт. Проверьте Markdown-файлы в «${source}»: у каждой страницы должен быть корректный уникальный slug; также проверьте шаблон templates/page.html. Если это настроено, проверьте доступ jasta admin к каталогу релизов и SQLite.`
    }
    return `Не удалось собрать релиз. Проверьте, что в каталоге «${root}» архива находятся ${index} и ${notFound}, а домены совпадают с конфигурацией сайта. Если всё верно, проверьте доступ jasta admin к каталогу релизов и SQLite.`
  }
  return labels[code || ''] || `Задание завершилось с ошибкой${code ? ` (${code})` : ''}.`
}

function errorMessage(error: unknown, appType?: ApplicationType, configuration?: SiteConfiguration | null): string {
  if (error instanceof AdminApiError && error.code === -32004) return 'Выбранный сайт или релиз не найден.'
  if (error instanceof AdminApiError && error.code === -32001) return 'Сессия завершена. Войдите снова.'
  if (error instanceof AdminApiError && error.jobErrorCode) return jobErrorMessage(error.jobErrorCode, appType, configuration)
  if (error instanceof AdminApiError && error.message === 'archive deployment failed') return 'Архив не опубликован. Подробности см. в задании ниже.'
  if (error instanceof AdminApiError) return error.message
  return 'Не удалось выполнить операцию. Попробуйте ещё раз.'
}

function configObject(value: unknown): Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value) ? value as Record<string, unknown> : {}
}

function configString(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

export default DeploymentsSection
