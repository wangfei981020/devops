import axios from 'axios'

const api = axios.create({
  baseURL: '/api',
  timeout: 30000
})

// 响应拦截器 - 统一取 data
api.interceptors.response.use(
  res => res.data,
  err => {
    const msg = err.response?.data?.message || err.message
    return Promise.reject(new Error(msg))
  }
)

// ========== Settings ==========
export const getSettings = () => api.get('/settings')
export const updateSettings = (data) => api.post('/settings', data)
export const testConfluence = (data) => api.post('/settings/test-confluence', data)
export const testJira = (data) => api.post('/settings/test-jira', data)

// ========== Triggers ==========
export const getTriggers = () => api.get('/triggers')
export const createTrigger = (data) => api.post('/triggers', data)
export const updateTrigger = (id, data) => api.put(`/triggers/${id}`, data)
export const deleteTrigger = (id) => api.delete(`/triggers/${id}`)

// ========== Permissions ==========
export const getPermissions = () => api.get('/permissions')
export const createPermission = (data) => api.post('/permissions', data)
export const updatePermission = (id, data) => api.put(`/permissions/${id}`, data)
export const deletePermission = (id) => api.delete(`/permissions/${id}`)

// ========== Events ==========
export const getEvents = (params) => api.get('/events', { params })
export const retryEvent = (id) => api.post(`/events/${id}/retry`)
