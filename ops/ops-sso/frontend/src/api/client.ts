/**
 * SSO 后端客户端。
 *
 * 后端只返回**错误码 + 参数**，绝不返回给用户看的句子（CONVENTIONS §3.2）。
 * 文案在 `packages/i18n` 里。后端返中文的那天，英文界面就废了。
 */

/** ApiError 带上码与参数，交给界面去翻译。 */
export class ApiError extends Error {
  constructor(
    readonly code: string,
    readonly status: number,
    readonly params?: Record<string, unknown>,
    readonly requestId?: string,
  ) {
    // message 是给运维看的技术描述，不展示给终端用户
    super(`${code} (HTTP ${status})`)
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let res: Response
  try {
    res = await fetch(`/api/v1${path}`, {
      // 会话走 Cookie。跨站请求不带 Cookie 是默认行为，
      // 而门户与后端同域，所以 same-origin 就够。
      credentials: 'same-origin',
      headers: init?.body ? { 'Content-Type': 'application/json' } : undefined,
      ...init,
    })
  } catch (e) {
    // 网络层失败：**必须与"后端返回了错误"分开**。
    // 压成同一种的话，断网会被显示成"用户名或密码错误"，
    // 用户会反复重输密码 —— 这个坑在旧版上真踩过。
    throw new ApiError('network_unreachable', 0, { detail: String(e) })
  }

  if (res.status === 204) return undefined as T
  const body = await res.json().catch(() => null)

  if (!res.ok) {
    const code = (body?.code as string) ?? 'common.internal'
    throw new ApiError(code, res.status, body?.params, body?.request_id)
  }
  return body as T
}

export const api = {
  get: <T>(p: string) => request<T>(p),
  post: <T>(p: string, body?: unknown) =>
    request<T>(p, { method: 'POST', body: body ? JSON.stringify(body) : undefined }),
  put: <T>(p: string, body?: unknown) =>
    request<T>(p, { method: 'PUT', body: body ? JSON.stringify(body) : undefined }),
  del: <T>(p: string) => request<T>(p, { method: 'DELETE' }),
}
