<template>
  <div>
    <div class="header-bar">
      <button class="btn btn-sm" @click="$router.push('/spaces')">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m15 18-6-6 6-6"/></svg>
        返回空间列表
      </button>
      <div v-if="space" class="space-info">
        <div class="space-avatar">{{ space.key?.charAt(0) }}</div>
        <div>
          <h2>{{ space.name }}</h2>
          <span class="space-key">{{ space.key }}</span>
        </div>
      </div>
    </div>

    <div v-if="loading" class="loading"><div class="spinner"></div>加载中...</div>

    <template v-else>
      <div class="card tree-card">
        <div class="card-header">
          <div class="card-title-group">
            <svg class="card-icon" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 20h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.93a2 2 0 0 1-1.66-.9l-.82-1.2A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13c0 1.1.9 2 2 2Z"/></svg>
            <h3>页面层级</h3>
          </div>
          <div class="card-actions">
            <div class="search-box">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/></svg>
              <input v-model="searchQuery" placeholder="搜索页面..." class="search-input" />
            </div>
            <span class="page-count-badge">{{ searchQuery ? filteredTree.length + ' / ' : '' }}{{ totalPages }} 个页面</span>
            <button v-if="tree.length && !searchQuery" class="btn btn-sm" @click="toggleAll">
              {{ allExpanded ? '全部折叠' : '全部展开' }}
            </button>
          </div>
        </div>

        <div v-if="!filteredTree.length" class="empty-state">
          <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" style="opacity:0.3"><path d="M14.5 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7.5L14.5 2z"/><polyline points="14,2 14,8 20,8"/></svg>
          <p>{{ searchQuery ? '未找到匹配页面' : '暂无页面' }}</p>
        </div>

        <div v-else class="tree-container">
          <tree-node
            v-for="node in filteredTree"
            :key="node.id"
            :node="node"
            :depth="0"
            :expand-all="searchQuery ? true : expandAllFlag"
            @navigate="id => $router.push('/content/' + id)"
          />
        </div>
      </div>
    </template>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, defineComponent, h, watch } from 'vue'
import { useRoute } from 'vue-router'
import api from '@/api'

const route = useRoute()
const spaceKey = route.params.key
const loading = ref(true)
const space = ref(null)
const tree = ref([])
const totalPages = ref(0)
const allExpanded = ref(false)
const expandAllFlag = ref(null) // null = use default, true/false = forced
const searchQuery = ref('')
const allPages = ref([]) // 保存原始页面数据，用于搜索

const filteredTree = computed(() => {
  if (!searchQuery.value.trim()) return tree.value
  const q = searchQuery.value.trim().toLowerCase()
  // 在所有页面中搜索标题匹配的
  const matched = allPages.value.filter(p => p.title.toLowerCase().includes(q))
  if (!matched.length) return []
  return matched.map(p => ({
    id: p.id,
    title: p.title,
    version: p.version?.number || 1,
    children: []
  }))
})

function toggleAll() {
  allExpanded.value = !allExpanded.value
  expandAllFlag.value = allExpanded.value
}

// SVG icon render helpers
function chevronIcon(expanded) {
  return h('svg', {
    width: 16, height: 16, viewBox: '0 0 24 24',
    fill: 'none', stroke: 'currentColor',
    'stroke-width': 2, 'stroke-linecap': 'round', 'stroke-linejoin': 'round',
    class: ['tree-chevron', { expanded }]
  }, [h('path', { d: 'm9 18 6-6-6-6' })])
}

function pageIcon(hasChildren) {
  if (hasChildren) {
    // folder-like icon for parent pages
    return h('svg', {
      width: 16, height: 16, viewBox: '0 0 24 24',
      fill: 'none', stroke: 'currentColor',
      'stroke-width': 2, 'stroke-linecap': 'round', 'stroke-linejoin': 'round',
      class: 'tree-page-icon parent'
    }, [h('path', { d: 'M4 20h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.93a2 2 0 0 1-1.66-.9l-.82-1.2A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13c0 1.1.9 2 2 2Z' })])
  }
  // document icon for leaf pages
  return h('svg', {
    width: 16, height: 16, viewBox: '0 0 24 24',
    fill: 'none', stroke: 'currentColor',
    'stroke-width': 2, 'stroke-linecap': 'round', 'stroke-linejoin': 'round',
    class: 'tree-page-icon leaf'
  }, [
    h('path', { d: 'M14.5 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7.5L14.5 2z' }),
    h('polyline', { points: '14,2 14,8 20,8' }),
    h('line', { x1: 8, y1: 13, x2: 16, y2: 13 }),
    h('line', { x1: 8, y1: 17, x2: 16, y2: 17 })
  ])
}

// Recursive tree node component
const TreeNode = defineComponent({
  name: 'TreeNode',
  props: {
    node: { type: Object, required: true },
    depth: { type: Number, default: 0 },
    expandAll: { default: null }
  },
  emits: ['navigate'],
  setup(props, { emit }) {
    // 默认只展开根节点（depth 0）
    const expanded = ref(props.depth === 0)
    const PAGE_SIZE = 10
    const showCount = ref(PAGE_SIZE)

    watch(() => props.expandAll, (val) => {
      if (val !== null) expanded.value = val
    })

    function toggle() {
      if (props.node.children?.length) {
        expanded.value = !expanded.value
        if (expanded.value) showCount.value = PAGE_SIZE // 重新展开时重置分页
      }
    }

    function showMore() {
      showCount.value += PAGE_SIZE
    }

    return () => {
      const node = props.node
      const hasChildren = node.children && node.children.length > 0
      const indent = 12 + props.depth * 28

      const rowItems = []

      // Toggle chevron
      rowItems.push(
        h('span', {
          class: ['tree-toggle-btn', { hidden: !hasChildren }],
          onClick: (e) => { e.stopPropagation(); toggle() }
        }, [chevronIcon(expanded.value)])
      )

      // Page icon
      rowItems.push(h('span', { class: 'tree-icon-wrap' }, [pageIcon(hasChildren)]))

      // Title and meta
      rowItems.push(
        h('span', { class: 'tree-content' }, [
          h('span', { class: 'tree-title' }, node.title),
          h('span', { class: 'tree-meta' }, [
            h('span', { class: 'tree-version-tag' }, `v${node.version || 1}`),
            hasChildren ? h('span', { class: 'tree-child-tag' }, `${node.children.length}`) : null
          ])
        ])
      )

      const elements = []

      elements.push(
        h('div', {
          class: ['tree-row', { 'depth-0': props.depth === 0 }],
          style: { paddingLeft: indent + 'px' },
          onClick: () => emit('navigate', node.id)
        }, rowItems)
      )

      if (hasChildren && expanded.value) {
        const visibleChildren = node.children.slice(0, showCount.value)
        const remaining = node.children.length - showCount.value

        const childElements = visibleChildren.map(child =>
          h(TreeNode, {
            key: child.id,
            node: child,
            depth: props.depth + 1,
            expandAll: props.expandAll,
            onNavigate: (id) => emit('navigate', id)
          })
        )

        // "加载更多"按钮
        if (remaining > 0) {
          childElements.push(
            h('div', {
              class: 'tree-load-more',
              style: { paddingLeft: (12 + (props.depth + 1) * 28) + 'px' },
              onClick: (e) => { e.stopPropagation(); showMore() }
            }, `展开更多（剩余 ${remaining} 项）`)
          )
        }

        elements.push(h('div', { class: 'tree-children' }, childElements))
      }

      return h('div', { class: 'tree-node' }, elements)
    }
  }
})

// Build tree from flat pages list using ancestors
function buildTree(pages) {
  const map = {}
  const roots = []

  pages.forEach(p => {
    map[p.id] = {
      id: p.id,
      title: p.title,
      version: p.version?.number || 1,
      children: []
    }
  })

  pages.forEach(p => {
    const ancestors = p.ancestors || []
    if (ancestors.length > 0) {
      // 从最近的祖先往上找，挂到第一个存在于 map 中的祖先
      let placed = false
      for (let i = ancestors.length - 1; i >= 0; i--) {
        if (map[ancestors[i].id]) {
          map[ancestors[i].id].children.push(map[p.id])
          placed = true
          break
        }
      }
      if (!placed) roots.push(map[p.id])
    } else {
      roots.push(map[p.id])
    }
  })

  // 倒序排列（最新在前）
  function sortChildren(nodes) {
    nodes.sort((a, b) => b.title.localeCompare(a.title))
    nodes.forEach(n => { if (n.children.length) sortChildren(n.children) })
  }
  sortChildren(roots)

  return roots
}

async function fetchAllPages() {
  const allPages = []
  let startAt = 0
  const limit = 100

  while (true) {
    const res = await api.get(`/api/confluence/spaces/${spaceKey}/content?type=page&start=${startAt}&limit=${limit}`)
    const data = res.data.data
    const pageData = data?.page || data
    const results = pageData?.results || []
    allPages.push(...results)

    // 用 results 数量判断是否还有更多
    if (results.length < limit) break
    startAt += results.length
  }

  return allPages
}

const rootPageTitle = ref('')

onMounted(async () => {
  try {
    // 获取设置：根目录页面
    try {
      const settingsRes = await api.get('/api/settings/public')
      const config = settingsRes.data.data || {}
      rootPageTitle.value = (config.confluence_root_page || '').trim()
    } catch (e) { /* ignore */ }

    const spaceRes = await api.get(`/api/confluence/spaces/${spaceKey}`)
    space.value = spaceRes.data.data

    const fetchedPages = await fetchAllPages()

    if (rootPageTitle.value) {
      // 只加载指定根目录的子页面
      const rootTitles = rootPageTitle.value.split(',').map(s => s.trim()).filter(Boolean)

      // 找到根目录页面（精确匹配标题）
      const rootPages = fetchedPages.filter(p => rootTitles.includes(p.title))

      if (rootPages.length) {
        // 过滤：只保留根目录页面及其后代
        const rootIds = new Set(rootPages.map(p => String(p.id)))
        const filtered = fetchedPages.filter(p => {
          if (rootIds.has(String(p.id))) return true
          const ancestors = p.ancestors || []
          return ancestors.some(a => rootIds.has(String(a.id)))
        })
        allPages.value = filtered
        totalPages.value = filtered.length
        tree.value = buildTree(filtered)
      } else {
        // 根目录未找到，显示全部
        allPages.value = fetchedPages
        totalPages.value = fetchedPages.length
        tree.value = buildTree(fetchedPages)
      }
    } else {
      // 无配置，显示全部
      allPages.value = fetchedPages
      totalPages.value = fetchedPages.length
      tree.value = buildTree(fetchedPages)
    }
  } catch (e) { console.error('Failed to load space:', e) }
  finally { loading.value = false }
})
</script>


<style scoped>
/* Header */
.header-bar {
  display: flex;
  align-items: center;
  gap: 20px;
  margin-bottom: 20px;
}
.space-info {
  display: flex;
  align-items: center;
  gap: 12px;
}
.space-avatar {
  width: 36px;
  height: 36px;
  border-radius: var(--radius);
  background: linear-gradient(135deg, var(--primary), var(--primary-light));
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 700;
  font-size: 16px;
  flex-shrink: 0;
}
.space-info h2 {
  font-size: 18px;
  font-weight: 600;
  color: var(--text-primary);
  line-height: 1.2;
}
.space-key {
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--text-muted);
  font-weight: 400;
}

/* Card */
.tree-card {
  padding: 0;
  overflow: hidden;
}
.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px 20px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-secondary);
}
.card-title-group {
  display: flex;
  align-items: center;
  gap: 8px;
}
.card-icon {
  color: var(--primary-light);
}
.card-header h3 {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-primary);
  margin: 0;
}
.card-actions {
  display: flex;
  align-items: center;
  gap: 10px;
}
.page-count-badge {
  font-size: 12px;
  color: var(--text-muted);
  background: var(--bg-tertiary);
  padding: 3px 10px;
  border-radius: 9999px;
}

/* Empty state */
.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  padding: 60px 20px;
  color: var(--text-muted);
}
.empty-state p {
  font-size: 14px;
}

/* Search box */
.search-box {
  display: flex;
  align-items: center;
  gap: 6px;
  background: var(--bg-input);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 4px 10px;
  transition: border-color var(--transition);
}
.search-box:focus-within {
  border-color: var(--primary-light);
}
.search-box svg {
  color: var(--text-muted);
  flex-shrink: 0;
}
.search-input {
  background: none;
  border: none;
  outline: none;
  color: var(--text-primary);
  font-size: 13px;
  font-family: var(--font-sans);
  width: 160px;
}
.search-input::placeholder {
  color: var(--text-muted);
}

/* Tree container */
.tree-container {
  padding: 4px 0;
}
</style>

<!-- Unscoped styles for render-function generated tree nodes -->
<style>
.tree-row {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 7px 16px;
  cursor: pointer;
  transition: background 200ms ease;
  position: relative;
}
.tree-row:hover {
  background: var(--bg-hover);
}
.tree-row:hover .tree-title {
  color: var(--primary-light);
}
.tree-row.depth-0 {
  padding-top: 9px;
  padding-bottom: 9px;
}

/* Chevron toggle */
.tree-toggle-btn {
  display: inline-flex;
  width: 22px;
  height: 22px;
  align-items: center;
  justify-content: center;
  border-radius: 6px;
  flex-shrink: 0;
  cursor: pointer;
  transition: background 200ms ease;
}
.tree-toggle-btn:hover {
  background: var(--bg-tertiary);
}
.tree-toggle-btn.hidden {
  visibility: hidden;
}

.tree-chevron {
  color: var(--text-muted);
  transition: transform 0.2s ease;
}
.tree-chevron.expanded {
  transform: rotate(90deg);
}

/* Page icons */
.tree-icon-wrap {
  display: inline-flex;
  flex-shrink: 0;
}
.tree-page-icon.parent {
  color: var(--accent);
}
.tree-page-icon.leaf {
  color: var(--text-muted);
}

/* Content area */
.tree-content {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 1;
  min-width: 0;
}
.tree-title {
  font-size: 13px;
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  transition: color 200ms ease;
}
.tree-meta {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
  margin-left: auto;
  padding-right: 4px;
}
.tree-version-tag {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--text-muted);
  background: var(--bg-tertiary);
  padding: 1px 7px;
  border-radius: 6px;
  line-height: 1.6;
}
.tree-child-tag {
  font-size: 11px;
  font-weight: 500;
  color: var(--primary-light);
  background: rgba(76, 154, 255, 0.1);
  padding: 1px 7px;
  border-radius: 6px;
  line-height: 1.6;
}
.tree-child-tag::after {
  content: ' 子页面';
}

/* Children container */
.tree-children {
  position: relative;
}

/* Load more button */
.tree-load-more {
  padding: 6px 16px;
  font-size: 12px;
  color: var(--primary-light);
  cursor: pointer;
  transition: background 200ms ease;
  user-select: none;
}
.tree-load-more:hover {
  background: var(--bg-hover);
}
</style>
