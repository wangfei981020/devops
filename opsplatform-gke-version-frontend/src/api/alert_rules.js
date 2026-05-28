import axios from 'axios'
const api = axios.create({ baseURL: '/api' })
export const listAlertRules = () => api.get('/alert_rules').then(r => r.data)
export const createAlertRule = (body) => api.post('/alert_rules', body).then(r => r.data)
export const updateAlertRule = (id, body) => api.put(`/alert_rules/${id}`, body).then(r => r.data)
export const deleteAlertRule = (id) => api.delete(`/alert_rules/${id}`).then(r => r.data)
export const listAlertHistory = () => api.get('/alert_history').then(r => r.data)
