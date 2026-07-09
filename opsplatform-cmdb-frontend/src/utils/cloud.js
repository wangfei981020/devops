// 云资源统一着色（全站共用）：厂商/项目/区域/类型 四组独立色系。
// 参考 NetBox / Ant Design 标签色板。厂商/项目=实底，区域/类型=描边。

// ---- 厂商（固定品牌色，实底）----
const PROVIDER = { gcp: '#4285f4', aliyun: '#ff6a00', aws: '#f0a020', tencent: '#13b5b1' }
const PROVIDER_LABEL = { gcp: 'GCP', aliyun: '阿里云', aws: 'AWS', tencent: '腾讯云' }

// ---- 项目/环境（常见环境固定语义色，其余按名 hash；实底）----
const PROJECT_FIXED = {
  生产: '#2f54eb', PROD: '#2f54eb', prod: '#2f54eb',
  测试: '#13c2c2', TEST: '#13c2c2', test: '#13c2c2',
  预发: '#fa8c16', UAT: '#fa8c16', uat: '#fa8c16',
  开发: '#52c41a', DEV: '#52c41a', dev: '#52c41a',
}
const PROJECT_POOL = ['#722ed1', '#eb2f96', '#08979c', '#d48806', '#c41d7f', '#531dab', '#096dd9', '#d4380d']
// ---- 区域（低饱和地理色，每个区域不同；描边）----
const REGION_POOL = ['#5c8a72', '#5c6f8a', '#8a6d5c', '#7a5c8a', '#6d8a5c', '#5c7d8a', '#8a5c6d', '#4d6a8a', '#8a7a5c', '#6a5c8a']
// ---- 类型（柔色，每值不同；描边）----
const TYPE_POOL = ['#8a5c8a', '#5c8a8a', '#8a7a5c', '#5c6f8a', '#7a5c8a', '#5c8a6d']

function hashPick(name, pool) {
  let h = 0
  const s = String(name || '')
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) | 0
  return pool[Math.abs(h) % pool.length]
}
function solid(hex) { return { background: hex, color: '#fff', borderColor: hex } }
function outline(hex) { return { background: hex + '1a', color: hex, borderColor: hex + '66' } }

export function providerLabel(p) { return PROVIDER_LABEL[p] || p || '—' }
export function providerStyle(p) { return solid(PROVIDER[p] || '#8896a5') }
export function projectStyle(name) { return solid(PROJECT_FIXED[name] || hashPick(name, PROJECT_POOL)) }
export function regionStyle(name) { return outline(hashPick(name, REGION_POOL)) }
export function typeStyle(name) { return outline(hashPick(name, TYPE_POOL)) }

// ---- 域名来源/注册商（品牌色，实底；未知按名 hash）----
const REGISTRAR = {
  godaddy: '#4CAF50', dnspod: '#1E88E5', aliyun: '#ff6a00', cloudflare: '#F6821F',
  namecheap: '#D4202C', tencent: '#13b5b1', name: '#0f4c81', 'google-domains': '#4285f4',
}
export function registrarStyle(name) {
  const k = String(name || '').toLowerCase()
  return solid(REGISTRAR[k] || hashPick(name, PROJECT_POOL))
}

// ---- 域名状态分类（展示标签 + 颜色 + 下拉筛选）----
const DOMAIN_CAT = {
  active: { label: '活跃', hex: '#52c41a', kind: 'solid' },
  pending: { label: '待激活', hex: '#1677ff', kind: 'solid' },
  dns_migrated: { label: 'DNS已迁移', hex: '#7a5c8a', kind: 'solid' },
  expired: { label: '已过期', hex: '#fa8c16', kind: 'outline' },
  transferred_out: { label: '已转出', hex: '#8c8c8c', kind: 'outline' },
  cancelled: { label: '已取消', hex: '#8c8c8c', kind: 'outline' },
  ownership: { label: '已过户', hex: '#8c8c8c', kind: 'outline' },
  removed: { label: '已移出账号', hex: '#595959', kind: 'solid' },
  ignored: { label: '已忽略', hex: '#bfbfbf', kind: 'outline' },
  unknown: { label: '未知', hex: '#fa541c', kind: 'outline' },
}
export function domainCatLabel(c) { return (DOMAIN_CAT[c] || {}).label || c || '—' }
export function domainCatStyle(c) { const m = DOMAIN_CAT[c] || { hex: '#8896a5', kind: 'outline' }; return m.kind === 'solid' ? solid(m.hex) : outline(m.hex) }
// 下拉筛选顺序：活跃/待激活 在前，异常态在后
export const DOMAIN_CAT_ORDER = ['active', 'pending', 'dns_migrated', 'expired', 'transferred_out', 'cancelled', 'ownership', 'removed', 'ignored', 'unknown']
