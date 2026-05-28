import axios from 'axios'
export const getOverview = () => axios.get('/api/overview').then(r => r.data)
