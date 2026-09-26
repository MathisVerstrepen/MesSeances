export const accountTokenPaths = new Set([
  '/verification',
  '/reinitialiser-mot-de-passe',
  '/compte/confirmer-email',
  '/compte/confirmer-identite',
])

export function accountFragmentToken(hash: string): string {
  const values = new URLSearchParams(hash.slice(1)).getAll('token')
  return values.length === 1 && /^[A-Za-z0-9_-]{43}$/.test(values[0]!)
    ? values[0]!
    : ''
}

// Client NuxtApp-owned, never serialized. Pending arrival is separate from local
// page secrets so initial session admission cannot erase its own token.
export function createAccountNavigation() {
  let pending: { path: string; target: string; token: string } | undefined
  let replacement = false
  let admitted = ''
  let navigation = 0
  const starts = new Set<() => void>()
  const arrivals = new Set<() => void>()
  return {
    ids: new WeakMap<object, number>(),
    starts,
    arrivals,
    begin(path: string, target: string, hash: string, bootstrap = '') {
      for (const clear of starts) clear()
      admitted = ''
      navigation++
      if (replacement && pending?.target === target && !hash) {
        replacement = false
        return navigation
      }
      pending = accountTokenPaths.has(path)
        ? { path, target, token: hash ? accountFragmentToken(hash) : bootstrap }
        : undefined
      replacement = !!hash
      return navigation
    },
    finish(id: number, path: string, target: string, success: boolean) {
      if (id !== navigation) return
      if (!success || pending?.path !== path || pending.target !== target)
        pending = undefined
      admitted = success ? path : ''
      for (const arrive of arrivals) arrive()
    },
    take(path: string) {
      if (admitted !== path || pending?.path !== path) return ''
      const token = pending.token
      pending = undefined
      return token
    },
    clear() {
      pending = undefined
      admitted = ''
      replacement = false
      navigation++
      for (const clear of starts) clear()
    },
  }
}
