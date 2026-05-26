import axios from 'axios'
const api = axios.create({ baseURL: '/api' })

export const listClusters = () => api.get('/clusters').then(r => r.data)
export const getCluster = (id) => api.get(`/clusters/${id}`).then(r => r.data)
export const createCluster = (body) => api.post('/clusters', body).then(r => r.data)
export const updateCluster = (id, body) => api.put(`/clusters/${id}`, body).then(r => r.data)
export const deleteCluster = (id) => api.delete(`/clusters/${id}`).then(r => r.data)
