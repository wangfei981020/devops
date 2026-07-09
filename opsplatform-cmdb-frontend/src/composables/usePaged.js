import { ref, computed, watch } from 'vue'

// usePaged 通用前端分页：传入一个 ref 数据源，返回当前页切片 + 页码/页大小。
// 数据源变化(筛选/刷新)时自动把页码收敛到有效范围，避免停在空页。
export function usePaged(source, defaultSize = 20) {
  const page = ref(1)
  const size = ref(defaultSize)
  const total = computed(() => source.value.length)
  const paged = computed(() => {
    const start = (page.value - 1) * size.value
    return source.value.slice(start, start + size.value)
  })
  watch(total, (n) => {
    const maxPage = Math.max(1, Math.ceil(n / size.value))
    if (page.value > maxPage) page.value = maxPage
  })
  return { page, size, total, paged }
}
