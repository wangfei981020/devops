/**
 * 后端调用封装。
 *
 * # 为什么不共用 @ops/api 的 client
 *
 * 那个包里的 generated 类型是 CMDB 的 OpenAPI 产物。等 OpsAlert 的
 * OpenAPI 定稿后会一起并进去；在那之前手写一层薄封装，比先塞一堆
 * `as any` 进共享包更干净。
 *
 * # 一条纪律
 *
 * 失败一律抛异常，绝不返回空数组兜底。返回空数组的话，
 * 「查询挂了」和「确实没有数据」在界面上长得一模一样——
 * 这正是本产品要根治的问题，自己的前端先不能犯。
 */

const TOKEN_KEY = 'opsalert.token'

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    readonly detail?: string,
  ) {
    super(detail ? `${code}: ${detail}` : code)
  }
}

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}

export function setToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token)
}

export function clearToken(): void {
  localStorage.removeItem(TOKEN_KEY)
}

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const token = getToken()
  const res = await fetch(`/api/v1${path}`, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(init?.headers ?? {}),
    },
  })

  if (res.status === 401) {
    // 会话过期就地清掉并回登录页。留着失效 token 会让后续每个请求
    // 都 401，界面表现为"每个模块都加载失败"，掩盖真正的原因。
    clearToken()
    location.reload()
    throw new ApiError(401, 'unauthorized')
  }

  const text = await res.text()
  const body = text ? (JSON.parse(text) as Record<string, unknown>) : {}
  if (!res.ok) {
    throw new ApiError(
      res.status,
      String(body.error ?? `http_${res.status}`),
      body.detail ? String(body.detail) : undefined,
    )
  }
  return body as T
}

export const get = <T>(path: string) => api<T>(path)
export const post = <T>(path: string, data?: unknown) =>
  api<T>(path, { method: 'POST', body: JSON.stringify(data ?? {}) })
export const put = <T>(path: string, data: unknown) =>
  api<T>(path, { method: 'PUT', body: JSON.stringify(data) })
export const del = <T>(path: string) => api<T>(path, { method: 'DELETE' })

/**
 * 把异常转成四态组件要的 LoadError。
 *
 * ⚠️ 这里只返回 i18n key，不返回成句的中文——lib 层写死中文的话，
 * 英文界面会在错误路径上零星漏中文，而错误路径平时走不到，
 * 这种漏能一路活到客户手里。翻译由调用方（页面）用 t() 完成。
 */
export function toLoadErrorKey(e: unknown): { key: string; detail?: string; retryable: boolean } {
  if (e instanceof ApiError) {
    return {
      key: ERROR_KEYS[e.code] ?? e.code,
      detail: e.detail,
      // 5xx 与网络错误可重试；4xx 是请求本身的问题，重试没有意义。
      retryable: e.status >= 500,
    }
  }
  return { key: 'opsalert:error.network', detail: String(e), retryable: true }
}

/** 认不出的错误码原样透传：露出 code 比显示"未知错误"更有助于排查。 */
const ERROR_KEYS: Record<string, string> = {
  query_failed: 'opsalert:error.queryFailed',
  no_tenant_context: 'opsalert:error.noTenant',
  unauthorized: 'opsalert:error.unauthorized',
  invalid_credentials: 'opsalert:error.invalidCredentials',
  read_only: 'opsalert:error.readOnly',
  in_use: 'opsalert:error.inUse',
  fallback_protected: 'opsalert:error.fallbackProtected',
  empty_matchers: 'opsalert:error.emptyMatchers',
  no_end_time: 'opsalert:error.noEndTime',
  unsupported_kind: 'opsalert:error.unsupportedKind',
  unsupported_type: 'opsalert:error.unsupportedType',
  invalid_spec: 'opsalert:error.invalidSpec',
}

/** 页面用：把 key 翻好再交给 AsyncBoundary。 */
export function makeLoadError(t: (k: string) => string) {
  return (e: unknown) => {
    const info = toLoadErrorKey(e)
    return { cause: t(info.key), detail: info.detail, retryable: info.retryable }
  }
}
