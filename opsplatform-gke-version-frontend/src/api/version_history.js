import axios from 'axios'
export const getVersionHistory = (clusterId) =>
  axios.get(`/api/clusters/${clusterId}/version_history`).then(r => r.data)
