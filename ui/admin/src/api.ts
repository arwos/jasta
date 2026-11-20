type TokenPair = {
  access_token: string
  access_expires_at: string
  refresh_token: string
  refresh_expires_at: string
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

export class AdminApiError extends Error {
  constructor(
    message: string,
    readonly code?: number,
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
    const refreshToken = this.tokens?.refresh_token
    if (!refreshToken) {
      this.clear()
      return
    }

    try {
      const current = this.tokens
      if (!current) {
        return
      }
      try {
        await this.request<{ ok: boolean }>('auth.logout', { refresh_token: refreshToken }, current.access_token)
      } catch (error) {
        if (!(error instanceof AdminApiError) || error.code !== -32001) {
          throw error
        }
        await this.refreshAccessToken()
        const refreshed = this.tokens
        if (refreshed) {
          await this.request<{ ok: boolean }>('auth.logout', { refresh_token: refreshed.refresh_token }, refreshed.access_token)
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

  private async refreshAccessToken(): Promise<void> {
    if (this.refreshing) {
      return this.refreshing
    }
    const refreshToken = this.tokens?.refresh_token
    if (!refreshToken) {
      this.clear()
      throw new AdminApiError('Сессия завершена. Войдите снова.', -32001)
    }

    this.refreshing = this.request<TokenPair>('auth.refresh', {
      refresh_token: refreshToken,
    })
      .then((tokens) => {
        this.setTokens(tokens)
      })
      .catch((error: unknown) => {
        this.clear()
        window.dispatchEvent(new Event('jasta:session-expired'))
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
        body: JSON.stringify({ jsonrpc: '2.0', id, method, params }),
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
