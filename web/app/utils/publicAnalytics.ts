// Explicit public catalog routes only. Never derive analytics from fullPath or titles.
const titles = {
  '/': 'Accueil',
  '/planning': 'Planning',
  '/recherche': 'Recherche',
  '/films': 'Films',
  '/films/prochainement': 'Prochainement',
  '/cinemas': 'Cinémas',
  '/statistiques': 'Statistiques',
  '/mentions-legales': 'Mentions légales',
  '/confidentialite': 'Confidentialité',
  '/credits': 'Crédits',
}

export function publicAnalyticsPage(path: string, matched = true) {
  if (!matched) return null
  const title = Object.entries(titles).find(([route]) => route === path)?.[1]
  if (title) return { path, title }
  if (/^\/film\/[a-z0-9]+(?:-[a-z0-9]+)*$/.test(path))
    return { path, title: 'Film' }
  if (/^\/cinema\/[a-z0-9]+(?:-[a-z0-9]+)*$/.test(path))
    return { path, title: 'Cinéma' }
  if (/^\/ville\/[a-z0-9]+(?:-[a-z0-9]+)*\/cinemas$/.test(path))
    return { path, title: 'Cinémas par ville' }
  return null
}

export function publicAnalyticsReferrer(raw: string, origin: string): string {
  try {
    const url = new URL(raw)
    return /^https?:$/.test(url.protocol) && url.origin !== origin
      ? url.origin
      : ''
  } catch {
    return ''
  }
}

export type PublicPageview = {
  website: string
  hostname: string
  language: string
  screen: string
  url: string
  title: string
  referrer: string
}

// Upstream page/event properties are optional. Additional runtime fields are
// rejected by the exact-key gate, never copied into the sanitized pageview.
export type TrackerPayload = Partial<PublicPageview> & {
  id?: string
  name?: string
}

// A synchronous, one-use capability around track(object). Umami clones this object
// before invoking before-send; no default payload, identity or arbitrary event is admitted.
export function createPublicAnalytics(
  base: Pick<PublicPageview, 'website' | 'hostname' | 'language' | 'screen'>,
  initialReferrer: string,
) {
  let page: ReturnType<typeof publicAnalyticsPage> = null
  let initialized = false
  let eligible = false
  let sent = false
  let referrer = initialReferrer
  let approved: PublicPageview | null = null
  return {
    pause() {
      eligible = false
      approved = null
    },
    settle(path: string, matched: boolean) {
      const next = publicAnalyticsPage(path, matched)
      if (!initialized || next?.path !== page?.path) {
        referrer = initialized
          ? page && next
            ? page.path
            : ''
          : initialReferrer
        sent = false
      }
      initialized = true
      page = next
      eligible = !!next
      return eligible
    },
    send(track: (payload: PublicPageview) => void | Promise<void>) {
      if (!eligible || !page || sent) return
      const payload = { ...base, url: page.path, title: page.title, referrer }
      approved = payload
      sent = true
      try {
        return track(payload)
      } finally {
        approved = null
      }
    },
    beforeSend(
      type: string,
      payload: TrackerPayload | null,
    ): PublicPageview | null {
      const expected = approved
      approved = null
      if (!eligible || !expected || type !== 'event' || !payload) return null
      const entries = Object.entries(expected)
      if (
        Object.keys(payload).length !== entries.length ||
        !entries.every(
          ([key, value]) =>
            Object.getOwnPropertyDescriptor(payload, key)?.value === value,
        )
      )
        return null
      return { ...expected }
    },
  }
}
