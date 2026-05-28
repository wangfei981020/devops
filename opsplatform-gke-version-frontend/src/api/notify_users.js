import axios from 'axios'
const api = axios.create({ baseURL: '/api' })
export const listNotifyUsers = () => api.get('/notify_users').then(r => r.data)
export const createNotifyUser = (body) => api.post('/notify_users', body).then(r => r.data)
export const updateNotifyUser = (id, body) => api.put(`/notify_users/${id}`, body).then(r => r.data)
export const deleteNotifyUser = (id) => api.delete(`/notify_users/${id}`).then(r => r.data)
