import axios from 'axios'
export const getNodes = (clusterId) =>
  axios.get(`/api/clusters/${clusterId}/nodes`).then(r => r.data)
