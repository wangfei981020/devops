import axios from 'axios'
const api = axios.create({ baseURL: '/api' })
export const listWebhooks = () => api.get('/lark_webhooks').then(r => r.data)
export const createWebhook = (body) => api.post('/lark_webhooks', body).then(r => r.data)
export const updateWebhook = (id, body) => api.put(`/lark_webhooks/${id}`, body).then(r => r.data)
export const deleteWebhook = (id) => api.delete(`/lark_webhooks/${id}`).then(r => r.data)
