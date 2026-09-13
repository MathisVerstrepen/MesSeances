import { readonly, ref } from 'vue'
import type { AdminMoviePoster, AdminMoviePostersResponse } from '../types/api.ts'
import { safePosterUrl } from '../utils/safeImageUrl.ts'

export function useAdminMoviePosters(fetchPosters: (id: string, signal: AbortSignal) => Promise<AdminMoviePostersResponse>) {
  const posters = ref<AdminMoviePoster[]>([])
  const status = ref<'idle' | 'loading' | 'ready' | 'error'>('idle')
  let activeRequest: AbortController | null = null

  function reset() {
    activeRequest?.abort()
    activeRequest = null
    posters.value = []
    status.value = 'idle'
  }

  async function load(id: string) {
    reset()
    const controller = new AbortController()
    activeRequest = controller
    status.value = 'loading'
    try {
      const response = await fetchPosters(id, controller.signal)
      if (controller.signal.aborted || activeRequest !== controller) return
      const seen = new Set<string>()
      posters.value = response.posters.filter((poster) => {
        const url = safePosterUrl(poster.url)
        if (!url?.startsWith('https://image.tmdb.org/t/p/w500/') || seen.has(url)) return false
        seen.add(url)
        return true
      })
      status.value = 'ready'
    } catch {
      if (controller.signal.aborted || activeRequest !== controller) return
      status.value = 'error'
    } finally {
      if (activeRequest === controller) activeRequest = null
    }
  }

  return { posters: readonly(posters), status: readonly(status), load, reset }
}
