<template>
  <div :data-theme="theme">
    <router-view />
    <div v-if="toast.show" :class="['toast', toast.type]">{{ toast.message }}</div>
  </div>
</template>

<script setup>
import { ref, onMounted, provide } from 'vue'

const theme = ref(localStorage.getItem('theme') || 'dark')
const toast = ref({ show: false, message: '', type: 'info' })

function setTheme(t) {
  theme.value = t
  localStorage.setItem('theme', t)
}

function toggleTheme() {
  setTheme(theme.value === 'dark' ? 'light' : 'dark')
}

function showToast(message, type = 'info', duration = 3000) {
  toast.value = { show: true, message, type }
  setTimeout(() => { toast.value.show = false }, duration)
}

onMounted(() => {
  document.documentElement.setAttribute('data-theme', theme.value)
})

provide('theme', { current: theme, toggle: toggleTheme, set: setTheme })
provide('toast', showToast)
</script>
