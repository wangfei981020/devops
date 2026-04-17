<template>
  <div class="ghost-wrap">
    <table class="table ghost-rows">
      <thead v-if="headers.length">
        <tr>
          <th v-for="(h, i) in headers" :key="i" :style="h.width ? `width:${h.width}` : ''">{{ h.label }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="n in rows" :key="n">
          <td v-for="(c, i) in headers.length || cols" :key="i">
            <div class="ghost-cell" :class="i === 0 ? 'ghost-cell-mid' : 'ghost-cell-short'"></div>
          </td>
        </tr>
      </tbody>
    </table>
    <div class="table-empty-overlay">
      <div class="table-empty-card">
        <div class="empty-icon"><component :is="icon" :size="22" /></div>
        <div class="empty-title">{{ title }}</div>
        <div class="text-sm text-muted">{{ description }}</div>
        <div v-if="ctaLabel" style="margin-top: 10px">
          <router-link v-if="ctaPath" :to="ctaPath" class="btn btn-primary btn-sm">
            <Plus :size="12" /> {{ ctaLabel }}
          </router-link>
          <button v-else class="btn btn-primary btn-sm" @click="$emit('cta')">
            <Plus :size="12" /> {{ ctaLabel }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { Plus, Inbox } from 'lucide-vue-next'

defineProps({
  headers: { type: Array, default: () => [] },
  cols: { type: Number, default: 6 },
  rows: { type: Number, default: 6 },
  icon: { type: Object, default: () => Inbox },
  title: { type: String, default: '暂无数据' },
  description: { type: String, default: '' },
  ctaLabel: { type: String, default: '' },
  ctaPath: { type: String, default: '' }
})

defineEmits(['cta'])
</script>

<style scoped>
.ghost-wrap { position: relative; }
</style>
