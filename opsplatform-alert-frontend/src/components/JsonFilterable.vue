<template>
  <div class="json-filterable">
    <template v-if="isPrimitive(data)">
      <span class="leaf-value" :class="valueClass(data)">{{ formatLeaf(data) }}</span>
      <span class="filter-actions">
        <button class="filter-btn" :title="'加为过滤: ' + path + ' = ' + data" @click="emit('filter-add', { field: path, value: data, op: 'term' })">+</button>
        <button class="filter-btn" :title="'排除: ' + path + ' = ' + data" @click="emit('filter-add', { field: path, value: data, op: 'not_term' })">−</button>
      </span>
    </template>
    <template v-else-if="Array.isArray(data)">
      <span class="bracket">[</span>
      <div class="children">
        <div v-for="(item, idx) in data" :key="idx" class="kv-row">
          <span class="index">{{ idx }}:</span>
          <JsonFilterable :data="item" :path="path" @filter-add="bubbleUp" />
          <span v-if="idx < data.length - 1" class="comma">,</span>
        </div>
      </div>
      <span class="bracket">]</span>
    </template>
    <template v-else-if="data === null">
      <span class="leaf-value null-value">null</span>
    </template>
    <template v-else>
      <span class="bracket">{</span>
      <div class="children">
        <div v-for="(value, key, idx) in data" :key="key" class="kv-row">
          <span class="key">"{{ key }}":</span>
          <JsonFilterable :data="value" :path="path ? path + '.' + key : key" @filter-add="bubbleUp" />
          <span v-if="idx < Object.keys(data).length - 1" class="comma">,</span>
        </div>
      </div>
      <span class="bracket">}</span>
    </template>
  </div>
</template>

<script setup>
defineProps({
  data: { type: null, required: true },
  path: { type: String, default: '' },
})
const emit = defineEmits(['filter-add'])

function isPrimitive(v) {
  return v === null || ['string', 'number', 'boolean'].includes(typeof v)
}
function formatLeaf(v) {
  if (typeof v === 'string') return '"' + v + '"'
  return String(v)
}
function valueClass(v) {
  if (typeof v === 'string') return 'string-value'
  if (typeof v === 'number') return 'number-value'
  if (typeof v === 'boolean') return 'bool-value'
  return ''
}
function bubbleUp(payload) {
  emit('filter-add', payload)
}
</script>

<style scoped>
.json-filterable { display: inline; font-family: monospace; font-size: 12px; line-height: 1.6; }
.children { padding-left: 16px; }
.kv-row { display: flex; align-items: flex-start; gap: 6px; }
.key { color: #0369a1; }
.index { color: #64748b; }
.bracket { color: #475569; font-weight: 600; }
.comma { color: #475569; }
.leaf-value { padding: 0 2px; border-radius: 2px; }
.string-value { color: #16a34a; }
.number-value { color: #ea580c; }
.bool-value { color: #9333ea; font-weight: 600; }
.null-value { color: #94a3b8; font-style: italic; }
.filter-actions { opacity: 0; transition: opacity 0.15s; margin-left: 4px; display: inline-flex; gap: 2px; }
.kv-row:hover > .filter-actions,
.json-filterable:hover > .filter-actions { opacity: 1; }
.filter-btn {
  width: 18px; height: 18px;
  border: 1px solid #cbd5e1;
  background: #f8fafc;
  color: #475569;
  border-radius: 3px;
  font-size: 12px;
  font-weight: 700;
  cursor: pointer;
  padding: 0;
  line-height: 1;
}
.filter-btn:hover { background: #e0f2fe; border-color: #0284c7; color: #0284c7; }
.filter-btn:last-child:hover { background: #fee2e2; border-color: #dc2626; color: #dc2626; }
</style>
