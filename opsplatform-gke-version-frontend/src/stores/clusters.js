import { defineStore } from 'pinia'
import { listClusters } from '../api/clusters'

export const useClustersStore = defineStore('clusters', {
  state: () => ({ items: [], loading: false }),
  actions: {
    async load() {
      this.loading = true
      try {
        this.items = await listClusters()
      } finally {
        this.loading = false
      }
    },
  },
})
