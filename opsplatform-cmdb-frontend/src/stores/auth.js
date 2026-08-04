import { defineStore } from 'pinia'
import { login as apiLogin } from '../api/cmdb'
import http, { TOKEN_KEY } from '../api/http'

const PERMS_KEY = 'cmdb_permissions'

// 权限码约定（运维平台的种子、CMDB 后端的 perm.go、这里，三处必须一致）：
//   菜单：menu:cmdb_<页面>   例 menu:cmdb_k8s_nodes
//   按钮：cmdb:<动作>        例 cmdb:manage_dns
export const useAuthStore = defineStore('auth', {
  state: () => ({
    user: JSON.parse(localStorage.getItem('cmdb_user') || 'null'),
    permissions: JSON.parse(localStorage.getItem(PERMS_KEY) || '{}'),
  }),
  getters: {
    isLoggedIn: () => !!localStorage.getItem(TOKEN_KEY),
    // 本地账号是运维平台不可用时的兜底通道，不受权限码约束（后端同样放行）
    isLocal: (s) => s.user?.auth_source !== 'portal',
  },
  actions: {
    // has 单个权限码：本地账号全放行，portal 账号严格查表。
    // 前端这层只管显隐，真正的拦截在后端——少一个 v-if 是体验问题，不是安全问题。
    has(code) {
      if (!code) return true
      if (this.isLocal) return true
      return !!this.permissions[code]
    },
    hasMenu(page) { return this.has('menu:cmdb_' + page) },
    hasButton(action) { return this.has('cmdb:' + action) },

    async login(u, p) {
      this._save(await apiLogin(u, p))
    },

    // portalAuth 用运维平台下发的一次性 token 换 CMDB 会话
    async portalAuth(portalToken) {
      const { data } = await http.post('/portal-auth', { token: portalToken })
      this._save(data)
      return data
    },

    // refreshPermissions 管理员改完角色，用户点一下就生效，不必重登。
    // 若访问权已被撤销，后端返回 403 并作废会话，由 http 拦截器统一踢回登录页。
    async refreshPermissions() {
      if (this.isLocal) return
      const { data } = await http.get('/refresh-permissions')
      if (data?.permissions) {
        this.permissions = data.permissions
        localStorage.setItem(PERMS_KEY, JSON.stringify(data.permissions))
      }
      return data
    },

    _save(r) {
      localStorage.setItem(TOKEN_KEY, r.token)
      const u = {
        username: r.username,
        display_name: r.display_name,
        auth_source: r.auth_source || 'local',
        role: r.role,
      }
      localStorage.setItem('cmdb_user', JSON.stringify(u))
      this.user = u
      this.permissions = r.permissions || {}
      localStorage.setItem(PERMS_KEY, JSON.stringify(this.permissions))
    },

    logout() {
      // 通知后端作废会话；失败也继续，本地状态必须清干净
      http.post('/logout').catch(() => {})
      localStorage.removeItem(TOKEN_KEY)
      localStorage.removeItem('cmdb_user')
      localStorage.removeItem(PERMS_KEY)
      this.user = null
      this.permissions = {}
    },
  },
})
