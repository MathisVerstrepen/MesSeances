import process from 'node:process'

const apiUrl = origin(process.env.API_URL ?? 'http://localhost:8080', 'API_URL')
const webUrl = origin(process.env.WEB_URL ?? 'http://localhost:3000', 'WEB_URL')
const siteUrl = origin(process.env.SITE_URL ?? 'http://localhost:3000', 'SITE_URL')
const expectUpstreamFailure = process.env.EXPECT_UPSTREAM_FAILURE === '1'

function origin(value, name) {
  const parsed = new URL(value)
  if (!['http:', 'https:'].includes(parsed.protocol) || parsed.pathname !== '/' || parsed.search || parsed.hash) {
    throw new Error(`${name} must be an HTTP(S) origin`)
  }
  return parsed.origin
}

function assert(condition, message) {
  if (!condition) throw new Error(message)
}

function decodeHtml(value) {
  return value
    .replace(/&#(\d+);/g, (_, code) => String.fromCodePoint(Number(code)))
    .replace(/&#x([\da-f]+);/gi, (_, code) => String.fromCodePoint(Number.parseInt(code, 16)))
    .replaceAll('&quot;', '"')
    .replaceAll('&apos;', "'")
    .replaceAll('&#39;', "'")
    .replaceAll('&lt;', '<')
    .replaceAll('&gt;', '>')
    .replaceAll('&amp;', '&')
}

function attributes(tag) {
  return Object.fromEntries([...tag.matchAll(/([:\w-]+)\s*=\s*(["'])(.*?)\2/gs)].map((match) => [match[1].toLowerCase(), decodeHtml(match[3])]))
}

function tags(html, name) {
  return [...html.matchAll(new RegExp(`<${name}\\b[^>]*>`, 'gi'))].map((match) => attributes(match[0]))
}

function one(values, label) {
  assert(values.length === 1, `${label}: expected exactly one value, received ${values.length}`)
  assert(values[0], `${label}: value is empty`)
  return values[0]
}

function meta(html, key) {
  return one(tags(html, 'meta').filter((item) => item.name === key || item.property === key).map((item) => item.content), `meta ${key}`)
}

function title(html) {
  return one([...html.matchAll(/<title>(.*?)<\/title>/gis)].map((match) => decodeHtml(match[1].trim())), 'title')
}

function canonical(html) {
  return one(tags(html, 'link').filter((item) => item.rel?.split(/\s+/).includes('canonical')).map((item) => item.href), 'canonical')
}

function visibleText(html) {
  const body = html.match(/<body\b[^>]*>(.*?)<\/body>/is)?.[1] ?? ''
  return decodeHtml(body.replace(/<script\b[^>]*>.*?<\/script>/gis, ' ').replace(/<style\b[^>]*>.*?<\/style>/gis, ' ').replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' '))
}

function absoluteHttpUrl(value, label) {
  const parsed = new URL(value)
  assert(['http:', 'https:'].includes(parsed.protocol), `${label}: expected absolute HTTP(S) URL`)
}

async function get(url) {
  const response = await fetch(url, { headers: { accept: 'text/html,application/json' }, redirect: 'manual' })
  return { response, body: await response.text() }
}

function verifyHead(page, expectedCanonical, expectedOgType) {
  const pageTitle = title(page.body)
  const description = meta(page.body, 'description')
  const canonicalUrl = canonical(page.body)
  assert(canonicalUrl === expectedCanonical, `${page.path}: canonical ${canonicalUrl} does not match ${expectedCanonical}`)
  assert(meta(page.body, 'og:title') === pageTitle, `${page.path}: Open Graph title does not match title`)
  assert(meta(page.body, 'og:description') === description, `${page.path}: Open Graph description does not match description`)
  assert(meta(page.body, 'og:url') === canonicalUrl, `${page.path}: Open Graph URL does not match canonical`)
  assert(meta(page.body, 'og:type') === expectedOgType, `${page.path}: unexpected Open Graph type`)
  assert(meta(page.body, 'twitter:title') === pageTitle, `${page.path}: Twitter title does not match title`)
  assert(meta(page.body, 'twitter:description') === description, `${page.path}: Twitter description does not match description`)
  assert(meta(page.body, 'twitter:card') === 'summary_large_image', `${page.path}: unexpected Twitter card`)
  absoluteHttpUrl(meta(page.body, 'og:image'), `${page.path} og:image`)
  absoluteHttpUrl(meta(page.body, 'twitter:image'), `${page.path} twitter:image`)
  return { title: pageTitle, description }
}

async function verifyFailureMode() {
  for (const path of ['/', '/films', '/film/upstream-check']) {
    const { response, body } = await get(`${webUrl}${path}`)
    assert(response.status === 502, `${path}: expected 502, received ${response.status}`)
    const text = visibleText(body)
    assert(text.includes('Impossible de joindre le service'), `${path}: recoverable French upstream error is missing`)
    assert(!text.includes('Film introuvable'), `${path}: upstream failure was rendered as film not found`)
  }
  console.log('Crawlability upstream-failure checks passed (3 routes, status 502).')
}

async function verifyNormalMode() {
  const discovery = await get(`${apiUrl}/api/v1/movies?currently_screened=true&sort=showtimes_desc&page=1&page_size=1`)
  assert(discovery.response.status === 200, `API discovery: expected 200, received ${discovery.response.status}`)
  const payload = JSON.parse(discovery.body)
  const movie = {
    slug: String(payload.items?.[0]?.slug ?? '').trim(),
    title: String(payload.items?.[0]?.title ?? '').trim()
  }
  assert(movie.slug, 'API discovery returned no current film slug')
  assert(movie.title, 'API discovery returned no current film title')

  const encodedSlug = encodeURIComponent(movie.slug)
  const pages = [
    { path: '/', canonical: `${siteUrl}/`, ogType: 'website' },
    { path: '/films', canonical: `${siteUrl}/films`, ogType: 'website' },
    { path: `/film/${encodedSlug}`, canonical: `${siteUrl}/film/${encodedSlug}`, ogType: 'video.movie' }
  ]
  const metadata = []
  for (const page of pages) {
    const result = await get(`${webUrl}${page.path}`)
    assert(result.response.status === 200, `${page.path}: expected 200, received ${result.response.status}`)
    assert(visibleText(result.body).includes(movie.title), `${page.path}: raw rendered body does not contain discovered film title`)
    metadata.push(verifyHead({ ...page, body: result.body }, page.canonical, page.ogType))
  }
  assert(new Set(metadata.map((item) => item.title)).size === pages.length, 'Route titles are not unique')
  assert(new Set(metadata.map((item) => item.description)).size === pages.length, 'Route descriptions are not unique')

  const missingPath = '/film/__crawlability-missing-film__'
  const missing = await get(`${webUrl}${missingPath}`)
  assert(missing.response.status === 404, `${missingPath}: expected 404, received ${missing.response.status}`)
  assert(visibleText(missing.body).includes('Film introuvable'), `${missingPath}: film-not-found UI is missing`)
  console.log(`Crawlability checks passed (3 SSR routes, metadata, known film ${movie.slug}, unknown-film 404).`)
}

await (expectUpstreamFailure ? verifyFailureMode() : verifyNormalMode())
