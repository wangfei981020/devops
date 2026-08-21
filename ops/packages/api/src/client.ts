import createClient, { type Middleware } from 'openapi-fetch'
import type { paths } from './generated/cmdb.js'
import { type ApiErrorBody, type NormalizedError, normalizeError } from './errors.js'

export interface ClientOptions {
  /**
   * 接口前缀。默认 `/api/v1` —— **一律用相对路径**。
   *
   * 把后端地址编进构建产物是上一代的做法，换个环境就得重新构建。
   * 走相对路径的话，同一个镜像能跑遍所有环境，由 nginx 决定反代到哪。
   */
  baseUrl?: string
  /** 附加请求头，如租户、追踪 ID */
  headers?: () => Record<string, string>
  /** 收到 401 时的处置（跳登录页等）。不传则只是抛错。 */
  onUnauthorized?: () => void
}

/**
 * 请求出错时抛这个，而不是裸 Error。
 *
 * 带上归一化后的 kind / messageKey，页面就能直接决定
 * 「显示什么文案、给不给重试按钮、要不要跳登录」，不用每个页面各判一遍状态码。
 */
export class ApiError extends Error {
  readonly info: NormalizedError

  constructor(info: NormalizedError) {
    // message 用技术描述，方便在控制台和日志里看；给用户的文案走 messageKey
    super(info.detail || info.code)
    this.name = 'ApiError'
    this.info = info
  }
}

export function createCmdbClient(opts: ClientOptions = {}) {
  const baseUrl = opts.baseUrl ?? '/api/v1'

  const middleware: Middleware = {
    async onRequest({ request }) {
      for (const [k, v] of Object.entries(opts.headers?.() ?? {})) {
        request.headers.set(k, v)
      }
      return request
    },

    async onResponse({ request, response }) {
      if (response.ok) {
        // 🔴 200 不等于拿到了数据。
        //
        //	CDN/WAF 的挑战页是 **200 + 一整页 HTML**：从 HTTP 层看这是一次
        //	"成功"的请求，放行之后 openapi-fetch 自己 `res.json()`，
        //	抛出的是裸的 `SyntaxError: Unexpected token '<'` ——
        //	那不是 ApiError，逃出了这里的错误归一，界面上显示的是
        //	"请求失败，原因未知 · Unexpected token '<'"，
        //	跟"被拦下了"这件事毫无关系（生产验收 NEW-6 / OPSCMDB-052）。
        //
        //	⚠️ 判据不能只看 content-type：有的网关拦截时照样标 application/json。
        //	以**能不能解析**为准。
        //	⚠️ 204 和空体是合法的，不能当成失败。
        //	⚠️ 必须 clone 后再读：body 只能读一次，直接读会让调用方拿到空流。
        // ⚠️ 不能靠 content-type 判断要不要检查：
        //	有的网关拦截时照样标 application/json，而截断的响应
        //	（`{"items":[{"na`）content-type 也是 json —— 两者都得挡。
        //	所以**一律尝试解析**，以能不能解析为准。
        //	204 与空体是合法的，跳过。
        if (response.status !== 204) {
          const text = await response.clone().text()
          if (text.trim() !== '') {
            try {
              JSON.parse(text)
            } catch {
              throw new ApiError(
                normalizeError(
                  {
                    // 走结构化错误那一分支：不给专属 code 的话会落进
                    // "按状态码兜底"，显示成「HTTP 200 · 服务端出错了」——
                    // 一句自相矛盾的话（实测过）
                    code: 'non_json_response',
                    message_key: 'error.nonJsonResponse',
                    message: nonJSONBody(text, response.status),
                  },
                  { method: request.method, path: new URL(request.url).pathname, status: response.status },
                ),
              )
            }
          }
        }
        return response
      }

      const path = new URL(request.url).pathname
      let body: ApiErrorBody | undefined
      try {
        // clone 是必须的：body 只能读一次，直接读会让调用方拿到一个空流
        body = (await response.clone().json()) as ApiErrorBody
      } catch {
        // 网关返回的 HTML 错误页之类，没有结构化 body。
        // 这不是异常情况，交给 normalizeError 按状态码兜底。
      }

      const info = normalizeError(body ?? response, {
        method: request.method,
        path,
        status: response.status,
      })

      if (info.kind === 'auth' && response.status === 401) {
        opts.onUnauthorized?.()
      }
      throw new ApiError(info)
    },
  }

  const client = createClient<paths>({ baseUrl })
  client.use(middleware)
  return client
}

/**
 * 把 fetch 层的网络异常也包成 ApiError。
 *
 * onResponse 只在**拿到响应**时触发；断网、DNS 挂掉、服务没起这几种情况
 * 连响应都没有，会直接抛 TypeError。不统一包起来的话，
 * 页面就得同时处理两种错误形态，而"没连上"恰恰是最常见的一种。
 */
export async function callApi<T>(fn: () => Promise<T>): Promise<T> {
  try {
    return await fn()
  } catch (e) {
    if (e instanceof ApiError) throw e
    throw new ApiError(normalizeError(e))
  }
}


/**
 * 「返回的不是 JSON」这句话。
 *
 * ⚠️ 把响应开头一小段带进去：那一段通常直接写着是谁拦的
 *	（Cloudflare / ModSecurity / nginx），比"解析失败"有用得多。
 */
function nonJSONBody(text: string, status: number): string {
  const t = text.trim()
  const head = t.slice(0, 120)
  if (/^<(!doctype|html|head)/i.test(t)) {
    return `接口返回了网页而不是数据（HTTP ${status}）——多半是被 CDN/WAF 拦下了：${head}`
  }
  return `接口返回的内容不是 JSON（HTTP ${status}）：${head}`
}
