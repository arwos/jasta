import { useEffect, useRef, useState } from 'react'
import { AdminApiError, adminApi, type CertificateSummary, type JobProgress, type SiteSummary } from './api'

type DomainEntry = { domain: string; site: SiteSummary }

function TLSSection({ domains, certificates, onRefresh }: { domains: DomainEntry[]; certificates: CertificateSummary[]; onRefresh: () => Promise<void> }) {
  const [busyDomain, setBusyDomain] = useState('')
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [jobID, setJobID] = useState('')
  const [job, setJob] = useState<JobProgress | null>(null)
  const [progressUnavailable, setProgressUnavailable] = useState(false)
  const closeProgress = useRef<(() => void) | null>(null)
  const expectedJobID = useRef('')
  const bufferedEvents = useRef<JobProgress[]>([])
  const handledTerminalJobs = useRef(new Set<string>())

  useEffect(() => () => closeProgress.current?.(), [])

  function finishJob(event: JobProgress) {
    if (handledTerminalJobs.current.has(event.job_id)) return
    handledTerminalJobs.current.add(event.job_id)
    setJob(event)
    setBusyDomain('')
    closeProgress.current?.()
    closeProgress.current = null
    if (event.status === 'succeeded') {
      setNotice(`Сертификат для задания ${shortID(event.job_id)} выпущен и применён.`)
      void onRefresh().catch(() => setError('Сертификат обработан, но список не обновился. Обновите его вручную.'))
    } else if (event.status === 'failed') {
      setNotice('')
      setError(certificateJobError(event.error_code))
    }
  }

  async function issue(domain: string, kind: 'self_signed' | 'letsencrypt') {
    setBusyDomain(domain)
    setError('')
    setNotice('')
    setJob(null)
    setJobID('')
    setProgressUnavailable(false)
    expectedJobID.current = ''
    bufferedEvents.current = []
    handledTerminalJobs.current.clear()
    closeProgress.current?.()
    closeProgress.current = null
    let connected = false
    try {
      if (kind === 'self_signed') {
        const certificate = await adminApi.issueSelfSignedCertificate(domain)
        setBusyDomain('')
        setNotice(`Самоподписанный сертификат для ${certificate.domain} создан.`)
        try {
          await onRefresh()
        } catch {
          setError('Сертификат создан, но список не обновился. Нажмите «Обновить».')
        }
        return
      }

      try {
        closeProgress.current = await adminApi.connectProgress((event) => {
          bufferedEvents.current.push(event)
          if (event.job_id !== expectedJobID.current) return
          setJob(event)
          if (event.status === 'succeeded' || event.status === 'failed') finishJob(event)
        })
        connected = true
      } catch {
        setProgressUnavailable(true)
      }

      const started = await adminApi.issueLetsEncryptCertificate(domain)
      expectedJobID.current = started.job_id
      setJobID(started.job_id)
      const last = bufferedEvents.current.filter((event) => event.job_id === started.job_id).at(-1)
      if (!connected) {
        setBusyDomain('')
        setNotice(`Let’s Encrypt принял задание для ${started.domain} (${shortID(started.job_id)}). Состояние WebSocket недоступно; обновите сертификаты после завершения.`)
      } else if (last && (last.status === 'succeeded' || last.status === 'failed')) {
        finishJob(last)
      } else {
        if (last) setJob(last)
        setNotice(`Let’s Encrypt выполняет задание для ${started.domain}.`)
      }
    } catch (requestError) {
      closeProgress.current?.()
      closeProgress.current = null
      setBusyDomain('')
      setError(certificateRequestError(requestError))
    }
  }

  const byDomain = new Map(certificates.map((certificate) => [certificate.domain, certificate]))
  return (
    <div className="mt-7 space-y-5">
      <section className="overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-sm">
        <div className="flex flex-wrap items-end justify-between gap-4 border-b border-slate-200 px-5 py-5 sm:px-7">
          <div>
            <h2 className="text-lg font-semibold tracking-tight">Домены и сертификаты</h2>
            <p className="mt-1 text-sm text-slate-500">Выдача Let’s Encrypt использует HTTP-01 и требует доступного извне порта 80.</p>
          </div>
          <button className="cursor-pointer rounded-lg border border-slate-300 px-3.5 py-2 text-sm font-semibold text-slate-700 hover:bg-slate-50 disabled:opacity-50" disabled={Boolean(busyDomain)} onClick={() => { setError(''); void onRefresh().catch(() => setError('Не удалось обновить список сертификатов.')) }} type="button">Обновить</button>
        </div>
        {error ? <p aria-live="polite" className="mx-5 mt-4 rounded-lg border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-800 sm:mx-7" role="alert">{error}</p> : null}
        {notice ? <p aria-live="polite" className="mx-5 mt-4 rounded-lg border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-900 sm:mx-7" role="status">{notice}</p> : null}
        {progressUnavailable ? <p className="mx-5 mt-4 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900 sm:mx-7" role="status">Канал прогресса недоступен. Задание выпуска всё равно отправлено; после завершения нажмите «Обновить».</p> : null}
        {job ? <div className="mx-5 mt-4 rounded-lg border border-blue-200 bg-blue-50 px-4 py-3 sm:mx-7" role="status"><div className="flex justify-between gap-3 text-sm font-semibold text-blue-950"><span>{job.status === 'failed' ? 'Выпуск завершился с ошибкой' : job.status === 'succeeded' ? 'Сертификат применён' : certificateStage(job.stage)}</span><span>{job.progress}%</span></div><progress aria-label="Прогресс выпуска сертификата" className="mt-2 h-2 w-full accent-blue-700" max={100} value={job.progress} /><p className="mt-1 text-xs text-blue-900">Задание {shortID(jobID)}</p></div> : null}
        {domains.length === 0 ? <p className="p-6 text-sm text-slate-600">Домены пока не настроены.</p> : (
          <div className="mt-4 overflow-x-auto">
            <table className="w-full min-w-[900px] text-left text-sm">
              <thead className="bg-slate-50 text-xs uppercase tracking-wide text-slate-500"><tr><th className="px-5 py-3 font-semibold sm:px-7">Домен</th><th className="px-5 py-3 font-semibold">Сайт</th><th className="px-5 py-3 font-semibold">Сертификат</th><th className="px-5 py-3 font-semibold">Срок действия</th><th className="px-5 py-3 font-semibold">Действия</th></tr></thead>
              <tbody className="divide-y divide-slate-100">
                {domains.map(({ domain, site }) => {
                  const certificate = byDomain.get(domain)
                  const local = isLocalCertificateDomain(domain)
                  return (
                    <tr key={`${site.id}-${domain}`}>
                      <td className="px-5 py-4 font-medium text-slate-900 sm:px-7">{domain}</td>
                      <td className="px-5 py-4 text-slate-600">{site.name}</td>
                      <td className="px-5 py-4"><StatusPill status={certificate?.status || 'missing'} label={certificate ? issuerName(certificate.issuer) : 'Не выпущен'} /></td>
                      <td className="px-5 py-4 text-slate-600">{certificate?.not_after ? new Date(certificate.not_after).toLocaleDateString('ru-RU') : '—'}</td>
                      <td className="px-5 py-4">
                        {local ? certificate ? <span className="text-xs text-slate-500">Локальный сертификат уже существует</span> : <button className="cursor-pointer whitespace-nowrap text-sm font-semibold text-blue-700 hover:text-blue-900 disabled:opacity-50" disabled={Boolean(busyDomain)} onClick={() => void issue(domain, 'self_signed')} type="button">{busyDomain === domain ? 'Создаём…' : 'Создать self-signed'}</button> : (
                          <button className="cursor-pointer whitespace-nowrap text-sm font-semibold text-blue-700 hover:text-blue-900 disabled:opacity-50" disabled={Boolean(busyDomain)} onClick={() => void issue(domain, 'letsencrypt')} type="button">{busyDomain === domain ? 'Выпускаем…' : certificate ? 'Выпустить / обновить Let’s Encrypt' : 'Выпустить Let’s Encrypt'}</button>
                        )}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
        {certificates.some((certificate) => !domains.some((entry) => entry.domain === certificate.domain)) ? (
          <div className="border-t border-slate-200 px-5 py-5 sm:px-7">
            <h3 className="text-sm font-semibold text-slate-800">Сертификаты других доменов</h3>
            <ul className="mt-3 space-y-2 text-sm text-slate-600">
              {certificates.filter((certificate) => !domains.some((entry) => entry.domain === certificate.domain)).map((certificate) => <li className="flex flex-wrap items-center justify-between gap-3" key={certificate.id}><span>{certificate.domain} · {issuerName(certificate.issuer)}</span><StatusPill status={certificate.status} label={statusName(certificate.status)} /></li>)}
            </ul>
          </div>
        ) : null}
      </section>
    </div>
  )
}

function isLocalCertificateDomain(domain: string): boolean {
  const normalized = domain.toLowerCase().replace(/^\[|\]$/g, '')
  if (['localhost', 'local', 'test', 'example', 'invalid', 'home.arpa'].some((suffix) => normalized === suffix || normalized.endsWith(`.${suffix}`))) return true
  const parts = normalized.split('.').map(Number)
  if (parts.length === 4 && parts.every((value) => Number.isInteger(value) && value >= 0 && value <= 255)) {
    return parts[0] === 10 || parts[0] === 127 || parts[0] === 192 && parts[1] === 168 || parts[0] === 172 && parts[1] >= 16 && parts[1] <= 31 || parts[0] === 169 && parts[1] === 254
  }
  if (normalized.includes(':')) return normalized === '::1' || normalized.startsWith('fc') || normalized.startsWith('fd') || normalized.startsWith('fe80:')
  return !normalized.includes('.')
}

function certificateRequestError(error: unknown): string {
  if (!(error instanceof AdminApiError)) return 'Не удалось выполнить операцию с сертификатом.'
  if (error.code === -32001) return 'Сессия завершена. Войдите снова.'
  if (error.code === -32009) return 'Для этого домена сертификат уже существует. Обновите список.'
  if (error.code === -32011) return 'В конфигурации сервера нужно принять условия Let’s Encrypt перед выпуском.'
  if (error.code === -32010) return 'Не удалось создать сертификат.'
  if (error.code === -32602) return 'Домен не подходит для выбранного типа сертификата.'
  return 'Nginx или центр сертификации отклонил операцию. Секреты и системные пути скрыты.'
}

function certificateJobError(code?: string): string {
  const messages: Record<string, string> = {
    nginx_apply_failed: 'Не удалось применить конфигурацию Nginx. Проверьте его состояние и попробуйте снова.',
    acme_failed: 'Центр сертификации не выдал сертификат. Проверьте DNS домена и доступность HTTP-01 по порту 80.',
    invalid_domain: 'Домен не подходит для Let’s Encrypt.',
    terms_not_accepted: 'В конфигурации сервера нужно принять условия Let’s Encrypt.',
    certificate_store_failed: 'Сертификат получен, но не удалось сохранить его метаданные.',
    certificate_restore_failed: 'Не удалось восстановить предыдущую конфигурацию сертификата. Проверьте состояние Nginx.',
    job_update_failed: 'Сертификат установлен, но состояние задания не удалось обновить.',
  }
  return messages[code || ''] || 'Операция завершилась ошибкой. Системные детали и секреты скрыты.'
}

function certificateStage(value: string): string {
  const names: Record<string, string> = { validate: 'Проверяем домен', nginx_apply: 'Подготавливаем Nginx', account: 'Подключаем ACME', challenge: 'Проверяем HTTP-01', issue: 'Получаем сертификат', apply_nginx: 'Применяем HTTPS' }
  return names[value] || 'Выпускаем сертификат'
}

function shortID(value: string): string {
  return value.length > 14 ? `${value.slice(0, 8)}…${value.slice(-4)}` : value
}

function StatusPill({ status, label }: { status: string; label: string }) {
  const colors = status === 'valid' ? 'bg-emerald-50 text-emerald-800' : status === 'expiring' || status === 'pending' ? 'bg-amber-50 text-amber-800' : status === 'missing' ? 'bg-slate-100 text-slate-600' : 'bg-rose-50 text-rose-800'
  return <span className={`inline-flex rounded-full px-2.5 py-1 text-xs font-semibold ${colors}`}>{label}</span>
}

function issuerName(issuer: string): string {
  return issuer === 'letsencrypt' ? 'Let’s Encrypt' : issuer === 'self_signed' ? 'Самоподписанный' : issuer
}

function statusName(status: string): string {
  const labels: Record<string, string> = { valid: 'Действует', expiring: 'Скоро истекает', expired: 'Истёк', invalid: 'Недействителен', pending: 'Выпускается' }
  return labels[status] || status
}

export default TLSSection
