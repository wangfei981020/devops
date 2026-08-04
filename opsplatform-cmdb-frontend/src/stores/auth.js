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
    // 本次页面加载有没有跟运维平台对过一次权限。
    // 见 ensureFresh——路由守卫必须等它，否则会拿旧快照做拦截判断。
    permsFresh: false,
    _freshing: null,
  }),
  getters: {
    isLoggedIn: () => !!localStorage.getItem(TOKEN_KEY),
    // 本地账号是运维平台不可用时的兜底通道，不受权限码约束（后端同样放行）
    isLocal: (s) => s.user?.auth_source !== 'portal',
    // 不受权限码约束的两类人，必须和后端 IsAdmin 保持一致：
    //   本地兜底账号，以及运维平台的超管（SSO 进来的 role=admin）。
    // 漏掉后者的后果实测过：超管菜单全空、每个路由都被拦，
    // 而后端其实是放行的——两边对"这个人能干什么"理解不一致最难查。
    isUnrestricted: (s) => s.user?.auth_source !== 'portal' || s.user?.is_admin === true,
  },
  actions: {
    // has 单个权限码：本地账号全放行，portal 账号严格查表。
    // 前端这层只管显隐，真正的拦截在后端——少一个 v-if 是体验问题，不是安全问题。
    has(code) {
      if (!code) return true
      if (this.isUnrestricted) return true
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

    // ensureFresh 保证"做权限判断之前，权限是新的"。整个页面生命周期只拉一次。
    //
    //	为什么必须有它：路由守卫是从 localStorage 的权限快照做拦截判断的，
    //	而刷新权限原本放在 App.vue 的 onMounted——也就是**守卫先跑、刷新后到**。
    //	结果就是菜单按新权限渲染（该有的都有），路由按旧快照拦截（全被挡），
    //	用户看到的是"菜单里有『总览』，点进去说无权访问"这种自相矛盾的画面。
    //	刚拿到权限的新用户、以及管理员刚调过角色的人，第一次进来必踩。
    //
    //	拉失败**不阻断**：沿用快照放行到下一步，由后端做真正的拦截。
    //	问不到答案时把人挡在门外，比让他进去被后端拒更糟——
    //	后者至少能看见具体缺哪个权限码。
    async ensureFresh() {
      if (this.isLocal || this.permsFresh) return
      if (this._freshing) return this._freshing // 并发导航去重，只发一次请求
      this._freshing = this.refreshPermissions()
        .catch(() => {})
        .finally(() => { this.permsFresh = true; this._freshing = null })
      return this._freshing
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
      // 超管标记也要跟着刷新——只更权限不更这个，刷一次菜单又空了
      if (this.user && typeof data?.is_admin === 'boolean') {
        this.user = { ...this.user, is_admin: data.is_admin }
        localStorage.setItem('cmdb_user', JSON.stringify(this.user))
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
        is_admin: r.is_admin === true,
      }
      localStorage.setItem('cmdb_user', JSON.stringify(u))
      this.user = u
      this.permissions = r.permissions || {}
      localStorage.setItem(PERMS_KEY, JSON.stringify(this.permissions))
      this.permsFresh = true // 刚登录拿到的就是最新的，不必再拉一次
    },

    logout() {
      // 通知后端作废会话；失败也继续，本地状态必须清干净
      http.post('/logout').catch(() => {})
      localStorage.removeItem(TOKEN_KEY)
      localStorage.removeItem('cmdb_user')
      localStorage.removeItem(PERMS_KEY)
      this.user = null
      this.permissions = {}
      this.permsFresh = false
    },
  },
})
