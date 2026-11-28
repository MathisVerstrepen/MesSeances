// Minimal unstorage driver: an in-memory LRU bounded by total key and value
// length. Nitro's default memory storage never evicts, and crawlers can
// request more distinct URLs than memory should hold.
export interface BoundedMemoryDriver {
  name: string
  hasItem: (key: string) => boolean
  getItem: (key: string) => string | null
  setItem: (key: string, value: string) => void
  removeItem: (key: string) => void
  getKeys: (base?: string) => string[]
  clear: (base?: string) => void
  dispose: () => void
  size: () => number
}

export function createBoundedMemoryDriver(
  maxChars: number,
): BoundedMemoryDriver {
  const entries = new Map<string, string>()
  let size = 0

  function remove(key: string) {
    const value = entries.get(key)
    if (value === undefined) return
    entries.delete(key)
    size -= key.length + value.length
  }

  return {
    name: 'bounded-memory',
    hasItem: (key) => entries.has(key),
    getItem(key) {
      const value = entries.get(key)
      if (value === undefined) return null
      // Map order is insertion order: re-inserting marks the key as recent.
      entries.delete(key)
      entries.set(key, value)
      return value
    },
    setItem(key, value) {
      remove(key)
      const cost = key.length + value.length
      if (cost > maxChars) return
      entries.set(key, value)
      size += cost
      for (const oldest of entries.keys()) {
        if (size <= maxChars) break
        remove(oldest)
      }
    },
    removeItem: remove,
    getKeys: (base = '') =>
      [...entries.keys()].filter((key) => key.startsWith(base)),
    clear(base = '') {
      for (const key of [...entries.keys()])
        if (key.startsWith(base)) remove(key)
    },
    dispose() {
      entries.clear()
      size = 0
    },
    size: () => size,
  }
}
