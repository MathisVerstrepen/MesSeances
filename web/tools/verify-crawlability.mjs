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

function jsonLdDocuments(html) {
  return [...html.matchAll(/<script\b([^>]*)>(.*?)<\/script>/gis)]
    .filter((match) => attributes(`<script ${match[1]}>`).type === 'application/ld+json')
    .map((match, index) => {
      try {
        return JSON.parse(decodeHtml(match[2]))
      } catch (error) {
        throw new Error(`JSON-LD script ${index + 1} is invalid: ${error.message}`)
      }
    })
}

function graphNodes(html) {
  return jsonLdDocuments(html).flatMap((document) => Array.isArray(document['@graph']) ? document['@graph'] : [document])
}

function nodesOfType(nodes, type) {
  return nodes.filter((node) => node?.['@type'] === type)
}

function verifyGlobalGraph(html) {
  const nodes = graphNodes(html)
  const organization = one(nodesOfType(nodes, 'Organization'), 'global Organization')
  const website = one(nodesOfType(nodes, 'WebSite'), 'global WebSite')
  assert(JSON.stringify(organization) === JSON.stringify({ '@type': 'Organization', '@id': `${siteUrl}/#organization`, name: 'MesSeances', url: `${siteUrl}/` }), 'global Organization facts mismatch')
  assert(JSON.stringify(website) === JSON.stringify({ '@type': 'WebSite', '@id': `${siteUrl}/#website`, name: 'MesSeances', url: `${siteUrl}/`, inLanguage: 'fr-FR', publisher: { '@id': `${siteUrl}/#organization` } }), 'global WebSite facts mismatch')
  return nodes
}

function assertStableInternalLinks(html, path) {
  for (const anchor of tags(html, 'a')) {
    if (!anchor.href) continue
    const target = new URL(anchor.href, webUrl)
    if (target.origin !== webUrl || !/^\/(?:film|cinema|ville)\//.test(target.pathname)) continue
    assert(!target.search && !target.hash, `${path}: stable internal link contains query or fragment: ${anchor.href}`)
    assert(!target.pathname.includes('/screening'), `${path}: screening route link emitted`)
  }
}

function visibleText(html) {
  const body = html.match(/<body\b[^>]*>(.*?)<\/body>/is)?.[1] ?? ''
  return decodeHtml(body.replace(/<script\b[^>]*>.*?<\/script>/gis, ' ').replace(/<style\b[^>]*>.*?<\/style>/gis, ' ').replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' '))
}

function absoluteHttpUrl(value, label) {
  const parsed = new URL(value)
  assert(['http:', 'https:'].includes(parsed.protocol), `${label}: expected absolute HTTP(S) URL`)
}

function hasSafeImagePath(url, origin) {
  const pathEnd = url.search(/[?#]/)
  let decodedPath = url.slice(origin.length, pathEnd === -1 ? undefined : pathEnd)
  try {
    for (let depth = 0; depth < 3; depth++) {
      const decoded = decodeURIComponent(decodedPath)
      if (decoded === decodedPath) break
      decodedPath = decoded
    }
  } catch {
    return false
  }
  return !decodedPath.includes('%')
    && !decodedPath.includes('\\')
    && !decodedPath.slice(1).split('/').some((segment) => segment === '')
    && !decodedPath.split('/').some((segment) => segment === '.' || segment === '..')
}

function safePosterUrl(value) {
  if (!value || String(value).includes('\\')) return null
  try {
    const parsed = new URL(String(value))
    const hostname = parsed.hostname.toLowerCase()
    const allowed = (hostname === 'image.tmdb.org' && parsed.pathname.startsWith('/t/p/w500/') && parsed.pathname !== '/t/p/w500/')
      || ((hostname === 'ugc.fr' || hostname.endsWith('.ugc.fr')) && parsed.pathname !== '/')
      || (hostname === 'cdn.kinepolis.fr' && parsed.pathname.startsWith('/images/') && parsed.pathname !== '/images/')
    return parsed.protocol === 'https:' && !parsed.port && !parsed.username && !parsed.password && !parsed.search && !parsed.hash && allowed && hasSafeImagePath(String(value), parsed.origin) ? parsed.href : null
  } catch {
    return null
  }
}

function safeBackdropUrl(value) {
  const url = String(value ?? '')
  const prefix = 'https://image.tmdb.org/t/p/w780/'
  if (!url.startsWith(prefix) || url.includes('\\')) return null
  try {
    const parsed = new URL(url)
    return parsed.protocol === 'https:' && parsed.hostname === 'image.tmdb.org' && !parsed.port && !parsed.username && !parsed.password && !parsed.search && !parsed.hash && parsed.pathname.startsWith('/t/p/w780/') && parsed.pathname !== '/t/p/w780/' && hasSafeImagePath(url, parsed.origin) ? parsed.href : null
  } catch {
    return null
  }
}

function reservationUrl(showtime) {
  const value = String(showtime.booking_url ?? '').trim()
  if (!value) return null
  try {
    const parsed = new URL(value)
    const hostProvider = parsed.hostname.toLowerCase() === 'www.ugc.fr' ? 'ugc' : parsed.hostname.toLowerCase() === 'kinepolis.fr' ? 'kinepolis' : null
    if (parsed.protocol !== 'https:' || !hostProvider || (showtime.provider && showtime.provider !== hostProvider) || parsed.username || parsed.password || parsed.port) return null
    return parsed.href
  } catch {
    return null
  }
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

async function fetchMovieInventory(includeEnded) {
  const items = []
  let page = 1
  let total
  let generatedAt
  let catalogRevision
  do {
    const scope = includeEnded ? 'all-canonical' : 'current'
    const inventoryQuery = includeEnded ? 'include_ended=true' : 'currently_screened=true'
    const result = await get(`${apiUrl}/api/v1/movies?${inventoryQuery}&sort=title_asc&page=${page}&page_size=${API_PAGE_SIZE}`)
    assert(result.response.status === 200, `API ${scope} catalog page ${page}: expected 200, received ${result.response.status}`)
    const payload = JSON.parse(result.body)
    if (page === 1) {
      total = payload.total
      generatedAt = payload.generated_at
      catalogRevision = payload.catalog_revision
      assert(Number.isSafeInteger(total) && total >= 0, `API ${scope} catalog: invalid total`)
      assert(generatedAt?.constructor === String && Number.isFinite(Date.parse(generatedAt)), `API ${scope} catalog: invalid generated_at`)
      assert(catalogRevision?.constructor === String && catalogRevision.trim(), `API ${scope} catalog: invalid catalog_revision`)
    }
    assert(payload.page === page, `API ${scope} catalog page ${page}: page mismatch`)
    assert(payload.page_size === API_PAGE_SIZE, `API ${scope} catalog page ${page}: page_size mismatch`)
    assert(payload.total === total, `API ${scope} catalog page ${page}: snapshot total changed`)
    assert(payload.generated_at === generatedAt, `API ${scope} catalog page ${page}: generated_at changed`)
    assert(payload.catalog_revision === catalogRevision, `API ${scope} catalog page ${page}: catalog_revision changed`)
    assert(Array.isArray(payload.items), `API ${scope} catalog page ${page}: items missing`)
    items.push(...payload.items)
    page++
  } while (items.length < total)

  assert(items.length === total, `API ${includeEnded ? 'all-canonical' : 'current'} catalog: collected ${items.length}, expected ${total}`)
  const slugs = items.map((item) => String(item.slug ?? '').trim())
  assert(slugs.every((slug) => /^film-[1-9]\d*$/.test(slug)), 'API catalog: non-neutral film slug')
  assert(new Set(slugs).size === slugs.length, 'API catalog: duplicate film slug')
  assert(items.every((item) => item.updated_at?.constructor === String && Number.isFinite(Date.parse(item.updated_at))), 'API catalog: invalid movie updated_at')
  return { items, slugs, total, generatedAt, catalogRevision }
}

async function fetchCityInventory() {
  const result = await get(`${apiUrl}/api/v1/cities`)
  assert(result.response.status === 200, `API city inventory: expected 200, received ${result.response.status}`)
  const payload = JSON.parse(result.body)
  assert(Array.isArray(payload.items) && payload.items.length > 0, 'API city inventory is empty or malformed')
  payload.generated_at = String(payload.generated_at ?? '')
  assert(Number.isFinite(Date.parse(payload.generated_at)), 'API city inventory generated_at invalid')
  const citySlugs = payload.items.map((city) => String(city.slug ?? '').trim())
  const theaters = payload.items.flatMap((city) => city.theaters.map((theater) => ({ ...theater, citySlug: city.slug, cityName: city.name })))
  const theaterSlugs = theaters.map((theater) => String(theater.slug ?? '').trim())
  assert(citySlugs.every(Boolean) && new Set(citySlugs).size === citySlugs.length, 'API city slugs invalid or duplicated')
  assert(theaterSlugs.every(Boolean) && new Set(theaterSlugs).size === theaterSlugs.length, 'API theater slugs invalid or duplicated')
  assert(theaters.every((theater) => String(theater.id ?? '').trim() && String(theater.name ?? '').trim()), 'API theater inventory malformed')
  return { ...payload, citySlugs, theaterSlugs, theaters }
}

async function fetchCityDetails(inventory) {
  const details = []
  for (const city of inventory.items) {
    const result = await get(`${apiUrl}/api/v1/cities/${encodeURIComponent(city.slug)}`)
    assert(result.response.status === 200, `API city ${city.slug}: expected 200, received ${result.response.status}`)
    const detail = JSON.parse(result.body)
    assert(detail.generated_at === inventory.generated_at, `API city ${city.slug}: generation mismatch`)
    assert(detail.city?.slug === city.slug && Array.isArray(detail.theaters) && Array.isArray(detail.movies), `API city ${city.slug}: malformed detail`)
    details.push(detail)
  }
  return details
}

function sitemapEntries(xml) {
  return [...xml.matchAll(/<url>\s*<loc>(.*?)<\/loc>(?:\s*<lastmod>(.*?)<\/lastmod>)?\s*<\/url>/gs)]
    .map((match) => ({ loc: decodeHtml(match[1]), lastmod: match[2] ? decodeHtml(match[2]) : null }))
}

async function verifyDiscovery(allCatalog, inventory) {
  const sitemap = await get(`${webUrl}/sitemap.xml`, 'application/xml')
  assert(sitemap.response.status === 200, `/sitemap.xml: expected 200, received ${sitemap.response.status}`)
  assert(sitemap.response.headers.get('content-type')?.toLowerCase() === 'application/xml; charset=utf-8', '/sitemap.xml: unexpected content type')
  assert(sitemap.response.headers.get('cache-control') === 'no-store', '/sitemap.xml: expected Cache-Control no-store')
  assert(sitemap.body.endsWith('\n'), '/sitemap.xml: missing trailing newline')
  const entries = sitemapEntries(sitemap.body)
  const expectedLocations = [
    `${siteUrl}/`,
    `${siteUrl}/films`,
    `${siteUrl}/cinemas`,
    `${siteUrl}/planning`,
    `${siteUrl}/recherche`,
    ...[...inventory.citySlugs].sort().map((slug) => `${siteUrl}/ville/${encodeURIComponent(slug)}/cinemas`),
    ...[...inventory.theaterSlugs].sort().map((slug) => `${siteUrl}/cinema/${encodeURIComponent(slug)}`),
    ...[...allCatalog.slugs].sort().map((slug) => `${siteUrl}/film/${encodeURIComponent(slug)}`)
  ]
  assert(entries.length === expectedLocations.length, `/sitemap.xml: expected ${expectedLocations.length} URLs, received ${entries.length}`)
  assert(new Set(entries.map((entry) => entry.loc)).size === entries.length, '/sitemap.xml: duplicate URL')
  assert(entries.every((entry, index) => entry.loc === expectedLocations[index]), '/sitemap.xml: URL order or set mismatch')
  for (const entry of entries) {
    const movieSlug = entry.loc.startsWith(`${siteUrl}/film/`) ? decodeURIComponent(entry.loc.slice(`${siteUrl}/film/`.length)) : null
    const movie = movieSlug ? allCatalog.items.find((item) => item.slug === movieSlug) : null
    const staticTool = entry.loc === `${siteUrl}/planning` || entry.loc === `${siteUrl}/recherche`
    const expectedLastmod = staticTool ? null : movie ? movie.updated_at : allCatalog.generatedAt
    assert(entry.lastmod === expectedLastmod, `/sitemap.xml: incorrect lastmod for ${entry.loc}`)
  }

  const robots = await get(`${webUrl}/robots.txt`, 'text/plain')
  assert(robots.response.status === 200, `/robots.txt: expected 200, received ${robots.response.status}`)
  assert(robots.response.headers.get('content-type')?.toLowerCase() === 'text/plain; charset=utf-8', '/robots.txt: unexpected content type')
  assert(robots.body === `User-agent: *\nAllow: /\nSitemap: ${siteUrl}/sitemap.xml\n`, '/robots.txt: body mismatch')
  assert(!robots.body.toLowerCase().includes('disallow'), '/robots.txt: noindex routes must not be disallowed')
}

async function verifyIndexMatrix(catalog, movie, city, theater, defaultDate) {
  const encodedSlug = encodeURIComponent(movie.slug)
  const queryless = [
    ['/', `${siteUrl}/`],
    ['/films', `${siteUrl}/films`],
    ['/cinemas', `${siteUrl}/cinemas`],
    ['/planning', `${siteUrl}/planning`],
    ['/recherche', `${siteUrl}/recherche`],
    [`/film/${encodedSlug}`, `${siteUrl}/film/${encodedSlug}`],
    [`/ville/${encodeURIComponent(city.slug)}/cinemas`, `${siteUrl}/ville/${encodeURIComponent(city.slug)}/cinemas`],
    [`/cinema/${encodeURIComponent(theater.slug)}`, `${siteUrl}/cinema/${encodeURIComponent(theater.slug)}`]
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
    ['/cinemas?city=Lille', `${siteUrl}/cinemas`],
    ['/planning?date=2026-01-01', `${siteUrl}/planning`],
    ['/planning?foreign=1', `${siteUrl}/planning`],
    ['/recherche?grouping=chronological', `${siteUrl}/recherche`],
    ['/recherche?layout=boxes', `${siteUrl}/recherche`],
    ['/recherche?grouping=chronological&layout=boxes', `${siteUrl}/recherche`],
    ['/recherche?foreign=1', `${siteUrl}/recherche`],
    [`/film/${encodedSlug}?date=2026-01-01`, `${siteUrl}/film/${encodedSlug}`],
    [`/film/${encodedSlug}?foreign=1`, `${siteUrl}/film/${encodedSlug}`],
    [`/ville/${encodeURIComponent(city.slug)}/cinemas?foreign=1`, `${siteUrl}/ville/${encodeURIComponent(city.slug)}/cinemas`],
    [`/cinema/${encodeURIComponent(theater.slug)}?date=bad`, `${siteUrl}/cinema/${encodeURIComponent(theater.slug)}`],
    [`/cinema/${encodeURIComponent(theater.slug)}?date=2026-02-31`, `${siteUrl}/cinema/${encodeURIComponent(theater.slug)}`],
    [`/cinema/${encodeURIComponent(theater.slug)}?date=`, `${siteUrl}/cinema/${encodeURIComponent(theater.slug)}`],
    [`/cinema/${encodeURIComponent(theater.slug)}?date=2026-01-01&date=2026-01-02`, `${siteUrl}/cinema/${encodeURIComponent(theater.slug)}`],
    [`/cinema/${encodeURIComponent(theater.slug)}?foreign=1`, `${siteUrl}/cinema/${encodeURIComponent(theater.slug)}`]
  ]
  if (defaultDate) noindexCanonicalCases.push([`/cinema/${encodeURIComponent(theater.slug)}?date=${defaultDate}`, `${siteUrl}/cinema/${encodeURIComponent(theater.slug)}`])
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

  for (const path of ['/credits', '/admin', '/admin/login', '/admin/sync', '/admin/tmdb-matches']) {
    const result = await get(`${webUrl}${path}`)
    assert(result.response.status < 400, `${path}: unexpected status ${result.response.status}`)
    assert(meta(result.body, 'robots') === 'noindex,follow', `${path}: expected noindex,follow`)
  }
}

async function verifyCatalogLinks(catalog) {
  const foundFilms = new Map()
  const totalPages = Math.max(1, Math.ceil(catalog.total / CATALOG_PAGE_SIZE))
  for (let page = 1; page <= totalPages; page++) {
    const path = page === 1 ? '/films' : `/films?page=${page}`
    const result = await get(`${webUrl}${path}`)
    assert(result.response.status === 200, `${path}: expected 200, received ${result.response.status}`)
    const hrefs = tags(result.body, 'a').map((anchor) => anchor.href).filter(Boolean)
    for (const href of hrefs) {
      const pathname = new URL(href, webUrl).pathname
      if (pathname.startsWith('/film/')) {
        const slug = decodeURIComponent(pathname.slice('/film/'.length))
        assert(/^film-[1-9]\d*$/.test(slug), `${path}: non-neutral film link ${slug}`)
        foundFilms.set(slug, (foundFilms.get(slug) ?? 0) + 1)
      }
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
  assert(foundFilms.size === catalog.slugs.length && catalog.slugs.every((slug) => foundFilms.get(slug) === 1), 'SSR /films anchors do not reach each current canonical film exactly once')
}

async function verifyCinemaDirectory(inventory) {
  const path = '/cinemas'
  const result = await get(`${webUrl}${path}`)
  verifyPolicy(result, path, 'index,follow', `${siteUrl}/cinemas`)
  const hrefs = tags(result.body, 'a').map((anchor) => new URL(anchor.href, webUrl).pathname)
  for (const slug of inventory.citySlugs) {
    assert(hrefs.includes(`/ville/${encodeURIComponent(slug)}/cinemas`), `${path}: missing SSR city link ${slug}`)
  }
  for (const slug of inventory.theaterSlugs) {
    assert(hrefs.includes(`/cinema/${encodeURIComponent(slug)}`), `${path}: missing SSR cinema link ${slug}`)
  }
  assertStableInternalLinks(result.body, path)
}

function verifyBreadcrumb(html, movie) {
  const nav = html.match(/<nav\b[^>]*aria-label=(["'])Fil d’Ariane\1[^>]*>(.*?)<\/nav>/is)
  assert(nav, 'Film breadcrumb nav missing')
  const hrefs = tags(nav[2], 'a').map((anchor) => new URL(anchor.href, webUrl).pathname)
  assert(hrefs.length === 2 && hrefs[0] === '/' && hrefs[1] === '/films', 'Film breadcrumb links mismatch')
  assert(/aria-current=(["'])page\1/i.test(nav[2]), 'Film breadcrumb current item missing aria-current')
  assert(visibleText(`<body>${nav[2]}</body>`).includes(movie.title), 'Film breadcrumb current title missing')
}

function expectedEvents(showtimes, theaterUrl, theaterId, movieIdFor) {
  const seen = new Set()
  return showtimes.flatMap((showtime) => {
    const id = String(showtime.id ?? '').trim()
    const start = Date.parse(showtime.start_time)
    const end = Date.parse(showtime.end_time)
    const movieId = movieIdFor(showtime)
    if (!id || seen.has(id) || !movieId || !Number.isFinite(start) || !Number.isFinite(end) || end <= start) return []
    seen.add(id)
    return [{
      '@type': 'ScreeningEvent',
      '@id': `${theaterUrl(showtime)}#screening-${encodeURIComponent(id)}`,
      startDate: showtime.start_time,
      endDate: showtime.end_time,
      location: { '@id': theaterId(showtime) },
      workPresented: { '@id': movieId }
    }]
  })
}

function verifyEventNodes(actual, expected, path) {
  assert(actual.length === expected.length, `${path}: expected ${expected.length} ScreeningEvent nodes, received ${actual.length}`)
  const byId = new Map(actual.map((event) => [event['@id'], event]))
  assert(byId.size === actual.length, `${path}: duplicate ScreeningEvent IDs`)
  for (const wanted of expected) {
    const event = byId.get(wanted['@id'])
    assert(event, `${path}: missing event ${wanted['@id']}`)
    assert(String(event.name ?? '').trim(), `${path}: event name missing`)
    for (const key of ['startDate', 'endDate', 'location', 'workPresented']) assert(JSON.stringify(event[key]) === JSON.stringify(wanted[key]), `${path}: event ${wanted['@id']} ${key} mismatch`)
    const allowed = ['@type', '@id', 'name', 'startDate', 'endDate', 'location', 'workPresented']
    assert(Object.keys(event).every((key) => allowed.includes(key)), `${path}: event ${wanted['@id']} contains unsupported facts`)
    assert(!('url' in event) && !('offers' in event), `${path}: event URL or offers must be omitted`)
  }
}

function verifyFilmJsonLd(html, movie, schedule, path) {
  const nodes = verifyGlobalGraph(html)
  const canonicalUrl = `${siteUrl}/film/${encodeURIComponent(movie.slug)}`
  const movieNode = nodes.find((node) => node['@type'] === 'Movie' && node['@id'] === `${canonicalUrl}#movie`)
  assert(movieNode?.name === movie.title && movieNode.url === canonicalUrl, `${path}: Movie identity mismatch`)
  assert(movieNode.duration === `PT${movie.runtime_minutes}M`, `${path}: Movie duration mismatch`)
  assert(movieNode.image !== `${siteUrl}/pwa-512x512.png`, `${path}: app icon used as Movie image`)
  const breadcrumb = nodes.find((node) => node['@type'] === 'BreadcrumbList' && node['@id'] === `${canonicalUrl}#breadcrumb`)
  assert(breadcrumb && breadcrumb.itemListElement?.length === 3, `${path}: BreadcrumbList missing`)
  assert(JSON.stringify(breadcrumb.itemListElement.map((item) => item.item)) === JSON.stringify([`${siteUrl}/`, `${siteUrl}/films`, canonicalUrl]), `${path}: BreadcrumbList links mismatch`)

  const showtimes = schedule.theaters.flatMap((theater) => theater.showtimes.map((showtime) => ({ ...showtime, theater })))
  const expected = expectedEvents(
    showtimes,
    (showtime) => `${siteUrl}/cinema/${encodeURIComponent(showtime.theater.slug)}`,
    (showtime) => `${siteUrl}/cinema/${encodeURIComponent(showtime.theater.slug)}#cinema`,
    () => `${canonicalUrl}#movie`
  )
  verifyEventNodes(nodesOfType(nodes, 'ScreeningEvent'), expected, path)
  const theaters = nodesOfType(nodes, 'MovieTheater')
  assert(theaters.length === schedule.theaters.filter((theater) => theater.showtimes.length).length, `${path}: MovieTheater count mismatch`)
  assertStableInternalLinks(html, path)
}

function verifyCinemaJsonLd(html, response, path) {
  const nodes = verifyGlobalGraph(html)
  const canonicalUrl = `${siteUrl}/cinema/${encodeURIComponent(response.theater.slug)}`
  const theaterId = `${canonicalUrl}#cinema`
  const theater = nodes.find((node) => node['@type'] === 'MovieTheater' && node['@id'] === theaterId)
  assert(theater?.name === response.theater.name && theater.url === canonicalUrl, `${path}: MovieTheater identity mismatch`)
  assert(theater.description === meta(html, 'description'), `${path}: MovieTheater description does not match metadata`)
  const completeAddress = response.theater.address.trim() && response.theater.city.trim() && response.theater.postal_code.trim()
  assert(completeAddress ? theater.address === response.theater.address.trim() : !('address' in theater), `${path}: structured address inclusion mismatch`)
  assert(!('geo' in theater) && !('latitude' in theater) && !('longitude' in theater), `${path}: fabricated geo facts present`)
  const movieIds = new Map()
  for (const showtime of response.showtimes) movieIds.set(showtime.movie.slug, `${siteUrl}/film/${encodeURIComponent(showtime.movie.slug)}#movie`)
  const expected = expectedEvents(
    response.showtimes,
    () => canonicalUrl,
    () => theaterId,
    (showtime) => movieIds.get(showtime.movie.slug)
  )
  verifyEventNodes(nodesOfType(nodes, 'ScreeningEvent'), expected, path)
  assert(nodesOfType(nodes, 'Movie').length === movieIds.size, `${path}: Movie node count mismatch`)
  assertStableInternalLinks(html, path)
}

function verifyEntityDescription(html, path, descriptions) {
  const description = meta(html, 'description')
  assert(description.trim(), `${path}: description is empty`)
  assert(meta(html, 'og:description') === description, `${path}: Open Graph description mismatch`)
  assert(visibleText(html).includes(description), `${path}: metadata description is not visible in SSR body`)
  assert(!descriptions.has(description), `${path}: description duplicates another city or cinema page`)
  descriptions.add(description)
}

function groupedShowtimes(response) {
  const groups = new Map()
  for (const showtime of response.showtimes) {
    const slug = showtime.movie.slug
    const current = groups.get(slug)
    if (current) {
      current.showtimes.push(showtime)
      current.posterUrl ||= safePosterUrl(showtime.poster_url)
      current.backdropUrl ||= safeBackdropUrl(showtime.backdrop_url)
    } else {
      groups.set(slug, {
        movie: showtime.movie,
        showtimes: [showtime],
        posterUrl: safePosterUrl(showtime.poster_url),
        backdropUrl: safeBackdropUrl(showtime.backdrop_url)
      })
    }
  }
  return [...groups.values()]
}

function verifyCinemaRendering(html, response, path) {
  assert(response.date, `${path}: API selected date is empty`)
  assert(tags(html, 'time').some((tag) => tag.datetime === response.date), `${path}: selected date <time> missing or incorrect`)
  const text = visibleText(html)
  if (response.showtimes.length === 0) assert(text.includes('Aucune séance à cette date'), `${path}: empty-date state missing`)

  const images = tags(html, 'img').filter((image) => image['data-media-kind'])
  const fallbacks = tags(html, 'div').filter((element) => element['data-poster-fallback'])
  for (const group of groupedShowtimes(response)) {
    assert(text.includes(group.movie.title), `${path}: film title ${group.movie.title} missing`)
    for (const [kind, expected] of [['poster', group.posterUrl], ['backdrop', group.backdropUrl]]) {
      const matching = images.filter((image) => image['data-movie-slug'] === group.movie.slug && image['data-media-kind'] === kind)
      assert(matching.length === (expected ? 1 : 0), `${path}: ${kind} count mismatch for ${group.movie.slug}`)
      if (expected) assert(matching[0].src === expected, `${path}: ${kind} source mismatch for ${group.movie.slug}`)
    }
    if (!group.posterUrl) assert(fallbacks.some((fallback) => fallback['data-poster-fallback'] === group.movie.slug), `${path}: poster fallback missing for ${group.movie.slug}`)
  }

  const bookingAnchors = tags(html, 'a').filter((anchor) => anchor['data-showtime-id'])
  const unavailableCards = tags(html, 'span').filter((span) => span['data-showtime-id'])
  for (const showtime of response.showtimes) {
    const expected = reservationUrl(showtime)
    const anchors = bookingAnchors.filter((anchor) => anchor['data-showtime-id'] === showtime.id)
    const unavailable = unavailableCards.filter((span) => span['data-showtime-id'] === showtime.id)
    if (expected) {
      assert(anchors.length === 1 && unavailable.length === 0, `${path}: booking anchor mismatch for ${showtime.id}`)
      assert(anchors[0].href === expected && anchors[0].target === '_blank', `${path}: booking target mismatch for ${showtime.id}`)
      assert(new Set(String(anchors[0].rel ?? '').split(/\s+/)).has('noopener') && new Set(String(anchors[0].rel ?? '').split(/\s+/)).has('noreferrer'), `${path}: booking rel mismatch for ${showtime.id}`)
      assert(anchors[0]['aria-label']?.includes(showtime.movie.title) && anchors[0]['aria-label']?.includes(response.theater.name), `${path}: booking label lacks film or cinema for ${showtime.id}`)
    } else {
      assert(anchors.length === 0 && unavailable.length === 1 && unavailable[0]['aria-disabled'] === 'true', `${path}: unavailable booking card mismatch for ${showtime.id}`)
    }
  }
}

async function discoverCinemaFixture(theaters) {
  let best = null
  for (const theater of theaters) {
    for (const date of theater.available_dates ?? []) {
      const result = await get(`${apiUrl}/api/v1/theaters/${encodeURIComponent(theater.slug)}/showtimes?date=${encodeURIComponent(date)}`)
      assert(result.response.status === 200, `API cinema fixture ${theater.slug} ${date}: expected 200, received ${result.response.status}`)
      const response = JSON.parse(result.body)
      const groups = groupedShowtimes(response)
      const mediaGroups = groups.filter((group) => group.posterUrl || group.backdropUrl).length
      const availableBooking = response.showtimes.some((showtime) => reservationUrl(showtime))
      const unavailableBooking = response.showtimes.some((showtime) => !reservationUrl(showtime))
      const score = groups.length * 10 + mediaGroups * 4 + Number(availableBooking) * 2 + Number(unavailableBooking)
      if (!best || score > best.score) best = { theater, date, response, groups: groups.length, mediaGroups, availableBooking, unavailableBooking, score }
      if (groups.length >= 2 && mediaGroups >= 2 && availableBooking && unavailableBooking) return best
    }
  }
  return best
}

async function discoverTodayEmptyFixture(theaters, today) {
  for (const theater of theaters) {
    const result = await get(`${apiUrl}/api/v1/theaters/${encodeURIComponent(theater.slug)}/showtimes?date=${today}`)
    assert(result.response.status === 200, `API today-empty fixture ${theater.slug}: expected 200, received ${result.response.status}`)
    const response = JSON.parse(result.body)
    if (response.showtimes.length === 0) return { theater, response }
  }
  return null
}

async function verifyEndedFilm(allCatalog, currentCatalog, today) {
  const currentSlugs = new Set(currentCatalog.slugs)
  let ended = null
  for (const movie of allCatalog.items.filter((item) => !currentSlugs.has(item.slug))) {
    const encodedSlug = encodeURIComponent(movie.slug)
    const apiResult = await get(`${apiUrl}/api/v1/movies/${encodedSlug}/showtimes?date=${today}`)
    assert(apiResult.response.status === 200, `Ended-film candidate API ${movie.slug}: expected 200, received ${apiResult.response.status}`)
    const schedule = JSON.parse(apiResult.body)
    assert(schedule.movie?.slug === movie.slug, `Ended-film candidate API ${movie.slug}: canonical metadata mismatch`)
    assert(Array.isArray(schedule.available_dates) && Array.isArray(schedule.theaters), `Ended-film candidate API ${movie.slug}: malformed screening inventory`)
    const showtimeCount = schedule.theaters.reduce((count, theater) => {
      assert(Array.isArray(theater.showtimes), `Ended-film candidate API ${movie.slug}: malformed theater showtimes`)
      return count + theater.showtimes.length
    }, 0)
    if (schedule.available_dates.length === 0 && schedule.theaters.length === 0 && showtimeCount === 0) {
      ended = { movie, schedule }
      break
    }
  }
  if (!ended) {
    console.log('Unconfirmed coverage: no ended canonical film fixture was available.')
    return false
  }

  const encodedSlug = encodeURIComponent(ended.movie.slug)
  const path = `/film/${encodedSlug}`
  const page = await get(`${webUrl}${path}`)
  verifyPolicy(page, path, 'index,follow', `${siteUrl}${path}`)
  assert(visibleText(page.body).includes('Aucune séance programmée pour le moment.'), `${path}: ended-film state missing`)
  verifyFilmJsonLd(page.body, ended.schedule.movie, ended.schedule, path)
  assert(nodesOfType(graphNodes(page.body), 'ScreeningEvent').length === 0, `${path}: ended film emitted ScreeningEvent JSON-LD`)
  console.log(`Ended-film fixture: ${ended.movie.slug}; indexable 200 with no-screenings state and no ScreeningEvent.`)
  return true
}

async function verifyHistoricalAlias(allCatalog, today) {
  for (const movie of allCatalog.items) {
    if (!Number.isSafeInteger(movie.tmdb_id) || movie.tmdb_id <= 0) continue
    const alias = `tmdb-film-${movie.tmdb_id}`
    const apiResult = await get(`${apiUrl}/api/v1/movies/${encodeURIComponent(alias)}/showtimes?date=${today}`)
    if (apiResult.response.status === 404) continue
    assert(apiResult.response.status === 200, `Historical alias API ${alias}: expected 200 or 404, received ${apiResult.response.status}`)
    const schedule = JSON.parse(apiResult.body)
    const canonicalSlug = String(schedule.movie?.slug ?? '')
    assert(/^film-[1-9]\d*$/.test(canonicalSlug) && canonicalSlug !== alias, `Historical alias API ${alias}: canonical neutral slug missing`)

    const query = 'scalar=one&repeat=first&repeat=second&flag'
    const result = await get(`${webUrl}/film/${encodeURIComponent(alias)}?${query}`)
    assert(result.response.status === 308, `Historical alias ${alias}: expected 308, received ${result.response.status}`)
    const locationValue = result.response.headers.get('location')
    assert(locationValue, `Historical alias ${alias}: Location missing`)
    const location = new URL(locationValue, webUrl)
    assert(location.pathname === `/film/${encodeURIComponent(canonicalSlug)}`, `Historical alias ${alias}: incorrect redirect path`)
    assert(location.searchParams.get('scalar') === 'one', `Historical alias ${alias}: scalar query lost`)
    assert(JSON.stringify(location.searchParams.getAll('repeat')) === JSON.stringify(['first', 'second']), `Historical alias ${alias}: repeated query values lost`)
    assert(location.searchParams.has('flag') && location.searchParams.get('flag') === '', `Historical alias ${alias}: flag query lost`)
    console.log(`Historical alias fixture: ${alias} -> ${canonicalSlug}; 308 preserves scalar, repeated, and flag query values.`)
    return true
  }
  console.log('Unconfirmed coverage: no confirmed-TMDB historical alias fixture was available.')
  return false
}

function todayInParis() {
  const parts = new Intl.DateTimeFormat('en-CA', { timeZone: 'Europe/Paris', year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(new Date())
  const value = Object.fromEntries(parts.map((part) => [part.type, part.value]))
  return `${value.year}-${value.month}-${value.day}`
}

async function verifyFailureMode() {
  for (const path of ['/', '/films', '/cinemas', '/film/upstream-check', '/cinema/upstream-check', '/ville/upstream-check/cinemas']) {
    const result = await get(`${webUrl}${path}`)
    verifyErrorPolicy(result, path, 502)
    const text = visibleText(result.body)
    assert(text.includes('Impossible de joindre le service'), `${path}: recoverable French upstream error is missing`)
    assert(!text.includes('Film introuvable'), `${path}: upstream failure was rendered as film not found`)
    verifyGlobalGraph(result.body)
  }
  const sitemap = await get(`${webUrl}/sitemap.xml`, 'application/xml')
  assert(sitemap.response.status === 503, `/sitemap.xml: expected 503, received ${sitemap.response.status}`)
  assert(!sitemap.body.includes('<urlset'), '/sitemap.xml: partial or stale sitemap returned on upstream failure')
  assert(sitemap.response.headers.get('x-robots-tag') === 'noindex,follow', '/sitemap.xml: missing error X-Robots-Tag')
  console.log('Crawlability upstream-failure checks passed (6 rendered 502 routes, global graph, and non-partial sitemap 503).')
}

async function verifyNormalMode() {
  const [currentCatalog, allCatalog] = await Promise.all([
    fetchMovieInventory(false),
    fetchMovieInventory(true)
  ])
  const inventory = await fetchCityInventory()
  assert(currentCatalog.catalogRevision === allCatalog.catalogRevision, 'Current and all-canonical catalog revisions differ')
  assert(currentCatalog.generatedAt === allCatalog.generatedAt, 'Current and all-canonical catalog generations differ')
  assert(inventory.generated_at === currentCatalog.generatedAt, 'Movie and city inventory generations differ')
  assert(currentCatalog.slugs.every((slug) => allCatalog.slugs.includes(slug)), 'Current catalog contains a film absent from all-canonical inventory')
  const cityDetails = await fetchCityDetails(inventory)
  assert(currentCatalog.items.length > 0, 'API discovery returned no current film')
  const discovery = await get(`${apiUrl}/api/v1/movies?currently_screened=true&sort=showtimes_desc&page=1&page_size=1`)
  assert(discovery.response.status === 200, `API discovery: expected 200, received ${discovery.response.status}`)
  const discoveryPayload = JSON.parse(discovery.body)
  const movie = {
    slug: String(discoveryPayload.items?.[0]?.slug ?? '').trim(),
    title: String(discoveryPayload.items?.[0]?.title ?? '').trim()
  }
  assert(currentCatalog.slugs.includes(movie.slug), 'API discovery film is absent from current catalog')
  assert(movie.title, 'API discovery returned an empty film title')

  const city = inventory.items[0]
  const theater = inventory.theaters[0]
  const today = todayInParis()
  const theaterApi = await get(`${apiUrl}/api/v1/theaters/${encodeURIComponent(theater.slug)}/showtimes?date=${today}`)
  assert(theaterApi.response.status === 200, 'API representative theater showtimes unavailable')
  const theaterResponse = JSON.parse(theaterApi.body)
  assert(theaterResponse.generated_at === currentCatalog.generatedAt, 'Representative theater generation mismatch')

  await verifyDiscovery(allCatalog, inventory)
  await verifyIndexMatrix(currentCatalog, movie, city, theater, today)
  await verifyCatalogLinks(currentCatalog)
  await verifyCinemaDirectory(inventory)

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
    verifyGlobalGraph(result.body)
  }
  assert(new Set(metadata.map((item) => item.title)).size === pages.length, 'Route titles are not unique')
  assert(new Set(metadata.map((item) => item.description)).size === pages.length, 'Route descriptions are not unique')

  const filmApi = await get(`${apiUrl}/api/v1/movies/${encodeURIComponent(movie.slug)}/showtimes?date=${todayInParis()}`)
  assert(filmApi.response.status === 200, 'API representative film showtimes unavailable')
  const filmSchedule = JSON.parse(filmApi.body)
  const filmPage = await get(`${webUrl}/film/${encodedSlug}`)
  verifyFilmJsonLd(filmPage.body, filmSchedule.movie, filmSchedule, `/film/${encodedSlug}`)

  const entityDescriptions = new Set()
  for (const cityDetail of cityDetails) {
    const cityPath = `/ville/${encodeURIComponent(cityDetail.city.slug)}/cinemas`
    const cityPage = await get(`${webUrl}${cityPath}`)
    verifyPolicy(cityPage, cityPath, 'index,follow', `${siteUrl}${cityPath}`)
    verifyGlobalGraph(cityPage.body)
    verifyEntityDescription(cityPage.body, cityPath, entityDescriptions)
    assertStableInternalLinks(cityPage.body, cityPath)
    const cityHrefs = tags(cityPage.body, 'a').map((anchor) => new URL(anchor.href, webUrl).pathname)
    assert(cityDetail.theaters.every((item) => cityHrefs.includes(`/cinema/${encodeURIComponent(item.slug)}`)), `${cityPath}: cinema links incomplete`)
    assert(cityDetail.movies.every((item) => /^film-[1-9]\d*$/.test(item.slug) && cityHrefs.includes(`/film/${encodeURIComponent(item.slug)}`)), `${cityPath}: neutral film links incomplete`)
  }

  for (const cinema of inventory.theaters) {
    const apiResult = await get(`${apiUrl}/api/v1/theaters/${encodeURIComponent(cinema.slug)}/showtimes?date=${today}`)
    assert(apiResult.response.status === 200, `API cinema ${cinema.slug}: expected 200, received ${apiResult.response.status}`)
    const cinemaResponse = JSON.parse(apiResult.body)
    assert(cinemaResponse.generated_at === currentCatalog.generatedAt, `API cinema ${cinema.slug}: generation mismatch`)
    const cinemaPath = `/cinema/${encodeURIComponent(cinema.slug)}`
    const cinemaPage = await get(`${webUrl}${cinemaPath}`)
    verifyPolicy(cinemaPage, cinemaPath, 'index,follow', `${siteUrl}${cinemaPath}`)
    verifyEntityDescription(cinemaPage.body, cinemaPath, entityDescriptions)
    verifyCinemaJsonLd(cinemaPage.body, cinemaResponse, cinemaPath)
    assert(visibleText(cinemaPage.body).includes(cinema.name), `${cinemaPath}: cinema name missing from SSR body`)
  }

  const cinemaPath = `/cinema/${encodeURIComponent(theater.slug)}`
  const cinemaPage = await get(`${webUrl}${cinemaPath}`)
  verifyCinemaRendering(cinemaPage.body, theaterResponse, cinemaPath)

  const cinemaFixture = await discoverCinemaFixture(cityDetails.flatMap((detail) => detail.theaters))
  if (cinemaFixture) {
    const fixturePath = `/cinema/${encodeURIComponent(cinemaFixture.theater.slug)}?date=${encodeURIComponent(cinemaFixture.date)}`
    const fixturePage = await get(`${webUrl}${fixturePath}`)
    verifyPolicy(fixturePage, fixturePath, 'noindex,follow', `${siteUrl}/cinema/${encodeURIComponent(cinemaFixture.theater.slug)}`)
    verifyCinemaJsonLd(fixturePage.body, cinemaFixture.response, fixturePath)
    verifyCinemaRendering(fixturePage.body, cinemaFixture.response, fixturePath)
    console.log(`Cinema fixture: ${cinemaFixture.theater.slug} ${cinemaFixture.date}; ${cinemaFixture.groups} film group(s), ${cinemaFixture.mediaGroups} media-backed group(s), booking available=${cinemaFixture.availableBooking}, unavailable=${cinemaFixture.unavailableBooking}.`)
    if (cinemaFixture.groups < 2) console.log('Unconfirmed coverage: no explicit-date cinema fixture with multiple film groups was available.')
    if (cinemaFixture.mediaGroups === 0) console.log('Unconfirmed coverage: no source-backed cinema media fixture was available.')
    if (!cinemaFixture.availableBooking) console.log('Unconfirmed coverage: no valid cinema booking URL fixture was available.')
    if (!cinemaFixture.unavailableBooking) console.log('Unconfirmed coverage: no unavailable cinema booking fixture was available.')
  } else {
    console.log('Unconfirmed coverage: no cinema with an available explicit date was returned by API discovery.')
  }

  const todayEmptyFixture = await discoverTodayEmptyFixture(cityDetails.flatMap((detail) => detail.theaters), today)
  if (todayEmptyFixture) {
    const emptyPath = `/cinema/${encodeURIComponent(todayEmptyFixture.theater.slug)}`
    const emptyPage = await get(`${webUrl}${emptyPath}`)
    verifyPolicy(emptyPage, emptyPath, 'index,follow', `${siteUrl}${emptyPath}`)
    verifyCinemaJsonLd(emptyPage.body, todayEmptyFixture.response, emptyPath)
    verifyCinemaRendering(emptyPage.body, todayEmptyFixture.response, emptyPath)
    console.log(`Today-empty fixture: ${todayEmptyFixture.theater.slug} ${today}; queryless page returned 200 with explicit empty state.`)
  } else {
    console.log('Unconfirmed coverage: no cinema without sessions today was available.')
  }

  await verifyEndedFilm(allCatalog, currentCatalog, today)
  await verifyHistoricalAlias(allCatalog, today)

  const missingPath = '/film/__crawlability-missing-film__'
  const missing = await get(`${webUrl}${missingPath}`)
  verifyErrorPolicy(missing, missingPath, 404)
  assert(visibleText(missing.body).includes('Film introuvable'), `${missingPath}: film-not-found UI is missing`)
  for (const [path, text] of [['/cinema/__crawlability-missing-cinema__', 'Cinéma introuvable'], ['/ville/__crawlability-missing-city__/cinemas', 'Ville introuvable']]) {
    const result = await get(`${webUrl}${path}`)
    verifyErrorPolicy(result, path, 404)
    assert(visibleText(result.body).includes(text), `${path}: not-found UI missing`)
    verifyGlobalGraph(result.body)
  }
  const unknownPath = '/__crawlability-missing-route__'
  verifyErrorPolicy(await get(`${webUrl}${unknownPath}`), unknownPath, 404)
  console.log(`Crawlability checks passed (${currentCatalog.total} current films, ${allCatalog.total} canonical films, ${inventory.citySlugs.length} cities, ${inventory.theaterSlugs.length} cinemas, exact sitemap, descriptions, JSON-LD, indexing, links, redirects, and errors).`)
}

await (expectUpstreamFailure ? verifyFailureMode() : verifyNormalMode())
