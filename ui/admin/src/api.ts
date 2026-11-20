type TokenPair = {
  access_token: string
  access_expires_at: string
}

type RpcErrorShape = {
  code: number
  message: string
}

type RpcReply<T> = {
  id: string
  result?: T
  error?: RpcErrorShape
}

export type SiteSummary = {
  id: string
  name: string
  app_type: ApplicationType
  domains: string[]
  created_at: string
  updated_at: string
  active_release_id?: string
}

export type ApplicationType = 'spa' | 'mpa' | 'mda' | 'rpa'

export type SiteConfiguration = {
  site_id: string
  app_type: ApplicationType
  revision: number
  config: Record<string, unknown>
}

export type CertificateSummary = {
  id: string
  domain: string
  issuer: string
  status: string
  not_before?: string
  not_after?: string
}

export type RegistrySource = {
  id: string
  name: string
  auth_type: 'none' | 'basic' | 'bearer' | 'query'
  updated_at: string
}

export type RegistryAuthType = RegistrySource['auth_type']

export type RegistrySourceInput = {
  name: string
  url: string
  auth_type: RegistryAuthType
  username?: string
  password?: string
  bearer_token?: string
}

export type ReleaseRecord = {
  id: string
  config_snapshot_id: string
  config_revision: number
  source_kind: 'upload' | 'url' | 'config'
  archive_name?: string
  status: string
  created_at: string
  active: boolean
  disk_bytes: number
}

export type ReleaseHistory = {
  site_id: string
  current_release_id?: string
  current_config_snapshot_id?: string
  current_config_revision?: number
  total_bytes: number
  releases: ReleaseRecord[]
}

export type JobProgress = {
  sequence: number
  job_id: string
  kind: string
  stage: string
  status: 'queued' | 'running' | 'succeeded' | 'failed' | 'cancelled'
  progress: number
  error_code?: string
  created_at: string
}

export type CreateReleaseResult = {
  job_id: string
  status: 'succeeded' | 'failed'
  error_code?: string
  release?: {
    id: string
    site_id: string
    config_snapshot_id: string
    created_at: string
  }
}

export class AdminApiError extends Error {
  constructor(
    message: string,
    readonly code?: number,
    readonly jobID?: string,
    readonly jobErrorCode?: string,
  ) {
    super(message)
    this.name = 'AdminApiError'
  }
}

class AdminApi {
  private tokens: TokenPair | null = null
  private nextID = 0
  private refreshing: Promise<void> | null = null
  private refreshTimer: ReturnType<typeof setTimeout> | null = null

  async restoreSession(): Promise<boolean> {
    try {
      await this.refreshAccessToken(false)
      return true
    } catch (error) {
      if (error instanceof AdminApiError && error.code === -32001) {
        return false
      }
      throw error
    }
  }

  async login(password: string, secondFactor: string): Promise<void> {
    const tokens = await this.request<TokenPair>('auth.login', {
      password,
      second_factor: secondFactor,
    })
    this.setTokens(tokens)

    try {
      await this.call<{ subject: string }>('auth.me', {})
    } catch (error) {
      this.clear()
      throw error
    }
  }

  async logout(): Promise<void> {
    const current = this.tokens
    if (!current) {
      this.clear()
      return
    }

    try {
      try {
        await this.request<{ ok: boolean }>('auth.logout', {}, current.access_token)
      } catch (error) {
        if (!(error instanceof AdminApiError) || error.code !== -32001) {
          throw error
        }
        await this.refreshAccessToken()
        const refreshed = this.tokens
        if (refreshed) {
          await this.request<{ ok: boolean }>('auth.logout', {}, refreshed.access_token)
        }
      }
    } finally {
      this.clear()
    }
  }

  clear(): void {
    this.tokens = null
    this.refreshing = null
    if (this.refreshTimer) {
      clearTimeout(this.refreshTimer)
      this.refreshTimer = null
    }
  }

  async call<T>(method: string, params: object): Promise<T> {
    const current = this.tokens
    if (!current) {
      throw new AdminApiError('Требуется вход в систему', -32001)
    }

    try {
      return await this.request<T>(method, params, current.access_token)
    } catch (error) {
      if (!(error instanceof AdminApiError) || error.code !== -32001 || isAuthMethod(method)) {
        throw error
      }

      await this.refreshAccessToken()
      const refreshed = this.tokens
      if (!refreshed) {
        throw new AdminApiError('Сессия завершена. Войдите снова.', -32001)
      }
      return this.request<T>(method, params, refreshed.access_token)
    }
  }

  listSites(): Promise<SiteSummary[]> {
    return this.call('sites.list', {})
  }

  createSite(name: string, appType: ApplicationType, config: Record<string, unknown>): Promise<SiteSummary> {
    return this.call('sites.create', { name, app_type: appType, config })
  }

  getSiteConfiguration(siteID: string): Promise<SiteConfiguration> {
    return this.call('sites.config.get', { site_id: siteID })
  }

  listCertificates(): Promise<CertificateSummary[]> {
    return this.call('certificates.list', {})
  }

  listSources(): Promise<RegistrySource[]> {
    return this.call('sources.list', {})
  }

  createSource(source: RegistrySourceInput): Promise<{ id: string; name: string; auth_type: RegistryAuthType }> {
    return this.call('sources.create', source)
  }

  updateSource(id: string, source: RegistrySourceInput): Promise<{ id: string; name: string; auth_type: RegistryAuthType }> {
    return this.call('sources.update', { ...source, id })
  }

  async deleteSource(id: string): Promise<void> {
    await this.call<{ ok: boolean }>('sources.delete', { id })
  }

  issueSelfSignedCertificate(domain: string): Promise<CertificateSummary> {
    return this.call('certificates.issue_self_signed', { domain })
  }

  issueLetsEncryptCertificate(domain: string): Promise<{ job_id: string; domain: string }> {
    return this.call('certificates.issue_letsencrypt', { domain })
  }

  releaseHistory(siteID: string): Promise<ReleaseHistory> {
    return this.call('releases.history', { site_id: siteID })
  }

  createRelease(siteID: string, sourceID: string, config: Record<string, unknown>): Promise<CreateReleaseResult> {
    return this.call('releases.create', { site_id: siteID, source_id: sourceID, config })
  }

  async uploadArchive(siteID: string, config: Record<string, unknown>, file: File): Promise<{ job_id: string; release: CreateReleaseResult['release'] }> {
    const form = new FormData()
    form.set('site_id', siteID)
    form.set('config', JSON.stringify(config))
    form.set('archive', file, file.name)
    const response = await this.authenticatedFetch('/uploads', { method: 'POST', body: form })
    const body: unknown = await response.json().catch(() => null)
    if (!response.ok) {
      const details = objectValue(body)
      throw new AdminApiError(
        typeof details.error === 'string' ? details.error : 'Не удалось загрузить архив.',
        undefined,
        typeof details.job_id === 'string' ? details.job_id : undefined,
        typeof details.error_code === 'string' ? details.error_code : undefined,
      )
    }
    const value = objectValue(body)
    if (typeof value.job_id !== 'string' || typeof value.release !== 'object' || value.release === null) {
      throw new AdminApiError('Сервер вернул неполный ответ о публикации.')
    }
    return { job_id: value.job_id, release: value.release as CreateReleaseResult['release'] }
  }

  activateRelease(siteID: string, releaseID: string): Promise<{ site_id: string; release_id: string; snapshot_revision: number }> {
    return this.call('releases.activate', { site_id: siteID, release_id: releaseID })
  }

  rollbackRelease(siteID: string, releaseID: string): Promise<{ site_id: string; release_id: string; snapshot_revision: number }> {
    return this.call('releases.rollback', { site_id: siteID, release_id: releaseID })
  }

  async connectProgress(onEvent: (event: JobProgress) => void): Promise<() => void> {
    let closed = false
    let socket: WebSocket | null = null
    let cursor = 0
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null
    let reconnectAttempt = 0
    let initialResolve: (() => void) | null = null
    let initialReject: ((error: Error) => void) | null = null
    const ready = new Promise<void>((resolve, reject) => {
      initialResolve = resolve
      initialReject = reject
    })
    let handshakeTimer: ReturnType<typeof setTimeout> | null = null

    const connect = async (initial: boolean) => {
      if (closed) return
      try {
        const current = this.tokens
        if (!current) throw new AdminApiError('Сессия завершена. Войдите снова.', -32001)
        if (Date.parse(current.access_expires_at) - Date.now() < 30_000) await this.refreshAccessToken()
        const fresh = this.tokens
        if (!fresh) throw new AdminApiError('Сессия завершена. Войдите снова.', -32001)

        const scheme = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
        socket = new WebSocket(`${scheme}//${window.location.host}/ws`)
        socket.onopen = () => socket?.send(JSON.stringify({ e: 1, d: { access_token: fresh.access_token } }))
        socket.onmessage = (message) => {
          const frame = parseProgressFrame(message.data)
          if (!frame) return
          if (frame.e === 1) {
            if (frame.err || !frame.d?.authenticated) {
              initialReject?.(new AdminApiError('Не удалось подключиться к каналу прогресса.'))
              socket?.close()
              return
            }
            socket?.send(JSON.stringify({ e: 2, d: { after_sequence: cursor } }))
            return
          }
          if (frame.e === 2 && frame.d && 'events' in frame.d && Array.isArray(frame.d.events)) {
            for (const event of frame.d.events) acceptProgress(event)
            if (typeof frame.d.cursor === 'number') cursor = Math.max(cursor, frame.d.cursor)
            reconnectAttempt = 0
            initialResolve?.()
            initialResolve = null
            initialReject = null
            return
          }
          if (frame.e === 100 && frame.d) acceptProgress(frame.d as JobProgress)
        }
        socket.onerror = () => {
          if (initial) initialReject?.(new AdminApiError('Не удалось подключиться к каналу прогресса.'))
        }
        socket.onclose = () => {
          if (closed) return
          if (initial) initialReject?.(new AdminApiError('Соединение с каналом прогресса закрыто.'))
          const delay = Math.min(30_000, 500 * 2 ** reconnectAttempt++)
          reconnectTimer = setTimeout(() => { void connect(false) }, delay)
        }
      } catch (error) {
        if (initial) initialReject?.(error instanceof Error ? error : new Error('Progress connection failed'))
        else if (!closed) {
          const delay = Math.min(30_000, 500 * 2 ** reconnectAttempt++)
          reconnectTimer = setTimeout(() => { void connect(false) }, delay)
        }
      }
    }

    const acceptProgress = (event: JobProgress) => {
      if (typeof event.sequence !== 'number' || typeof event.job_id !== 'string') return
      if (event.sequence <= cursor) return
      cursor = event.sequence
      onEvent(event)
    }

    const close = () => {
      closed = true
      if (reconnectTimer) clearTimeout(reconnectTimer)
      socket?.close()
    }
    await connect(true)
    try {
      await Promise.race([
        ready,
        new Promise<void>((_, reject) => {
          handshakeTimer = setTimeout(() => reject(new AdminApiError('Не удалось подключиться к каналу прогресса.')), 5000)
        }),
      ])
    } catch (error) {
      close()
      throw error
    } finally {
      if (handshakeTimer) clearTimeout(handshakeTimer)
    }
    return close
  }

  validateSiteConfiguration(appType: ApplicationType, config: Record<string, unknown>): Promise<{ valid: boolean }> {
    return this.call('sites.config.validate', { app_type: appType, config })
  }

  updateSiteConfiguration(siteID: string, config: Record<string, unknown>): Promise<{
    site_id: string
    revision: number
    domains: string[]
    status: 'draft'
  }> {
    return this.call('sites.config.update', { site_id: siteID, config })
  }

  private async refreshAccessToken(notifyExpired = true): Promise<void> {
    if (this.refreshing) {
      return this.refreshing
    }
    this.refreshing = this.request<TokenPair>('auth.refresh', {})
      .then((tokens) => {
        this.setTokens(tokens)
      })
      .catch((error: unknown) => {
        this.clear()
        if (notifyExpired) {
          window.dispatchEvent(new Event('jasta:session-expired'))
        }
        throw error
      })
      .finally(() => {
        this.refreshing = null
      })
    return this.refreshing
  }

  private setTokens(tokens: TokenPair): void {
    this.tokens = tokens
    if (this.refreshTimer) {
      clearTimeout(this.refreshTimer)
    }
    const expiresAt = Date.parse(tokens.access_expires_at)
    const refreshAfter = Number.isNaN(expiresAt) ? 0 : Math.max(0, expiresAt - Date.now() - 30_000)
    this.refreshTimer = setTimeout(() => {
      void this.refreshAccessToken().catch(() => undefined)
    }, refreshAfter)
  }

  private async request<T>(method: string, params: object, accessToken?: string): Promise<T> {
    const id = String(++this.nextID)
    const headers = new Headers({ 'Content-Type': 'application/json' })
    if (accessToken) {
      headers.set('Authorization', `Bearer ${accessToken}`)
    }

    let response: Response
    try {
      response = await fetch('/jsonrpc', {
        method: 'POST',
        credentials: 'same-origin',
        headers,
        body: JSON.stringify([{ jsonrpc: '2.0', id, method, params }]),
      })
    } catch {
      throw new AdminApiError('Не удалось связаться с сервером. Проверьте подключение.')
    }

    if (!response.ok) {
      throw new AdminApiError('Сервер временно недоступен.')
    }
    const body: unknown = await response.json()
    const reply = extractReply<T>(body, id)
    if (reply.error) {
      throw new AdminApiError(reply.error.message, reply.error.code)
    }
    if (reply.result === undefined) {
      throw new AdminApiError('Сервер вернул неполный ответ.')
    }
    return reply.result
  }

  private async authenticatedFetch(path: string, init: RequestInit): Promise<Response> {
    let current = this.tokens
    if (!current) throw new AdminApiError('Требуется вход в систему', -32001)
    const send = (token: string) => {
      const headers = new Headers(init.headers)
      headers.set('Authorization', `Bearer ${token}`)
      return fetch(path, { ...init, credentials: 'same-origin', headers })
    }
    let response = await send(current.access_token)
    if (response.status === 401) {
      await this.refreshAccessToken()
      current = this.tokens
      if (!current) throw new AdminApiError('Сессия завершена. Войдите снова.', -32001)
      response = await send(current.access_token)
    }
    return response
  }
}

type ProgressFrame = {
  e: number
  d?: Record<string, unknown>
  err?: string
}

function parseProgressFrame(value: unknown): ProgressFrame | null {
  if (typeof value !== 'string') return null
  try {
    const frame: unknown = JSON.parse(value)
    if (typeof frame !== 'object' || frame === null || !('e' in frame) || typeof frame.e !== 'number') return null
    return frame as ProgressFrame
  } catch {
    return null
  }
}

function objectValue(value: unknown): Record<string, unknown> {
  return typeof value === 'object' && value !== null ? value as Record<string, unknown> : {}
}

function extractReply<T>(body: unknown, id: string): RpcReply<T> {
  const candidates = Array.isArray(body) ? body : [body]
  const reply = candidates.find(
    (candidate): candidate is RpcReply<T> =>
      typeof candidate === 'object' &&
      candidate !== null &&
      'id' in candidate &&
      candidate.id === id,
  )
  if (!reply) {
    throw new AdminApiError('Сервер вернул ответ в неизвестном формате.')
  }
  return reply
}

function isAuthMethod(method: string): boolean {
  return method === 'auth.login' || method === 'auth.refresh' || method === 'auth.logout'
}

export const adminApi = new AdminApi()
