import { canonicalizeTheaterSelection, parseSharedTheaterSelection, SHARED_THEATERS_QUERY_KEY, theaterSelectionsEqual } from '~/utils/sharedTheaterSelection'

export function usePageCinemaSelection() {
  const route = useRoute()
  const preferences = useCinemaPreferences()

  const sharedTheaterIds = computed(() => parseSharedTheaterSelection(route.query[SHARED_THEATERS_QUERY_KEY]))
  const knownSharedTheaterIds = computed(() => canonicalizeTheaterSelection(sharedTheaterIds.value ?? [], preferences.theaters.value))
  const activeTheaterIds = computed(() => sharedTheaterIds.value === null
    ? [...preferences.favoriteTheaterIds.value]
    : knownSharedTheaterIds.value)
  const activeTheaters = computed(() => {
    const selected = new Set(activeTheaterIds.value)
    return preferences.theaters.value.filter((theater) => selected.has(theater.id))
  })
  const hasSharedSelection = computed(() => sharedTheaterIds.value !== null)
  const isSharedSelectionDifferent = computed(() => hasSharedSelection.value
    && !theaterSelectionsEqual(knownSharedTheaterIds.value, preferences.favoriteTheaterIds.value))

  return {
    ...preferences,
    activeTheaterIds,
    activeTheaters,
    hasSharedSelection,
    isSharedSelectionDifferent
  }
}
