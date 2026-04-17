import axios from 'axios'

const api = axios.create({
  baseURL: '/api',
  timeout: 60000,
  withCredentials: true
})

api.interceptors.response.use(
  response => response.data,
  error => {
    return Promise.reject(error)
  }
)

export default api

// ===== 项目 =====
export const projectsApi = {
  list: () => api.get('/projects'),
  get: (id) => api.get(`/projects/${id}`),
  create: (data) => api.post('/projects', data),
  update: (id, data) => api.put(`/projects/${id}`, data),
  delete: (id) => api.delete(`/projects/${id}`)
}

// ===== 环境 =====
export const environmentsApi = {
  list: () => api.get('/environments'),
  create: (data) => api.post('/environments', data),
  update: (id, data) => api.put(`/environments/${id}`, data),
  delete: (id) => api.delete(`/environments/${id}`),
  testArgocd: (id) => api.post(`/environments/${id}/test-argocd`)
}

// ===== 项目-环境 =====
export const projectEnvsApi = {
  list: (params) => api.get('/project-envs', { params }),
  get: (id) => api.get(`/project-envs/${id}`),
  create: (data) => api.post('/project-envs', data),
  update: (id, data) => api.put(`/project-envs/${id}`, data),
  delete: (id) => api.delete(`/project-envs/${id}`),
  testGit: (id) => api.post(`/project-envs/${id}/test-git`),
  testArgocd: (id) => api.post(`/project-envs/${id}/test-argocd`)
}

// ===== Chart 模板 =====
export const chartTemplatesApi = {
  list: (params) => api.get('/chart-templates', { params }),
  get: (id) => api.get(`/chart-templates/${id}`),
  create: (data) => api.post('/chart-templates', data),
  update: (id, data) => api.put(`/chart-templates/${id}`, data),
  delete: (id) => api.delete(`/chart-templates/${id}`),
  preview: (id, data) => api.post(`/chart-templates/${id}/preview`, data)
}

// ===== 模块 =====
export const modulesApi = {
  list: (params) => api.get('/modules', { params }),
  get: (id) => api.get(`/modules/${id}`),
  create: (data) => api.post('/modules', data),
  update: (id, data) => api.put(`/modules/${id}`, data),
  delete: (id) => api.delete(`/modules/${id}`),
  updateImage: (id, data) => api.post(`/modules/${id}/update-image`, data),
  restart: (id) => api.post(`/modules/${id}/restart`),
  scale: (id, data) => api.post(`/modules/${id}/scale`, data),
  sync: (id) => api.post(`/modules/${id}/sync`),
  rollback: (id, data) => api.post(`/modules/${id}/rollback`, data),
  values: (id) => api.get(`/modules/${id}/values`),
  runtime: (id) => api.get(`/modules/${id}/runtime`)
}

// ===== Secret =====
export const secretsApi = {
  list: (params) => api.get('/secrets', { params }),
  get: (id, reveal = false) => api.get(`/secrets/${id}`, { params: reveal ? { reveal: 1 } : {} }),
  create: (data) => api.post('/secrets', data),
  update: (id, data) => api.put(`/secrets/${id}`, data),
  delete: (id) => api.delete(`/secrets/${id}`),
  batchUpdate: (data) => api.post('/secrets/batch-update', data),
  referencedBy: (id) => api.get(`/secrets/${id}/referenced-by`)
}

// ===== 通知人 =====
export const contactsApi = {
  list: () => api.get('/contacts'),
  get: (id) => api.get(`/contacts/${id}`),
  create: (data) => api.post('/contacts', data),
  update: (id, data) => api.put(`/contacts/${id}`, data),
  delete: (id) => api.delete(`/contacts/${id}`)
}

// ===== Lark 配置 =====
export const larkConfigsApi = {
  list: () => api.get('/lark-configs'),
  create: (data) => api.post('/lark-configs', data),
  update: (id, data) => api.put(`/lark-configs/${id}`, data),
  delete: (id) => api.delete(`/lark-configs/${id}`),
  test: (id) => api.post(`/lark-configs/${id}/test`)
}

// ===== 通知绑定 =====
export const projectEnvNotifyApi = {
  get: (project_env_id) => api.get('/project-env-notify', { params: { project_env_id } }),
  set: (data) => api.put('/project-env-notify', data)
}

// ===== 发布历史 =====
export const deploymentsApi = {
  list: (params) => api.get('/deployments', { params }),
  get: (id) => api.get(`/deployments/${id}`),
  diff: (id) => api.get(`/deployments/${id}/diff`)
}

// ===== Harbor 代理 =====
export const harborApi = {
  projects: () => api.get('/harbor/projects'),
  repositories: (project) => api.get('/harbor/repositories', { params: { project } }),
  tags: (repo) => api.get('/harbor/tags', { params: { repo } })
}

// ===== ArgoCD 代理 =====
export const argocdApi = {
  applications: () => api.get('/argocd/applications'),
  application: (name) => api.get(`/argocd/applications/${name}`),
  status: (name) => api.get(`/argocd/applications/${name}/status`),
  sync: (name) => api.post(`/argocd/applications/${name}/sync`),
  events: (name) => api.get(`/argocd/applications/${name}/events`)
}

// ===== 全局配置 =====
export const globalConfigApi = {
  get: () => api.get('/global-config'),
  update: (data) => api.put('/global-config', data),
  testGitlab: () => api.post('/global-config/test-gitlab'),
  testHarbor: () => api.post('/global-config/test-harbor'),
  testArgocd: () => api.post('/global-config/test-argocd')
}

// ===== Env 模板 =====
export const envTemplatesApi = {
  list: () => api.get('/env-templates'),
  create: (data) => api.post('/env-templates', data),
  update: (id, data) => api.put(`/env-templates/${id}`, data),
  delete: (id) => api.delete(`/env-templates/${id}`)
}
