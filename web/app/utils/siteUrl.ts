export function siteOrigin(configuredUrl: string): string {
  const parsed = new URL(configuredUrl)
  if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
    throw new TypeError('Site URL must use HTTP or HTTPS')
  }
  return parsed.origin
}

export function absoluteSiteUrl(configuredUrl: string, path: string): string {
  const origin = siteOrigin(configuredUrl)
  const normalizedPath = `/${path.replace(/^\/+/, '')}`
  return new URL(normalizedPath, `${origin}/`).href
}
