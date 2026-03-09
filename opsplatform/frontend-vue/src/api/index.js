import axios from 'axios'

const api = axios.create({
  baseURL: '',
  timeout: 30000,
  headers: {
    'Content-Type': 'application/json'
  }
})

api.interceptors.request.use(
  config => {
    const token = localStorage.getItem('token')
    if (token && token !== 'cookie-auth') {
      config.headers.Authorization = `Bearer ${token}`
    }
    const username = JSON.parse(localStorage.getItem('currentUser') || '{}')?.username
    if (username) {
      config.headers['X-Operator'] = username
    }
    return config
  },
  error => Promise.reject(error)
)

api.interceptors.response.use(
  response => response,
  error => {
    if (error.response?.status === 401) {
      // 如果当前已经在登录页面，不要跳转（让登录页面显示错误提示）
      const isLoginPage = window.location.pathname === '/login' || window.location.pathname === '/'
      if (!isLoginPage) {
        localStorage.removeItem('token')
        window.location.href = '/login'
      }
    }
    return Promise.reject(error)
  }
)

export default api
