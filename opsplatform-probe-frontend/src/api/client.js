import axios from 'axios'
import router from '../router'
import { ElMessage } from 'element-plus'

const api = axios.create({
  baseURL: '/api',
  timeout: 60000,
  withCredentials: true
})

// Cookie-only auth: do not read JWT from localStorage. The httpOnly cookie
// `probe_auth_token` is sent automatically thanks to withCredentials.
// This eliminates XSS-token-theft risk. (fix #4)
api.interceptors.request.use(cfg => cfg)

api.interceptors.response.use(
  resp => {
    if (resp.data && resp.data.code !== 0 && resp.data.code !== undefined) {
      ElMessage.error(resp.data.message || '请求失败')
      return Promise.reject(new Error(resp.data.message))
    }
    return resp.data
  },
  err => {
    if (err.response?.status === 401) {
      localStorage.removeItem('probe_logged_in')
      router.push('/login')
    } else {
      ElMessage.error(err.response?.data?.message || err.message)
    }
    return Promise.reject(err)
  }
)

export default api
