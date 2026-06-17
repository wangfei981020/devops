import { defineStore } from 'pinia'
import { login as apiLogin } from '../api/cmdb'
import { TOKEN_KEY } from '../api/http'

export const useAuthStore = defineStore('auth', {
  state: () => ({ user: JSON.parse(localStorage.getItem('cmdb_user') || 'null') }),
  getters: { isLoggedIn: () => !!localStorage.getItem(TOKEN_KEY) },
  actions: {
    async login(u, p) {
      const r = await apiLogin(u, p)
      localStorage.setItem(TOKEN_KEY, r.token)
      localStorage.setItem('cmdb_user', JSON.stringify(r))
      this.user = r
    },
    logout() {
      localStorage.removeItem(TOKEN_KEY)
      localStorage.removeItem('cmdb_user')
      this.user = null
    },
  },
})
