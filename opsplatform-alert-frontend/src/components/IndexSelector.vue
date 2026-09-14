<template>
  <!-- One set of three fields, laid out two ways. Nested (the default) keeps
       its own grid, for callers that put this under a single "ES 索引" label.
       Flat drops to display:contents so the three fields join the caller's
       grid directly and line up with its other fields. -->
  <div class="index-selector" :class="{ 'index-selector-flat': flat }">
    <div class="form-group index-field">
      <label class="form-label">项目环境</label>
      <select v-model="selectedProject" class="form-select" @change="onProjectChange">
        <option value="">全部</option>
        <option v-for="p in projects" :key="p.code" :value="p.code">
          {{ p.display_name }} ({{ p.code }})
        </option>
      </select>
    </div>
    <div class="form-group index-field">
      <label class="form-label">索引 (共 {{ filteredIndices.length }})</label>
      <div class="index-search">
        <input
          v-model="indexSearch"
          class="form-input"
          placeholder="搜索索引名..."
          @focus="dropdownOpen = true"
          @blur="onBlur"
        />
        <div v-if="dropdownOpen && filteredIndices.length > 0" class="dropdown-list">
          <div
            v-for="idx in displayedIndices"
            :key="idx"
            class="dropdown-item"
            @mousedown.prevent="selectIndex(idx)"
          >
            {{ idx }}
          </div>
          <div v-if="filteredIndices.length > 50" class="dropdown-more">
            ...还有 {{ filteredIndices.length - 50 }} 条，请继续输入过滤
          </div>
        </div>
      </div>
    </div>
    <div class="form-group index-field">
      <label class="form-label">索引值 (高级模式可手填通配符)</label>
      <input v-model="advancedValue" class="form-input" placeholder="* 或 prod-app-g32-*" @input="onAdvancedInput" />
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import api from '../api'

const props = defineProps({
  modelValue: { type: String, default: '*' },
  esConnectionId: { type: Number, default: 0 },
  // Let the three fields join the caller's own form grid instead of nesting
  // a second grid inside one of its cells.
  flat: { type: Boolean, default: false },
})
const emit = defineEmits(['update:modelValue'])

const projects = ref([])
const indices = ref([])
const selectedProject = ref('')
const indexSearch = ref('')
const dropdownOpen = ref(false)
const advancedValue = ref(props.modelValue)

const filteredIndices = computed(() => {
  let arr = indices.value
  if (selectedProject.value) {
    const p = projects.value.find(x => x.code === selectedProject.value)
    if (p) {
      const kws = p.match_keywords.split(',').map(k => k.trim()).filter(Boolean)
      arr = arr.filter(idx => kws.every(kw => idx.toLowerCase().includes(kw.toLowerCase())))
    }
  }
  const q = indexSearch.value.trim().toLowerCase()
  if (q) arr = arr.filter(idx => idx.toLowerCase().includes(q))
  return arr
})

const displayedIndices = computed(() => filteredIndices.value.slice(0, 50))

async function loadProjects() {
  try {
    const res = await api.get('/es-projects')
    if (res.code === 0) projects.value = (res.data || []).filter(p => p.enabled === 1)
  } catch (e) { /* ignore */ }
}

async function loadIndices() {
  indices.value = []
  if (!props.esConnectionId) return
  try {
    const res = await api.get('/es-explore/indices', { params: { es_connection_id: props.esConnectionId } })
    if (res.code === 0) indices.value = (res.data || []).sort()
  } catch (e) { /* ignore */ }
}

function selectIndex(idx) {
  advancedValue.value = idx
  indexSearch.value = idx
  dropdownOpen.value = false
  emit('update:modelValue', idx)
}

function onProjectChange() {
  indexSearch.value = ''
  dropdownOpen.value = true
}

function onAdvancedInput() {
  emit('update:modelValue', advancedValue.value)
}

function onBlur() {
  setTimeout(() => { dropdownOpen.value = false }, 200)
}

watch(() => props.modelValue, (v) => { advancedValue.value = v })
watch(() => props.esConnectionId, loadIndices)

onMounted(() => {
  loadProjects()
  loadIndices()
})
</script>

<style scoped>
/* Nested: keep the old two-up-then-full-width shape and the smaller labels,
   because here the three fields are sub-fields under a caller's own label. */
.index-selector:not(.index-selector-flat) {
  width: 100%;
  display: grid;
  /* Three across when the caller gives the group a full-width row, folding to
     two and then one as the space narrows. */
  grid-template-columns: repeat(auto-fit, minmax(190px, 1fr));
  gap: var(--space-12) var(--space-16);
  align-items: start;
}
.index-selector:not(.index-selector-flat) .index-field { margin-bottom: 0; }
.index-selector:not(.index-selector-flat) .form-label { font-size: var(--fs-12); }

/* Flat: the three fields become grid items of the caller's own .form-row,
   so they line up with its other fields and share its label size. */
.index-selector-flat { display: contents; }

.index-search { position: relative; }

.dropdown-list {
  position: absolute;
  top: 100%;
  left: 0;
  right: 0;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 6px;
  max-height: 280px;
  overflow-y: auto;
  z-index: 1000;
  box-shadow: var(--shadow-md);
  margin-top: 2px;
}
.dropdown-item {
  padding: var(--space-6) var(--space-12);
  cursor: pointer;
  font-size: var(--fs-14);
  font-family: ui-monospace, "SF Mono", Menlo, monospace;
}
.dropdown-item:hover { background: var(--bg); }
.dropdown-more {
  padding: var(--space-6) var(--space-12);
  font-size: var(--fs-12);
  color: var(--text-secondary);
  background: var(--bg);
  font-style: italic;
}
</style>
