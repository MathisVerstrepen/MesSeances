export default defineNuxtRouteMiddleware(async () => {
  if (import.meta.server) return

  try {
    const session = await useMovieFlowApi().adminSession()
    if (!session.authenticated) return navigateTo('/admin/login')
  } catch {
    // Une panne ou une administration désactivée doit rester visible sur la page cible.
  }
})
