import type { WatchedSection } from '~/types/watched'
import { showWatched, withoutWatched } from '~/utils/watchedDiscovery'

export function useWatchedDiscovery(section: WatchedSection) {
  const watched = useWatched()
  const mounted = ref(false)
  const temporary = ref<boolean | undefined>(undefined)
  onMounted(() => {
    mounted.value = true
  })
  const personal = computed(() => mounted.value && !!watched.owner.value)
  const blocked = computed(() => personal.value && !watched.ready.value)
  const show = computed(
    () =>
      !personal.value ||
      !watched.preferences.value ||
      showWatched(watched.preferences.value, section, temporary.value),
  )
  const hide = computed(
    () =>
      personal.value &&
      watched.ready.value &&
      !show.value &&
      watched.slugs.value.size > 0,
  )
  const key = computed(() =>
    JSON.stringify([
      personal.value,
      blocked.value,
      show.value,
      watched.scopeKey.value,
      // Metadata and unrelated preferences cannot change this projection.
      // Sort membership so server ordering alone cannot reload a public page.
      hide.value ? [...watched.slugs.value].sort() : [],
    ]),
  )
  watch(
    watched.scopeKey,
    () => {
      temporary.value = undefined
    },
    { flush: 'sync' },
  )
  function filter<T>(items: readonly T[], identity: (item: T) => string): T[] {
    if (blocked.value) return []
    return hide.value
      ? withoutWatched(items, watched.slugs.value, identity)
      : [...items]
  }
  return { watched, personal, blocked, show, hide, key, temporary, filter }
}
