import { defineStore } from 'pinia'
import { ElMessageBox } from 'element-plus'
import { listCITypes, listProjects, listEnvironments, listCdns, listStatuses } from '../api/cmdb'

export const useAppStore = defineStore('app', {
  state: () => ({ ciTypes: [], projects: [], environments: [], cdns: [], statuses: [] }),
  getters: {
    projectStatuses: (s) => s.statuses.filter((x) => x.scope === 'project'),
    domainStatuses: (s) => s.statuses.filter((x) => x.scope === 'domain'),
    // label → color 查表（跨 scope 合并；同名同色，够用）
    statusColor: (s) => (label) => (s.statuses.find((x) => x.label === label) || {}).color || '#909399',
  },
  actions: {
    async loadCITypes() {
      try {
        this.ciTypes = await listCITypes()
      } catch (e) {
        this.ciTypes = []
      }
    },
    // 项目 / 环境：基础配置页统一维护
    async loadProjects() {
      try { this.projects = await listProjects() } catch (e) { this.projects = [] }
    },
    async loadEnvironments() {
      try { this.environments = await listEnvironments() } catch (e) { this.environments = [] }
    },
    async loadCdns() {
      try { this.cdns = await listCdns() } catch (e) { this.cdns = [] }
    },
    async loadStatuses() {
      try { this.statuses = await listStatuses() } catch (e) { this.statuses = [] }
    },
    async loadBasics() { await Promise.all([this.loadProjects(), this.loadEnvironments(), this.loadCdns(), this.loadStatuses()]) },
    // 统一二次确认（禁用浏览器原生弹窗）
    showConfirm(message, title = '确认') {
      return ElMessageBox.confirm(message, title, { type: 'warning' })
    },
  },
})
