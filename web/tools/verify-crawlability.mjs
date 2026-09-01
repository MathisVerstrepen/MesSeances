import { spawn } from 'node:child_process'
import { access } from 'node:fs/promises'
import { createServer } from 'node:http'
import { fileURLToPath } from 'node:url'
import process from 'node:process'
import { setTimeout as delay } from 'node:timers/promises'
import { parse as parseDevalue } from 'devalue'

const apiUrl = origin(process.env.API_URL ?? 'http://localhost:8080', 'API_URL')
const webUrl = origin(process.env.WEB_URL ?? 'http://localhost:3000', 'WEB_URL')
const siteUrl = origin(process.env.SITE_URL ?? 'http://localhost:3000', 'SITE_URL')
const expectUpstreamFailure = process.env.EXPECT_UPSTREAM_FAILURE === '1'
const expectSeoOnlyFailure = process.env.EXPECT_SEO_ONLY_FAILURE === '1'
const expectSsrSuccess = process.env.EXPECT_SSR_SUCCESS === '1'
const expectSitemapFixture = process.env.EXPECT_SITEMAP_FIXTURE === '1'
const API_PAGE_SIZE = 100
const CATALOG_PAGE_SIZE = 24
const FIXTURE_INTERNAL_SECRET = 'a'.repeat(64)
const REQUEST_ID_PATTERN = /^[0-9a-f]{32}$/

assert([expectUpstreamFailure, expectSeoOnlyFailure, expectSsrSuccess, expectSitemapFixture].filter(Boolean).length <= 1, 'EXPECT_UPSTREAM_FAILURE, EXPECT_SEO_ONLY_FAILURE, EXPECT_SSR_SUCCESS, and EXPECT_SITEMAP_FIXTURE are mutually exclusive')

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
  return jsonLdScripts(html).map((script, index) => {
    try {
      return JSON.parse(decodeHtml(script.body))
    } catch (error) {
      throw new Error(`JSON-LD script ${index + 1} is invalid: ${error.message}`)
    }
  })
}

function scriptElements(html) {
  return [...html.matchAll(/<script\b([^>]*)>(.*?)<\/script>/gis)]
    .map((match) => ({ attributes: attributes(`<script ${match[1]}>`), body: match[2], source: match[0] }))
}

function jsonLdScripts(html) {
  return scriptElements(html).filter((script) => script.attributes.type === 'application/ld+json')
}

function withoutJsonLd(html) {
  return scriptElements(html)
    .filter((script) => script.attributes.type === 'application/ld+json')
    .reduce((result, script) => result.replace(script.source, ''), html)
}

function nuxtPayload(html, path) {
  const scripts = scriptElements(html).filter((script) => script.attributes.id === '__NUXT_DATA__' || 'data-nuxt-data' in script.attributes)
  const script = one(scripts, `${path}: Nuxt hydration payload`)
  try {
    const identity = (value) => value
    const revivers = Object.fromEntries(['NuxtError', 'EmptyShallowRef', 'EmptyRef', 'ShallowRef', 'ShallowReactive', 'Ref', 'Reactive', 'Island'].map((type) => [type, identity]))
    return { payload: parseDevalue(script.body, revivers), bytes: Buffer.byteLength(script.body) }
  } catch (error) {
    throw new Error(`${path}: Nuxt hydration payload is invalid: ${error.message}`)
  }
}

function filmPayloadState(html, slug, path) {
  const result = nuxtPayload(html, path)
  const entries = Object.entries(result.payload?.data ?? {}).filter(([key]) => key.startsWith(`film-schedule:${slug}|`))
  const entry = one(entries, `${path}: film async-data entry`)
  return { key: entry[0], state: entry[1], payloadBytes: result.bytes }
}

function graphNodes(html) {
  return jsonLdDocuments(html).flatMap((document) => Array.isArray(document['@graph']) ? document['@graph'] : [document])
}

function nodesOfType(nodes, type) {
  return nodes.filter((node) => node?.['@type'] === type)
}

function verifyItemList(html, id, expectedUrls, path) {
  assert(expectedUrls.length > 0, `${path}: ItemList expectation must be nonempty`)
  const lists = nodesOfType(graphNodes(html), 'ItemList')
  const list = lists.find((node) => node['@id'] === id)
  assert(list, `${path}: ItemList ${id} missing`)
  const items = list.itemListElement
  assert(Array.isArray(items) && items.length === expectedUrls.length, `${path}: ItemList ${id} length mismatch`)
  for (const [index, item] of items.entries()) {
    assert(JSON.stringify(Object.keys(item).sort()) === JSON.stringify(['@type', 'position', 'url'].sort()), `${path}: ItemList ${id} item ${index + 1} keys mismatch`)
    assert(item['@type'] === 'ListItem' && item.position === index + 1 && item.url === expectedUrls[index], `${path}: ItemList ${id} item ${index + 1} mismatch`)
  }
  return lists
}

function assertNoItemList(html, path) {
  assert(nodesOfType(graphNodes(html), 'ItemList').length === 0, `${path}: unexpected ItemList`)
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
      || ((hostname === 'pathe.fr' || hostname.endsWith('.pathe.fr')) && parsed.pathname !== '/' && !parsed.pathname.includes('%'))
      || ((hostname === 'acsta.net' || hostname.endsWith('.acsta.net')) && parsed.pathname !== '/' && !parsed.pathname.includes('%'))
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
    const hostname = parsed.hostname.toLowerCase()
    const hostProvider = hostname === 'www.ugc.fr' ? 'ugc' : hostname === 'kinepolis.fr' ? 'kinepolis' : hostname === 's.pathe.fr' ? 'pathe' : hostname === 'achat.cgrcinemas.fr' ? 'cgr' : null
    const isSafePatheBooking = hostProvider !== 'pathe' || (!parsed.search && !parsed.hash && parsed.href === value && /^\/fr\/[A-Za-z0-9_-]*S[1-9][0-9]*\/booking$/.test(parsed.pathname))
    const isSafeCgrBooking = hostProvider !== 'cgr' || (
      value.length <= 2048
      && /^https:\/\/achat\.cgrcinemas\.fr\/[a-z0-9-]+\/r\/[1-9][0-9]*$/.test(value)
    )
    if (parsed.protocol !== 'https:' || !hostProvider || (showtime.provider && showtime.provider !== hostProvider) || parsed.username || parsed.password || parsed.port || !isSafePatheBooking || !isSafeCgrBooking) return null
    return parsed.href
  } catch {
    return null
  }
}

async function get(url, accept = 'text/html,application/json', headers = {}) {
  const response = await fetch(url, { headers: { accept, ...headers }, redirect: 'manual' })
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

function sitemapIndexLocations(xml) {
  return [...xml.matchAll(/<sitemap>\s*<loc>(.*?)<\/loc>\s*<\/sitemap>/gs)].map((match) => decodeHtml(match[1]))
}

function hasSubstantialEvergreenMovieMetadata(movie) {
  const nonblank = (value) => value?.constructor === String && value.trim().length > 0
  const hasIdentity = nonblank(movie.poster_url)
    || (Number.isSafeInteger(movie.tmdb_id) && movie.tmdb_id > 0)
    || nonblank(movie.imdb_id)
  return nonblank(movie.overview)
    && calendarDate(String(movie.release_date ?? '').trim()) !== null
    && Array.isArray(movie.genres)
    && movie.genres.some(nonblank)
    && hasIdentity
}

function laterTimestamp(...values) {
  assert(values.length > 0 && values.every((value) => value?.constructor === String && Number.isFinite(Date.parse(value))), 'lastmod source timestamp is invalid')
  return values.reduce((latest, value) => Date.parse(value) > Date.parse(latest) ? value : latest)
}

async function fetchCurrentLandingCatalog(pageSize) {
  const result = await get(`${apiUrl}/api/v1/movies?currently_screened=true&sort=showtimes_desc&page=1&page_size=${pageSize}`)
  assert(result.response.status === 200, `API landing catalog page_size=${pageSize}: expected 200, received ${result.response.status}`)
  const payload = JSON.parse(result.body)
  assert(payload.page === 1 && payload.page_size === pageSize && Array.isArray(payload.items), `API landing catalog page_size=${pageSize}: malformed page`)
  assert(Number.isFinite(Date.parse(payload.generated_at)) && String(payload.catalog_revision ?? '').trim(), `API landing catalog page_size=${pageSize}: malformed snapshot`)
  assert(payload.items.every((movie) => Number.isFinite(Date.parse(movie.updated_at))), `API landing catalog page_size=${pageSize}: invalid movie updated_at`)
  return payload
}

async function verifyDiscovery(allCatalog, inventory) {
  const [homepageCatalog, filmsCatalog] = await Promise.all([fetchCurrentLandingCatalog(6), fetchCurrentLandingCatalog(24)])
  assert(homepageCatalog.generated_at === allCatalog.generatedAt && filmsCatalog.generated_at === allCatalog.generatedAt, 'Landing and sitemap catalog generations differ')
  assert(homepageCatalog.catalog_revision === allCatalog.catalogRevision && filmsCatalog.catalog_revision === allCatalog.catalogRevision, 'Landing and sitemap catalog revisions differ')

  const index = await get(`${webUrl}/sitemap.xml`, 'application/xml')
  assert(index.response.status === 200, `/sitemap.xml: expected 200, received ${index.response.status}`)
  assert(index.response.headers.get('content-type')?.toLowerCase() === 'application/xml; charset=utf-8', '/sitemap.xml: unexpected content type')
  assert(index.response.headers.get('cache-control') === 'max-age=300', '/sitemap.xml: expected Cache-Control max-age=300')
  assert(index.body.endsWith('\n') && index.body.includes('<sitemapindex'), '/sitemap.xml: invalid sitemap index')
  const expectedChildren = ['/sitemaps/films.xml', '/sitemaps/cinemas.xml', '/sitemaps/cities.xml'].map((path) => `${siteUrl}${path}`)
  assert(JSON.stringify(sitemapIndexLocations(index.body)) === JSON.stringify(expectedChildren), '/sitemap.xml: child sitemap order or set mismatch')
  assert(!index.body.includes('<lastmod>'), '/sitemap.xml: static child references must omit lastmod')

  const childResults = await Promise.all(expectedChildren.map((location) => get(location.replace(siteUrl, webUrl), 'application/xml')))
  for (const [childIndex, child] of childResults.entries()) {
    const path = new URL(expectedChildren[childIndex]).pathname
    assert(child.response.status === 200, `${path}: expected 200, received ${child.response.status}`)
    assert(child.response.headers.get('content-type')?.toLowerCase() === 'application/xml; charset=utf-8', `${path}: unexpected content type`)
    assert(child.response.headers.get('cache-control') === 's-maxage=300, stale-while-revalidate', `${path}: unexpected SWR Cache-Control policy`)
    assert(child.body.endsWith('\n') && child.body.includes('<urlset'), `${path}: invalid URL set`)
  }
  const [filmEntries, cinemaEntries, cityEntries] = childResults.map((child) => sitemapEntries(child.body))
  const expectedFilmEntries = [
    { loc: `${siteUrl}/`, lastmod: laterTimestamp(allCatalog.generatedAt, ...homepageCatalog.items.map((movie) => movie.updated_at)) },
    { loc: `${siteUrl}/films`, lastmod: laterTimestamp(allCatalog.generatedAt, ...allCatalog.items.filter((movie) => (movie.showtime_count ?? 0) > 0).map((movie) => movie.updated_at)) },
    ...allCatalog.items
      .filter((movie) => (movie.showtime_count ?? 0) > 0 || hasSubstantialEvergreenMovieMetadata(movie))
      .map((movie) => ({
        loc: `${siteUrl}/film/${encodeURIComponent(movie.slug)}`,
        lastmod: (movie.showtime_count ?? 0) > 0 ? laterTimestamp(movie.updated_at, allCatalog.generatedAt) : movie.updated_at
      }))
      .sort((left, right) => left.loc.localeCompare(right.loc))
  ]
  const expectedCinemaEntries = [
    { loc: `${siteUrl}/cinemas`, lastmod: inventory.generated_at },
    ...[...inventory.theaterSlugs].sort().map((slug) => ({ loc: `${siteUrl}/cinema/${encodeURIComponent(slug)}`, lastmod: inventory.generated_at }))
  ]
  const expectedCityEntries = [...inventory.citySlugs].sort().map((slug) => ({ loc: `${siteUrl}/ville/${encodeURIComponent(slug)}/cinemas`, lastmod: inventory.generated_at }))
  assert(JSON.stringify(filmEntries) === JSON.stringify(expectedFilmEntries), '/sitemaps/films.xml: URL inventory or lastmod mismatch')
  assert(JSON.stringify(cinemaEntries) === JSON.stringify(expectedCinemaEntries), '/sitemaps/cinemas.xml: URL inventory or lastmod mismatch')
  assert(JSON.stringify(cityEntries) === JSON.stringify(expectedCityEntries), '/sitemaps/cities.xml: URL inventory or lastmod mismatch')
  const union = [...filmEntries, ...cinemaEntries, ...cityEntries]
  assert(new Set(union.map((entry) => entry.loc)).size === union.length, 'Child sitemaps contain duplicate URLs')
  assert(union.every((entry) => entry.lastmod !== null), 'Child sitemap URL lacks lastmod')
  assert(!union.some((entry) => ['/planning', '/recherche'].includes(new URL(entry.loc).pathname)), 'Utility route present in child sitemap union')

  const robots = await get(`${webUrl}/robots.txt`, 'text/plain')
  assert(robots.response.status === 200, `/robots.txt: expected 200, received ${robots.response.status}`)
  assert(robots.response.headers.get('content-type')?.toLowerCase() === 'text/plain; charset=utf-8', '/robots.txt: unexpected content type')
  assert(robots.body === `User-agent: *\nAllow: /\nSitemap: ${siteUrl}/sitemap.xml\n`, '/robots.txt: body mismatch')
  assert(!robots.body.toLowerCase().includes('disallow'), '/robots.txt: noindex routes must not be disallowed')
  return { filmLocations: new Set(filmEntries.map((entry) => new URL(entry.loc).pathname)) }
}

async function verifyIndexMatrix(catalog, movie, city, theater, defaultDate) {
  const encodedSlug = encodeURIComponent(movie.slug)
  const queryless = [
    ['/', `${siteUrl}/`],
    ['/films', `${siteUrl}/films`],
    ['/cinemas', `${siteUrl}/cinemas`],
    [`/film/${encodedSlug}`, `${siteUrl}/film/${encodedSlug}`],
    [`/ville/${encodeURIComponent(city.slug)}/cinemas`, `${siteUrl}/ville/${encodeURIComponent(city.slug)}/cinemas`],
    [`/cinema/${encodeURIComponent(theater.slug)}`, `${siteUrl}/cinema/${encodeURIComponent(theater.slug)}`]
  ]
  for (const [path, expectedCanonical] of queryless) {
    verifyPolicy(await get(`${webUrl}${path}`), path, 'index,follow', expectedCanonical)
  }
  for (const path of ['/planning', '/recherche']) {
    verifyPolicy(await get(`${webUrl}${path}`), path, 'noindex,follow', `${siteUrl}${path}`)
  }

  const noindexCanonicalCases = [
    ['/?campaign=1', `${siteUrl}/`],
    ['/films?q=test', `${siteUrl}/films`],
    ['/films?sort=title_asc', `${siteUrl}/films`],
    ['/films?genres=Drame&duration=medium&date=today', `${siteUrl}/films`],
    ['/films?all_theaters=1', `${siteUrl}/films`],
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
    const result = await get(`${webUrl}${path}`)
    verifyPolicy(result, path, 'noindex,follow', expectedCanonical)
    assertNoItemList(result.body, path)
  }

  const cityQueryPath = `/ville/${encodeURIComponent(city.slug)}/cinemas?foreign=1`
  const cityQueryPage = await get(`${webUrl}${cityQueryPath}`)
  verifyBreadcrumb(cityQueryPage.body, {
    path: cityQueryPath,
    id: `${siteUrl}/ville/${encodeURIComponent(city.slug)}/cinemas#breadcrumb`,
    names: ['Accueil', 'Cinémas', city.name],
    urls: [`${siteUrl}/`, `${siteUrl}/cinemas`, `${siteUrl}/ville/${encodeURIComponent(city.slug)}/cinemas`]
  })
  const cinemaQueryPath = `/cinema/${encodeURIComponent(theater.slug)}?foreign=1`
  const cinemaQueryPage = await get(`${webUrl}${cinemaQueryPath}`)
  verifyBreadcrumb(cinemaQueryPage.body, {
    path: cinemaQueryPath,
    id: `${siteUrl}/cinema/${encodeURIComponent(theater.slug)}#breadcrumb`,
    names: ['Accueil', theater.cityName, theater.name],
    urls: [`${siteUrl}/`, `${siteUrl}/ville/${encodeURIComponent(theater.citySlug)}/cinemas`, `${siteUrl}/cinema/${encodeURIComponent(theater.slug)}`]
  })

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
    const filmUrls = hrefs
      .map((href) => new URL(href, webUrl))
      .filter((target) => target.pathname.startsWith('/film/'))
      .map((target) => `${siteUrl}${target.pathname}`)
    const canonicalUrl = page === 1 ? `${siteUrl}/films` : `${siteUrl}/films?page=${page}`
    const lists = verifyItemList(result.body, `${canonicalUrl}#film-list`, filmUrls, path)
    assert(lists.length === 1, `${path}: unexpected additional ItemList`)
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
  const cinemaUrls = tags(result.body, 'a')
    .map((anchor) => new URL(anchor.href, webUrl))
    .filter((target) => target.pathname.startsWith('/cinema/'))
    .map((target) => `${siteUrl}${target.pathname}`)
  const lists = verifyItemList(result.body, `${siteUrl}/cinemas#cinema-list`, cinemaUrls, path)
  assert(lists.length === 1, `${path}: unexpected additional ItemList`)
  assertStableInternalLinks(result.body, path)
}

function verifyBreadcrumb(html, { path, id, names, urls }) {
  const nav = html.match(/<nav\b[^>]*aria-label=(["'])Fil d’Ariane\1[^>]*>(.*?)<\/nav>/is)
  assert(nav, `${path}: breadcrumb nav missing`)
  const hrefs = tags(nav[2], 'a').map((anchor) => new URL(anchor.href, webUrl).href)
  assert(JSON.stringify(hrefs) === JSON.stringify(urls.slice(0, -1)), `${path}: visible breadcrumb links mismatch`)
  assert(/aria-current=(["'])page\1/i.test(nav[2]), `${path}: breadcrumb current item missing aria-current`)
  const text = visibleText(`<body>${nav[2]}</body>`)
  assert(names.every((name) => text.includes(name)), `${path}: visible breadcrumb labels mismatch`)
  const breadcrumbs = nodesOfType(graphNodes(html), 'BreadcrumbList')
  assert(breadcrumbs.length === 1, `${path}: expected exactly one BreadcrumbList`)
  const breadcrumb = breadcrumbs[0]
  assert(breadcrumb['@id'] === id, `${path}: BreadcrumbList ID mismatch`)
  assert(JSON.stringify(breadcrumb.itemListElement?.map(({ name, item }) => ({ name, item }))) === JSON.stringify(names.map((name, index) => ({ name, item: urls[index] }))), `${path}: BreadcrumbList labels or URLs mismatch`)
  assert(breadcrumb.itemListElement?.every((item, index) => item['@type'] === 'ListItem' && item.position === index + 1), `${path}: BreadcrumbList positions mismatch`)
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

function filmEventFacts(event) {
  return JSON.stringify({
    startDate: event.startDate,
    endDate: event.endDate,
    location: event.location,
    workPresented: event.workPresented
  })
}

function verifyCompactFilmEvents(actual, schedule, movieId, path) {
  const expected = schedule.theaters.flatMap((theater) => {
    const theaterId = `${siteUrl}/cinema/${encodeURIComponent(theater.slug)}#cinema`
    return theater.showtimes.map((showtime) => ({
      startDate: showtime.start_time,
      endDate: showtime.end_time,
      location: { '@id': theaterId },
      workPresented: { '@id': movieId }
    }))
  })
  assert(actual.length === expected.length, `${path}: expected all ${expected.length} nationwide ScreeningEvent nodes, received ${actual.length}`)
  for (const [index, event] of actual.entries()) {
    assert(JSON.stringify(Object.keys(event).sort()) === JSON.stringify(['@type', 'startDate', 'endDate', 'location', 'workPresented'].sort()), `${path}: compact ScreeningEvent ${index + 1} keys mismatch`)
    assert(event['@type'] === 'ScreeningEvent', `${path}: compact ScreeningEvent ${index + 1} type mismatch`)
    assert(!('@id' in event) && !('name' in event), `${path}: compact ScreeningEvent ${index + 1} repeats identity or name`)
  }
  const actualFacts = actual.map(filmEventFacts).sort()
  const expectedFacts = expected.map(filmEventFacts).sort()
  assert(JSON.stringify(actualFacts) === JSON.stringify(expectedFacts), `${path}: nationwide ScreeningEvent multiset mismatch`)
}

function verifyFilmJsonLd(html, movie, schedule, path) {
  const nodes = verifyGlobalGraph(html)
  const canonicalUrl = `${siteUrl}/film/${encodeURIComponent(movie.slug)}`
  const movieId = `${canonicalUrl}#movie`
  const movieNodes = nodesOfType(nodes, 'Movie')
  assert(movieNodes.length === 1, `${path}: expected exactly one Movie node`)
  const movieNode = movieNodes.find((node) => node['@id'] === movieId)
  assert(movieNode?.name === movie.title && movieNode.url === canonicalUrl, `${path}: Movie identity mismatch`)
  assert(movieNode.duration === `PT${movie.runtime_minutes}M`, `${path}: Movie duration mismatch`)
  assert(movieNode.image !== `${siteUrl}/pwa-512x512.png`, `${path}: app icon used as Movie image`)
  const breadcrumb = nodes.find((node) => node['@type'] === 'BreadcrumbList' && node['@id'] === `${canonicalUrl}#breadcrumb`)
  assert(breadcrumb && breadcrumb.itemListElement?.length === 3, `${path}: BreadcrumbList missing`)
  assert(JSON.stringify(breadcrumb.itemListElement.map((item) => item.item)) === JSON.stringify([`${siteUrl}/`, `${siteUrl}/films`, canonicalUrl]), `${path}: BreadcrumbList links mismatch`)
  verifyBreadcrumb(html, { path, id: `${canonicalUrl}#breadcrumb`, names: ['Accueil', 'Films', movie.title], urls: [`${siteUrl}/`, `${siteUrl}/films`, canonicalUrl] })

  verifyCompactFilmEvents(nodesOfType(nodes, 'ScreeningEvent'), schedule, movieId, path)
  const theaters = nodesOfType(nodes, 'MovieTheater')
  const expectedTheaters = schedule.theaters.filter((theater) => theater.showtimes.length)
  assert(theaters.length === expectedTheaters.length, `${path}: MovieTheater count mismatch`)
  assert(new Set(theaters.map((theater) => theater['@id'])).size === theaters.length, `${path}: duplicate MovieTheater identity`)
  for (const expectedTheater of expectedTheaters) {
    const theaterUrl = `${siteUrl}/cinema/${encodeURIComponent(expectedTheater.slug)}`
    const theater = theaters.find((node) => node['@id'] === `${theaterUrl}#cinema`)
    assert(JSON.stringify(theater) === JSON.stringify({ '@type': 'MovieTheater', '@id': `${theaterUrl}#cinema`, name: expectedTheater.name, url: theaterUrl }), `${path}: MovieTheater ${expectedTheater.slug} mismatch`)
  }
  assertNoItemList(html, path)
  assertStableInternalLinks(html, path)

  const scripts = jsonLdScripts(html).filter((script) => {
    try {
      return JSON.stringify(JSON.parse(decodeHtml(script.body))).includes(movieId)
    } catch {
      return false
    }
  })
  const script = one(scripts, `${path}: inline film JSON-LD`)
  return { jsonLdBytes: Buffer.byteLength(script.body), eventCount: nodesOfType(nodes, 'ScreeningEvent').length, theaterCount: theaters.length }
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
  verifyBreadcrumb(html, {
    path,
    id: `${canonicalUrl}#breadcrumb`,
    names: ['Accueil', response.theater.city, response.theater.name],
    urls: [`${siteUrl}/`, `${siteUrl}/ville/${encodeURIComponent(response.theater.city_slug)}/cinemas`, canonicalUrl]
  })
  assertNoItemList(html, path)
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

async function verifyEndedFilm(allCatalog, currentCatalog, today, filmLocations) {
  const currentSlugs = new Set(currentCatalog.slugs)
  const endedCandidates = allCatalog.items.filter((item) => !currentSlugs.has(item.slug))
  const representatives = [
    endedCandidates.find(hasSubstantialEvergreenMovieMetadata),
    endedCandidates.find((movie) => !hasSubstantialEvergreenMovieMetadata(movie))
  ].filter(Boolean)
  if (representatives.length === 0) {
    console.log('Unconfirmed coverage: no ended canonical film fixture was available.')
    return false
  }

  for (const movie of representatives) {
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
    assert(schedule.currently_screened === false && schedule.available_dates.length === 0 && schedule.theaters.length === 0 && showtimeCount === 0, `Ended-film candidate API ${movie.slug}: candidate is not ended`)

    const path = `/film/${encodedSlug}`
    const indexable = hasSubstantialEvergreenMovieMetadata(movie)
    const page = await get(`${webUrl}${path}`)
    verifyPolicy(page, path, indexable ? 'index,follow' : 'noindex,follow', `${siteUrl}${path}`)
    assert(filmLocations.has(path) === indexable, `${path}: page robots and film sitemap qualification diverge`)
    assert(visibleText(page.body).includes('Aucune séance programmée pour le moment.'), `${path}: ended-film state missing`)
    verifyFilmJsonLd(page.body, schedule.movie, schedule, path)
    assert(nodesOfType(graphNodes(page.body), 'ScreeningEvent').length === 0, `${path}: ended film emitted ScreeningEvent JSON-LD`)
    console.log(`Ended-film fixture: ${movie.slug}; indexable=${indexable}; sitemap parity and no-screenings state verified.`)
  }
  if (!representatives.some(hasSubstantialEvergreenMovieMetadata)) console.log('Unconfirmed coverage: no rich ended-film fixture was available.')
  if (!representatives.some((movie) => !hasSubstantialEvergreenMovieMetadata(movie))) console.log('Unconfirmed coverage: no thin ended-film fixture was available.')
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

function calendarDate(value) {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(String(value ?? ''))
  if (!match) return null
  const year = Number(match[1])
  const month = Number(match[2])
  const day = Number(match[3])
  const date = new Date(Date.UTC(year, month - 1, day, 12))
  return date.getUTCFullYear() === year && date.getUTCMonth() === month - 1 && date.getUTCDate() === day ? match[0] : null
}

function nonPastAvailableDates(response, today) {
  return [...new Set(response.available_dates)]
    .filter((date) => calendarDate(date) === date && date >= today)
    .sort()
}

function resolvedAvailableDate(dates, requestedDate, today) {
  if (dates.includes(requestedDate)) return requestedDate
  return dates.includes(today) ? today : dates[0] ?? today
}

function filmShowtimeCount(schedule) {
  return schedule.theaters.reduce((count, theater) => count + theater.showtimes.length, 0)
}

async function fetchFilmSchedule(slug, date, city) {
  const query = new URLSearchParams({ date })
  if (city) query.set('city', city)
  const result = await get(`${apiUrl}/api/v1/movies/${encodeURIComponent(slug)}/showtimes?${query}`)
  assert(result.response.status === 200, `API film fixture ${slug} ${date}${city ? ` ${city}` : ' nationwide'}: expected 200, received ${result.response.status}`)
  const schedule = JSON.parse(result.body)
  assert(schedule.movie?.slug === slug && Array.isArray(schedule.available_dates) && Array.isArray(schedule.theaters), `API film fixture ${slug}: malformed schedule`)
  assert(schedule.theaters.every((theater) => Array.isArray(theater.showtimes)), `API film fixture ${slug}: malformed theater showtimes`)
  return schedule
}

async function resolveFilmFixture(movie, today) {
  let paris = await fetchFilmSchedule(movie.slug, today, 'Paris')
  const dates = nonPastAvailableDates(paris, today)
  const resolvedDate = resolvedAvailableDate(dates, today, today)
  if (!dates.includes(today) && dates.length > 0) paris = await fetchFilmSchedule(movie.slug, resolvedDate, 'Paris')
  const nationwide = await fetchFilmSchedule(movie.slug, resolvedDate)
  assert(paris.date === resolvedDate, `Film fixture ${movie.slug}: Paris response date ${paris.date} differs from resolved ${resolvedDate}`)
  assert(nationwide.date === resolvedDate, `Film fixture ${movie.slug}: nationwide response date ${nationwide.date} differs from Paris-resolved ${resolvedDate}`)
  return { movie: paris.movie, requestedDate: today, resolvedDate, paris, nationwide }
}

function nationwideOnlyMarkers(fixture) {
  const parisTheaterIds = new Set(fixture.paris.theaters.map((theater) => theater.id))
  const parisTheaterSlugs = new Set(fixture.paris.theaters.map((theater) => theater.slug))
  const parisShowtimeIds = new Set(fixture.paris.theaters.flatMap((theater) => theater.showtimes.map((showtime) => showtime.id)))
  const markers = []
  for (const theater of fixture.nationwide.theaters) {
    if (theater.showtimes.length && !parisTheaterIds.has(theater.id)) markers.push({ type: 'theater ID', value: theater.id })
    if (theater.showtimes.length && !parisTheaterSlugs.has(theater.slug)) markers.push({ type: 'theater slug', value: theater.slug })
    for (const showtime of theater.showtimes) {
      if (!parisShowtimeIds.has(showtime.id)) markers.push({ type: 'showtime ID', value: showtime.id })
    }
  }
  return [...new Map(markers.filter((marker) => String(marker.value).length >= 4).map((marker) => [`${marker.type}:${marker.value}`, marker])).values()]
}

async function discoverFilmFixture(catalog, preferredMovie, today) {
  const candidates = [preferredMovie, ...catalog.items.filter((movie) => movie.slug !== preferredMovie.slug)]
  let best = null
  for (const candidate of candidates) {
    const fixture = await resolveFilmFixture(candidate, today)
    const parisEvents = filmShowtimeCount(fixture.paris)
    const nationwideEvents = filmShowtimeCount(fixture.nationwide)
    const markers = nationwideOnlyMarkers(fixture)
    const score = Number(parisEvents > 0) * 1000 + Number(nationwideEvents > 0) * 100 + Number(markers.length > 0) * 10 + Math.min(parisEvents, 9)
    if (!best || score > best.score) best = { ...fixture, markers, score }
    if (parisEvents > 0 && nationwideEvents > 0 && markers.length > 0) return { ...fixture, markers }
  }
  assert(best, 'No film fixture could be resolved')
  return best
}

function openingTags(html) {
  return [...html.matchAll(/<[a-z][^>]*>/gi)].map((match) => attributes(match[0]))
}

function classCount(html, className) {
  return openingTags(html).filter((item) => String(item.class ?? '').split(/\s+/).includes(className)).length
}

function verifyFilmRuntimeIsolation(page, fixture, path) {
  assert(page.response.status === 200, `${path}: expected 200, received ${page.response.status}`)
  assert(fixture.paris.date === fixture.resolvedDate && fixture.nationwide.date === fixture.resolvedDate, `${path}: Paris/nationwide date alignment mismatch`)
  const { state, key, payloadBytes } = filmPayloadState(page.body, fixture.movie.slug, path)
  assert(JSON.stringify(Object.keys(state).sort()) === JSON.stringify(['kind', 'schedule', 'selectedDate', 'errorMessage'].sort()), `${path}: film async-data state keys mismatch`)
  assert(state.kind === 'success' && state.errorMessage === '', `${path}: film async-data success state mismatch`)
  assert(state.selectedDate === fixture.resolvedDate, `${path}: hydration selected date ${state.selectedDate} differs from Paris-resolved ${fixture.resolvedDate}`)
  assert(state.schedule?.date === fixture.resolvedDate, `${path}: hydrated Paris schedule date mismatch`)
  assert(JSON.stringify(state.schedule) === JSON.stringify(fixture.paris), `${path}: hydration schedule is not exact Paris-scoped response`)

  const expectedTheaters = fixture.paris.theaters.filter((theater) => theater.showtimes.length).length
  const expectedShowtimes = filmShowtimeCount(fixture.paris)
  assert(classCount(page.body, 'theater-section') === expectedTheaters, `${path}: SSR theater section count is not Paris scoped`)
  assert(classCount(page.body, 'showtime-card') === expectedShowtimes, `${path}: SSR showtime card count is not Paris scoped`)

  const stripped = decodeHtml(withoutJsonLd(page.body))
  const serializedState = JSON.stringify(state)
  if (fixture.markers.length === 0) {
    console.log(`Unconfirmed coverage: ${path} has no nationwide-only theater/showtime marker outside Paris scope.`)
  } else {
    for (const marker of fixture.markers) {
      assert(!stripped.includes(marker.value), `${path}: nationwide-only ${marker.type} leaked outside JSON-LD: ${marker.value}`)
      assert(!serializedState.includes(marker.value), `${path}: nationwide-only ${marker.type} leaked into Nuxt hydration: ${marker.value}`)
    }
  }
  return { payloadBytes, payloadKey: key, parisTheaters: expectedTheaters, parisEvents: expectedShowtimes, nationwideMarkers: fixture.markers.length }
}

async function verifyFailureMode() {
  for (const path of ['/', '/films', '/cinemas', '/film/upstream-check', '/cinema/upstream-check', '/ville/upstream-check/cinemas']) {
    const result = await get(`${webUrl}${path}`)
    verifyErrorPolicy(result, path, 502)
    const text = visibleText(result.body)
    assert(text.includes('Impossible de joindre le service'), `${path}: recoverable French upstream error is missing`)
    assert(!text.includes('Film introuvable'), `${path}: upstream failure was rendered as film not found`)
    verifyGlobalGraph(result.body)
    assert(nodesOfType(graphNodes(result.body), 'BreadcrumbList').length === 0, `${path}: upstream failure emitted BreadcrumbList`)
    assertNoItemList(result.body, path)
  }
  const index = await get(`${webUrl}/sitemap.xml`, 'application/xml')
  assert(index.response.status === 200 && index.body.includes('<sitemapindex'), `/sitemap.xml: static index must remain available during upstream failure`)
  for (const path of ['/sitemaps/films.xml', '/sitemaps/cinemas.xml', '/sitemaps/cities.xml']) {
    const child = await get(`${webUrl}${path}`, 'application/xml')
    assert(child.response.status === 503, `${path}: expected 503, received ${child.response.status}`)
    assert(!child.body.includes('<urlset'), `${path}: partial URL set returned on upstream failure`)
    assert(child.response.headers.get('x-robots-tag') === 'noindex,follow', `${path}: missing error X-Robots-Tag`)
  }
  console.log('Crawlability upstream-failure checks passed (6 rendered 502 routes, static sitemap index 200, and three non-partial child sitemap 503 responses).')
}

function listen(server, port = 0) {
  return new Promise((resolve, reject) => {
    const onError = (error) => {
      server.off('listening', onListening)
      reject(error)
    }
    const onListening = () => {
      server.off('error', onError)
      resolve(server.address())
    }
    server.once('error', onError)
    server.once('listening', onListening)
    server.listen(port, '127.0.0.1')
  })
}

function closeServer(server) {
  return new Promise((resolve, reject) => server.close((error) => error ? reject(error) : resolve()))
}

async function availablePort() {
  const server = createServer()
  const address = await listen(server)
  await closeServer(server)
  return address.port
}

function fixtureMovieSchedule(date) {
  const movie = {
    slug: 'film-990001',
    title: 'Film fixture Paris SEO',
    runtime_minutes: 101,
    updated_at: `${date}T00:00:00Z`,
    poster_url: null,
    tmdb_id: null,
    imdb_id: null,
    overview: 'Fixture de vérification SSR.',
    release_date: date,
    genres: ['Drame']
  }
  const showtime = {
    provider: 'ugc',
    id: 'paris-showtime-fixture-990001',
    movie: { slug: movie.slug, title: movie.title, runtime_minutes: movie.runtime_minutes, updated_at: movie.updated_at },
    start_time: `${date}T18:00:00+02:00`,
    end_time: `${date}T19:41:00+02:00`,
    language: 'VOSTFR',
    format: '2D',
    room: 'Salle fixture',
    booking_url: null
  }
  return {
    movie,
    backdrop_url: null,
    date,
    currently_screened: true,
    available_dates: [date],
    theaters: [{
      provider: 'ugc',
      id: 'paris-theater-fixture-990001',
      slug: 'paris-theater-fixture-990001',
      name: 'Cinéma fixture Paris SEO',
      city: 'Paris',
      city_slug: 'paris',
      showtimes: [showtime]
    }]
  }
}

function addCalendarDays(date, days) {
  const [year, month, day] = date.split('-').map(Number)
  const value = new Date(Date.UTC(year, month - 1, day + days, 12))
  return `${value.getUTCFullYear()}-${String(value.getUTCMonth() + 1).padStart(2, '0')}-${String(value.getUTCDate()).padStart(2, '0')}`
}

function successfulFixtureSchedules(requestedDate) {
  const resolvedDate = addCalendarDays(requestedDate, 1)
  const movie = {
    slug: 'film-990002',
    title: 'Film fixture SSR national',
    runtime_minutes: 112,
    updated_at: `${requestedDate}T00:00:00Z`,
    poster_url: 'https://image.tmdb.org/t/p/w500/fixture-990002.jpg',
    tmdb_id: 990002,
    imdb_id: null,
    overview: 'Fixture positive de vérification SSR.',
    release_date: requestedDate,
    genres: ['Drame', 'Comédie']
  }
  const showtimeMovie = {
    slug: movie.slug,
    title: movie.title,
    runtime_minutes: movie.runtime_minutes,
    updated_at: movie.updated_at
  }
  const showtime = (id, start, end, provider = 'ugc') => ({
    provider,
    id,
    movie: showtimeMovie,
    start_time: `${resolvedDate}T${start}+02:00`,
    end_time: `${resolvedDate}T${end}+02:00`,
    language: 'VOSTFR',
    format: '2D',
    room: 'Fixture',
    booking_url: null
  })
  const parisTheater = {
    provider: 'ugc',
    id: 'paris-theater-success-990002',
    slug: 'paris-theater-success-990002',
    name: 'Cinéma fixture Paris positif',
    city: 'Paris',
    city_slug: 'paris',
    showtimes: [
      showtime('shared-success-showtime-990002', '18:00:00', '19:52:00'),
      showtime('paris-success-showtime-990002', '20:15:00', '22:07:00')
    ]
  }
  const lyonTheater = {
    provider: 'pathe',
    id: 'lyon-theater-success-990002',
    slug: 'lyon-theater-success-990002',
    name: 'Cinéma fixture Lyon national',
    city: 'Lyon',
    city_slug: 'lyon',
    showtimes: [
      showtime('shared-success-showtime-990002', '17:30:00', '19:22:00', 'pathe'),
      showtime('lyon-only-showtime-990002', '21:37:00', '23:29:00', 'pathe')
    ]
  }
  const base = {
    movie,
    backdrop_url: 'https://image.tmdb.org/t/p/w780/fixture-990002.jpg',
    currently_screened: true,
    available_dates: [resolvedDate]
  }
  const initialParis = { ...base, date: requestedDate, theaters: [] }
  const initialNationwide = {
    ...base,
    date: requestedDate,
    theaters: [{
      provider: 'cgr',
      id: 'discarded-theater-success-990002',
      slug: 'discarded-theater-success-990002',
      name: 'Cinéma fixture national date initiale',
      city: 'Bordeaux',
      city_slug: 'bordeaux',
      showtimes: [{ ...showtime('discarded-showtime-success-990002', '16:00:00', '17:52:00', 'cgr'), start_time: `${requestedDate}T16:00:00+02:00`, end_time: `${requestedDate}T17:52:00+02:00` }]
    }]
  }
  const paris = { ...base, date: resolvedDate, theaters: [parisTheater] }
  const nationwide = { ...base, date: resolvedDate, theaters: [structuredClone(parisTheater), lyonTheater] }
  return {
    requestedDate,
    resolvedDate,
    movie,
    initialParis,
    initialNationwide,
    paris,
    nationwide,
    discardedMarkers: ['discarded-theater-success-990002', 'discarded-showtime-success-990002'],
    nationwideEventMarkers: [lyonTheater.showtimes[0].start_time, lyonTheater.showtimes[1].start_time]
  }
}

function sitemapFixtureData(date) {
  const generatedAt = `${date}T10:00:00Z`
  const catalogRevision = 'sitemap-fixture-revision-1'
  const baseMovie = {
    runtime_minutes: 100,
    poster_url: null,
    tmdb_id: null,
    imdb_id: null,
    overview: null,
    release_date: null,
    genres: []
  }
  const current = {
    ...baseMovie,
    slug: 'film-991001',
    title: 'Film fixture actuel',
    updated_at: `${date}T12:00:00Z`,
    showtime_count: 2
  }
  const pageOneFillers = Array.from({ length: 23 }, (_, index) => ({
    ...baseMovie,
    slug: `film-${992001 + index}`,
    title: `Film fixture actuel page un ${index + 1}`,
    updated_at: `${date}T09:00:00Z`,
    genres: ['Drame'],
    showtime_count: 1
  }))
  const offPageCurrent = {
    ...baseMovie,
    slug: 'film-992024',
    title: 'Film fixture actuel hors page',
    updated_at: `${date}T14:00:00Z`,
    genres: ['Genre hors première page'],
    showtime_count: 1
  }
  const currentMovies = [current, ...pageOneFillers, offPageCurrent]
  const richEnded = {
    ...baseMovie,
    slug: 'film-991002',
    title: 'Film fixture terminé riche',
    updated_at: `${date}T08:00:00Z`,
    poster_url: 'https://image.tmdb.org/t/p/w500/sitemap-rich-ended.jpg',
    overview: 'Film terminé avec des informations éditoriales durables.',
    release_date: date,
    genres: ['Drame']
  }
  const thinEnded = {
    ...baseMovie,
    slug: 'film-991003',
    title: 'Film fixture terminé mince',
    updated_at: `${date}T07:00:00Z`,
    overview: 'Résumé sans identité externe.',
    release_date: date,
    genres: ['Drame']
  }
  const cityInventory = {
    generated_at: generatedAt,
    items: [{
      name: 'Paris',
      slug: 'paris',
      theaters: [{ provider: 'ugc', id: 'ugc-sitemap-fixture', slug: 'ugc-sitemap-fixture', name: 'UGC Sitemap Fixture' }]
    }]
  }
  const catalogPage = (items, pageSize, total = items.length) => ({
    items,
    available_genres: ['Drame'],
    page: 1,
    page_size: pageSize,
    total,
    generated_at: generatedAt,
    catalog_revision: catalogRevision
  })
  const endedSchedule = (movie) => ({
    movie,
    backdrop_url: null,
    date,
    currently_screened: false,
    available_dates: [],
    theaters: []
  })
  return {
    generatedAt,
    catalogRevision,
    current,
    currentMovies,
    offPageCurrent,
    richEnded,
    thinEnded,
    allCatalog: catalogPage([...currentMovies, richEnded, thinEnded], API_PAGE_SIZE),
    homepageCatalog: catalogPage(currentMovies.slice(0, 6), 6, currentMovies.length),
    filmsCatalog: {
      ...catalogPage(currentMovies.slice(0, 24), 24, currentMovies.length),
      available_genres: ['Drame', 'Genre hors première page']
    },
    cityInventory,
    schedules: new Map([
      [richEnded.slug, endedSchedule(richEnded)],
      [thinEnded.slug, endedSchedule(thinEnded)]
    ])
  }
}

async function verifySitemapFixtureMode() {
  const builtServerPath = fileURLToPath(new URL('../.output/server/index.mjs', import.meta.url))
  try {
    await access(builtServerPath)
  } catch {
    throw new Error(`Sitemap fixture mode requires current Nuxt build at ${builtServerPath}; run npm --prefix web run build first`)
  }

  const fixture = sitemapFixtureData(todayInParis())
  let healthy = false
  let requestCount = 0
  const requests = []
  const mockApi = createServer((request, response) => {
    const target = new URL(request.url ?? '/', 'http://127.0.0.1')
    const requestId = String(request.headers['x-request-id'] ?? '')
    requestCount++
    requests.push({
      pathname: target.pathname,
      search: target.search,
      requestId,
      authenticated: request.headers['x-messeances-internal-token'] === FIXTURE_INTERNAL_SECRET
    })
    response.setHeader('content-type', 'application/json; charset=utf-8')
    if (request.headers['x-messeances-internal-token'] !== FIXTURE_INTERNAL_SECRET || !REQUEST_ID_PATTERN.test(requestId)) {
      response.statusCode = 401
      response.end(JSON.stringify({ error: { code: 'unauthorized' } }))
      return
    }
    if (!healthy) {
      setTimeout(() => {
        response.statusCode = 503
        response.end(JSON.stringify({ error: { code: 'fixture_unavailable' } }))
      }, 25)
      return
    }
    if (request.method !== 'GET') {
      response.statusCode = 405
      response.end(JSON.stringify({ error: { code: 'method_not_allowed' } }))
      return
    }
    if (target.pathname === '/api/v1/movies') {
      let payload
      if (target.searchParams.get('include_ended') === 'true'
        && target.searchParams.get('sort') === 'title_asc'
        && target.searchParams.get('page') === '1'
        && target.searchParams.get('page_size') === String(API_PAGE_SIZE)) payload = fixture.allCatalog
      else if (target.searchParams.get('currently_screened') === 'true'
        && target.searchParams.get('sort') === 'showtimes_desc'
        && target.searchParams.get('page') === '1'
        && target.searchParams.get('page_size') === '6') payload = fixture.homepageCatalog
      else if (target.searchParams.get('currently_screened') === 'true'
        && target.searchParams.get('sort') === 'showtimes_desc'
        && target.searchParams.get('page') === '1'
        && target.searchParams.get('page_size') === '24') payload = fixture.filmsCatalog
      if (payload) {
        response.statusCode = 200
        response.end(JSON.stringify(payload))
        return
      }
    }
    if (target.pathname === '/api/v1/cities' && !target.search) {
      response.statusCode = 200
      response.end(JSON.stringify(fixture.cityInventory))
      return
    }
    const scheduleMatch = /^\/api\/v1\/movies\/(film-99100[23])\/showtimes$/.exec(target.pathname)
    if (scheduleMatch && target.searchParams.get('date') === fixture.richEnded.release_date && !target.searchParams.has('theaters')) {
      const schedule = fixture.schedules.get(scheduleMatch[1])
      response.statusCode = 200
      response.end(JSON.stringify(schedule))
      return
    }
    const bundleMatch = /^\/api\/v1\/internal\/movies\/(film-99100[23])\/showtimes-bundle$/.exec(target.pathname)
    if (bundleMatch && target.searchParams.get('date') === fixture.richEnded.release_date && target.searchParams.get('city') === 'Paris') {
      const schedule = fixture.schedules.get(bundleMatch[1])
      response.statusCode = 200
      response.end(JSON.stringify({ scoped: schedule, nationwide: schedule }))
      return
    }
    response.statusCode = 404
    response.end(JSON.stringify({ error: { code: 'not_found' } }))
  })

  let child
  try {
    const apiAddress = await listen(mockApi)
    const privateApiOrigin = `http://127.0.0.1:${apiAddress.port}`
    const port = await availablePort()
    const fixtureWebOrigin = `http://127.0.0.1:${port}`
    let stdout = ''
    let stderr = ''
    child = spawn(process.execPath, [builtServerPath], {
      cwd: fileURLToPath(new URL('..', import.meta.url)),
      env: {
        ...process.env,
        HOST: '127.0.0.1',
        PORT: String(port),
        NITRO_HOST: '127.0.0.1',
        NITRO_PORT: String(port),
        NUXT_API_BASE: privateApiOrigin,
        NUXT_INTERNAL_API_SHARED_SECRET: FIXTURE_INTERNAL_SECRET,
        NUXT_PUBLIC_API_BASE: privateApiOrigin,
        NUXT_PUBLIC_SITE_URL: siteUrl
      },
      stdio: ['ignore', 'pipe', 'pipe']
    })
    child.stdout.on('data', (chunk) => { stdout += chunk })
    child.stderr.on('data', (chunk) => { stderr += chunk })
    const output = () => `${stdout}${stderr}`.trim().slice(-4000)
    const index = await waitForBuiltPage(`${fixtureWebOrigin}/sitemap.xml`, child, output)
    assert(index.response.status === 200, 'Sitemap fixture: static index failed while API unavailable')
    assert(index.response.headers.get('cache-control') === 'max-age=300', 'Sitemap fixture: index cache header mismatch')
    assert(JSON.stringify(sitemapIndexLocations(index.body)) === JSON.stringify([
      `${siteUrl}/sitemaps/films.xml`,
      `${siteUrl}/sitemaps/cinemas.xml`,
      `${siteUrl}/sitemaps/cities.xml`
    ]), 'Sitemap fixture: index children mismatch')
    assert(requestCount === 0, 'Sitemap fixture: static index contacted API')

    const childPaths = ['/sitemaps/films.xml', '/sitemaps/cinemas.xml', '/sitemaps/cities.xml']
    const coldRequestCounts = new Map([
      ['/sitemaps/films.xml', 3],
      ['/sitemaps/cinemas.xml', 1],
      ['/sitemaps/cities.xml', 1]
    ])
    const successfulRequestCounts = new Map([
      ['/sitemaps/films.xml', 3],
      ['/sitemaps/cinemas.xml', 1],
      ['/sitemaps/cities.xml', 1]
    ])
    for (const path of childPaths) {
      for (let attempt = 1; attempt <= 2; attempt++) {
        const before = requestCount
        const failedPair = await Promise.all([
          get(`${fixtureWebOrigin}${path}?attempt=${attempt}&variant=one`, 'application/xml'),
          get(`${fixtureWebOrigin}${path}?attempt=${attempt}&variant=two`, 'application/xml')
        ])
        assert(failedPair.every((failed) => failed.response.status === 503), `Sitemap fixture: ${path} failure pair ${attempt} did not return 503`)
        assert(failedPair.every((failed) => !failed.body.includes('<urlset')), `Sitemap fixture: ${path} failure returned partial URL set`)
        assert(requestCount - before === coldRequestCounts.get(path), `Sitemap fixture: ${path} cold failure pair ${attempt} was not exactly coalesced`)
        const responseIds = failedPair.map((failed) => failed.response.headers.get('x-request-id'))
        assert(responseIds.every((id) => id && REQUEST_ID_PATTERN.test(id)) && new Set(responseIds).size === 2, `Sitemap fixture: ${path} failure response IDs invalid`)
        const upstreamIds = new Set(requests.slice(before).map((request) => request.requestId))
        assert(upstreamIds.size === 1 && responseIds.includes([...upstreamIds][0]), `Sitemap fixture: ${path} coalesced upstream request ID not propagated`)
      }
    }

    healthy = true
    const successfulChildren = []
    for (const path of childPaths) {
      const before = requestCount
      const result = await get(`${fixtureWebOrigin}${path}?variant=seed`, 'application/xml')
      assert(result.response.status === 200, `Sitemap fixture: ${path} expected 200, received ${result.response.status}`)
      assert(result.response.headers.get('cache-control') === 's-maxage=300, stale-while-revalidate', `Sitemap fixture: ${path} cache header mismatch`)
      const responseId = result.response.headers.get('x-request-id')
      assert(responseId && REQUEST_ID_PATTERN.test(responseId), `Sitemap fixture: ${path} response request ID invalid`)
      assert(requestCount - before === successfulRequestCounts.get(path), `Sitemap fixture: ${path} success request count mismatch`)
      assert(requests.slice(before).every((request) => request.requestId === responseId), `Sitemap fixture: ${path} did not correlate all API calls`)
      successfulChildren.push(result)
    }
    const [films, cinemas, cities] = successfulChildren.map((result) => sitemapEntries(result.body))
    const expectedFilmEntries = [
      { loc: `${siteUrl}/`, lastmod: fixture.current.updated_at },
      { loc: `${siteUrl}/films`, lastmod: fixture.offPageCurrent.updated_at },
      ...[...fixture.currentMovies, fixture.richEnded]
        .map((movie) => ({
          loc: `${siteUrl}/film/${movie.slug}`,
          lastmod: movie.showtime_count > 0 ? laterTimestamp(movie.updated_at, fixture.generatedAt) : movie.updated_at
        }))
        .sort((left, right) => left.loc.localeCompare(right.loc))
    ]
    assert(JSON.stringify(films) === JSON.stringify(expectedFilmEntries), 'Sitemap fixture: film inventory or source timestamps mismatch')
    assert(fixture.filmsCatalog.items.length === 24 && !fixture.filmsCatalog.items.some((movie) => movie.slug === fixture.offPageCurrent.slug), 'Sitemap fixture: updated current movie must remain off page one')
    assert(fixture.filmsCatalog.available_genres.includes('Genre hors première page'), 'Sitemap fixture: off-page genre must affect rendered catalog metadata')
    assert(films.find((entry) => entry.loc === `${siteUrl}/films`)?.lastmod === fixture.offPageCurrent.updated_at, 'Sitemap fixture: off-page current metadata update did not advance /films lastmod')
    assert(JSON.stringify(cinemas) === JSON.stringify([
      { loc: `${siteUrl}/cinemas`, lastmod: fixture.generatedAt },
      { loc: `${siteUrl}/cinema/ugc-sitemap-fixture`, lastmod: fixture.generatedAt }
    ]), 'Sitemap fixture: cinema inventory or source timestamps mismatch')
    assert(JSON.stringify(cities) === JSON.stringify([
      { loc: `${siteUrl}/ville/paris/cinemas`, lastmod: fixture.generatedAt }
    ]), 'Sitemap fixture: city inventory or source timestamps mismatch')

    const afterMisses = requestCount
    await get(`${fixtureWebOrigin}/sitemap.xml`, 'application/xml')
    for (const path of childPaths) await get(`${fixtureWebOrigin}${path}?variant=cached-query`, 'application/xml')
    assert(requestCount === afterMisses, 'Sitemap fixture: query variant bypassed cached sitemap success')

    for (const path of ['/planning', '/planning?date=2026-01-01', '/recherche', '/recherche?grouping=chronological']) {
      const page = await get(`${fixtureWebOrigin}${path}`)
      const canonicalPath = path.split('?')[0]
      verifyPolicy(page, path, 'noindex,follow', `${siteUrl}${canonicalPath}`)
    }
    for (const [movie, expectedRobots, expectedMembership] of [
      [fixture.richEnded, 'index,follow', true],
      [fixture.thinEnded, 'noindex,follow', false]
    ]) {
      const path = `/film/${movie.slug}`
      const page = await get(`${fixtureWebOrigin}${path}`)
      verifyPolicy(page, path, expectedRobots, `${siteUrl}${path}`)
      assert(films.some((entry) => new URL(entry.loc).pathname === path) === expectedMembership, `Sitemap fixture: ${movie.slug} robots/sitemap parity mismatch`)
    }
    assert(!films.some((entry) => ['/planning', '/recherche', `/film/${fixture.thinEnded.slug}`].includes(new URL(entry.loc).pathname)), 'Sitemap fixture: excluded utility or thin-film URL present')
    assert(requests.some((request) => request.pathname === '/api/v1/movies') && requests.some((request) => request.pathname === '/api/v1/cities'), 'Sitemap fixture: production API routes were not exercised')
    assert(requests.every((request) => request.authenticated && REQUEST_ID_PATTERN.test(request.requestId)), 'Sitemap fixture: unauthenticated or uncorrelated API request observed')
    assert(!JSON.stringify(requests).includes(FIXTURE_INTERNAL_SECRET), 'Sitemap fixture: secret entered captured fixture output')
    console.log(`Crawlability sitemap-fixture checks passed (static index, 6 coalesced cold-failure pairs, 3 cached SWR child successes, authenticated request correlation, off-page current-film lastmod, utility noindex, and rich/thin ended-film parity; ${requestCount} API requests).`)
  } finally {
    if (child) await stopChild(child)
    if (mockApi.listening) await closeServer(mockApi)
  }
}

async function stopChild(child) {
  if (child.exitCode !== null || child.signalCode !== null) return
  child.kill('SIGTERM')
  await Promise.race([
    new Promise((resolve) => child.once('exit', resolve)),
    delay(3000)
  ])
  if (child.exitCode === null && child.signalCode === null) {
    child.kill('SIGKILL')
    await new Promise((resolve) => child.once('exit', resolve))
  }
}

async function waitForBuiltPage(url, child, output, headers = {}) {
  let lastError
  for (let attempt = 0; attempt < 100; attempt++) {
    if (child.exitCode !== null || child.signalCode !== null) throw new Error(`Built Nuxt server exited before readiness.\n${output()}`)
    try {
      return await get(url, 'text/html,application/json', headers)
    } catch (error) {
      lastError = error
      await delay(100)
    }
  }
  throw new Error(`Built Nuxt server did not become ready: ${lastError?.message ?? 'unknown error'}\n${output()}`)
}

async function verifySeoOnlyFailureMode() {
  const builtServerPath = fileURLToPath(new URL('../.output/server/index.mjs', import.meta.url))
  try {
    await access(builtServerPath)
  } catch {
    throw new Error(`SEO-only failure mode requires current Nuxt build at ${builtServerPath}; run npm --prefix web run build first`)
  }

  const fixtureDate = todayInParis()
  const schedule = fixtureMovieSchedule(fixtureDate)
  const requests = []
  const failureMarker = 'nationwide-seo-only-failure-990001'
  const mockApi = createServer((request, response) => {
    const target = new URL(request.url ?? '/', 'http://127.0.0.1')
    const requestId = String(request.headers['x-request-id'] ?? '')
    requests.push({
      pathname: target.pathname,
      query: Object.fromEntries(target.searchParams),
      requestId,
      authenticated: request.headers['x-messeances-internal-token'] === FIXTURE_INTERNAL_SECRET
    })
    response.setHeader('content-type', 'application/json; charset=utf-8')
    if (request.headers['x-messeances-internal-token'] !== FIXTURE_INTERNAL_SECRET || !REQUEST_ID_PATTERN.test(requestId)) {
      response.statusCode = 401
      response.end(JSON.stringify({ error: { code: 'unauthorized' } }))
      return
    }
    if (request.method !== 'GET' || target.pathname !== `/api/v1/internal/movies/${schedule.movie.slug}/showtimes-bundle`) {
      response.statusCode = 404
      response.end(JSON.stringify({ error: { code: 'not_found' } }))
      return
    }
    if (target.searchParams.get('date') !== fixtureDate) {
      response.statusCode = 400
      response.end(JSON.stringify({ error: { code: 'invalid_date' } }))
      return
    }
    if (target.searchParams.get('city') === 'Paris' && !target.searchParams.has('theaters')) {
      response.statusCode = 502
      response.end(JSON.stringify({ error: { code: 'seo_fixture_failure' }, marker: failureMarker }))
      return
    }
    response.statusCode = 400
    response.end(JSON.stringify({ error: { code: 'unexpected_scope' } }))
  })

  let child
  try {
    const apiAddress = await listen(mockApi)
    const privateApiOrigin = `http://127.0.0.1:${apiAddress.port}`
    const port = await availablePort()
    const fixtureWebOrigin = `http://127.0.0.1:${port}`
    let stdout = ''
    let stderr = ''
    child = spawn(process.execPath, [builtServerPath], {
      cwd: fileURLToPath(new URL('..', import.meta.url)),
      env: {
        ...process.env,
        HOST: '127.0.0.1',
        PORT: String(port),
        NITRO_HOST: '127.0.0.1',
        NITRO_PORT: String(port),
        NUXT_API_BASE: privateApiOrigin,
        NUXT_INTERNAL_API_SHARED_SECRET: FIXTURE_INTERNAL_SECRET,
        NUXT_PUBLIC_API_BASE: privateApiOrigin,
        NUXT_PUBLIC_SITE_URL: siteUrl
      },
      stdio: ['ignore', 'pipe', 'pipe']
    })
    child.stdout.on('data', (chunk) => { stdout += chunk })
    child.stderr.on('data', (chunk) => { stderr += chunk })
    const output = () => `${stdout}${stderr}`.trim().slice(-4000)
    const path = `/film/${schedule.movie.slug}`
    const page = await waitForBuiltPage(`${fixtureWebOrigin}${path}`, child, output)

    verifyErrorPolicy(page, path, 502)
    const text = visibleText(page.body)
    const expectedMessage = 'Impossible de joindre le service. Vérifiez que l’API est démarrée, puis réessayez.'
    assert(text.includes(expectedMessage), `${path}: existing generic French upstream UI is missing`)
    assert(!text.includes('Film introuvable'), `${path}: SEO-only failure rendered not-found UI`)
    const nodes = verifyGlobalGraph(page.body)
    for (const type of ['Movie', 'BreadcrumbList', 'MovieTheater', 'ScreeningEvent']) {
      assert(nodesOfType(nodes, type).length === 0, `${path}: SEO-only failure emitted ${type} film graph`)
    }

    const { state, payloadBytes } = filmPayloadState(page.body, schedule.movie.slug, path)
    assert(JSON.stringify(Object.keys(state).sort()) === JSON.stringify(['kind', 'schedule', 'selectedDate', 'errorMessage'].sort()), `${path}: failure async-data state keys mismatch`)
    assert(state.kind === 'upstream-error' && state.schedule === null, `${path}: failure hydration state must contain only null schedule upstream error`)
    assert(state.selectedDate === fixtureDate && state.errorMessage === expectedMessage, `${path}: failure hydration date or French message mismatch`)
    const serializedState = JSON.stringify(state)
    for (const marker of [schedule.movie.title, schedule.theaters[0].id, schedule.theaters[0].slug, schedule.theaters[0].showtimes[0].id, failureMarker]) {
      assert(!serializedState.includes(marker), `${path}: fixture schedule marker leaked into failure hydration: ${marker}`)
      assert(!withoutJsonLd(page.body).includes(marker), `${path}: fixture schedule marker leaked outside JSON-LD: ${marker}`)
    }
    for (const marker of ['available_dates', 'theaters']) assert(!serializedState.includes(marker), `${path}: schedule structure leaked into failure hydration: ${marker}`)

    const fixtureRequests = requests.filter((request) => request.pathname === `/api/v1/internal/movies/${schedule.movie.slug}/showtimes-bundle`)
    assert(fixtureRequests.length === 1, `SEO-only mode expected one bundle request, received ${fixtureRequests.length}`)
    assert(fixtureRequests[0].query.date === fixtureDate && fixtureRequests[0].query.city === 'Paris' && fixtureRequests[0].query.theaters === undefined, 'SEO-only mode observed unexpected bundle query')
    assert(fixtureRequests[0].authenticated && REQUEST_ID_PATTERN.test(fixtureRequests[0].requestId), 'SEO-only mode bundle auth or request ID missing')
    assert(page.response.headers.get('x-request-id') === fixtureRequests[0].requestId, 'SEO-only mode response/API request ID mismatch')
    assert(!page.body.includes(FIXTURE_INTERNAL_SECRET) && !JSON.stringify(requests).includes(FIXTURE_INTERNAL_SECRET), 'SEO-only mode exposed fixture secret')
    console.log(`Crawlability SEO-only failure checks passed (one authenticated bundle 502 -> film HTTP 502; payload ${payloadBytes} bytes; correlated request ID; no film graph, schedule payload, or secret exposure).`)
  } finally {
    if (child) await stopChild(child)
    if (mockApi.listening) await closeServer(mockApi)
  }
}

async function verifySsrSuccessMode() {
  const builtServerPath = fileURLToPath(new URL('../.output/server/index.mjs', import.meta.url))
  try {
    await access(builtServerPath)
  } catch {
    throw new Error(`SSR success mode requires current Nuxt build at ${builtServerPath}; run npm --prefix web run build first`)
  }

  const fixture = successfulFixtureSchedules(todayInParis())
  fixture.markers = nationwideOnlyMarkers(fixture)
  assert(fixture.markers.length > 0, 'SSR success fixture must contain nationwide-only theater/showtime markers')
  const requests = []
  const mockApi = createServer((request, response) => {
    const target = new URL(request.url ?? '/', 'http://127.0.0.1')
    const requestId = String(request.headers['x-request-id'] ?? '')
    requests.push({
      pathname: target.pathname,
      query: Object.fromEntries(target.searchParams),
      requestId,
      authenticated: request.headers['x-messeances-internal-token'] === FIXTURE_INTERNAL_SECRET
    })
    response.setHeader('content-type', 'application/json; charset=utf-8')
    if (request.headers['x-messeances-internal-token'] !== FIXTURE_INTERNAL_SECRET || !REQUEST_ID_PATTERN.test(requestId)) {
      response.statusCode = 401
      response.end(JSON.stringify({ error: { code: 'unauthorized' } }))
      return
    }
    if (request.method !== 'GET' || target.pathname !== `/api/v1/internal/movies/${fixture.movie.slug}/showtimes-bundle` || target.searchParams.has('theaters')) {
      response.statusCode = 404
      response.end(JSON.stringify({ error: { code: 'not_found' } }))
      return
    }

    const date = target.searchParams.get('date')
    const city = target.searchParams.get('city')
    let bundle
    if (city === 'Paris' && date === fixture.requestedDate) bundle = { scoped: fixture.initialParis, nationwide: fixture.initialNationwide }
    else if (city === 'Paris' && date === fixture.resolvedDate) bundle = { scoped: fixture.paris, nationwide: fixture.nationwide }
    if (!bundle) {
      response.statusCode = 400
      response.end(JSON.stringify({ error: { code: 'unexpected_scope_or_date' } }))
      return
    }
    response.statusCode = 200
    response.end(JSON.stringify(bundle))
  })

  let child
  try {
    const apiAddress = await listen(mockApi)
    const privateApiOrigin = `http://127.0.0.1:${apiAddress.port}`
    const port = await availablePort()
    const fixtureWebOrigin = `http://127.0.0.1:${port}`
    let stdout = ''
    let stderr = ''
    child = spawn(process.execPath, [builtServerPath], {
      cwd: fileURLToPath(new URL('..', import.meta.url)),
      env: {
        ...process.env,
        HOST: '127.0.0.1',
        PORT: String(port),
        NITRO_HOST: '127.0.0.1',
        NITRO_PORT: String(port),
        NUXT_API_BASE: privateApiOrigin,
        NUXT_INTERNAL_API_SHARED_SECRET: FIXTURE_INTERNAL_SECRET,
        NUXT_PUBLIC_API_BASE: privateApiOrigin,
        NUXT_PUBLIC_SITE_URL: siteUrl
      },
      stdio: ['ignore', 'pipe', 'pipe']
    })
    child.stdout.on('data', (chunk) => { stdout += chunk })
    child.stderr.on('data', (chunk) => { stderr += chunk })
    const output = () => `${stdout}${stderr}`.trim().slice(-4000)
    const path = `/film/${fixture.movie.slug}`
    const visitorRequestId = 'f'.repeat(32)
    const page = await waitForBuiltPage(`${fixtureWebOrigin}${path}`, child, output, { 'X-Request-ID': visitorRequestId })

    verifyPolicy(page, path, 'index,follow', `${siteUrl}${path}`)
    const graphEvidence = verifyFilmJsonLd(page.body, fixture.movie, fixture.nationwide, path)
    const runtimeEvidence = verifyFilmRuntimeIsolation(page, fixture, path)
    assert(fixture.resolvedDate !== fixture.requestedDate, `${path}: fixture did not exercise Paris date fallback`)

    const filmScript = one(jsonLdScripts(page.body).filter((script) => script.body.includes(`${siteUrl}${path}#movie`)), `${path}: successful inline film JSON-LD`)
    const outsideJsonLd = decodeHtml(withoutJsonLd(page.body))
    const { state } = filmPayloadState(page.body, fixture.movie.slug, path)
    const serializedState = JSON.stringify(state)
    for (const marker of fixture.discardedMarkers) {
      assert(!filmScript.body.includes(marker), `${path}: discarded initial-date nationwide marker entered final JSON-LD: ${marker}`)
      assert(!outsideJsonLd.includes(marker), `${path}: discarded initial-date nationwide marker leaked outside JSON-LD: ${marker}`)
      assert(!serializedState.includes(marker), `${path}: discarded initial-date nationwide marker leaked into hydration: ${marker}`)
    }
    for (const marker of fixture.nationwideEventMarkers) {
      assert(filmScript.body.includes(marker), `${path}: nationwide-only showtime fact missing from inline JSON-LD: ${marker}`)
      assert(!outsideJsonLd.includes(marker), `${path}: nationwide-only showtime fact leaked outside JSON-LD: ${marker}`)
      assert(!serializedState.includes(marker), `${path}: nationwide-only showtime fact leaked into hydration: ${marker}`)
    }

    const fixtureRequests = requests.filter((request) => request.pathname === `/api/v1/internal/movies/${fixture.movie.slug}/showtimes-bundle`)
    const expectedRequests = [
      { date: fixture.requestedDate, city: 'Paris' },
      { date: fixture.resolvedDate, city: 'Paris' }
    ]
    for (const expected of expectedRequests) {
      assert(fixtureRequests.some((request) => request.query.date === expected.date && request.query.city === expected.city && request.query.theaters === undefined), `${path}: missing bundle request for ${expected.date}`)
    }
    assert(fixtureRequests.length === expectedRequests.length, `${path}: expected ${expectedRequests.length} distinct-date bundle requests, received ${fixtureRequests.length}`)
    const responseRequestId = page.response.headers.get('x-request-id')
    assert(responseRequestId && REQUEST_ID_PATTERN.test(responseRequestId) && responseRequestId !== visitorRequestId, `${path}: visitor request ID was trusted or response ID invalid`)
    assert(fixtureRequests.every((request) => request.authenticated && request.requestId === responseRequestId), `${path}: bundle auth or correlated request ID missing`)
    assert(!page.body.includes(FIXTURE_INTERNAL_SECRET) && !JSON.stringify(requests).includes(FIXTURE_INTERNAL_SECRET), `${path}: internal secret leaked into response, hydration, or fixture output`)
    console.log(`Crawlability SSR-success checks passed (${fixture.requestedDate} -> Paris fallback ${fixture.resolvedDate}; 2 authenticated distinct-date bundle requests; fresh correlated request ID; HTTP 200; Paris UI/hydration ${runtimeEvidence.parisTheaters} theater(s)/${runtimeEvidence.parisEvents} event(s); nationwide JSON-LD ${graphEvidence.theaterCount} theater(s)/${graphEvidence.eventCount} event(s), ${graphEvidence.jsonLdBytes} UTF-8 bytes; Nuxt payload ${runtimeEvidence.payloadBytes} bytes; ${runtimeEvidence.nationwideMarkers} nationwide-only identity marker(s) and ${fixture.nationwideEventMarkers.length} showtime fact marker(s) isolated; no secret exposure).`)
  } finally {
    if (child) await stopChild(child)
    if (mockApi.listening) await closeServer(mockApi)
  }
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

  const discoveryEvidence = await verifyDiscovery(allCatalog, inventory)
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
    if (page.path.startsWith('/film/')) assert(title(result.body) === `${movie.title} : horaires et séances au cinéma - MesSeances`, `${page.path}: exact film title mismatch`)
    if (!['/films'].includes(page.path)) assertNoItemList(result.body, page.path)
    verifyGlobalGraph(result.body)
  }
  assert(new Set(metadata.map((item) => item.title)).size === pages.length, 'Route titles are not unique')
  assert(new Set(metadata.map((item) => item.description)).size === pages.length, 'Route descriptions are not unique')

  const filmFixture = await discoverFilmFixture(currentCatalog, movie, today)
  const filmEncodedSlug = encodeURIComponent(filmFixture.movie.slug)
  const filmPath = `/film/${filmEncodedSlug}`
  const filmPage = await get(`${webUrl}${filmPath}`)
  const graphEvidence = verifyFilmJsonLd(filmPage.body, filmFixture.movie, filmFixture.nationwide, filmPath)
  const runtimeEvidence = verifyFilmRuntimeIsolation(filmPage, filmFixture, filmPath)
  const filmHrefs = tags(filmPage.body, 'a').map((anchor) => new URL(anchor.href, webUrl))
  const cityTargets = new Set()
  for (const scheduleTheater of filmFixture.paris.theaters) {
    assert(String(scheduleTheater.city_slug ?? '').trim(), `${filmPath}: API theater city_slug missing`)
    cityTargets.add(`/ville/${encodeURIComponent(scheduleTheater.city_slug)}/cinemas`)
  }
  for (const cityTarget of cityTargets) {
    assert(filmHrefs.some((target) => target.pathname === cityTarget && !target.search && !target.hash), `${filmPath}: city link ${cityTarget} missing`)
  }
  console.log(`Film SSR fixture: ${filmFixture.movie.slug}; resolved ${filmFixture.resolvedDate}; Paris UI ${runtimeEvidence.parisTheaters} theater(s)/${runtimeEvidence.parisEvents} event(s); nationwide JSON-LD ${graphEvidence.theaterCount} theater(s)/${graphEvidence.eventCount} event(s), ${graphEvidence.jsonLdBytes} UTF-8 bytes; Nuxt payload ${runtimeEvidence.payloadBytes} bytes; ${runtimeEvidence.nationwideMarkers} nationwide-only marker(s) absent outside JSON-LD.`)

  const entityDescriptions = new Set()
  for (const cityDetail of cityDetails) {
    const cityPath = `/ville/${encodeURIComponent(cityDetail.city.slug)}/cinemas`
    const cityPage = await get(`${webUrl}${cityPath}`)
    verifyPolicy(cityPage, cityPath, 'index,follow', `${siteUrl}${cityPath}`)
    verifyGlobalGraph(cityPage.body)
    verifyEntityDescription(cityPage.body, cityPath, entityDescriptions)
    verifyBreadcrumb(cityPage.body, {
      path: cityPath,
      id: `${siteUrl}${cityPath}#breadcrumb`,
      names: ['Accueil', 'Cinémas', cityDetail.city.name],
      urls: [`${siteUrl}/`, `${siteUrl}/cinemas`, `${siteUrl}${cityPath}`]
    })
    assertStableInternalLinks(cityPage.body, cityPath)
    const cityHrefs = tags(cityPage.body, 'a').map((anchor) => new URL(anchor.href, webUrl).pathname)
    assert(cityDetail.theaters.every((item) => cityHrefs.includes(`/cinema/${encodeURIComponent(item.slug)}`)), `${cityPath}: cinema links incomplete`)
    assert(cityDetail.movies.every((item) => /^film-[1-9]\d*$/.test(item.slug) && cityHrefs.includes(`/film/${encodeURIComponent(item.slug)}`)), `${cityPath}: neutral film links incomplete`)
    const expectedCinemaUrls = cityDetail.theaters.map((item) => `${siteUrl}/cinema/${encodeURIComponent(item.slug)}`)
    const expectedFilmUrls = cityDetail.movies.map((item) => `${siteUrl}/film/${encodeURIComponent(item.slug)}`)
    let expectedListCount = 0
    if (expectedCinemaUrls.length) {
      verifyItemList(cityPage.body, `${siteUrl}${cityPath}#cinema-list`, expectedCinemaUrls, cityPath)
      expectedListCount++
    }
    if (expectedFilmUrls.length) {
      verifyItemList(cityPage.body, `${siteUrl}${cityPath}#film-list`, expectedFilmUrls, cityPath)
      expectedListCount++
    }
    assert(nodesOfType(graphNodes(cityPage.body), 'ItemList').length === expectedListCount, `${cityPath}: unexpected city ItemList count`)
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
    assert(title(cinemaPage.body) === `${cinemaResponse.theater.name}, ${cinemaResponse.theater.city} : séances et horaires`, `${cinemaPath}: exact cinema title mismatch`)
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

  await verifyEndedFilm(allCatalog, currentCatalog, today, discoveryEvidence.filmLocations)
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
    assert(nodesOfType(graphNodes(result.body), 'BreadcrumbList').length === 0, `${path}: error response emitted BreadcrumbList`)
    assertNoItemList(result.body, path)
  }
  const unknownPath = '/__crawlability-missing-route__'
  verifyErrorPolicy(await get(`${webUrl}${unknownPath}`), unknownPath, 404)
  console.log(`Crawlability checks passed (${currentCatalog.total} current films, ${allCatalog.total} canonical films, ${inventory.citySlugs.length} cities, ${inventory.theaterSlugs.length} cinemas, exact sitemap, descriptions, JSON-LD, indexing, links, redirects, and errors).`)
}

await (expectSitemapFixture
  ? verifySitemapFixtureMode()
  : expectSsrSuccess
    ? verifySsrSuccessMode()
    : expectSeoOnlyFailure
      ? verifySeoOnlyFailureMode()
      : expectUpstreamFailure
        ? verifyFailureMode()
        : verifyNormalMode())
