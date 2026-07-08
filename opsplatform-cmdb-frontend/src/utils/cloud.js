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
