import process from 'node:process'

const apiUrl = origin(process.env.API_URL ?? 'http://localhost:8080', 'API_URL')
const webUrl = origin(process.env.WEB_URL ?? 'http://localhost:3000', 'WEB_URL')
const siteUrl = origin(process.env.SITE_URL ?? 'http://localhost:3000', 'SITE_URL')
const expectUpstreamFailure = process.env.EXPECT_UPSTREAM_FAILURE === '1'
const API_PAGE_SIZE = 100
const CATALOG_PAGE_SIZE = 24

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

async function get(url, accept = 'text/html,application/json') {
  const response = await fetch(url, { headers: { accept }, redirect: 'manual' })
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

function verifyPolicy(result, path, expectedRobots, expectedCanonical) {
  assert(result.response.status === 200, `${path}: expected 200, received ${result.response.status}`)
  assert(meta(result.body, 'robots') === expectedRobots, `${path}: unexpected robots policy`)
  if (expectedCanonical) assert(canonical(result.body) === expectedCanonical, `${path}: unexpected canonical`)
}

function verifyErrorPolicy(result, path, expectedStatus) {
  assert(result.response.status === expectedStatus, `${path}: expected ${expectedStatus}, received ${result.response.status}`)
  assert(meta(result.body, 'robots') === 'noindex,follow', `${path}: rendered error must be noindex,follow`)
  assert(result.response.headers.get('x-robots-tag') === 'noindex,follow', `${path}: missing X-Robots-Tag`)
}

async function fetchCatalog() {
  const items = []
  let page = 1
  let total
  let generatedAt
  do {
    const result = await get(`${apiUrl}/api/v1/movies?currently_screened=true&sort=title_asc&page=${page}&page_size=${API_PAGE_SIZE}`)
    assert(result.response.status === 200, `API catalog page ${page}: expected 200, received ${result.response.status}`)
    const payload = JSON.parse(result.body)
    if (page === 1) {
      total = payload.total
      generatedAt = payload.generated_at
      assert(Number.isSafeInteger(total) && total >= 0, 'API catalog: invalid total')
      assert(generatedAt?.constructor === String && Number.isFinite(Date.parse(generatedAt)), 'API catalog: invalid generated_at')
    }
    assert(payload.page === page, `API catalog page ${page}: page mismatch`)
    assert(payload.page_size === API_PAGE_SIZE, `API catalog page ${page}: page_size mismatch`)
    assert(payload.total === total, `API catalog page ${page}: snapshot total changed`)
    assert(payload.generated_at === generatedAt, `API catalog page ${page}: generated_at changed`)
    assert(Array.isArray(payload.items), `API catalog page ${page}: items missing`)
    items.push(...payload.items)
    page++
  } while (items.length < total)

  assert(items.length === total, `API catalog: collected ${items.length}, expected ${total}`)
  const slugs = items.map((item) => String(item.slug ?? '').trim())
  assert(slugs.every(Boolean), 'API catalog: empty film slug')
  assert(new Set(slugs).size === slugs.length, 'API catalog: duplicate film slug')
  return { items, slugs, total, generatedAt }
}

function sitemapEntries(xml) {
  return [...xml.matchAll(/<url>\s*<loc>(.*?)<\/loc>(?:\s*<lastmod>(.*?)<\/lastmod>)?\s*<\/url>/gs)]
    .map((match) => ({ loc: decodeHtml(match[1]), lastmod: match[2] ? decodeHtml(match[2]) : null }))
}

async function verifyDiscovery(catalog) {
  const sitemap = await get(`${webUrl}/sitemap.xml`, 'application/xml')
  assert(sitemap.response.status === 200, `/sitemap.xml: expected 200, received ${sitemap.response.status}`)
  assert(sitemap.response.headers.get('content-type')?.toLowerCase() === 'application/xml; charset=utf-8', '/sitemap.xml: unexpected content type')
  assert(sitemap.response.headers.get('cache-control') === 'no-store', '/sitemap.xml: expected Cache-Control no-store')
  assert(sitemap.body.endsWith('\n'), '/sitemap.xml: missing trailing newline')
  const entries = sitemapEntries(sitemap.body)
  const expectedLocations = [
    `${siteUrl}/`,
    `${siteUrl}/films`,
    `${siteUrl}/planning`,
    `${siteUrl}/recherche`,
    ...[...catalog.slugs].sort().map((slug) => `${siteUrl}/film/${encodeURIComponent(slug)}`)
  ]
  assert(entries.length === expectedLocations.length, `/sitemap.xml: expected ${expectedLocations.length} URLs, received ${entries.length}`)
  assert(new Set(entries.map((entry) => entry.loc)).size === entries.length, '/sitemap.xml: duplicate URL')
  assert(entries.every((entry, index) => entry.loc === expectedLocations[index]), '/sitemap.xml: URL order or set mismatch')
  for (const entry of entries) {
    const staticTool = entry.loc === `${siteUrl}/planning` || entry.loc === `${siteUrl}/recherche`
    assert(entry.lastmod === (staticTool ? null : catalog.generatedAt), `/sitemap.xml: incorrect lastmod for ${entry.loc}`)
  }

  const robots = await get(`${webUrl}/robots.txt`, 'text/plain')
  assert(robots.response.status === 200, `/robots.txt: expected 200, received ${robots.response.status}`)
  assert(robots.response.headers.get('content-type')?.toLowerCase() === 'text/plain; charset=utf-8', '/robots.txt: unexpected content type')
  assert(robots.body === `User-agent: *\nAllow: /\nSitemap: ${siteUrl}/sitemap.xml\n`, '/robots.txt: body mismatch')
  assert(!robots.body.toLowerCase().includes('disallow'), '/robots.txt: noindex routes must not be disallowed')
}

async function verifyIndexMatrix(catalog, movie) {
  const encodedSlug = encodeURIComponent(movie.slug)
  const queryless = [
    ['/', `${siteUrl}/`],
    ['/films', `${siteUrl}/films`],
    ['/planning', `${siteUrl}/planning`],
    ['/recherche', `${siteUrl}/recherche`],
    [`/film/${encodedSlug}`, `${siteUrl}/film/${encodedSlug}`]
  ]
  for (const [path, expectedCanonical] of queryless) {
    verifyPolicy(await get(`${webUrl}${path}`), path, 'index,follow', expectedCanonical)
  }

  const noindexCanonicalCases = [
    ['/?campaign=1', `${siteUrl}/`],
    ['/films?q=test', `${siteUrl}/films`],
    ['/films?sort=title_asc', `${siteUrl}/films`],
    ['/films?foreign=1', `${siteUrl}/films`],
    ['/films?page=1', `${siteUrl}/films`],
    ['/films?page=01', `${siteUrl}/films`],
    ['/films?page=bad', `${siteUrl}/films`],
    ['/films?page=2&page=3', `${siteUrl}/films`],
    ['/planning?date=2026-01-01', `${siteUrl}/planning`],
    ['/planning?foreign=1', `${siteUrl}/planning`],
    ['/recherche?view=chronological', `${siteUrl}/recherche`],
    ['/recherche?foreign=1', `${siteUrl}/recherche`],
    [`/film/${encodedSlug}?date=2026-01-01`, `${siteUrl}/film/${encodedSlug}`],
    [`/film/${encodedSlug}?foreign=1`, `${siteUrl}/film/${encodedSlug}`]
  ]
  for (const [path, expectedCanonical] of noindexCanonicalCases) {
    verifyPolicy(await get(`${webUrl}${path}`), path, 'noindex,follow', expectedCanonical)
  }

  const totalPages = Math.max(1, Math.ceil(catalog.total / CATALOG_PAGE_SIZE))
  if (totalPages >= 2) {
    verifyPolicy(await get(`${webUrl}/films?page=2`), '/films?page=2', 'index,follow', `${siteUrl}/films?page=2`)
  }
  const outOfRange = totalPages + 1
  const clampedCanonical = totalPages >= 2 ? `${siteUrl}/films?page=${totalPages}` : `${siteUrl}/films`
  verifyPolicy(await get(`${webUrl}/films?page=${outOfRange}`), `/films?page=${outOfRange}`, 'noindex,follow', clampedCanonical)

  for (const path of ['/cinemas', '/cinemas?city=Lille', '/credits', '/admin', '/admin/login', '/admin/sync', '/admin/tmdb-matches']) {
    const result = await get(`${webUrl}${path}`)
    assert(result.response.status < 400, `${path}: unexpected status ${result.response.status}`)
    assert(meta(result.body, 'robots') === 'noindex,follow', `${path}: expected noindex,follow`)
  }
}

async function verifyCatalogLinks(catalog) {
  const foundFilms = new Set()
  const totalPages = Math.max(1, Math.ceil(catalog.total / CATALOG_PAGE_SIZE))
  for (let page = 1; page <= totalPages; page++) {
    const path = page === 1 ? '/films' : `/films?page=${page}`
    const result = await get(`${webUrl}${path}`)
    assert(result.response.status === 200, `${path}: expected 200, received ${result.response.status}`)
    const hrefs = tags(result.body, 'a').map((anchor) => anchor.href).filter(Boolean)
    for (const href of hrefs) {
      const pathname = new URL(href, webUrl).pathname
      if (pathname.startsWith('/film/')) foundFilms.add(decodeURIComponent(pathname.slice('/film/'.length)))
    }
    if (page > 1) {
      assert(hrefs.some((href) => {
        const target = new URL(href, webUrl)
        return target.pathname === '/films' && target.searchParams.get('page') === (page - 1 === 1 ? null : String(page - 1))
      }), `${path}: previous SSR anchor missing`)
    }
    if (page < totalPages) {
      assert(hrefs.some((href) => {
        const target = new URL(href, webUrl)
        return target.pathname === '/films' && target.searchParams.get('page') === String(page + 1)
      }), `${path}: next SSR anchor missing`)
    }
  }
  assert(foundFilms.size === catalog.slugs.length && catalog.slugs.every((slug) => foundFilms.has(slug)), 'SSR catalog anchors do not reach every sitemap film')
}

function verifyBreadcrumb(html, movie) {
  const nav = html.match(/<nav\b[^>]*aria-label=(["'])Fil d’Ariane\1[^>]*>(.*?)<\/nav>/is)
  assert(nav, 'Film breadcrumb nav missing')
  const hrefs = tags(nav[2], 'a').map((anchor) => new URL(anchor.href, webUrl).pathname)
  assert(hrefs.length === 2 && hrefs[0] === '/' && hrefs[1] === '/films', 'Film breadcrumb links mismatch')
  assert(/aria-current=(["'])page\1/i.test(nav[2]), 'Film breadcrumb current item missing aria-current')
  assert(visibleText(`<body>${nav[2]}</body>`).includes(movie.title), 'Film breadcrumb current title missing')
}

async function verifyFailureMode() {
  for (const path of ['/', '/films', '/film/upstream-check']) {
    const result = await get(`${webUrl}${path}`)
    verifyErrorPolicy(result, path, 502)
    const text = visibleText(result.body)
    assert(text.includes('Impossible de joindre le service'), `${path}: recoverable French upstream error is missing`)
    assert(!text.includes('Film introuvable'), `${path}: upstream failure was rendered as film not found`)
  }
  const sitemap = await get(`${webUrl}/sitemap.xml`, 'application/xml')
  assert(sitemap.response.status === 503, `/sitemap.xml: expected 503, received ${sitemap.response.status}`)
  assert(!sitemap.body.includes('<urlset'), '/sitemap.xml: partial or stale sitemap returned on upstream failure')
  assert(sitemap.response.headers.get('x-robots-tag') === 'noindex,follow', '/sitemap.xml: missing error X-Robots-Tag')
  console.log('Crawlability upstream-failure checks passed (3 rendered 502 routes and non-partial sitemap 503).')
}

async function verifyNormalMode() {
  const catalog = await fetchCatalog()
  assert(catalog.items.length > 0, 'API discovery returned no current film')
  const discovery = await get(`${apiUrl}/api/v1/movies?currently_screened=true&sort=showtimes_desc&page=1&page_size=1`)
  assert(discovery.response.status === 200, `API discovery: expected 200, received ${discovery.response.status}`)
  const discoveryPayload = JSON.parse(discovery.body)
  const movie = {
    slug: String(discoveryPayload.items?.[0]?.slug ?? '').trim(),
    title: String(discoveryPayload.items?.[0]?.title ?? '').trim()
  }
  assert(catalog.slugs.includes(movie.slug), 'API discovery film is absent from full catalog')
  assert(movie.title, 'API discovery returned an empty film title')

  await verifyDiscovery(catalog)
  await verifyIndexMatrix(catalog, movie)
  await verifyCatalogLinks(catalog)

  const encodedSlug = encodeURIComponent(movie.slug)
  const pages = [
    { path: '/', canonical: `${siteUrl}/`, ogType: 'website' },
    { path: '/films', canonical: `${siteUrl}/films`, ogType: 'website' },
    { path: `/film/${encodedSlug}`, canonical: `${siteUrl}/film/${encodedSlug}`, ogType: 'video.movie' }
  ]
  const metadata = []
  for (const page of pages) {
    const result = await get(`${webUrl}${page.path}`)
    assert(visibleText(result.body).includes(movie.title), `${page.path}: raw rendered body does not contain discovered film title`)
    metadata.push(verifyHead({ ...page, body: result.body }, page.canonical, page.ogType))
    if (page.path.startsWith('/film/')) verifyBreadcrumb(result.body, movie)
  }
  assert(new Set(metadata.map((item) => item.title)).size === pages.length, 'Route titles are not unique')
  assert(new Set(metadata.map((item) => item.description)).size === pages.length, 'Route descriptions are not unique')

  const missingPath = '/film/__crawlability-missing-film__'
  const missing = await get(`${webUrl}${missingPath}`)
  verifyErrorPolicy(missing, missingPath, 404)
  assert(visibleText(missing.body).includes('Film introuvable'), `${missingPath}: film-not-found UI is missing`)
  const unknownPath = '/__crawlability-missing-route__'
  verifyErrorPolicy(await get(`${webUrl}${unknownPath}`), unknownPath, 404)
  console.log(`Crawlability checks passed (${catalog.total} sitemap films, indexing matrix, SSR pagination, breadcrumbs, and error controls).`)
}

await (expectUpstreamFailure ? verifyFailureMode() : verifyNormalMode())
