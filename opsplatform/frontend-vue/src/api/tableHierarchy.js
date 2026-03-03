import api from './index'

const HIERARCHY_KEY = 'table_hierarchy_config'

// 层级配置缓存
let hierarchyCache = null
let cacheTimestamp = 0
const CACHE_TTL = 60000 // 缓存1分钟

// 获取层级配置
export async function getTableHierarchy(forceRefresh = false) {
  const now = Date.now()
  if (!forceRefresh && hierarchyCache && (now - cacheTimestamp) < CACHE_TTL) {
    return hierarchyCache
  }
  
  try {
    const res = await api.get(`/api/settings?key=${HIERARCHY_KEY}`)
    const val = res.data?.[HIERARCHY_KEY]
    if (val) {
      hierarchyCache = JSON.parse(val)
    } else {
      hierarchyCache = {
        projects: [],
        sites: [],
        tables: [],
        maintTypes: ['无', '紧急维护', '临时维护', '例行维护']
      }
    }
    cacheTimestamp = now
    return hierarchyCache
  } catch (e) {
    console.error('加载层级配置失败', e)
    return {
      projects: [],
      sites: [],
      tables: [],
      maintTypes: ['无', '紧急维护', '临时维护', '例行维护']
    }
  }
}

// 保存层级配置
export async function saveTableHierarchy(config) {
  await api.post('/api/settings', {
    [HIERARCHY_KEY]: JSON.stringify(config)
  })
  hierarchyCache = config
  cacheTimestamp = Date.now()
}

// 清除缓存
export function clearHierarchyCache() {
  hierarchyCache = null
  cacheTimestamp = 0
}

// 获取项目列表
export async function getProjects() {
  const config = await getTableHierarchy()
  return config.projects || []
}

// 获取现场列表
export async function getSites() {
  const config = await getTableHierarchy()
  return config.sites || []
}

// 根据项目获取现场列表
export async function getSitesByProject(projectName) {
  const config = await getTableHierarchy()
  return (config.sites || []).filter(s => s.project === projectName)
}

// 根据现场获取桌台列表
export async function getTablesBySite(siteName) {
  const config = await getTableHierarchy()
  return (config.tables || []).filter(t => t.site === siteName)
}

// 获取维护类型列表
export async function getMaintTypes() {
  const config = await getTableHierarchy()
  return config.maintTypes || ['无', '紧急维护', '临时维护', '例行维护']
}

export default {
  getTableHierarchy,
  saveTableHierarchy,
  clearHierarchyCache,
  getProjects,
  getSites,
  getSitesByProject,
  getTablesBySite,
  getMaintTypes
}
