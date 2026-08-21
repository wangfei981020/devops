import { ApiError, normalizeError, shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { getToken } from './auth.js'

/** 裸 fetch 但抛 ApiError —— 否则这一处的错误处理会和全站不一样。 */
export async function apiGet<T>(path: string): Promise<T> {
  let res: Response
  try {
    res = await fetch(path, { headers: { Authorization: `Bearer ${getToken()}` } })
  } catch (e) {
    throw new ApiError(normalizeError(e, { path }))
  }
  if (!res.ok) {
    throw new ApiError(
      normalizeError(await res.json().catch(() => ({})), { path, status: res.status }),
    )
  }
  return parseJSONOrThrow<T>(res, path)
}

/**
 * 取纯文本响应（容器日志）。
 *
 * ⚠️ 不能复用 apiGet：那个无条件 `res.json()`，而日志接口返回的是 text/plain，
 * 解析必然抛错——错误信息还会是"Unexpected token"这类跟日志毫无关系的东西。
 * 错误归一仍然走 ApiError，与全站一致。
 */
export async function apiGetText(path: string): Promise<string> {
  let res: Response
  try {
    res = await fetch(path, { headers: { Authorization: `Bearer ${getToken()}` } })
  } catch (e) {
    throw new ApiError(normalizeError(e, { path }))
  }
  if (!res.ok) {
    throw new ApiError(
      normalizeError(await res.json().catch(() => ({})), { path, status: res.status }),
    )
  }
  return res.text()
}

/**
 * 写请求。抛 ApiError，与读路径同一套错误归一。
 *
 * ⚠️ 不做重试。写操作大多**不是幂等的**（建账号、触发同步、删除），
 * 自动重试一次可能就是建了两个账号。要重试由用户点。
 */
export async function apiSend<T = unknown>(
  path: string,
  method: 'POST' | 'PUT' | 'DELETE',
  body?: unknown,
): Promise<T> {
  let res: Response
  try {
    res = await fetch(path, {
      method,
      headers: {
        Authorization: `Bearer ${getToken()}`,
        ...(body === undefined ? {} : { 'Content-Type': 'application/json' }),
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
  } catch (e) {
    throw new ApiError(normalizeError(e, { method, path }))
  }
  if (!res.ok) {
    throw new ApiError(
      normalizeError(await res.json().catch(() => ({})), { method, path, status: res.status }),
    )
  }
  // 有些写接口返回 204 / 空体
  // ⚠️ 非空时走 parseJSONOrThrow：直接 JSON.parse 会在被 WAF 拦下时
  //	抛裸的 SyntaxError，逃出全站的错误归一（同 apiGet 的理由）
  const text = await res.text()
  if (!text) return {} as T
  try {
    return JSON.parse(text) as T
  } catch {
    throw new ApiError(
      normalizeError(
        { code: 'non_json_response', message_key: 'error.nonJsonResponse', message: nonJSONMessage(text, res.status) },
        { method, path, status: res.status },
      ),
    )
  }
}

/**
 * 动作类请求（测连通、立即同步……）。
 *
 * ⚠️ 这类老接口的约定是 **HTTP 200 + `{ ok: false, error: "…" }`**，
 * 不是 4xx/5xx。只看 HTTP 状态码的话，"没配凭据"会被当成成功 ——
 * 实测撞到过：三个没有 kubeconfig 的集群，测连通全显示绿色的"连通"。
 *
 * 测连通按钮存在的唯一意义就是告诉你通不通。它撒谎比没有这个按钮更糟：
 * 没有按钮时人还会去别处确认，绿灯亮着就不会了。
 *
 * 所以这里把 `ok === false` 也当作失败，并把后端给的 error 原样带出去。
 */
export async function apiAction<
  T extends {
    ok?: boolean
    error?: string
    error_key?: string
    error_params?: Record<string, unknown>
  },
>(path: string, method: 'POST' | 'PUT' | 'DELETE' = 'POST', body?: unknown): Promise<T> {
  const d = await apiSend<T>(path, method, body)
  if (d && d.ok === false) {
    // 走同一套 ApiError，让上层的错误展示逻辑不用分两种情况
    throw new ApiError({
      kind: 'permanent',
      code: 'action_failed',
      // 🔴 error_key 优先。
      //
      //	这类接口的约定是 HTTP 200 + {ok:false, error:"中文"}，而那句中文
      //	落在 detail（技术描述位）—— 英文界面下它照样是中文。
      //	后端给了 error_key 就用它当主文案，中文原句仍留在 detail 给运维看。
      //	没给的（还没迁的接口）退回通用的"操作失败"，行为不变。
      messageKey: d.error_key || 'error.actionFailed',
      params: d.error_params,
      detail: d.error || '',
      retryable: false,
    })
  }
  return d
}

export { shouldRetry, useQuery }


/**
 * 解析 JSON 响应，解析不了就抛 **ApiError**（而不是裸的 SyntaxError）。
 *
 * # 🔴 为什么需要这一层
 *
 * 原来是无条件 `res.json()`。**HTTP 200 但 body 不是 JSON** 时它抛的是
 * `SyntaxError: Unexpected token '<'` —— 那不是 ApiError，逃出了全站的错误归一：
 *
 *   · react-query 会把它当普通异常，`isError` 确实为 true，
 *     但错误信息是 "Unexpected token '<'"，跟"发生了什么"毫无关系；
 *   · 而调用方若只判 `data` 有没有值，就会**继续用上一次的数据** ——
 *     页面看起来一切正常，实际显示的是过期内容（生产验收 NEW-6）。
 *
 * 最常撞上的就是 **CDN/WAF 的挑战页**：它返回 200 + 一整页 HTML。
 * 从 HTTP 层看这是一次"成功"的请求，只有解析那一步才会发现不对。
 *
 * ⚠️ 判据不能只看 content-type：有的网关拦截时照样标着 application/json。
 *	所以以**能不能解析**为准，并把响应开头的一小段带进错误里 ——
 *	那一段通常直接写着是谁拦的（Cloudflare / ModSecurity / nginx）。
 */
async function parseJSONOrThrow<T>(res: Response, path: string): Promise<T> {
  const text = await res.text()
  try {
    return JSON.parse(text) as T
  } catch {
    throw new ApiError(
      normalizeError({ code: 'non_json_response', message_key: 'error.nonJsonResponse', message: nonJSONMessage(text, res.status) }, { path, status: res.status }),
    )
  }
}

/**
 * 「返回的不是 JSON」这句话。两处共用 —— 各写一份必然分叉。
 *
 * ⚠️ 把响应开头的一小段带进去：那一段通常直接写着是谁拦的
 *	（Cloudflare / ModSecurity / nginx），比"解析失败"有用得多。
 */
export function nonJSONMessage(text: string, status: number): string {
  const t = text.trim()
  const head = t.slice(0, 120)
  if (/^<(!doctype|html|head)/i.test(t)) {
    return `接口返回了网页而不是数据（HTTP ${status}）——多半是被 CDN/WAF 拦下了：${head}`
  }
  return `接口返回的内容不是 JSON（HTTP ${status}）：${head}`
}
