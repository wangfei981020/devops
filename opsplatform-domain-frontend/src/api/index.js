import axios from 'axios'
import router from '../router/index.js'

const api = axios.create({
  baseURL: '/api',
  timeout: 30000,
  headers: {
    'Content-Type': 'application/json'
  }
})

// Request interceptor: attach token
api.interceptors.request.use(
  config => {
    const token = localStorage.getItem('domain_token')
    if (token) {
      config.headers.Authorization = `Bearer ${token}`
    }
    return config
  },
  error => Promise.reject(error)
)

// Response interceptor: handle 401
api.interceptors.response.use(
  response => response,
  error => {
    if (error.response && error.response.status === 401) {
      localStorage.removeItem('domain_token')
      router.push('/login')
    }
    return Promise.reject(error)
  }
)

export default api
