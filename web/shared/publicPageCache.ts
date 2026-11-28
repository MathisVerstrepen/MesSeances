// Schedules change a few times a day: five minutes keeps pages fresh while a
// crawler re-reading a page costs no API call.
export const PUBLIC_PAGE_CACHE_SECONDS = 300

// Public pages whose server render depends only on the URL. Account state is
// loaded client-side, so these renders never contain visitor data.
export const publicPageCacheRoutes = [
  '/',
  '/films',
  '/films/**',
  '/film/**',
  '/cinemas',
  '/cinema/**',
  '/ville/**',
]

// Bounds the in-memory page and Markdown cache (UTF-16 code units, so at
// most twice as many bytes). The least recently used entries go first.
export const PAGE_CACHE_MAX_CHARS = 64 * 1024 * 1024
