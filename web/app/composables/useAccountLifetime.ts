// Local work expires at navigation start, not at eventual transition unmount.
// Same-route reuse starts a fresh local generation without remounting the app.
export function useAccountLifetime(cleanup: () => void = () => {}) {
  const account = useAccountSession()
  const runtime = import.meta.client ? useAccountNavigation() : undefined
  let generation = 0
  function invalidate() {
    generation++
    cleanup()
  }
  runtime?.starts.add(invalidate)
  onBeforeUnmount(() => {
    runtime?.starts.delete(invalidate)
    invalidate()
  })
  function capture(checkSession = true) {
    const current = generation
    const revision = account.revision.value
    return () =>
      current === generation &&
      (!checkSession || revision === account.revision.value)
  }
  return { capture, invalidate }
}
