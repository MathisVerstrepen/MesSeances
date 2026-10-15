declare module '#app' {
  interface NuxtApp {
    _collectionWrite?: ReturnType<typeof createCollectionWrite>
  }
}

function createCollectionWrite() {
  const busy = ref(false)
  let generation = 0
  let pending = Promise.resolve()
  let finish = () => {}
  return {
    busy,
    wait: () => pending,
    reset() {
      generation++
      busy.value = false
      finish()
      pending = Promise.resolve()
    },
    acquire() {
      const lease = ++generation
      pending = new Promise<void>((resolve) => {
        finish = resolve
      })
      busy.value = true
      return () => {
        if (lease === generation) {
          busy.value = false
          finish()
        }
      }
    },
  }
}

// A single non-queuing lane fences every local watchlist and watched write.
export function useCollectionWrite() {
  const app = useNuxtApp()
  return (app._collectionWrite ??= createCollectionWrite())
}
