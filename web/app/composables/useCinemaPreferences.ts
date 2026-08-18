import type { Theater } from '~/types/api'

const STORAGE_KEY = 'messeances.favoriteTheaterIds.v1'

let initializationPromise: Promise<void> | null = null
let memoryFavoriteIds: string[] | null = null

interface StoredFavoriteIds {
  exists: boolean
  ids: string[]
}

function uniqueIds(ids: unknown[]): string[] {
  return [...new Set(ids.filter((id): id is string => id === String(id) && id.trim().length > 0).map((id) => id.trim()))]
}

function storedFavoriteIds(): StoredFavoriteIds {
  let raw: string | null
  try {
    raw = localStorage.getItem(STORAGE_KEY)
  } catch {
    return { exists: memoryFavoriteIds !== null, ids: memoryFavoriteIds ? [...memoryFavoriteIds] : [] }
  }

  if (raw === null) {
    return { exists: memoryFavoriteIds !== null, ids: memoryFavoriteIds ? [...memoryFavoriteIds] : [] }
  }

  try {
    const value: unknown = JSON.parse(raw)
    if (!Array.isArray(value)) return { exists: true, ids: memoryFavoriteIds ? [...memoryFavoriteIds] : [] }

    const ids = uniqueIds(value)
    memoryFavoriteIds = [...ids]
    return { exists: true, ids }
  } catch {
    return { exists: true, ids: memoryFavoriteIds ? [...memoryFavoriteIds] : [] }
  }
}

export function useCinemaPreferences() {
  const api = useMesSeancesApi()
  const theaters = useState<Theater[]>('cinema-preferences:theaters', () => [])
  const favoriteTheaterIds = useState<string[]>('cinema-preferences:favorite-ids', () => [])
  const isInitialized = useState<boolean>('cinema-preferences:initialized', () => false)
  const isLoading = useState<boolean>('cinema-preferences:loading', () => false)
  const error = useState<string | null>('cinema-preferences:error', () => null)

  const favoriteTheaters = computed(() => {
    const selected = new Set(favoriteTheaterIds.value)
    return theaters.value.filter((theater) => selected.has(theater.id))
  })

  function orderCurrentIds(ids: string[]): string[] {
    const selected = new Set(uniqueIds(ids))
    return theaters.value.filter((theater) => selected.has(theater.id)).map((theater) => theater.id)
  }

  function persist(ids: string[]) {
    memoryFavoriteIds = [...ids]
    if (!import.meta.client) return

    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(ids))
    } catch {
      // Shared Nuxt state and the module fallback remain usable for this session.
    }
  }

  function setFavoriteTheaterIds(ids: string[]): boolean {
    const nextIds = orderCurrentIds(ids)
    if (theaters.value.length > 0 && nextIds.length === 0) return false

    favoriteTheaterIds.value = nextIds
    persist(nextIds)
    return true
  }

  function toggleFavoriteTheater(id: string): boolean {
    const selected = new Set(favoriteTheaterIds.value)
    if (selected.has(id)) selected.delete(id)
    else selected.add(id)
    return setFavoriteTheaterIds([...selected])
  }

  async function initialize(): Promise<void> {
    if (!import.meta.client || isInitialized.value) return
    if (initializationPromise) return initializationPromise

    initializationPromise = (async () => {
      isLoading.value = true
      error.value = null
      const stored = storedFavoriteIds()
      favoriteTheaterIds.value = stored.ids

      try {
        const currentTheaters = await api.theaters()
        theaters.value = currentTheaters

        const currentIds = orderCurrentIds(stored.ids)
        if (currentIds.length > 0) {
          favoriteTheaterIds.value = currentIds
        } else {
          let defaultIds: string[] = []
          try {
            defaultIds = (await api.theaters({ city: 'Lille' })).map((theater) => theater.id)
          } catch {
            // National catalog still provides a deterministic fallback.
          }
          favoriteTheaterIds.value = orderCurrentIds(defaultIds)
          if (favoriteTheaterIds.value.length === 0 && currentTheaters[0]) {
            favoriteTheaterIds.value = [currentTheaters[0].id]
          }
        }

        persist(favoriteTheaterIds.value)
        isInitialized.value = true
      } catch (cause) {
        error.value = getFrenchApiError(cause)
        isInitialized.value = false
      } finally {
        isLoading.value = false
        initializationPromise = null
      }
    })()

    return initializationPromise
  }

  return {
    theaters: readonly(theaters),
    favoriteTheaterIds: readonly(favoriteTheaterIds),
    favoriteTheaters,
    isInitialized: readonly(isInitialized),
    isLoading: readonly(isLoading),
    error: readonly(error),
    initialize,
    setFavoriteTheaterIds,
    toggleFavoriteTheater
  }
}
