import axios from 'axios'
import { ElMessage } from 'element-plus'

const http = axios.create({
  baseURL: '/api',
  timeout: 30000,
})

// 请求拦截器：自动加 X-Operator header
http.interceptors.request.use((cfg) => {
  const identity = localStorage.getItem('deploy_identity')
  if (identity) {
    cfg.headers = cfg.headers || {}
    cfg.headers['X-Operator'] = identity
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
    const msg = err.response?.data?.message || err.message || '网络错误'
    ElMessage.error(msg)
    return Promise.reject(err)
  }
)

// Global Config
export const getGlobalConfig = () => http.get('/global-config')
export const updateGlobalConfig = (data) => http.put('/global-config', data)
export const testGitlab = (data) => http.post('/global-config/test-gitlab', data || {})

// Accounts（身份选择 / Lark 艾特）
export const listAccounts = () => http.get('/accounts')
export const createAccount = (data) => http.post('/accounts', data)
export const updateAccount = (id, data) => http.put(`/accounts/${id}`, data)
export const deleteAccount = (id) => http.delete(`/accounts/${id}`)

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

// Deploy actions
export const previewImage = (data) => http.post('/deploy/preview-image', data)
export const updateImage = (data) => http.post('/deploy/update-image', data)
export const restartModules = (data) => http.post('/deploy/restart', data)
export const rollback = (data) => http.post('/deploy/rollback', data)

export default http
