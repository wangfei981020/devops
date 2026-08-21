import { ApiError } from '@ops/api'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { apiGet, nonJSONMessage } from './fetchJson.js'

/**
 * 🔴 WAF 挑战页是 **HTTP 200 + 一整页 HTML**。
 *
 * 从 HTTP 层看这是一次"成功"的请求，只有解析那一步才会发现不对 ——
 * 而原来那一步抛的是裸的 `SyntaxError: Unexpected token '<'`，
 * 逃出了全站的错误归一：错误信息与"发生了什么"毫无关系，
 * 而只判 data 有没有值的调用方会继续用上一次的数据（生产验收 NEW-6）。
 */
describe('nonJSONMessage', () => {
  it('识别出 WAF/CDN 的 HTML 挑战页', () => {
    const msg = nonJSONMessage(
      '<!DOCTYPE html><html><head><title>Attention Required! | Cloudflare</title>',
      200,
    )
    expect(msg).toContain('网页')
    expect(msg).toContain('CDN/WAF')
    // 开头那段要带出来 —— 它直接写着是谁拦的
    expect(msg).toContain('Cloudflare')
  })

  it('非 HTML 的乱码也要说清是"不是 JSON"，而不是笼统的解析失败', () => {
    const msg = nonJSONMessage('upstream connect error or disconnect', 503)
    expect(msg).toContain('不是 JSON')
    expect(msg).toContain('upstream connect error')
    expect(msg).toContain('503')
  })

  it('截断超长响应，但保留开头', () => {
    const msg = nonJSONMessage('<html>' + 'x'.repeat(5000), 200)
    expect(msg.length).toBeLessThan(300)
    expect(msg).toContain('<html>')
  })
})

/**
 * 🔴 这一组是防回退的关键。
 *
 * 上一版的 bug 不是"没抛错"——错抛出来了，但抛的是 `{ error: "…" }`，
 * 不符合 isApiErrorBody（它要求 code 字段），于是落进 normalizeError
 * 的"只有状态码"兜底分支，按 status=200 显示成
 * 「HTTP 200 · 服务端出错了」——一句自相矛盾的话。
 *
 * 只测 nonJSONMessage 这个纯函数是测不到的：那个函数一直是对的，
 * 错的是它产出的字符串被塞进了哪种形状。所以这里必须断言**错误码**。
 */
describe('非 JSON 响应抛出的错误码', () => {
  const mockFetch = (body: string, init: ResponseInit) => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(body, init)))
  }
  afterEach(() => vi.unstubAllGlobals())

  it('WAF 挑战页（200 + HTML）抛 non_json_response，不是按状态码兜底', async () => {
    mockFetch('<html><body>blocked</body></html>', {
      status: 200,
      headers: { 'content-type': 'text/html' },
    })
    const err = await apiGet('/api/hosts').then(
      () => null,
      (e: unknown) => e as ApiError,
    )
    expect(err).toBeInstanceOf(ApiError)
    expect(err?.info.code).toBe('non_json_response')
    // detail 必须带上原文：那里面写着是谁拦的（Cloudflare / nginx / ModSecurity）
    expect(err?.info.detail ?? '').toContain('blocked')
  })

  it('截断的 JSON 也走同一个码（content-type 标着 json 也不能放过）', async () => {
    mockFetch('{"items":[{"na', {
      status: 200,
      headers: { 'content-type': 'application/json' },
    })
    const err = await apiGet('/api/hosts').then(
      () => null,
      (e: unknown) => e as ApiError,
    )
    expect(err?.info.code).toBe('non_json_response')
  })

  it('正常 JSON 不能被误伤', async () => {
    mockFetch('{"items":[]}', { status: 200, headers: { 'content-type': 'application/json' } })
    await expect(apiGet('/api/hosts')).resolves.toEqual({ items: [] })
  })
})
