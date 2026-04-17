<template>
  <router-view />
  <Toast ref="toastInstance" />
  <ConfirmDialog ref="confirmInstance" />
</template>

<script setup>
import { ref, onMounted } from 'vue'
import Toast from './components/Toast.vue'
import ConfirmDialog from './components/ConfirmDialog.vue'
import { setToastRef, setConfirmRef } from './stores/ui'

const toastInstance = ref(null)
const confirmInstance = ref(null)

onMounted(() => {
  setToastRef(toastInstance.value)
  setConfirmRef(confirmInstance.value)
})
</script>
