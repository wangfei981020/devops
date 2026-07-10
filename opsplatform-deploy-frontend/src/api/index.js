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
      // 40901「资源冲突 / 进行中」由调用方自处理（如 VM 发布冲突弹专属弹窗），不走通用 ElMessage
      if (resp.data.code !== 40901) {
        ElMessage.error(resp.data.message || '请求失败')
      }
      return Promise.reject(resp.data)
    }
    return resp.data?.data
  },
  (err) => {
    const status = err.response?.status
    if (status === 401) {
      // 关键：URL 含 portal_token 时，正在走 SSO 入站握手（router beforeEach 的 portalAuth）。
      // 这时如果 App.vue onMounted 的 recoverInflight 等"启动期 API 调用"用了过期的 deploy_token
      // 撞 401，不能 hard reload，否则会把握手中的 portalAuth 也冲掉，用户回到 /login。
      // 让 401 静默 reject，beforeEach 的 portalAuth 会接着把新 token 写进 localStorage。
      const hasPortalToken = new URLSearchParams(location.search).has('portal_token')
      if (!hasPortalToken) {
        localStorage.removeItem('deploy_token')
        localStorage.removeItem('deploy_user')
        localStorage.removeItem('deploy_permissions')
        if (location.pathname !== '/login') {
          location.href = '/login'
        }
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
export const testMinIO = (data) => http.post('/global-config/test-minio', data || {})
// Harbor 测试连接 + 拉镜像 tag 列表 + 刷新 ArgoCD app 缓存
export const testHarbor = (data) => http.post('/global-config/test-harbor', data || {})
// listHarborTags(envID, module, page=1, limit=100) → { module, image_repo, page, tags, has_more, fetched_at }
export const listHarborTags = (envID, module, page, limit) =>
  http.get('/harbor/tags', { params: {
    project_env_id: envID,
    module,
    page: page || 1,
    limit: limit || 100,
  }})
// 用户手动刷新 ArgoCD app 列表缓存
export const refreshArgocdAppCache = (envID) =>
  http.post('/argocd-app-cache/refresh', null, { params: { project_env_id: envID } })

// History cleanup
export const previewHistoryCleanup = (params) => http.get('/history-cleanup/preview', { params })
export const runHistoryCleanup = (data) => http.post('/history-cleanup', data || {})

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
// testLarkBot body 方式，未保存也能测。body = { id?, webhook?, secret? }
export const testLarkBot = (body) => http.post('/lark-bots/test', body || {})

// GitLab 仓库（登记 + project_env 下拉选）
export const listGitlabRepos = () => http.get('/gitlab-repos')
export const createGitlabRepo = (data) => http.post('/gitlab-repos', data)
export const updateGitlabRepo = (id, data) => http.put(`/gitlab-repos/${id}`, data)
export const deleteGitlabRepo = (id) => http.delete(`/gitlab-repos/${id}`)

// ArgoCD Instances
export const listArgocdInstances = () => http.get('/argocd-instances')
export const createArgocdInstance = (data) => http.post('/argocd-instances', data)
export const updateArgocdInstance = (id, data) => http.put(`/argocd-instances/${id}`, data)
export const deleteArgocdInstance = (id) => http.delete(`/argocd-instances/${id}`)
// testArgocdInstanceByBody 新：body 方式，未保存也能测
// body = { id?, url?, token? }
export const testArgocdInstance = (body) => http.post('/argocd-instances/test', body || {})

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
// testProjectEnvGit body = { git_repo, git_branch }
export const testProjectEnvGit = (body) => http.post('/project-envs/test-git', body || {})
// testProjectEnvArgocd body = { argocd_instance_id }
export const testProjectEnvArgocd = (body) => http.post('/project-envs/test-argocd', body || {})
export const scanModules = (id) => http.post(`/project-envs/${id}/scan-modules`)

// 服务编排（Service Orchestration）
export const listTemplates = (params) => http.get('/orchestration/templates', { params })
export const createTemplate = (data) => http.post('/orchestration/templates', data)
export const updateTemplate = (id, data) => http.put(`/orchestration/templates/${id}`, data)
export const deleteTemplate = (id) => http.delete(`/orchestration/templates/${id}`)
// prefill body = { template_id, target_env_id, module_name } → { values_yaml, suggest_namespace, ... }
export const prefillModule = (body) => http.post('/orchestration/prefill', body)
// preview/submit body = { template_id, target_env_id, module_name, namespace, values_yaml, disable }
export const previewModule = (body) => http.post('/orchestration/preview', body)
export const submitModule = (body) => http.post('/orchestration/submit', body)
// batch body = { template_id, target_env_id, disable, rows: [{module_name, namespace}] }
export const batchPreviewModules = (body) => http.post('/orchestration/batch-preview', body)
export const batchSubmitModules = (body) => http.post('/orchestration/batch-submit', body)

// 可配置环境（dev/test/uat/prod）
export const listEnvironments = () => http.get('/environments')
export const createEnvironment = (data) => http.post('/environments', data)
export const updateEnvironment = (name, data) => http.put(`/environments/${name}`, data)
export const deleteEnvironment = (name) => http.delete(`/environments/${name}`)

// Modules
export const listModules = (projectEnvID) => http.get('/modules', { params: { project_env_id: projectEnvID } })
export const getModuleTagHistory = (id, limit = 10) => http.get(`/modules/${id}/tag-history`, { params: { limit } })

// Deployments
export const listDeployments = (params) => http.get('/deployments', { params })
export const getDeployment = (id) => http.get(`/deployments/${id}`)
// 取消等待：仅 status=pending 的发布可取消，git 改动不会回滚
export const cancelDeployment = (id) => http.post(`/deployments/${id}/cancel`)
export const getRollbackPreview = (id) => http.get(`/deployments/${id}/rollback-preview`)
export const getDeploymentPods = (id, app) => http.get(`/deployments/${id}/pods`, { params: { app } })
export const getDeploymentPodLogs = (id, params) => http.get(`/deployments/${id}/pod-logs`, { params })
export const getDeploymentPodEvents = (id, params) => http.get(`/deployments/${id}/pod-events`, { params })
export const getDeploymentArchivedPods = (id, app) => http.get(`/deployments/${id}/archived-pods`, { params: { app } })

// Audit logs (admin only)
export const listAuditLogs = (params) => http.get('/audit-logs', { params })
export const listAuditActionTypes = () => http.get('/audit-logs/action-types')

// Dashboard
export const getDashboardStats = () => http.get('/dashboard/stats')

// Deploy actions (K8s)
export const previewImage = (data) => http.post('/deploy/preview-image', data)
export const updateImage = (data) => http.post('/deploy/update-image', data)
export const restartModules = (data) => http.post('/deploy/restart', data)
export const rollback = (data) => http.post('/deploy/rollback', data)

// VM 相关：deploy_agent / vm_project_env / vm_service / VM deploy
// agent
export const listDeployAgents   = () => http.get('/deploy-agents')
export const createDeployAgent  = (data) => http.post('/deploy-agents', data)
export const updateDeployAgent  = (id, data) => http.put(`/deploy-agents/${id}`, data)
export const deleteDeployAgent  = (id) => http.delete(`/deploy-agents/${id}`)
// testDeployAgent body { id?, url?, token? }，未保存也能测
export const testDeployAgent    = (body) => http.post('/deploy-agents/test', body || {})

// vm project env
export const listVmProjectEnvs   = (projectId) => http.get('/vm-project-envs', { params: projectId ? { project_id: projectId } : {} })
export const getVmProjectEnv     = (id) => http.get(`/vm-project-envs/${id}`)
export const createVmProjectEnv  = (data) => http.post('/vm-project-envs', data)
export const updateVmProjectEnv  = (id, data) => http.put(`/vm-project-envs/${id}`, data)
export const deleteVmProjectEnv  = (id) => http.delete(`/vm-project-envs/${id}`)
export const scanVmServices      = (id) => http.post(`/vm-project-envs/${id}/scan-services`)
export const listVmServices      = (id) => http.get(`/vm-project-envs/${id}/services`)

// versions（list-version API 代理）
export const listVmServiceVersions = (id) => http.get(`/vm-services/${id}/versions`)

// VM 部署
export const vmDeploy            = (data) => http.post('/deploy/vm-run', data)
export const vmDeployCancel      = (id) => http.post(`/deployments/${id}/vm-cancel`)
// vm-logs 走 SSE，前端用 EventSource 或 fetch+ReadableStream，这里只导出 URL 给上层用
export const vmDeployLogsURL     = (id, since = 0, stream = true) =>
  `/api/deployments/${id}/vm-logs?since=${since}${stream ? '&stream=true' : ''}`
// vm 任务终态后归档在 MinIO 的完整 ansible 日志（成败都归档）。
// 批量场景传 service 参数挑某个服务的日志；单服务可省略
// 用原生 fetch 拿 raw text，绕开 axios 的 JSON 响应拦截器
export async function fetchVmArchivedLog(id, service = '') {
  const token = localStorage.getItem('deploy_token') || ''
  const url = service
    ? `/api/deployments/${id}/vm-archived-log?service=${encodeURIComponent(service)}`
    : `/api/deployments/${id}/vm-archived-log`
  const resp = await fetch(url, {
    headers: { Authorization: 'Bearer ' + token },
  })
  const text = await resp.text()
  if (!resp.ok) {
    try { const j = JSON.parse(text); throw new Error(j.message || `HTTP ${resp.status}`) }
    catch (e) { if (e.message?.startsWith('HTTP ')) throw e; throw new Error(text || `HTTP ${resp.status}`) }
  }
  return { text, size: Number(resp.headers.get('X-Log-Size') || text.length), status: resp.headers.get('X-Log-Status') || '' }
}

export default http
