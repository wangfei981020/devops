import { defineStore } from 'pinia'
import { ElMessageBox } from 'element-plus'
import { listCITypes, listProjects, listEnvironments, listCdns } from '../api/cmdb'

export const useAppStore = defineStore('app', {
  state: () => ({ ciTypes: [], projects: [], environments: [], cdns: [] }),
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
    async loadBasics() { await Promise.all([this.loadProjects(), this.loadEnvironments(), this.loadCdns()]) },
    // 统一二次确认（禁用浏览器原生弹窗）
    showConfirm(message, title = '确认') {
      return ElMessageBox.confirm(message, title, { type: 'warning' })
    },
  },
})
