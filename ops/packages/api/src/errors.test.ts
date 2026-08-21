import { describe, expect, it } from 'vitest'
import { normalizeError, shouldRetry, toErrorInfo } from './errors.js'

describe('后端结构化错误', () => {
  it('保留 code / messageKey / requestId', () => {
    const r = normalizeError(
      {
        code: 'upstream_timeout',
        message_key: 'error.upstreamTimeout',
        params: { cluster: 'g32-prod' },
        request_id: '8f2a91c3',
      },
      { method: 'GET', path: '/api/v1/hosts', status: 504 },
    )
    expect(r.kind).toBe('transient')
    expect(r.messageKey).toBe('error.upstreamTimeout')
    expect(r.params).toEqual({ cluster: 'g32-prod' })
    // request id 要出现在 detail 里 —— 用户截图里能看到，运维据此捞日志
    expect(r.detail).toContain('8f2a91c3')
    expect(r.detail).toContain('/api/v1/hosts')
  })

  it('鉴权类不可重试', () => {
    // 重试 3 次只是让用户多等 3 秒，结果一模一样，
    // 还会在后端日志里刷出 3 倍的 403
    for (const code of ['unauthorized', 'forbidden']) {
      const r = normalizeError({ code })
      expect(r.kind, code).toBe('auth')
      expect(r.retryable, code).toBe(false)
    }
  })

  it('永久性错误不可重试', () => {
    for (const code of ['bad_request', 'not_found', 'conflict']) {
      expect(normalizeError({ code }).retryable, code).toBe(false)
    }
  })

  it('授权限制单独成一类，不混进 auth', () => {
    // read_only 是「license 过期了」，forbidden 是「这个账号没权限」。
    // 两者的出路完全不同：一个找采购续期，一个找管理员加权限。
    // 混成一类的话，界面只能给一句模糊的"没有权限"。
    for (const code of ['read_only', 'capacity_exceeded', 'feature_not_licensed']) {
      const r = normalizeError({ code })
      expect(r.kind, code).toBe('license')
      expect(r.retryable, code).toBe(false)
    }
  })

  it('瞬时故障可重试', () => {
    for (const code of ['upstream_timeout', 'upstream_error', 'internal']) {
      expect(normalizeError({ code }).retryable, code).toBe(true)
    }
  })
})

describe('网络层异常', () => {
  it('fetch 抛错归为 unreachable，与服务端 5xx 区分开', () => {
    // 这两者的排查方向完全相反：一个查自己的网络，一个查服务端日志。
    // 混成一类会让用户拿着"服务端错误"去找运维，而实际是他自己断网了。
    const r = normalizeError(new TypeError('Failed to fetch'))
    expect(r.kind).toBe('unreachable')
    expect(r.messageKey).toBe('error.unreachable')
    expect(r.retryable).toBe(true)
  })
})

describe('只有状态码没有结构化 body', () => {
  it('按状态码兜底映射', () => {
    // 网关直接返回 HTML 错误页时没有 body，不能因此崩掉
    const r = normalizeError(new Response(null, { status: 403 }), {
      path: '/api/v1/hosts',
      status: 403,
    })
    expect(r.kind).toBe('auth')
    expect(r.retryable).toBe(false)
  })

  it('未知状态码保守当作可重试', () => {
    const r = normalizeError({}, { path: '/x', status: 599 })
    expect(r.retryable).toBe(true)
  })
})

describe('shouldRetry', () => {
  it('可重试的最多两次', () => {
    const err = { code: 'upstream_timeout' }
    expect(shouldRetry(0, err)).toBe(true)
    expect(shouldRetry(1, err)).toBe(true)
    expect(shouldRetry(2, err)).toBe(false)
  })

  it('不可重试的一次都不试', () => {
    expect(shouldRetry(0, { code: 'forbidden' })).toBe(false)
  })
})

describe('toErrorInfo', () => {
  // 这一组是这个函数存在的全部理由：客户端抛的是 ApiError，
  // 直接丢给 normalizeError 会退化成"未知"且 retryable=true
  it('ApiError 里已经规范化过的信息要原样取出，不能退化成 unknown', () => {
    const info = {
      kind: 'auth' as const,
      code: 'forbidden',
      messageKey: 'error.forbidden',
      retryable: false,
      detail: 'GET /api/me → 403',
    }
    const err = { info, message: 'GET /api/me → 403' }
    expect(toErrorInfo(err)).toEqual(info)
  })

  it('403 经 ApiError 传过来时不可重试 —— 否则每个 403 会被重试三次', () => {
    const err = {
      info: { kind: 'auth', code: 'forbidden', messageKey: 'error.forbidden', retryable: false },
    }
    expect(shouldRetry(0, err)).toBe(false)
  })

  it('不是 ApiError 的照常走 normalizeError', () => {
    expect(toErrorInfo({ code: 'not_found' }).messageKey).toBe('error.unknown')
    expect(toErrorInfo({ code: 'not_found', message_key: 'error.notFound' }).kind).toBe('permanent')
  })

  it('info 结构不完整时不认，回落到 normalizeError', () => {
    // 业务对象恰好带个 info 字段是完全可能的，不能见 info 就当规范化结果
    expect(toErrorInfo({ info: { hello: 'world' } }).code).toBe('unknown')
  })
})
