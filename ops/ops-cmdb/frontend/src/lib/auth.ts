/**
 * 最小登录态。
 *
 * ⚠️ 这里**只做认证，不做授权**。权限（菜单过滤、按钮显隐、无权限提示）
 * 是阶段 1.5 的事，且判据一律以后端返回的 is_admin 为准 ——
 * 前端禁止按登录来源之类自行推导（这条栽过三次）。
 */

const TOKEN_KEY = 'ops.token'
const USER_KEY = 'ops.user'

export interface SessionUser {
  username: string
  displayName: string
  isAdmin: boolean
  /** 登录来源：local / portal。**只用于展示，不参与权限判断。** */
  authSource: string
}

function safeGet(k: string): string | null {
  try {
    return localStorage.getItem(k)
  } catch {
    return null
  }
}

export function getToken(): string | null {
  return safeGet(TOKEN_KEY)
}

export function getUser(): SessionUser | null {
  const raw = safeGet(USER_KEY)
  if (!raw) return null
  try {
    return JSON.parse(raw) as SessionUser
  } catch {
    // 存的内容坏了（手改过、版本不兼容）就当没登录，
    // 而不是抛异常把整个应用炸掉
    return null
  }
}

export function isSignedIn(): boolean {
  return getToken() !== null
}

interface LoginResponse {
  token: string
  username: string
  display_name: string
  is_admin: boolean
  auth_source: string
}

export async function signIn(username: string, password: string): Promise<void> {
  const res = await fetch('/api/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  })
  if (!res.ok) {
    // 不区分「用户不存在」和「密码错误」——区分了等于送给攻击者一个账号枚举接口
    throw new Error(String(res.status))
  }
  const d = (await res.json()) as LoginResponse
  const user: SessionUser = {
    username: d.username,
    displayName: d.display_name || d.username,
    isAdmin: d.is_admin,
    authSource: d.auth_source,
  }
  try {
    localStorage.setItem(TOKEN_KEY, d.token)
    localStorage.setItem(USER_KEY, JSON.stringify(user))
  } catch {
    /* 隐私模式下存不下，本次会话仍可用 */
  }
}

/**
 * 退出登录。
 *
 * ⚠️ 必须先让**服务端**作废会话，再清本地。
 * 原来只 removeItem 了两个 key —— token 在库里仍然有效，
 * 谁要是从浏览器里把它抄走（或者这台机器被别人用），退出等于没退。
 * 后端 `POST /api/logout` 一直都在，前端从来没调过。
 *
 * 吊销失败也照样清本地并跳走：本地不清的话人根本退不出去。
 * 但要留日志，否则"服务端会话没吊销"这件事没有任何痕迹。
 */
export async function signOut(): Promise<void> {
  const token = getToken()
  if (token) {
    try {
      const res = await fetch('/api/logout', {
        method: 'POST',
        headers: { Authorization: `Bearer ${token}` },
      })
      if (!res.ok) console.warn('[auth] 服务端会话吊销失败，本地已登出', res.status)
    } catch (e) {
      console.warn('[auth] 服务端会话吊销请求发不出去，本地已登出', e)
    }
  }
  try {
    localStorage.removeItem(TOKEN_KEY)
    localStorage.removeItem(USER_KEY)
  } catch {
    /* 同上 */
  }
  // 整页重载而不是改状态：登录态一变，所有已缓存的查询结果都可能越权，
  // 逐个失效容易漏，重载最干净
  window.location.reload()
}

/**
 * 接住 SSO 回调带回来的会话 token。
 *
 * 后端把 token 放在 URL **fragment**（# 后面）里而不是 query：fragment 不会进
 * Referer、不会被反向代理和 CDN 记进访问日志。query 里的 token 会一路留在日志中。
 *
 * ⚠️ 取到后立刻用 replaceState 把 hash 抹掉：留在地址栏里的话，
 * 用户复制链接发给同事就等于把自己的会话送出去了。
 */
export async function adoptSSOToken(token: string): Promise<void> {
  const res = await fetch('/api/me', { headers: { Authorization: `Bearer ${token}` } })
  if (!res.ok) {
    throw new Error(String(res.status))
  }
  const d = (await res.json()) as {
    username: string
    display_name: string
    is_admin: boolean
    auth_source: string
  }
  const user: SessionUser = {
    username: d.username,
    displayName: d.display_name || d.username,
    isAdmin: d.is_admin,
    authSource: d.auth_source,
  }
  try {
    localStorage.setItem(TOKEN_KEY, token)
    localStorage.setItem(USER_KEY, JSON.stringify(user))
  } catch {
    /* 隐私模式下存不下，本次会话仍可用 */
  }
}
