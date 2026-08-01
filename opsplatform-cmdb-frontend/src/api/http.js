import axios from 'axios'
import { ElMessage } from 'element-plus'

export const TOKEN_KEY = 'cmdb_token'

const http = axios.create({ baseURL: '/api', timeout: 30000 })

http.interceptors.request.use((c) => {
  const t = localStorage.getItem(TOKEN_KEY)
  if (t) c.headers.Authorization = 'Bearer ' + t
  return c
})

// 相同错误 3 秒内只弹一次：一个页面往往并发好几个接口，后端挂掉时会一次性全失败，
// 不去重就是满屏同样的红条。
const shown = new Map()
function toast(msg) {
  const now = Date.now()
  if (shown.get(msg) > now - 3000) return
  shown.set(msg, now)
  ElMessage.error(msg)
}

// normalizeError 把 axios 的各种失败形态压成一个统一的、能直接展示给人看的错误。
// 必须区分「后端返回了错误」和「后端根本没回应」——2026-07-31 MySQL 盘写满时
// 全站接口都是后一种（连接挂起直到超时），而页面把它当成了"数据为空"（CMDB-013）。
export function normalizeError(e) {
  if (e && e.__cmdb) return e
  let msg
  const status = e?.response?.status
  if (e?.code === 'ECONNABORTED' || /timeout/i.test(e?.message || '')) {
    msg = '请求超时（30s 无响应）—— 后端可能不可用或数据库卡住'
  } else if (!e?.response) {
    msg = '连不上后端（网络不可达或服务无响应）'
  } else {
    const d = e.response.data
    const detail = (d && (d.error || d.message)) || ''
    if (status >= 500) msg = '后端错误' + (detail ? '：' + detail : `（HTTP ${status}）`)
    else if (status === 403) msg = '没有权限访问该数据' + (detail ? '：' + detail : '')
    else if (status === 404) msg = '接口不存在' + (detail ? '：' + detail : `（HTTP ${status}）`)
    else msg = detail || `请求失败（HTTP ${status}）`
  }
  const err = new Error(msg)
  err.__cmdb = true
  err.status = status
  err.raw = e
  return err
}

http.interceptors.response.use(
  (r) => r,
  (e) => {
    if (e.response?.status === 401) {
      localStorage.removeItem(TOKEN_KEY)
      if (location.pathname !== '/login') location.href = '/login'
      return Promise.reject(normalizeError(e))
    }
    const err = normalizeError(e)
    // 全局提示：无论在哪个页面、页面自己有没有处理，用户都能立刻知道"后端有问题"，
    // 而不是对着一屏 0 以为一切正常。
    toast(err.message)
    return Promise.reject(err)
  }
)

export default http
