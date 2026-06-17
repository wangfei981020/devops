import axios from 'axios'

export const TOKEN_KEY = 'cmdb_token'

const http = axios.create({ baseURL: '/api', timeout: 30000 })

http.interceptors.request.use((c) => {
  const t = localStorage.getItem(TOKEN_KEY)
  if (t) c.headers.Authorization = 'Bearer ' + t
  return c
})

http.interceptors.response.use(
  (r) => r,
  (e) => {
    if (e.response?.status === 401) {
      localStorage.removeItem(TOKEN_KEY)
      if (location.pathname !== '/login') location.href = '/login'
    }
    return Promise.reject(e)
  }
)

export default http
