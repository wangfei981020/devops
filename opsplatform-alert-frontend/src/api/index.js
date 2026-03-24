import axios from 'axios'

const api = axios.create({
  baseURL: '/api',
  timeout: 30000,
  withCredentials: true
})

// Request interceptor
api.interceptors.request.use(config => {
  const token = localStorage.getItem('alert_token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// Response interceptor
api.interceptors.response.use(
  response => response.data,
  error => {
    if (error.response?.status === 401) {
      localStorage.removeItem('alert_token')
      localStorage.removeItem('alert_user')
      window.location.href = '/login'
    }
    return Promise.reject(error)
  }
)

export default api
