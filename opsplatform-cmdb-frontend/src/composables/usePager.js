import { ref, computed, watch } from 'vue'

// usePager 本地分页：传入一个 ref 数组，返回 page/pageSize/paged；源变化自动回第一页。
export function usePager(sourceRef, size = 20) {
  const page = ref(1)
  const pageSize = ref(size)
  const paged = computed(() => sourceRef.value.slice((page.value - 1) * pageSize.value, page.value * pageSize.value))
  watch(() => sourceRef.value.length, () => { page.value = 1 })
  return { page, pageSize, paged }
}
