export default defineNuxtPlugin((nuxtApp) => {
  const account = useAccountSession()
  let active = true
  const refresh = () => {
    if (active) void account.refresh()
  }
  const focus = () => {
    if (active) void account.revalidate()
  }
  const offline = () => {
    account.clear()
    void account.refresh()
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
  if (channel)
    channel.onmessage = (event) => {
      if (event.data === 'changed') refresh()
    }
  nuxtApp.hook('app:mounted', () => {
    if (account.status.value === 'idle') refresh()
    window.addEventListener('focus', focus)
    window.addEventListener('online', refresh)
    window.addEventListener('offline', offline)
    window.addEventListener('pagehide', pagehide)
    window.addEventListener('pageshow', pageshow)
  })
})
