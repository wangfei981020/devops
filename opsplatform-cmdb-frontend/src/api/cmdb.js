import http from './http'

// 总览/展示台
export const dashboard = () => http.get('/dashboard').then((r) => r.data)

// CI 类型 / 通用
export const listCITypes = () => http.get('/ci-types').then((r) => r.data)
export const listCIs = (params) => http.get('/cis', { params }).then((r) => r.data)
export const getCI = (id) => http.get('/cis/' + id).then((r) => r.data)

// 域名
export const listDomains = () => http.get('/domains').then((r) => r.data)
export const createDomain = (b) => http.post('/domains', b).then((r) => r.data)
export const updateDomain = (id, b) => http.put('/domains/' + id, b).then((r) => r.data)
export const deleteDomain = (id) => http.delete('/domains/' + id).then((r) => r.data)
export const syncDomains = () => http.post('/domains/sync').then((r) => r.data)

// 注册商
export const listRegistrars = () => http.get('/registrars').then((r) => r.data)
export const createRegistrar = (b) => http.post('/registrars', b).then((r) => r.data)
export const updateRegistrar = (id, b) => http.put('/registrars/' + id, b).then((r) => r.data)
export const deleteRegistrar = (id) => http.delete('/registrars/' + id).then((r) => r.data)

// 证书
export const listCerts = () => http.get('/certs').then((r) => r.data)
export const getCert = (id) => http.get('/certs/' + id).then((r) => r.data)
export const applyCert = (b) => http.post('/certs', b).then((r) => r.data)
export const renewCert = (id) => http.post(`/certs/${id}/renew`).then((r) => r.data)
export const certDnsReady = (id) => http.post(`/certs/${id}/dns-ready`).then((r) => r.data)
export const revokeCert = (id) => http.delete('/certs/' + id).then((r) => r.data)
export const downloadCert = (id) => http.get(`/certs/${id}/download`).then((r) => r.data)

// ACME 账户
export const listAcme = () => http.get('/acme-accounts').then((r) => r.data)
export const createAcme = (b) => http.post('/acme-accounts', b).then((r) => r.data)
export const deleteAcme = (id) => http.delete('/acme-accounts/' + id).then((r) => r.data)

// 关系
export const listRelations = (params) => http.get('/relations', { params }).then((r) => r.data)
export const createRelation = (b) => http.post('/relations', b).then((r) => r.data)
export const deleteRelation = (id) => http.delete('/relations/' + id).then((r) => r.data)

// 设置
export const getSettings = () => http.get('/settings').then((r) => r.data)
export const updateSettings = (b) => http.put('/settings', b).then((r) => r.data)

// 项目 / 环境（基础配置）
export const listProjects = () => http.get('/projects').then((r) => r.data)
export const createProject = (b) => http.post('/projects', b).then((r) => r.data)
export const updateProject = (id, b) => http.put('/projects/' + id, b).then((r) => r.data)
export const deleteProject = (id) => http.delete('/projects/' + id).then((r) => r.data)
export const listEnvironments = () => http.get('/environments').then((r) => r.data)
export const createEnvironment = (b) => http.post('/environments', b).then((r) => r.data)
export const updateEnvironment = (id, b) => http.put('/environments/' + id, b).then((r) => r.data)
export const deleteEnvironment = (id) => http.delete('/environments/' + id).then((r) => r.data)

// 域名到期刷新
export const refreshDomain = (id) => http.post(`/domains/${id}/refresh`).then((r) => r.data)
export const refreshAllDomains = () => http.post('/domains/refresh-all').then((r) => r.data)

// 登录
export const login = (username, password) => http.post('/login', { username, password }).then((r) => r.data)
