<template>
  <div v-if="connections.length > 0" class="conn-bar">
    <div class="conn-info">
      <span class="conn-type-label">{{ typeLabel }}</span>
      <select v-if="connections.length > 1" v-model="selected" class="conn-select" @change="onChange">
        <option v-for="c in connections" :key="c.id" :value="c.id">
          {{ c.name }}{{ c.is_default ? ' (默认)' : '' }} - {{ c.url }}
        </option>
      </select>
      <span v-else class="conn-name">
        {{ connections[0].name }} <span class="conn-url">{{ connections[0].url }}</span>
      </span>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, watch } from 'vue'
import api from '@/api'

const props = defineProps({
  type: { type: String, required: true }, // 'confluence' or 'jira'
  modelValue: { type: [Number, String], default: '' }
})
const emit = defineEmits(['update:modelValue', 'connection-change'])

const connections = ref([])
const selected = ref(props.modelValue)

const typeLabel = props.type === 'confluence' ? 'Confluence' : 'Jira'

watch(() => props.modelValue, v => { selected.value = v })

function getSelectedConn() {
  return connections.value.find(c => c.id === selected.value) || null
}

function onChange() {
  emit('update:modelValue', selected.value)
  emit('connection-change', getSelectedConn())
}

onMounted(async () => {
  try {
    const res = await api.get('/api/connections/public')
    const all = res.data.data || []
    connections.value = all.filter(c => c.type === props.type)
    if (connections.value.length > 0 && !selected.value) {
      const def = connections.value.find(c => c.is_default) || connections.value[0]
      selected.value = def.id
      emit('update:modelValue', def.id)
      emit('connection-change', def)
    }
  } catch (e) { /* ignore */ }
})
</script>

<style scoped>
.conn-bar {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 16px;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  margin-bottom: 16px;
}
.conn-info {
  display: flex;
  align-items: center;
  gap: 10px;
  flex: 1;
  min-width: 0;
}
.conn-type-label {
  font-size: 12px;
  font-weight: 600;
  color: var(--primary-light);
  background: rgba(76,154,255,0.1);
  padding: 2px 8px;
  border-radius: 4px;
  white-space: nowrap;
}
.conn-select {
  flex: 1;
  min-width: 200px;
  padding: 4px 8px;
  background: var(--bg-input);
  color: var(--text-primary);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  font-size: 13px;
  cursor: pointer;
}
.conn-name {
  font-size: 13px;
  color: var(--text-primary);
  font-weight: 500;
}
.conn-url {
  font-size: 12px;
  color: var(--text-muted);
  font-family: var(--font-mono);
  margin-left: 6px;
}
</style>
