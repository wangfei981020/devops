/**
 * 后端错误 → 前端可消费的形态。
 *
 * 后端返回的是 `{ code, message_key, params, request_id }`，**不含面向用户的句子**。
 * 翻译在这里做，文案在语言包里 —— 后端拼好中文发过来，英文界面就永远漏中文，
 * 而且只在错误路径上出现，正常测试根本走不到。
 */

/** 与后端 internal/httpx/errors.go 的错误码一一对应。 */
export type ApiErrorCode =
  | 'bad_request'
  | 'unauthorized'
  | 'forbidden'
  | 'not_found'
  | 'conflict'
  | 'upstream_timeout'
  | 'upstream_error'
  | 'internal'
  | 'read_only'
  | 'capacity_exceeded'
  | 'feature_not_licensed'
  /**
   * 🔴 接口返回的不是 JSON（CDN/WAF 挑战页、网关错误页）。
   *
   * 这个码**不是后端产出的**，是客户端在解析失败时自己造的 ——
   * 因为从 HTTP 层看那是一次 200 的"成功"请求，只有解析那一步才发现不对。
   * 不给它专属码的话会落进"按状态码兜底"那一档，
   * 显示成「HTTP 200 · 服务端出错了」这种自相矛盾的话（实测过）。
   */
  | 'non_json_response'
  /** 重名：同名的资源已经存在。带 params.what / params.name 指明是哪一个。 */
  | 'duplicate_name'

export interface ApiErrorBody {
  code: ApiErrorCode | string
  message_key?: string
  params?: Record<string, unknown>
  /** 英文技术描述，给运维排查用，不展示给终端用户 */
  message?: string
  request_id?: string
}

/**
 * 错误的处置类别。决定「要不要重试」和「重试有没有意义」。
 *
 * 分类而不是逐个错误码判断，是因为重试策略必须统一：
 * 同一个错误在不同页面有不同的重试行为，用户就没法建立预期
 * （这次点重试好了，下次点了没反应）。
 */
export type ErrorKind =
  /** 瞬时故障，重试大概率能好：超时、502、网络断 */
  | 'transient'
  /** 永久性，重试一万次也一样：参数错、404、冲突 */
  | 'permanent'
  /** 身份问题，要重新登录或找管理员，不是重试能解决的 */
  | 'auth'
  /** 授权限制（过期只读、超容量、功能未授权），要去续期或升级 */
  | 'license'
  /** 压根没连上服务端：断网、DNS 挂了、服务没起 */
  | 'unreachable'

const KIND: Record<string, ErrorKind> = {
  // 被 WAF 拦下多半是本次请求的特征触发的（参数/频率），重试有可能过 ——
  // 但更常见的是需要人去调白名单，所以给 transient 而不是 permanent：
  // 让按钮在，但文案要说清可能是被拦了
  non_json_response: 'transient',
  // 重名重试多少次都还是重名 —— 给重试按钮只会让人白点
  duplicate_name: 'permanent',
  bad_request: 'permanent',
  not_found: 'permanent',
  conflict: 'permanent',
  unauthorized: 'auth',
  forbidden: 'auth',
  upstream_timeout: 'transient',
  upstream_error: 'transient',
  internal: 'transient',
  read_only: 'license',
  capacity_exceeded: 'license',
  feature_not_licensed: 'license',
}

export interface NormalizedError {
  kind: ErrorKind
  code: string
  /** 语言包 key，调用方用 t() 翻译 */
  messageKey: string
  params?: Record<string, unknown>
  /** 可定位的技术细节，给运维贴进工单用 */
  detail?: string
  requestId?: string
  status?: number
  /** 是否值得给用户一个「重试」按钮 */
  retryable: boolean
  /**
   * 错误体里除已知字段之外的东西，原样保留。
   *
   * 🔴 为什么需要：有些失败**必须同时给出逐行明细**。批量写 DNS 失败时
   *	`errors` 数组里是"第几行为什么没做成" —— 归一时把它丢掉，
   *	界面上就只剩一句"全部行校验不通过"，而人下一个问题必然是"哪一行"。
   *
   * ⚠️ 这里放的是**结构化明细**，不是给用户看的成句文案。
   *	后端往 extra 里塞中文句子的话，又绕回"英文界面显示中文"了（OPSCMDB-054）。
   */
  extra?: Record<string, unknown>
}

/**
 * 把任何异常收敛成统一形态。
 *
 * 三个来源都要覆盖：后端的结构化错误、HTTP 层的裸状态码、以及
 * fetch 抛出的网络异常（这类连响应都没有，最容易被漏掉）。
 */
export function normalizeError(
  err: unknown,
  ctx?: { method?: string; path?: string; status?: number },
): NormalizedError {
  const base = {
    detail: buildDetail(ctx),
    status: ctx?.status,
  }

  // 1) 后端返回的结构化错误
  if (isApiErrorBody(err)) {
    const kind = KIND[err.code] ?? 'transient'
    // 已知字段之外的原样带走 —— 见 NormalizedError.extra 的说明
    const KNOWN = new Set(['code', 'message_key', 'params', 'message', 'request_id'])
    const extra: Record<string, unknown> = {}
    for (const [k, v] of Object.entries(err as unknown as Record<string, unknown>)) {
      if (!KNOWN.has(k)) extra[k] = v
    }
    return {
      ...base,
      ...(Object.keys(extra).length > 0 ? { extra } : {}),
      kind,
      code: err.code,
      messageKey: err.message_key || 'error.unknown',
      params: err.params,
      requestId: err.request_id,
      // ⚠️ 有 message 时把它接在 detail 后面：那是给运维看的技术描述，
      //	被 WAF 拦时里面直接写着是谁拦的（Cloudflare / ModSecurity / nginx）。
      //	丢掉它，界面上就只剩一句通用文案，最有用的那段没了。
      detail: err.message
        ? `${buildDetail({ ...ctx, requestId: err.request_id })}\n${err.message}`
        : buildDetail({ ...ctx, requestId: err.request_id }),
      // auth 和 permanent 重试没有意义，给了按钮只会让人白等
      retryable: kind === 'transient',
    }
  }

  // 2) 网络层异常：连响应都没拿到
  //    这类最容易被当成"服务端 500"处理，但它俩的排查方向完全相反 ——
  //    一个查自己的网络，一个查服务端日志。
  if (err instanceof TypeError || (err instanceof Error && /fetch|network/i.test(err.message))) {
    return {
      ...base,
      kind: 'unreachable',
      code: 'unreachable',
      messageKey: 'error.unreachable',
      detail: err.message,
      retryable: true,
    }
  }

  // 3) 只有状态码，没有结构化 body（网关直接返回的错误页等）
  if (ctx?.status) {
    const code = statusToCode(ctx.status)
    const kind = KIND[code] ?? 'transient'
    return { ...base, kind, code, messageKey: `error.${camel(code)}`, retryable: kind === 'transient' }
  }

  return {
    ...base,
    kind: 'transient',
    code: 'unknown',
    messageKey: 'error.unknown',
    detail: err instanceof Error ? err.message : String(err),
    retryable: true,
  }
}

function isApiErrorBody(v: unknown): v is ApiErrorBody {
  return typeof v === 'object' && v !== null && typeof (v as ApiErrorBody).code === 'string'
}

function statusToCode(status: number): string {
  if (status === 400) return 'bad_request'
  if (status === 401) return 'unauthorized'
  if (status === 403) return 'forbidden'
  if (status === 404) return 'not_found'
  if (status === 409) return 'conflict'
  if (status === 504) return 'upstream_timeout'
  if (status === 502 || status === 503) return 'upstream_error'
  return 'internal'
}

function camel(snake: string): string {
  return snake.replace(/_([a-z])/g, (_, c: string) => c.toUpperCase())
}

function buildDetail(ctx?: { method?: string; path?: string; status?: number; requestId?: string }) {
  if (!ctx?.path) return undefined
  const parts = [`${ctx.method ?? 'GET'} ${ctx.path}`]
  if (ctx.status) parts.push(`→ ${ctx.status}`)
  if (ctx.requestId) parts.push(`· req_id ${ctx.requestId}`)
  return parts.join(' ')
}

/**
 * 拿到错误的规范化信息。**UI 层一律用这个，不要直接调 normalizeError。**
 *
 * ⚠️ 客户端抛出来的是 `ApiError`，它把规范化结果放在 `.info` 里。
 * 直接把它丢给 `normalizeError` 会走到"什么都不匹配"的兜底分支 ——
 * 于是一个明明是 403 的错误被说成「请求失败，原因未知」，
 * 而 `retryable` 变成 true，**403 还会被重试三次**。
 *
 * 这个坑不会报错，只会让所有错误文案退化成同一句"未知"，
 * 而"未知"看起来像个合理的兜底，没人会怀疑它。
 */
export function toErrorInfo(
  err: unknown,
  ctx?: { method?: string; path?: string; status?: number },
): NormalizedError {
  // 不用 instanceof：errors.ts 被 client.ts 依赖，反过来 import 会成环。
  // 结构判断在这里够用，且对跨包重复打包的情况更稳。
  if (typeof err === 'object' && err !== null && 'info' in err) {
    const info = (err as { info: unknown }).info
    if (isNormalized(info)) return info
  }
  return normalizeError(err, ctx)
}

function isNormalized(v: unknown): v is NormalizedError {
  return (
    typeof v === 'object' &&
    v !== null &&
    typeof (v as NormalizedError).messageKey === 'string' &&
    typeof (v as NormalizedError).retryable === 'boolean'
  )
}

/**
 * TanStack Query 的重试判据。
 *
 * ⚠️ 鉴权和永久性错误**绝不重试**：重试 3 次只是让用户多等 3 秒，
 * 结果一模一样，而且会在后端日志里刷出 3 倍的 403。
 */
export function shouldRetry(failureCount: number, err: unknown): boolean {
  const e = toErrorInfo(err)
  if (!e.retryable) return false
  return failureCount < 2
}
