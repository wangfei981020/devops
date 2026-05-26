import axios from 'axios'
export const refresh = (body) => axios.post('/api/refresh', body).then(r => r.data)
