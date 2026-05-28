import axios from 'axios'
export const getUpgrades = (clusterId) =>
  axios.get(`/api/clusters/${clusterId}/upgrades`).then(r => r.data)
