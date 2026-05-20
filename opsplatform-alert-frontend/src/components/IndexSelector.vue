<template>
  <div class="index-selector">
    <div class="form-row" style="margin-bottom: 6px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" style="font-size: 12px;">项目环境</label>
        <select v-model="selectedProject" class="form-select" @change="onProjectChange">
          <option value="">全部</option>
          <option v-for="p in projects" :key="p.code" :value="p.code">
            {{ p.display_name }} ({{ p.code }})
          </option>
        </select>
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" style="font-size: 12px;">索引 (共 {{ filteredIndices.length }})</label>
        <div style="position: relative;">
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
    </div>
    <div class="form-group" style="margin-bottom: 0;">
      <label class="form-label" style="font-size: 12px;">索引值 (高级模式可手填通配符)</label>
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
.index-selector { width: 100%; }
.dropdown-list {
  position: absolute;
  top: 100%;
  left: 0;
  right: 0;
  background: white;
  border: 1px solid #e2e8f0;
  border-radius: 6px;
  max-height: 280px;
  overflow-y: auto;
  z-index: 1000;
  box-shadow: 0 4px 12px rgba(0,0,0,0.1);
  margin-top: 2px;
}
.dropdown-item {
  padding: 6px 12px;
  cursor: pointer;
  font-size: 13px;
  font-family: monospace;
}
.dropdown-item:hover { background: #f1f5f9; }
.dropdown-more {
  padding: 6px 12px;
  font-size: 12px;
  color: #64748b;
  background: #f8fafc;
  font-style: italic;
}
</style>
