<template>
  <div class="contact-selector">
    <!-- A value we could not read is shown as it is, and left alone. Replacing
         it with an empty selector would let a save wipe a mention list whose
         only problem is that something else wrote it badly. -->
    <div v-if="unreadable" class="contact-unreadable">
      <div class="contact-unreadable-title">⚠ 这条规则的通知人配置无法解析，已原样保留</div>
      <code class="contact-unreadable-raw">{{ modelValue }}</code>
      <div class="contact-unreadable-hint">
        保存不会改动它。要改成选择方式，先
        <button type="button" class="contact-link" @click="discardUnreadable">清空并重新选择</button>
      </div>
    </div>

    <div v-else class="contact-box" :class="{ 'is-open': dropdownOpen }">
      <div v-if="entries.length > 0" class="contact-chips">
        <span
          v-for="e in entries"
          :key="e.name"
          class="contact-chip"
          :class="{ 'is-unknown': loaded && !e.known, 'is-disabled': loaded && e.known && e.disabled }"
          :title="chipTitle(e)"
        >
          {{ e.name }}<span v-if="loaded && !e.known" class="contact-mark">⚠</span><span
            v-else-if="loaded && e.disabled" class="contact-mark">⊘</span>
          <button type="button" class="contact-chip-x" @click="remove(e)" :aria-label="`移除 ${e.name}`">×</button>
        </span>
      </div>

      <div class="contact-search">
        <input
          ref="searchEl"
          v-model="query"
          class="contact-input"
          type="text"
          placeholder="🔍 搜索姓名…"
          @focus="dropdownOpen = true"
          @blur="onBlur"
          @keydown.esc="dropdownOpen = false"
        />
        <div v-if="dropdownOpen" class="dropdown-list">
          <div
            v-for="c in filtered"
            :key="c.id"
            class="dropdown-item"
            :class="{ 'is-picked': isPicked(c.name) }"
            @mousedown.prevent="toggle(c)"
          >
            <span class="dropdown-tick">{{ isPicked(c.name) ? '✓' : '' }}</span>
            <span class="dropdown-name">{{ c.name }}</span>
            <span v-if="Number(c.status) === 0" class="dropdown-off">已禁用 ⊘</span>
          </div>
          <div v-if="filtered.length === 0" class="dropdown-empty">
            {{ contacts.length === 0 ? '「通知人管理」里还没有人' : '没有匹配的姓名' }}
          </div>
        </div>
      </div>
    </div>

    <div v-if="!unreadable" class="contact-summary">
      <template v-if="entries.length === 0">未选择通知人，告警不会 @ 任何人。名单来自「通知人管理」。</template>
      <template v-else>
        已选 {{ entries.length }} 人
        <span v-if="unknownCount > 0" class="contact-warn">
          · ⚠ {{ unknownCount }} 人不在通知人管理中，不会收到 @
        </span>
        <span v-if="disabledCount > 0" class="contact-off">
          · ⊘ {{ disabledCount }} 人已禁用，不会收到 @
        </span>
      </template>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import api from '../api'
import { parseAtUsers, resolveNames, serialize, sameName, matchesQuery } from '../utils/atusers'

const props = defineProps({
  modelValue: { type: String, default: '' },
})
const emit = defineEmits(['update:modelValue'])

const contacts = ref([])
// Until the contact list has arrived, every name looks unfamiliar. Marking
// them on that basis would flash a wall of red warnings on a rule whose
// mentions are all perfectly valid, so the badges wait for the real answer.
const loaded = ref(false)
const query = ref('')
const dropdownOpen = ref(false)
const searchEl = ref(null)

// names holds what will be stored, in order. It is seeded from the prop and
// then owned here; the watcher below re-seeds it only when the rule itself
// changes underneath us (loading a different rule into the same form).
const names = ref([])
const unreadable = ref(false)

function seed(raw) {
  const parsed = parseAtUsers(raw)
  if (parsed === null) {
    unreadable.value = true
    names.value = []
    return
  }
  unreadable.value = false
  names.value = parsed
}

const entries = computed(() => resolveNames(names.value, contacts.value))
const unknownCount = computed(() => (loaded.value ? entries.value.filter(e => !e.known).length : 0))
const disabledCount = computed(() =>
  loaded.value ? entries.value.filter(e => e.known && e.disabled).length : 0)

const filtered = computed(() => contacts.value.filter(c => matchesQuery(c.name, query.value)))

function isPicked(name) {
  return names.value.some(n => sameName(n, name))
}

function chipTitle(e) {
  if (!loaded.value) return e.name
  if (!e.known) return `${e.name}：不在「通知人管理」中，不会收到 @`
  if (e.disabled) return `${e.name}：已禁用，不会收到 @`
  return e.name
}

function toggle(c) {
  if (isPicked(c.name)) {
    names.value = names.value.filter(n => !sameName(n, c.name))
  } else {
    // Appended, never inserted: the order a rule already had is left as it is.
    names.value = [...names.value, c.name]
  }
  push()
}

function remove(e) {
  names.value = names.value.filter(n => !sameName(n, e.name))
  push()
}

function discardUnreadable() {
  unreadable.value = false
  names.value = []
  push()
}

function push() {
  emit('update:modelValue', serialize(entries.value))
}

function onBlur() {
  // The dropdown closes on blur, but a click on one of its rows has to land
  // first; mousedown.prevent keeps focus, and this delay covers the rest.
  setTimeout(() => { dropdownOpen.value = false }, 150)
}

async function loadContacts() {
  try {
    const res = await api.get('/alert-contacts')
    if (res.code === 0) contacts.value = res.data || []
  } catch (e) { /* leave the list empty; names stay as stored */ }
  loaded.value = true
  // Now that the real spellings are known, write them back once so the stored
  // value matches what the page shows. Only when it actually differs — an
  // unconditional emit would mark an untouched form as edited.
  if (!unreadable.value) {
    const canonical = serialize(entries.value)
    if (canonical !== String(props.modelValue ?? '').trim()) emit('update:modelValue', canonical)
  }
}

watch(() => props.modelValue, (v) => {
  // Ignore the echo of our own emit; re-seeding on it would fight the user.
  if (!unreadable.value && serialize(entries.value) === String(v ?? '').trim()) return
  seed(v)
})

onMounted(() => {
  seed(props.modelValue)
  loadContacts()
})
</script>

<style scoped>
.contact-selector { width: 100%; }

.contact-box {
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--bg-card);
  padding: var(--space-8);
}
.contact-box.is-open { border-color: var(--primary, #4f46e5); }

.contact-chips {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-6);
  padding-bottom: var(--space-8);
  margin-bottom: var(--space-8);
  border-bottom: 1px solid var(--border);
}

.contact-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px var(--space-8);
  border-radius: 4px;
  font-size: var(--fs-13, 13px);
  background: #eff6ff;
  color: #1e40af;
  border: 1px solid #bfdbfe;
}
/* Unknown and disabled read differently on purpose: one is a name this
   platform cannot resolve at all, the other a person it can resolve and will
   still not reach. No strike-through — a struck name looks already removed. */
.contact-chip.is-unknown {
  background: #fef2f2;
  color: var(--danger, #dc2626);
  border-color: #fecaca;
}
.contact-chip.is-disabled {
  background: var(--bg, #f1f5f9);
  color: var(--text-secondary, #64748b);
  border-color: var(--border, #e2e8f0);
}
.contact-mark { font-size: var(--fs-12, 12px); }

.contact-chip-x {
  border: 0;
  background: none;
  cursor: pointer;
  color: inherit;
  opacity: 0.55;
  font-size: var(--fs-14, 14px);
  line-height: 1;
  padding: 0 0 0 2px;
}
.contact-chip-x:hover { opacity: 1; }

.contact-search { position: relative; }
.contact-input {
  width: 100%;
  border: 0;
  outline: none;
  background: transparent;
  font-size: var(--fs-14);
  color: var(--text);
  padding: var(--space-4) 2px;
}

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
  margin-top: 4px;
}
.dropdown-item {
  display: flex;
  align-items: center;
  gap: var(--space-8);
  padding: var(--space-6) var(--space-12);
  cursor: pointer;
  font-size: var(--fs-14);
}
.dropdown-item:hover { background: var(--bg); }
.dropdown-item.is-picked { color: var(--primary, #4f46e5); }
.dropdown-tick { width: 12px; flex-shrink: 0; }
.dropdown-name { flex: 1; }
.dropdown-off {
  font-size: var(--fs-12);
  color: var(--text-secondary);
}
.dropdown-empty {
  padding: var(--space-8) var(--space-12);
  font-size: var(--fs-13, 13px);
  color: var(--text-secondary);
  font-style: italic;
}

.contact-summary {
  margin-top: var(--space-6);
  font-size: var(--fs-12);
  color: var(--text-secondary);
}
.contact-warn { color: var(--danger, #dc2626); }
.contact-off { color: var(--text-secondary); }

.contact-unreadable {
  border: 1px solid #fecaca;
  background: #fef2f2;
  border-radius: 6px;
  padding: var(--space-8) var(--space-12);
}
.contact-unreadable-title {
  font-size: var(--fs-13, 13px);
  color: var(--danger, #dc2626);
  margin-bottom: var(--space-6);
}
.contact-unreadable-raw {
  display: block;
  font-size: var(--fs-12);
  word-break: break-all;
  color: var(--text);
  margin-bottom: var(--space-6);
}
.contact-unreadable-hint {
  font-size: var(--fs-12);
  color: var(--text-secondary);
}
.contact-link {
  border: 0;
  background: none;
  padding: 0;
  cursor: pointer;
  color: var(--primary, #4f46e5);
  text-decoration: underline;
  font-size: inherit;
}
</style>
