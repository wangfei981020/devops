import { ref, computed } from 'vue'
import { domainCatLabel, DOMAIN_CAT_ORDER } from '../utils/cloud'

// 域名类页面统一筛选：状态分类 + 来源(注册商) + 到期档位 + 关键词。
// 传入一个域名数组的 ref/computed（每项需含 category / registrar_name / origin / expiry_at / name）。
const ABNORMAL_CATS = ['dns_migrated', 'expired', 'transferred_out', 'cancelled', 'ownership', 'removed', 'unknown']

export function expiryTier(expiryAt) {
  if (!expiryAt) return 'none'
  const days = (new Date(expiryAt) - Date.now()) / 86400000
  if (days < 0) return 'expired'
  if (days < 30) return 'soon'
  return 'normal'
}

export function useDomainFilter(source) {
  const kw = ref('')
  const query = ref('')
  const view = ref('active') // 状态分类
  const src = ref('') // 来源 registrar_name，空=全部
  const expiry = ref('') // 到期档位：expired/soon/normal，空=全部

  const catCounts = computed(() => {
    const m = {}
    for (const d of source.value) { const c = d.category || 'active'; m[c] = (m[c] || 0) + 1 }
    m.all = source.value.length
    m.abnormal = source.value.filter((d) => ABNORMAL_CATS.includes(d.category || 'active')).length
    return m
  })
  const viewOptions = computed(() => {
    const cc = catCounts.value
    const opts = [{ value: 'active', label: '活跃' }, { value: 'pending', label: '待激活' }, { value: 'abnormal', label: '异常' }]
    for (const c of DOMAIN_CAT_ORDER) {
      if (['active', 'pending', 'ignored'].includes(c)) continue
      if (cc[c]) opts.push({ value: c, label: domainCatLabel(c) })
    }
    opts.push({ value: 'ignored', label: '已忽略' }, { value: 'all', label: '全部' })
    return opts.filter((o, i, a) => a.findIndex((x) => x.value === o.value) === i).map((o) => ({ ...o, count: cc[o.value] || 0 }))
  })
  const sourceOptions = computed(() => [...new Set(source.value.filter((d) => d.origin !== 'manual').map((d) => d.registrar_name).filter(Boolean))])

  const filtered = computed(() => source.value.filter((d) => {
    if (query.value && !d.name.toLowerCase().includes(query.value.toLowerCase())) return false
    const cat = d.category || 'active'
    if (view.value === 'abnormal') { if (!ABNORMAL_CATS.includes(cat)) return false } else if (view.value !== 'all') { if (cat !== view.value) return false }
    if (src.value && d.registrar_name !== src.value) return false
    if (expiry.value && expiryTier(d.expiry_at) !== expiry.value) return false
    return true
  }))
  function doSearch() { query.value = kw.value }
  function reset() { kw.value = ''; query.value = ''; view.value = 'active'; src.value = ''; expiry.value = '' }
  return { kw, query, view, src, expiry, filtered, viewOptions, sourceOptions, doSearch, reset }
}
