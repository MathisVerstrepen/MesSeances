export function useAccountToken() {
  const token = ref('')
  const ready = ref(false)
  function clear() {
    token.value = ''
  }

  onMounted(() => {
    token.value = window.__takeAccountToken?.() ?? ''
    delete window.__takeAccountToken
    ready.value = true
    window.addEventListener('pagehide', clear)
  })
  onBeforeUnmount(() => {
    clear()
    if (import.meta.client) window.removeEventListener('pagehide', clear)
  })
  return { token, ready, clear }
}
