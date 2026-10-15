export default defineNuxtPlugin((nuxtApp) => {
  const account = useAccountSession()
  const preferences = useCinemaPreferences()
  const watchlist = useWatchlist()
  const watched = useWatched()
  const follows = useCinemaFollows()
  let active = true
  const refresh = () => {
    if (active) void account.refresh()
  }
  const focus = () => {
    if (active) void account.revalidate()
  }
  const offline = () => {
    account.clear()
  }
  const pagehide = () => {
    active = false
    account.clear()
  }
  const pageshow = (event: PageTransitionEvent) => {
    active = true
    if (event.persisted) refresh()
  }
  const channel =
    'BroadcastChannel' in window
      ? new BroadcastChannel('messeances-account')
      : null
  if (channel) nuxtApp._accountChannel = channel
  nuxtApp.hook('app:mounted', () => {
    // Private refs are absent from SSR. Admit them only after hydration so a
    // fast GET cannot replace the server's skeleton during its first render.
    preferences.startSynchronization()
    watchlist.startSynchronization()
    watched.startSynchronization()
    follows.startSynchronization()
    if (channel)
      channel.onmessage = (event) => {
        if (event.data === 'changed') refresh()
        if (event.data === 'theaters-changed') focus()
        if (event.data === 'watchlist-changed') focus()
        if (event.data === 'watched-changed') focus()
        if (event.data === 'theater-follows-changed') focus()
      }
    if (account.status.value === 'idle') refresh()
    window.addEventListener('focus', focus)
    window.addEventListener('online', focus)
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'visible') focus()
    })
    window.addEventListener('offline', offline)
    window.addEventListener('pagehide', pagehide)
    window.addEventListener('pageshow', pageshow)
  })
})
