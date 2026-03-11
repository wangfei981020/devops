import axios from 'axios'

const api = axios.create({
  baseURL: '',
  withCredentials: true,
  headers: { 'Content-Type': 'application/json' }
})

// 请求拦截器
api.interceptors.request.use(config => {
  const token = localStorage.getItem('token')
  if (token && token !== 'cookie-auth') {
    config.headers.Authorization = `Bearer ${token}`
  }
  const user = JSON.parse(localStorage.getItem('currentUser') || '{}')
  if (user.username) {
    config.headers['X-Operator'] = user.username
  }
  return config
})

// 响应拦截器
api.interceptors.response.use(
  res => res,
  err => {
    if (err.response?.status === 401) {
      localStorage.removeItem('token')
      localStorage.removeItem('currentUser')
      const currentPath = window.location.pathname
      if (currentPath !== '/login') {
        window.location.href = '/login'
      }
    }
    return Promise.reject(err)
  }
)

export default api
