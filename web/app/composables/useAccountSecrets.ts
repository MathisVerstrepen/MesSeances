import type { Ref } from 'vue'

export function useAccountSecrets(...values: Ref<string>[]) {
  function clear() {
    for (const value of values) value.value = ''
  }
  onMounted(() => window.addEventListener('pagehide', clear))
  onBeforeUnmount(() => {
    clear()
    if (import.meta.client) window.removeEventListener('pagehide', clear)
  })
  return clear
}
