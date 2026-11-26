import type { Ref } from 'vue'

const SHOWTIME_CLOCK_INTERVAL_MS = 30_000

// Client-only shared clock: only written from mounted hooks, never during SSR.
const sharedNow = ref<number | null>(null)
let subscribers = 0
let timer: number | undefined

function tick() {
  sharedNow.value = Date.now()
}

function handleVisibilityChange() {
  if (document.visibilityState === 'visible') tick()
}

/**
 * Current time for showtime timing states (started / past).
 * Stays `null` during SSR and hydration so markup matches, then ticks every
 * 30 seconds on a single interval shared by every mounted consumer.
 */
export function useShowtimeClock(): Readonly<Ref<number | null>> {
  const mounted = ref(false)

  onMounted(() => {
    mounted.value = true
    subscribers += 1
    if (subscribers > 1) return
    tick()
    timer = window.setInterval(tick, SHOWTIME_CLOCK_INTERVAL_MS)
    document.addEventListener('visibilitychange', handleVisibilityChange)
  })

  onBeforeUnmount(() => {
    if (!mounted.value) return
    mounted.value = false
    subscribers -= 1
    if (subscribers > 0) return
    if (timer !== undefined) window.clearInterval(timer)
    timer = undefined
    document.removeEventListener('visibilitychange', handleVisibilityChange)
  })

  return computed(() => (mounted.value ? sharedNow.value : null))
}
