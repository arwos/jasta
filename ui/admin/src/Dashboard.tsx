import { useEffect, useState, type FormEvent } from 'react'
import {
  AdminApiError,
  adminApi,
  type ApplicationType,
  type CertificateSummary,
  type RegistrySource,
  type ReleaseHistory,
  type SiteConfiguration,
  type SiteSummary,
} from './api'
import DeploymentsSection from './DeploymentsSection'
import RegistrySourcesSection from './RegistrySourcesSection'
import TLSSection from './TLSSection'

type Section = 'overview' | 'sites' | 'sources' | 'deployments' | 'tls'
type Draft = Record<string, unknown>

const applicationNames: Record<ApplicationType, string> = {
  spa: 'Single Page Application',
  mpa: 'Multi Page Application',
  mda: 'Markdown Application',
  rpa: 'Redirect Page Application',
}

const sectionNames: Record<Section, string> = {
  overview: 'Обзор',
  sites: 'Сайты и конфигурация',
  sources: 'Источники',
  deployments: 'Публикации',
  tls: 'Домены и TLS',
}

function Dashboard({ onLogout }: { onLogout: () => void }) {
  const [section, setSection] = useState<Section>('overview')
  const [sites, setSites] = useState<SiteSummary[]>([])
  const [certificates, setCertificates] = useState<CertificateSummary[]>([])
  const [sources, setSources] = useState<RegistrySource[]>([])
  const [releaseHistory, setReleaseHistory] = useState<ReleaseHistory | null>(null)
  const [selectedID, setSelectedID] = useState('')
  const [configuration, setConfiguration] = useState<SiteConfiguration | null>(null)
  const [draft, setDraft] = useState<Draft | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')

  useEffect(() => {
    let current = true
    Promise.all([adminApi.listSites(), adminApi.listCertificates(), adminApi.listSources()])
      .then(([siteItems, certificateItems, sourceItems]) => {
        if (!current) return
        setSites(siteItems)
        setCertificates(certificateItems)
        setSources(sourceItems)
        setSelectedID((previous) => previous || siteItems[0]?.id || '')
      })
      .catch((requestError: unknown) => {
        if (current) setError(errorMessage(requestError))
      })
      .finally(() => {
        if (current) setLoading(false)
      })
    return () => { current = false }
  }, [])

  useEffect(() => {
    if (!selectedID) return
    let current = true
    adminApi.getSiteConfiguration(selectedID)
      .then((value) => {
        if (!current) return
        setConfiguration(value)
        setDraft(value.config)
      })
      .catch((requestError: unknown) => {
        if (current) {
          setError(errorMessage(requestError))
        }
      })
    return () => { current = false }
  }, [selectedID])

  useEffect(() => {
    if (!selectedID) return
    let current = true
    adminApi.releaseHistory(selectedID)
      .then((value) => { if (current) setReleaseHistory(value) })
      .catch((requestError: unknown) => { if (current) setError(errorMessage(requestError)) })
    return () => { current = false }
  }, [selectedID])

  const selectedSite = sites.find((site) => site.id === selectedID) ?? null
  const displayedConfiguration = configuration?.site_id === selectedID ? configuration : null
  const displayedDraft = displayedConfiguration ? draft : null
  const configLoading = Boolean(selectedID && !displayedConfiguration && !error && !loading)
  const historyLoading = Boolean(selectedID && releaseHistory?.site_id !== selectedID && !error && !loading)
  const allDomains = sites.flatMap((site) => site.domains.map((domain) => ({ domain, site })))

  async function saveConfiguration(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!selectedSite || !draft) return
    setSaving(true)
    setError('')
    setNotice('')
    try {
      await adminApi.validateSiteConfiguration(selectedSite.app_type, draft)
      const result = await adminApi.updateSiteConfiguration(selectedSite.id, draft)
      setConfiguration({ site_id: result.site_id, app_type: selectedSite.app_type, revision: result.revision, config: draft })
      setDraft(draft)
      setSites((previous) => previous.map((site) => site.id === result.site_id ? { ...site, domains: result.domains, updated_at: new Date().toISOString() } : site))
      setNotice(`Черновик сохранён: ревизия ${result.revision}. Для применения опубликуйте новый релиз.`)
      void adminApi.listSites()
        .then(setSites)
        .catch(() => undefined)
    } catch (requestError) {
      setError(errorMessage(requestError))
    } finally {
      setSaving(false)
    }
  }

  async function createSite(name: string, appType: ApplicationType, config: Draft): Promise<SiteSummary> {
    const created = await adminApi.createSite(name, appType, config)
    setSites((previous) => [...previous.filter((site) => site.id !== created.id), created])
    setSelectedID(created.id)
    setError('')
    setNotice('Сайт создан. Конфигурация сохранена как черновик; для публикации загрузите архив.')
    void adminApi.listSites().then(setSites).catch(() => undefined)
    return created
  }

  async function refreshReleaseData(siteID: string) {
    const [nextHistory, nextSites] = await Promise.all([adminApi.releaseHistory(siteID), adminApi.listSites()])
    setReleaseHistory(nextHistory)
    setSites(nextSites)
  }

  async function refreshSources() {
    setSources(await adminApi.listSources())
  }

  async function refreshCertificates() {
    setCertificates(await adminApi.listCertificates())
  }

  async function activateRelease(siteID: string, releaseID: string) {
    await adminApi.activateRelease(siteID, releaseID)
    await refreshReleaseData(siteID)
    setNotice(`Релиз ${releaseID} активирован.`)
  }

  async function rollbackRelease(siteID: string, releaseID: string) {
    await adminApi.rollbackRelease(siteID, releaseID)
    await refreshReleaseData(siteID)
    setNotice(`Откат выполнен на релиз ${releaseID}. Контент и конфигурация восстановлены вместе.`)
  }

  return (
    <div className="min-h-screen bg-[#f4f6f8] text-slate-900 lg:grid lg:grid-cols-[250px_1fr]">
      <aside className="flex flex-col border-b border-slate-200 bg-white px-5 py-5 lg:min-h-screen lg:border-b-0 lg:border-r lg:px-6">
        <a className="flex w-fit items-center gap-3 rounded-lg focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-blue-700" href="#main" aria-label="Jasta, на главную">
          <span className="grid size-10 place-items-center rounded-xl bg-blue-700 text-lg font-bold text-white">J</span>
          <span>
            <span className="block text-base font-semibold tracking-tight">jasta</span>
            <span className="block text-xs text-slate-500">панель владельца</span>
          </span>
        </a>

        <nav aria-label="Главная навигация" className="mt-6 flex gap-2 overflow-x-auto lg:mt-9 lg:flex-col">
          {(Object.keys(sectionNames) as Section[]).map((item) => (
            <button
              aria-current={section === item ? 'page' : undefined}
              className={`shrink-0 cursor-pointer rounded-lg px-3 py-2.5 text-left text-sm font-medium transition focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-700 ${section === item ? 'bg-blue-50 text-blue-800' : 'text-slate-600 hover:bg-slate-100 hover:text-slate-950'}`}
              key={item}
              onClick={() => { setSection(item); setError(''); setNotice('') }}
              type="button"
            >
              {sectionNames[item]}
            </button>
          ))}
        </nav>

        <div className="mt-auto hidden rounded-xl bg-slate-50 p-4 lg:block">
          <span className="text-xs font-semibold uppercase tracking-[0.14em] text-slate-500">Подключение</span>
          <p className="mt-2 flex items-center gap-2 text-sm font-medium text-slate-700">
            <span aria-hidden="true" className="size-2 rounded-full bg-emerald-500" />
            Админка защищена TOTP
          </p>
        </div>
      </aside>

      <main className="mx-auto w-full max-w-7xl px-5 py-7 sm:px-8 lg:px-12 lg:py-10" id="main">
        <header className="flex flex-wrap items-center justify-between gap-4 border-b border-slate-200 pb-6">
          <div>
            <p className="text-sm font-medium text-blue-700">Панель управления</p>
            <h1 className="mt-1 text-2xl font-semibold tracking-tight sm:text-3xl">{sectionNames[section]}</h1>
          </div>
          <button className="cursor-pointer rounded-lg border border-slate-300 bg-white px-4 py-2.5 text-sm font-semibold text-slate-700 shadow-sm transition hover:bg-slate-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-700" onClick={onLogout} type="button">
            Выйти
          </button>
        </header>

        {error ? <p aria-live="polite" className="mt-5 rounded-lg border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-800" role="alert">{error}</p> : null}
        {notice ? <p aria-live="polite" className="mt-5 rounded-lg border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-900" role="status">{notice}</p> : null}
        {loading ? <p className="mt-8 text-sm text-slate-600" role="status">Загружаем данные…</p> : null}
        {!loading && section === 'overview' ? <Overview sites={sites} domains={allDomains} certificates={certificates} onOpenSites={() => setSection('sites')} /> : null}
        {!loading && section === 'sites' ? (
          <SitesSection
            sites={sites}
            selectedSite={selectedSite}
            selectedID={selectedID}
            onSelect={(id) => { setError(''); setNotice(''); setSelectedID(id) }}
            configuration={displayedConfiguration}
            draft={displayedDraft}
            configLoading={configLoading}
            saving={saving}
            onDraftChange={setDraft}
            onSubmit={saveConfiguration}
            onCreate={createSite}
          />
        ) : null}
        {!loading && section === 'deployments' ? (
          <DeploymentsSection
            sites={sites}
            selectedSite={selectedSite}
            selectedID={selectedID}
            onSelect={(id) => { setError(''); setNotice(''); setSelectedID(id) }}
            configuration={displayedConfiguration}
            sources={sources}
            history={releaseHistory?.site_id === selectedID ? releaseHistory : null}
            historyLoading={historyLoading}
            onRefresh={refreshReleaseData}
            onActivate={activateRelease}
            onRollback={rollbackRelease}
          />
        ) : null}
        {!loading && section === 'sources' ? <RegistrySourcesSection sources={sources} onRefresh={refreshSources} /> : null}
        {!loading && section === 'tls' ? <TLSSection domains={allDomains} certificates={certificates} onRefresh={refreshCertificates} /> : null}
      </main>
    </div>
  )
}

function Overview({ sites, domains, certificates, onOpenSites }: {
  sites: SiteSummary[]
  domains: { domain: string; site: SiteSummary }[]
  certificates: CertificateSummary[]
  onOpenSites: () => void
}) {
  const validCertificates = certificates.filter((certificate) => certificate.status === 'valid').length
  return (
    <div className="mt-7 space-y-7">
      <section className="grid gap-4 sm:grid-cols-3">
        <Metric label="Сайты" value={sites.length} detail="Конфигурации в системе" />
        <Metric label="Домены" value={domains.length} detail="Назначены сайтам" />
        <Metric label="Сертификаты" value={`${validCertificates} / ${certificates.length}`} detail="Действующие / всего" />
      </section>
      <section className="rounded-2xl border border-slate-200 bg-white p-6 shadow-sm sm:p-8">
        <div className="flex flex-wrap items-start justify-between gap-5">
          <div>
            <p className="text-sm font-semibold text-blue-700">Начало работы</p>
            <h2 className="mt-2 text-xl font-semibold tracking-tight">Настройте сайт и подготовьте публикацию</h2>
            <p className="mt-2 max-w-2xl text-sm leading-6 text-slate-600">Изменения конфигурации сохраняются как черновик. Они начинают обслуживаться после создания и активации релиза.</p>
          </div>
          <button className="cursor-pointer rounded-lg bg-blue-700 px-4 py-2.5 text-sm font-semibold text-white shadow-sm hover:bg-blue-800 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-700" onClick={onOpenSites} type="button">Открыть сайты</button>
        </div>
      </section>
      <section aria-labelledby="recent-sites-title">
        <div className="flex items-end justify-between gap-3">
          <div>
            <h2 className="text-lg font-semibold tracking-tight" id="recent-sites-title">Сайты</h2>
            <p className="mt-1 text-sm text-slate-500">Конфигурация и активный релиз</p>
          </div>
          <button className="cursor-pointer text-sm font-semibold text-blue-700 hover:text-blue-900" onClick={onOpenSites} type="button">Все сайты →</button>
        </div>
        <div className="mt-4 grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {sites.slice(0, 6).map((site) => <SiteCard site={site} key={site.id} />)}
          {sites.length === 0 ? <EmptySites /> : null}
        </div>
      </section>
    </div>
  )
}

function SitesSection({ sites, selectedSite, selectedID, onSelect, configuration, draft, configLoading, saving, onDraftChange, onSubmit, onCreate }: {
  sites: SiteSummary[]
  selectedSite: SiteSummary | null
  selectedID: string
  onSelect: (id: string) => void
  configuration: SiteConfiguration | null
  draft: Draft | null
  configLoading: boolean
  saving: boolean
  onDraftChange: (next: Draft | null) => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
  onCreate: (name: string, appType: ApplicationType, config: Draft) => Promise<SiteSummary>
}) {
  const [creatingSite, setCreatingSite] = useState(false)
  return (
    <div className="mt-7 grid gap-6 xl:grid-cols-[300px_1fr]">
      <section aria-labelledby="site-list-title">
        <div className="flex items-end justify-between gap-3">
          <div>
            <h2 className="text-lg font-semibold tracking-tight" id="site-list-title">Ваши сайты</h2>
            <p className="mt-1 text-sm text-slate-500">{sites.length} в системе</p>
          </div>
          <button className="cursor-pointer rounded-lg bg-blue-700 px-3 py-2 text-sm font-semibold text-white hover:bg-blue-800" onClick={() => setCreatingSite((value) => !value)} type="button">
            {creatingSite ? 'Закрыть' : 'Добавить сайт'}
          </button>
        </div>
        <div className="mt-4 space-y-2">
          {sites.map((site) => (
            <button
              aria-pressed={selectedID === site.id}
              className={`w-full cursor-pointer rounded-xl border p-4 text-left transition focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-700 ${selectedID === site.id ? 'border-blue-300 bg-blue-50 shadow-sm' : 'border-slate-200 bg-white hover:border-slate-300'}`}
              key={site.id}
              onClick={() => onSelect(site.id)}
              type="button"
            >
              <span className="block font-semibold text-slate-900">{site.name}</span>
              <span className="mt-1 block text-xs font-medium text-blue-700">{applicationNames[site.app_type]}</span>
              <span className="mt-3 block truncate text-xs text-slate-500">{site.domains.join(', ') || 'Домены не настроены'}</span>
            </button>
          ))}
          {sites.length === 0 ? <EmptySites /> : null}
        </div>
      </section>

      <section aria-labelledby="config-title" className="min-w-0">
        {creatingSite ? (
          <CreateSiteForm
            onCancel={() => setCreatingSite(false)}
            onCreate={async (name, appType, config) => {
              const created = await onCreate(name, appType, config)
              setCreatingSite(false)
              return created
            }}
          />
        ) : !selectedSite ? (
          <div className="rounded-2xl border border-dashed border-slate-300 bg-white p-8 text-center text-sm text-slate-600">Выберите сайт для просмотра конфигурации.</div>
        ) : (
          <>
            <div className="flex flex-wrap items-start justify-between gap-4">
              <div>
                <p className="text-sm font-medium text-blue-700">{applicationNames[selectedSite.app_type]}</p>
                <h2 className="mt-1 text-2xl font-semibold tracking-tight" id="config-title">{selectedSite.name}</h2>
                <p className="mt-1 text-sm text-slate-500">Активный релиз: {selectedSite.active_release_id || 'ещё не опубликован'}</p>
              </div>
              {configuration ? <span className="rounded-full bg-slate-100 px-3 py-1 text-xs font-semibold text-slate-700">Ревизия {configuration.revision}</span> : null}
            </div>
            {configLoading ? <p className="mt-6 text-sm text-slate-600" role="status">Загружаем конфигурацию…</p> : null}
            {draft && !configLoading ? (
              <form className="mt-5 rounded-2xl border border-slate-200 bg-white p-5 shadow-sm sm:p-7" onSubmit={onSubmit}>
                <ConfigFields appType={selectedSite.app_type} value={draft} onChange={onDraftChange} />
                <div className="mt-7 flex flex-wrap items-center justify-between gap-4 border-t border-slate-200 pt-5">
                  <p className="max-w-xl text-xs leading-5 text-slate-500">Сохранение создаёт черновую ревизию и обновляет список доменов. Активный сайт изменится после публикации нового релиза.</p>
                  <button className="cursor-pointer rounded-lg bg-blue-700 px-4 py-2.5 text-sm font-semibold text-white shadow-sm hover:bg-blue-800 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-700 disabled:cursor-wait disabled:bg-blue-400" disabled={saving} type="submit">
                    {saving ? 'Проверяем и сохраняем…' : 'Проверить и сохранить черновик'}
                  </button>
                </div>
              </form>
            ) : null}
          </>
        )}
      </section>
    </div>
  )
}

function CreateSiteForm({ onCancel, onCreate }: {
  onCancel: () => void
  onCreate: (name: string, appType: ApplicationType, config: Draft) => Promise<SiteSummary>
}) {
  const [name, setName] = useState('')
  const [appType, setAppType] = useState<ApplicationType>('spa')
  const [draft, setDraft] = useState<Draft>(() => defaultConfig('spa'))
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setSaving(true)
    setError('')
    try {
      await adminApi.validateSiteConfiguration(appType, draft)
      await onCreate(name, appType, draft)
    } catch (requestError) {
      setError(errorMessage(requestError))
    } finally {
      setSaving(false)
    }
  }

  return (
    <form className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm sm:p-7" onSubmit={submit}>
      <div>
        <p className="text-sm font-medium text-blue-700">Новая конфигурация</p>
        <h2 className="mt-1 text-2xl font-semibold tracking-tight" id="config-title">Создать сайт</h2>
        <p className="mt-1 text-sm text-slate-500">Укажите название, тип приложения и параметры сайта.</p>
      </div>
      <div className="mt-6 grid gap-4 sm:grid-cols-2">
        <label className="block text-sm font-medium text-slate-800">
          Название
          <input className={inputClass} maxLength={128} onChange={(event) => setName(event.target.value)} placeholder="Документация" required value={name} />
        </label>
        <label className="block text-sm font-medium text-slate-800">
          Тип приложения
          <select className={inputClass} onChange={(event) => {
            const nextType = event.target.value as ApplicationType
            setAppType(nextType)
            setDraft(defaultConfig(nextType))
          }} value={appType}>
            {(Object.keys(applicationNames) as ApplicationType[]).map((type) => <option key={type} value={type}>{applicationNames[type]}</option>)}
          </select>
        </label>
      </div>
      <div className="mt-7"><ConfigFields appType={appType} value={draft} onChange={setDraft} /></div>
      {error ? <p aria-live="polite" className="mt-4 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-800" role="alert">{error}</p> : null}
      <div className="mt-7 flex flex-wrap items-center justify-end gap-2 border-t border-slate-200 pt-5">
        <button className="cursor-pointer rounded-lg border border-slate-300 px-3 py-2.5 text-sm font-semibold text-slate-700 hover:bg-slate-50" onClick={onCancel} type="button">Отмена</button>
        <button className="cursor-pointer rounded-lg bg-blue-700 px-4 py-2.5 text-sm font-semibold text-white shadow-sm hover:bg-blue-800 disabled:cursor-wait disabled:bg-blue-400" disabled={saving} type="submit">{saving ? 'Создаём…' : 'Создать сайт'}</button>
      </div>
    </form>
  )
}

function ConfigFields({ appType, value, onChange }: { appType: ApplicationType; value: Draft; onChange: (draft: Draft) => void }) {
  if (appType === 'rpa') {
    const redirects = Array.isArray(value.redirects) ? value.redirects as Draft[] : []
    function updateRule(index: number, field: string, nextValue: string | number) {
      const updated = redirects.map((rule, ruleIndex) => ruleIndex === index ? { ...rule, [field]: nextValue } : rule)
      onChange({ ...value, redirects: updated })
    }
    return (
      <div>
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h3 className="font-semibold">Правила перенаправления</h3>
            <p className="mt-1 text-sm text-slate-500">Путь и query исходного запроса сохраняются.</p>
          </div>
          <button className="cursor-pointer rounded-lg border border-slate-300 px-3 py-2 text-sm font-semibold text-slate-700 hover:bg-slate-50" onClick={() => onChange({ ...value, redirects: [...redirects, { domain: '', url_from: '/', url_to: 'https://', status_code: 301 }] })} type="button">Добавить правило</button>
        </div>
        <div className="mt-5 space-y-4">
          {redirects.map((rule, index) => (
            <fieldset className="grid gap-4 rounded-xl border border-slate-200 bg-slate-50/70 p-4 sm:grid-cols-2" key={`${String(rule.domain)}-${index}`}>
              <legend className="px-1 text-xs font-semibold text-slate-600">Правило {index + 1}</legend>
              <Field label="Домен" value={stringValue(rule.domain)} onChange={(next) => updateRule(index, 'domain', next)} placeholder="old.example.com" />
              <Field label="Исходный путь" value={stringValue(rule.url_from)} onChange={(next) => updateRule(index, 'url_from', next)} placeholder="/old-path" />
              <Field label="Целевой URL" value={stringValue(rule.url_to)} onChange={(next) => updateRule(index, 'url_to', next)} placeholder="https://new.example.com/path" />
              <label className="block text-sm font-medium text-slate-800">
                Код ответа
                <select className={inputClass} value={Number(rule.status_code) === 308 ? 308 : 301} onChange={(event) => updateRule(index, 'status_code', Number(event.target.value))}>
                  <option value={301}>301 — постоянный redirect</option>
                  <option value={308}>308 — сохранить метод запроса</option>
                </select>
              </label>
              <button className="cursor-pointer text-left text-sm font-semibold text-rose-700 hover:text-rose-900 sm:col-span-2" onClick={() => onChange({ ...value, redirects: redirects.filter((_, ruleIndex) => ruleIndex !== index) })} type="button">Удалить правило</button>
            </fieldset>
          ))}
          {redirects.length === 0 ? <p className="rounded-lg border border-dashed border-slate-300 p-5 text-sm text-slate-600">Добавьте хотя бы одно правило перенаправления.</p> : null}
        </div>
      </div>
    )
  }

  const domains = Array.isArray(value.domains) ? value.domains.map(String).join('\n') : ''
  const assets = objectValue(value.assets)
  const pages = objectValue(value.pages)
  return (
    <div className="space-y-6">
      <div>
        <h3 className="font-semibold">Основные параметры</h3>
        <p className="mt-1 text-sm text-slate-500">Домены и пути, используемые при сборке и публикации сайта.</p>
      </div>
      <label className="block text-sm font-medium text-slate-800">
        Домены сайта
        <textarea className={`${inputClass} min-h-24 font-mono text-sm`} onChange={(event) => onChange({ ...value, domains: event.target.value.split('\n').map((domain) => domain.trim()).filter(Boolean) })} placeholder={'www.example.com\nexample.com'} value={domains} />
        <span className="mt-1 block text-xs font-normal text-slate-500">Один домен на строку.</span>
      </label>
      <Field label="Корневой каталог сайта" value={stringValue(value.root)} onChange={(next) => onChange({ ...value, root: next })} placeholder="." />
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Путь к статическим файлам" value={stringValue(assets.path)} onChange={(next) => onChange({ ...value, assets: { ...assets, path: next } })} placeholder="assets" />
        <Field label="TTL кэша (секунды)" inputMode="numeric" value={stringValue(assets.cache_ttl ?? 0)} onChange={(next) => onChange({ ...value, assets: { ...assets, cache_ttl: Number(next) || 0 } })} placeholder="0" />
        <Field label="Файл главной страницы" value={stringValue(pages.index)} onChange={(next) => onChange({ ...value, pages: { ...pages, index: next } })} placeholder="index.html" />
        <Field label="Файл страницы 404" value={stringValue(pages.not_found)} onChange={(next) => onChange({ ...value, pages: { ...pages, not_found: next } })} placeholder="404.html" />
      </div>
      <p className="rounded-lg border border-blue-100 bg-blue-50 px-3.5 py-3 text-xs leading-5 text-blue-900">
        {appType === 'spa' ? 'SPA возвращает index.html для маршрутов без отдельного файла.' : appType === 'mpa' ? 'MPA ищет index.html в каталоге URL; отсутствующие страницы остаются 404.' : 'Markdown-файлы должны содержать front matter со slug; архив также должен включать templates/page.html. Публикация собирает готовый HTML.'}
      </p>
    </div>
  )
}

const inputClass = 'mt-2 w-full rounded-lg border border-slate-300 bg-white px-3 py-2.5 text-base font-normal shadow-sm outline-none focus:border-blue-600 focus:ring-4 focus:ring-blue-600/10'

function Field({ label, value, onChange, placeholder, inputMode }: { label: string; value: string; onChange: (value: string) => void; placeholder?: string; inputMode?: 'numeric' }) {
  return (
    <label className="block text-sm font-medium text-slate-800">
      {label}
      <input className={inputClass} inputMode={inputMode} onChange={(event) => onChange(event.target.value)} placeholder={placeholder} type="text" value={value} />
    </label>
  )
}

function Metric({ label, value, detail }: { label: string; value: string | number; detail: string }) {
  return <article className="rounded-xl border border-slate-200 bg-white p-5 shadow-sm"><p className="text-sm font-medium text-slate-600">{label}</p><p className="mt-2 text-3xl font-semibold tracking-tight">{value}</p><p className="mt-1 text-xs text-slate-500">{detail}</p></article>
}

function SiteCard({ site }: { site: SiteSummary }) {
  return <article className="rounded-xl border border-slate-200 bg-white p-5 shadow-sm"><div className="flex items-start justify-between gap-3"><div><h3 className="font-semibold">{site.name}</h3><p className="mt-1 text-xs text-blue-700">{applicationNames[site.app_type]}</p></div><span aria-hidden="true" className={`mt-1 size-2.5 rounded-full ${site.active_release_id ? 'bg-emerald-500' : 'bg-amber-400'}`} /></div><p className="mt-4 truncate text-sm text-slate-600">{site.domains.join(', ') || 'Домены не настроены'}</p><p className="mt-3 text-xs text-slate-500">{site.active_release_id ? `Релиз ${site.active_release_id}` : 'Нет активного релиза'}</p></article>
}

function EmptySites() {
  return <div className="rounded-xl border border-dashed border-slate-300 bg-white p-6 text-sm leading-6 text-slate-600">Сайты пока не созданы. Перейдите в раздел «Сайты и конфигурация» и добавьте первый сайт.</div>
}

function stringValue(value: unknown): string {
  return typeof value === 'string' || typeof value === 'number' ? String(value) : ''
}

function defaultConfig(appType: ApplicationType): Draft {
  if (appType === 'rpa') {
    return { redirects: [{ domain: '', url_from: '/', url_to: 'https://', status_code: 301 }] }
  }
  return {
    domains: [],
    root: '.',
    assets: { path: 'assets', cache_ttl: 0 },
    pages: { index: 'index.html', not_found: '404.html' },
    ...(appType === 'mda' ? { source: '' } : {}),
  }
}

function objectValue(value: unknown): Draft {
  return typeof value === 'object' && value !== null && !Array.isArray(value) ? value as Draft : {}
}

function errorMessage(error: unknown): string {
  if (error instanceof AdminApiError && error.code === -32004) return 'Запрошенный сайт не найден.'
  if (error instanceof AdminApiError && error.code === -32602) return error.message === 'invalid params' ? 'Сервер не принял конфигурацию. Проверьте обязательные поля и домены.' : error.message
  if (error instanceof AdminApiError) return error.message
  return 'Не удалось загрузить данные. Попробуйте ещё раз.'
}

export default Dashboard
