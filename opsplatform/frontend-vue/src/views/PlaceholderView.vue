<script setup>
import { useRoute } from 'vue-router'

const route = useRoute()
const title = route.meta?.title || '页面'
</script>

<template>
  <div class="placeholder-page">
    <div class="placeholder-content">
      <div class="placeholder-icon">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
          <rect x="3" y="3" width="18" height="18" rx="2" ry="2"/>
          <line x1="3" y1="9" x2="21" y2="9"/>
          <line x1="9" y1="21" x2="9" y2="9"/>
        </svg>
      </div>
      <h2>{{ title }}</h2>
      <p>此功能正在开发中，敬请期待...</p>
    </div>
  </div>
</template>

<style scoped>
.placeholder-page {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 60vh;
}

.placeholder-content {
  text-align: center;
  padding: 40px;
}

.placeholder-icon {
  width: 80px;
  height: 80px;
  margin: 0 auto 20px;
  background: var(--bg-hover);
  border-radius: 20px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.placeholder-icon svg {
  width: 40px;
  height: 40px;
  color: var(--text-muted);
}

.placeholder-content h2 {
  margin: 0 0 12px;
  font-size: 24px;
  font-weight: 600;
  color: var(--text-primary);
}

.placeholder-content p {
  margin: 0;
  color: var(--text-muted);
  font-size: 14px;
}
</style>
