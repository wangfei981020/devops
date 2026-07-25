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

// 解析层（域名下挂的解析记录）
export const listAllRecords = (status) => http.get('/records', { params: status ? { status } : {} }).then((r) => r.data) // 拉平：主机头台账（status: ''/ignored/all）
export const listRecords = (ciid) => http.get(`/domains/${ciid}/records`).then((r) => r.data)
export const createRecord = (ciid, b) => http.post(`/domains/${ciid}/records`, b).then((r) => r.data)
export const updateRecord = (id, b) => http.put('/records/' + id, b).then((r) => r.data)
export const bulkUpdateRecords = (b) => http.post('/records/bulk-update', b).then((r) => r.data) // 批量设项目/环境/模块
export const bulkIgnoreRecords = (b) => http.post('/records/bulk-ignore', b).then((r) => r.data) // 批量忽略/取消忽略
export const deleteRecord = (id) => http.delete('/records/' + id).then((r) => r.data)
export const checkRecordCert = (id) => http.post(`/records/${id}/check-cert`).then((r) => r.data)
export const checkAllRecordCerts = (ciid) => http.post(`/domains/${ciid}/check-all-certs`).then((r) => r.data)
export const syncDomainRecords = (ciid) => http.post(`/domains/${ciid}/sync-records`).then((r) => r.data)
// 源站映射规则（回源CNAME → 源站IP）
export const listOriginRules = () => http.get('/origin-rules').then((r) => r.data)
export const upsertOriginRule = (b) => http.post('/origin-rules', b).then((r) => r.data)
export const deleteOriginRule = (id) => http.delete('/origin-rules/' + id).then((r) => r.data)

// DNS 记录（厂商原始记录缓存，只读）
export const listDnsRecords = (ciid) => http.get(`/domains/${ciid}/dns-records`).then((r) => r.data)
// DNS 解析写回 GoDaddy（增/改/删）
export const createDnsRecord = (ciid, b) => http.post(`/domains/${ciid}/dns-records`, b).then((r) => r.data)
export const batchCreateDnsRecords = (ciid, records) => http.post(`/domains/${ciid}/dns-records/batch`, { records }).then((r) => r.data)
export const batchDeleteDnsRecords = (ciid, ids) => http.post(`/domains/${ciid}/dns-records/batch-delete`, { ids }).then((r) => r.data)
export const batchUpdateDnsRecords = (ciid, records) => http.post(`/domains/${ciid}/dns-records/batch-update`, { records }).then((r) => r.data)
export const updateDnsRecord = (id, b) => http.put(`/dns-records/${id}`, b).then((r) => r.data)
export const deleteDnsRecord = (id) => http.delete(`/dns-records/${id}`).then((r) => r.data)
// 域名续费 / 自动续费（写回 GoDaddy；续费会真实扣费）
export const godaddyDetail = (ciid) => http.get(`/domains/${ciid}/godaddy-detail`).then((r) => r.data)
export const renewDomain = (ciid, body) => http.post(`/domains/${ciid}/renew`, body).then((r) => r.data)
export const setAutoRenew = (ciid, enabled) => http.post(`/domains/${ciid}/auto-renew`, { enabled }).then((r) => r.data)
export const listRenewals = (params) => http.get('/renewals', { params }).then((r) => r.data) // 续费记录历史

// 证书巡检（跨所有域名的线上证书 + ACME 签发）
export const listCertInspect = () => http.get('/cert-inspect').then((r) => r.data)
export const recordCertIgnore = (id, b) => http.put(`/records/${id}/cert-ignore`, b).then((r) => r.data)

// 数据源同步 + API 用量（数据源 = 注册商 registrars）
export const syncSource = (id) => http.post(`/sources/${id}/sync`).then((r) => r.data)
export const syncSourceStatus = (id) => http.get(`/sources/${id}/sync-status`).then((r) => r.data)
export const bulkIgnoreDomains = (ci_ids, ignored, reason) => http.post('/domains/bulk-ignore', { ci_ids, ignored, reason }).then((r) => r.data)
export const sourceUsage = (id) => http.get(`/sources/${id}/usage`).then((r) => r.data)

// CDN 厂商（基础配置）
export const listCdns = () => http.get('/cdns').then((r) => r.data)
export const createCdn = (b) => http.post('/cdns', b).then((r) => r.data)
export const updateCdn = (id, b) => http.put('/cdns/' + id, b).then((r) => r.data)
export const deleteCdn = (id) => http.delete('/cdns/' + id).then((r) => r.data)

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
export const downloadCert = (id) => http.get(`/certs/${id}/download`, { responseType: 'blob' }).then((r) => r.data)

// 定时任务
export const listScheduledTasks = () => http.get('/scheduled-tasks').then((r) => r.data)
export const updateScheduledTask = (key, data) => http.put(`/scheduled-tasks/${key}`, data).then((r) => r.data)
export const runScheduledTask = (key) => http.post(`/scheduled-tasks/${key}/run`).then((r) => r.data)
export const listTaskRuns = (params) => http.get('/task-runs', { params }).then((r) => r.data)
export const retryTaskRunFailures = (id) => http.post(`/task-runs/${id}/retry-failures`).then((r) => r.data)
export const cancelTaskRun = (id) => http.post(`/task-runs/${id}/cancel`).then((r) => r.data)

// 通知
// 云主机（GCP，只读）
export const listHosts = () => http.get('/hosts').then((r) => r.data)
export const getHost = (ciid, asOf) => http.get(`/hosts/${ciid}`, { params: asOf ? { as_of: asOf } : {} }).then((r) => r.data)
export const listCloudAccounts = () => http.get('/cloud-accounts').then((r) => r.data)
export const createCloudAccount = (b) => http.post('/cloud-accounts', b).then((r) => r.data)
export const updateCloudAccount = (id, b) => http.put(`/cloud-accounts/${id}`, b).then((r) => r.data)
export const deleteCloudAccount = (id) => http.delete(`/cloud-accounts/${id}`).then((r) => r.data)
export const syncCloudAccount = (id) => http.post(`/cloud-accounts/${id}/sync`).then((r) => r.data)
export const cloudAccountSyncStatus = (id) => http.get(`/cloud-accounts/${id}/sync-status`).then((r) => r.data)
// 云项目（凭据在这一层）
export const createCloudProject = (accountId, b) => http.post(`/cloud-accounts/${accountId}/projects`, b).then((r) => r.data)
export const updateCloudProject = (pid, b) => http.put(`/cloud-projects/${pid}`, b).then((r) => r.data)
export const deleteCloudProject = (pid) => http.delete(`/cloud-projects/${pid}`).then((r) => r.data)
export const syncCloudProject = (pid) => http.post(`/cloud-projects/${pid}/sync`).then((r) => r.data)
// 成本费率（分档）
// 云网络资源（多云，当前 GCP）
export const listCloudIps = () => http.get('/cloud-ips').then((r) => r.data)
export const listCloudNetworks = () => http.get('/cloud-networks').then((r) => r.data)
export const listCloudSubnets = () => http.get('/cloud-subnets').then((r) => r.data)
export const listCloudFirewalls = () => http.get('/cloud-firewalls').then((r) => r.data)
export const listCloudLoadBalancers = () => http.get('/cloud-loadbalancers').then((r) => r.data)

export const listComputeRates = () => http.get('/cloud-compute-rates').then((r) => r.data)
export const createComputeRate = (b) => http.post('/cloud-compute-rates', b).then((r) => r.data)
export const updateComputeRate = (id, b) => http.put(`/cloud-compute-rates/${id}`, b).then((r) => r.data)
export const deleteComputeRate = (id) => http.delete(`/cloud-compute-rates/${id}`).then((r) => r.data)
export const listDiskRates = () => http.get('/cloud-disk-rates').then((r) => r.data)
export const createDiskRate = (b) => http.post('/cloud-disk-rates', b).then((r) => r.data)
export const updateDiskRate = (id, b) => http.put(`/cloud-disk-rates/${id}`, b).then((r) => r.data)
export const deleteDiskRate = (id) => http.delete(`/cloud-disk-rates/${id}`).then((r) => r.data)

export const listLarkGroups = () => http.get('/lark-groups').then((r) => r.data)
export const createLarkGroup = (b) => http.post('/lark-groups', b).then((r) => r.data)
export const updateLarkGroup = (id, b) => http.put(`/lark-groups/${id}`, b).then((r) => r.data)
export const deleteLarkGroup = (id) => http.delete(`/lark-groups/${id}`).then((r) => r.data)
export const testLarkGroup = (id) => http.post(`/lark-groups/${id}/test`).then((r) => r.data)
export const listNotifyUsers = () => http.get('/notify-users').then((r) => r.data)
export const createNotifyUser = (data) => http.post('/notify-users', data).then((r) => r.data)
export const deleteNotifyUser = (id) => http.delete(`/notify-users/${id}`).then((r) => r.data)
export const testNotify = () => http.post('/notify/test').then((r) => r.data)

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
// 生命周期状态字典（scope=project/domain，可自定义）
export const listStatuses = (scope) => http.get('/lifecycle-statuses', { params: scope ? { scope } : {} }).then((r) => r.data)
export const createStatus = (b) => http.post('/lifecycle-statuses', b).then((r) => r.data)
export const updateStatus = (id, b) => http.put('/lifecycle-statuses/' + id, b).then((r) => r.data)
export const deleteStatus = (id) => http.delete('/lifecycle-statuses/' + id).then((r) => r.data)
// 批量/单个设主域名生命周期状态
export const bulkDomainStatus = (ci_ids, status) => http.post('/domains/bulk-status', { ci_ids, status }).then((r) => r.data)

// 域名到期刷新
export const refreshDomain = (id) => http.post(`/domains/${id}/refresh`).then((r) => r.data)
export const refreshAllDomains = () => http.post('/domains/refresh-all').then((r) => r.data)

// 登录
export const login = (username, password) => http.post('/login', { username, password }).then((r) => r.data)
