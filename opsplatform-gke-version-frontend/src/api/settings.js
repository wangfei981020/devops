import axios from 'axios'
export const getSettings = () => axios.get('/api/settings').then(r => r.data)
export const updateSetting = (k, v) => axios.put('/api/settings', { k, v }).then(r => r.data)
