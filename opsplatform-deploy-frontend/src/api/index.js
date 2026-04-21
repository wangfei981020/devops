import axios from 'axios'
import { ElMessage } from 'element-plus'

const http = axios.create({
  baseURL: '/api',
  timeout: 30000,
})

// 请求拦截器：挂 Authorization Bearer token
http.interceptors.request.use((cfg) => {
  const token = localStorage.getItem('deploy_token')
  if (token) {
    cfg.headers = cfg.headers || {}
    cfg.headers['Authorization'] = 'Bearer ' + token
  }
  return cfg
})

http.interceptors.response.use(
  (resp) => {
    if (resp.data && resp.data.code !== 0) {
      ElMessage.error(resp.data.message || '请求失败')
      return Promise.reject(resp.data)
    }
    return resp.data?.data
  },
  (err) => {
    const status = err.response?.status
    if (status === 401) {
      localStorage.removeItem('deploy_token')
      localStorage.removeItem('deploy_user')
      localStorage.removeItem('deploy_permissions')
      if (location.pathname !== '/login') {
        location.href = '/login'
      }
    }
    const msg = err.response?.data?.message || err.message || '网络错误'
    if (status !== 401) ElMessage.error(msg)
    return Promise.reject(err)
  }
)

// Global Config
export const getGlobalConfig = () => http.get('/global-config')
export const updateGlobalConfig = (data) => http.put('/global-config', data)
export const testGitlab = (data) => http.post('/global-config/test-gitlab', data || {})

// Users（平台登录账号，admin only）
export const listUsers = () => http.get('/users')
export const createUser = (data) => http.post('/users', data)
export const updateUser = (id, data) => http.put(`/users/${id}`, data)
export const toggleUser = (id) => http.put(`/users/${id}/toggle`)
export const resetUserPassword = (id, password) => http.post(`/users/${id}/reset-password`, { password })
export const deleteUser = (id) => http.delete(`/users/${id}`)

// Auth
export const apiLogin = (data) => http.post('/login', data)
export const apiLogout = () => http.post('/logout')
export const getCurrentUser = () => http.get('/users/me')
export const refreshPerms = () => http.get('/refresh-permissions')

// Contacts（通知人，Lark 艾特专用）
export const listContacts = () => http.get('/contacts')
export const createContact = (data) => http.post('/contacts', data)
export const updateContact = (id, data) => http.put(`/contacts/${id}`, data)
export const deleteContact = (id) => http.delete(`/contacts/${id}`)

// Lark Bots
export const listLarkBots = () => http.get('/lark-bots')
export const createLarkBot = (data) => http.post('/lark-bots', data)
export const updateLarkBot = (id, data) => http.put(`/lark-bots/${id}`, data)
export const deleteLarkBot = (id) => http.delete(`/lark-bots/${id}`)
export const testLarkBot = (id) => http.post(`/lark-bots/${id}/test`)

// ArgoCD Instances
export const listArgocdInstances = () => http.get('/argocd-instances')
export const createArgocdInstance = (data) => http.post('/argocd-instances', data)
export const updateArgocdInstance = (id, data) => http.put(`/argocd-instances/${id}`, data)
export const deleteArgocdInstance = (id) => http.delete(`/argocd-instances/${id}`)
export const testArgocdInstance = (id) => http.post(`/argocd-instances/${id}/test`)

// Projects (空项目注册表 + 派生)
export const listProjects = () => http.get('/projects')
export const createProject = (data) => http.post('/projects', data)
export const updateProject = (id, data) => http.put(`/projects/${id}`, data)
export const deleteProject = (id) => http.delete(`/projects/${id}`)

// Project Envs
export const listProjectEnvs = () => http.get('/project-envs')
export const getProjectEnv = (id) => http.get(`/project-envs/${id}`)
export const createProjectEnv = (data) => http.post('/project-envs', data)
export const updateProjectEnv = (id, data) => http.put(`/project-envs/${id}`, data)
export const deleteProjectEnv = (id) => http.delete(`/project-envs/${id}`)
export const testProjectEnvGit = (id) => http.post(`/project-envs/${id}/test-git`)
export const testProjectEnvArgocd = (id) => http.post(`/project-envs/${id}/test-argocd`)
export const scanModules = (id) => http.post(`/project-envs/${id}/scan-modules`)

// Modules
export const listModules = (projectEnvID) => http.get('/modules', { params: { project_env_id: projectEnvID } })
export const getModuleTagHistory = (id, limit = 10) => http.get(`/modules/${id}/tag-history`, { params: { limit } })

// Deployments
export const listDeployments = (params) => http.get('/deployments', { params })
export const getDeployment = (id) => http.get(`/deployments/${id}`)
export const getRollbackPreview = (id) => http.get(`/deployments/${id}/rollback-preview`)

// Dashboard
export const getDashboardStats = () => http.get('/dashboard/stats')

// Deploy actions
export const previewImage = (data) => http.post('/deploy/preview-image', data)
export const updateImage = (data) => http.post('/deploy/update-image', data)
export const restartModules = (data) => http.post('/deploy/restart', data)
export const rollback = (data) => http.post('/deploy/rollback', data)

export default http
