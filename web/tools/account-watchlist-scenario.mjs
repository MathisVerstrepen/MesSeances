// DB-free browser contract: public metadata and private account ownership stay separate.
import { createServer } from 'node:http'
import { writeFile } from 'node:fs/promises'
import { setTimeout as delay } from 'node:timers/promises'

export async function watchlistScenario({
  getCDP,
  launch,
  tab,
  go,
  evaluate,
  until,
  click,
  fill,
  check,
  setServer,
}) {
  const owner = {
    username: 'private_watchlist_owner',
    email: 'watchlist@example.test',
    has_password: true,
    google_linked: false,
  }
  let session = { enabled: true, state: 'complete', account: owner }
  let revision = 0
  let saved = []
  const frenchReleases = new Map([['saved-film', '1998-10-14']])
  let externalStatus = 'ready'
  let emptySearch = false
  let conflict = false
  let hold = false
  let holdSearch = false
  let releaseSearch
  let release
  const writes = []
  const requests = []
  const date = new Intl.DateTimeFormat('en-CA', {
    timeZone: 'Europe/Paris',
  }).format(new Date())
  const movie = (slug) => ({
    slug,
    title:
      slug === 'external-film'
        ? 'Film externe'
        : slug === 'saved-film'
          ? 'Film favori'
          : 'Autre film',
    original_language: 'fr',
    runtime_minutes: 90,
    updated_at: `${date}T00:00:00Z`,
    poster_url: null,
    tmdb_id: null,
    imdb_id: null,
    overview: 'Un film de cinéma.',
    release_date: slug === 'saved-film' ? '1997-07-24' : '2026-01-01',
    genres: [],
    french_release_date: null,
  })
  const theater = {
    provider: 'ugc',
    id: 'ugc-1',
    slug: 'cinema-test',
    name: 'Cinéma test',
    city: 'Paris',
    city_slug: 'paris',
    address: 'Rue du cinéma',
    postal_code: '75001',
    available_dates: [date],
    accepted_passes: [],
  }
  const snapshot = () => ({
    username: session.account.username,
    revision: String(revision),
    items: saved.map((slug) => ({
      ...movie(slug),
      french_release_date: frenchReleases.get(slug),
      added_at: `${date}T00:00:00Z`,
    })),
    external_search_available: true,
  })
  const slot = (slug, hour, id, provider = 'ugc') => {
    const start = `${date}T${hour}:00:00+02:00`
    const end = `${date}T${Number(hour) + 2}:00:00+02:00`
    return {
      showtime: {
        provider,
        id,
        movie: movie(slug),
        start_time: start,
        end_time: end,
        language: 'VF',
        format: '2D',
        room: '',
        booking_url: null,
      },
      theater,
      effective_start_time: start,
      effective_end_time: end,
      buffer_ads_minutes: 0,
      slack_before_minutes: 0,
      slack_after_minutes: 0,
      poster_url: null,
      backdrop_url: null,
    }
  }
  const server = createServer(async (req, res) => {
    const url = new URL(req.url, 'http://fixture')
    const path = url.pathname
    requests.push({ path, query: url.search })
    res.setHeader('Content-Type', 'application/json')
    res.setHeader('Cache-Control', 'no-store')
    let raw = ''
    for await (const chunk of req) raw += chunk
    const body = raw ? JSON.parse(raw) : {}
    if (req.method === 'POST') writes.push({ path, body })
    const send = (value, status = 200) => {
      res.statusCode = status
      res.end(JSON.stringify(value))
    }
    if (path === '/api/v1/auth/session') return send(session)
    if (path === '/api/v1/theaters') return send([theater])
    if (path === '/api/v1/account/theaters')
      return send({
        username: session.account?.username ?? '',
        revision: '1',
        theater_ids: ['ugc-1'],
      })
    if (path === '/api/v1/search/slot')
      return send([
        slot('other-film', '12', 'early'),
        slot('saved-film', '18', 'b'),
        slot('saved-film', '18', 'a', 'kinepolis'),
      ])
    if (/^\/api\/v1\/movies\/[^/]+\/showtimes$/.test(path))
      return send({
        movie: movie(path.split('/')[4]),
        backdrop_url: null,
        date,
        release_status: 'unavailable',
        currently_screened: false,
        available_dates: [],
        theaters: [],
      })
    if (path === '/api/v1/auth/logout') {
      session = { enabled: true, state: 'anonymous', account: null }
      return send({})
    }
    if (path.startsWith('/api/v1/account/watchlist')) {
      if (session.state !== 'complete')
        return send({ error: { code: 'authentication_required' } }, 401)
      if (
        req.method === 'POST' &&
        body.expected_username !== session.account.username
      )
        return send({ error: { code: 'authentication_required' } }, 401)
      if (path.endsWith('/search')) {
        if (holdSearch)
          await new Promise((resolve) => {
            releaseSearch = resolve
          })
        return send({
          username: session.account.username,
          catalog: emptySearch
            ? []
            : [
                movie('saved-film'),
                ...Array.from({ length: 19 }, (_, index) => ({
                  ...movie(`catalog-${index}`),
                  title: `Un très long titre de cinéma pour vérifier les résultats ${index}`,
                })),
              ],
          external:
            externalStatus === 'ready' && !emptySearch
              ? [
                  {
                    tmdb_id: '999',
                    title: 'Film externe',
                    release_date: '2026-01-01',
                  },
                ]
              : [],
          external_status: externalStatus,
          catalog_has_more: false,
        })
      }
      if (req.method === 'GET') return send(snapshot())
      if (conflict || body.expected_revision !== String(revision)) {
        conflict = false
        return send({ error: { code: 'watchlist_changed' } }, 409)
      }
      if (hold)
        await new Promise((resolve) => {
          release = resolve
        })
      const slug = path.endsWith('/import') ? 'external-film' : body.movie_slug
      saved = saved.filter((item) => item !== slug)
      if (path.endsWith('/import') || body.saved === 'true') saved.unshift(slug)
      revision++
      return send(
        path.endsWith('/import')
          ? { watchlist: snapshot(), movie_slug: slug }
          : snapshot(),
      )
    }
    send({})
  })
  setServer(server)
  await new Promise((resolve, reject) => {
    server.once('error', reject)
    server.listen(18089, '127.0.0.1', resolve)
  })
  const router = `document.querySelector('#__nuxt').__vue_app__.config.globalProperties.$router`
  async function screenshot(page, name) {
    if (!process.argv.includes('--visual')) return
    const image = await getCDP().send(
      'Page.captureScreenshot',
      { format: 'png', captureBeyondViewport: true },
      page.sessionId,
    )
    await writeFile(
      `/tmp/opencode/watchlist-${name}.png`,
      Buffer.from(image.data, 'base64'),
    )
  }
  const route = async (page, path) => {
    await evaluate(page, `${router}.push(${JSON.stringify(path)})`)
    await until(
      page,
      `location.pathname === ${JSON.stringify(path.split('?')[0])}`,
      'watchlist SPA arrival',
    )
  }
  const savedRow = (slug) =>
    `document.querySelector('section[aria-labelledby="saved-heading"] a[href="/film/${slug}"]')?.closest('li')`
  await launch()
  const page = await tab()
  await getCDP().send('Page.bringToFront', {}, page.sessionId)
  await getCDP().send(
    'Emulation.setDeviceMetricsOverride',
    { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false },
    page.sessionId,
  )
  await go(page, '/compte/watchlist')
  await until(
    page,
    `document.querySelector('main').textContent.includes('Votre watchlist est vide')`,
    'watchlist empty state',
  )
  check(
    await evaluate(
      page,
      `!document.querySelector('#watchlist-query').disabled && document.querySelectorAll('h1').length === 1`,
    ),
    'watchlist form enabled with single heading',
  )
  await fill(page, 'watchlist-query', 'private candidate query')
  check(
    await evaluate(page, `!document.querySelector('#watchlist-results')`),
    'typing alone never opens or submits search',
  )
  const documentHeight = await evaluate(
    page,
    'document.documentElement.scrollHeight',
  )
  await click(page, 'Rechercher')
  await until(
    page,
    `!!document.querySelector('#watchlist-catalog-panel li')`,
    'catalog results ready',
  )
  check(
    await evaluate(
      page,
      `(() => { const panel = document.querySelector('#watchlist-results'); const scroll = panel.querySelector('.overflow-y-auto'); const form = document.querySelector('form'); return getComputedStyle(panel).position === 'absolute' && panel.getBoundingClientRect().top >= document.querySelector('#watchlist-query').getBoundingClientRect().bottom && panel.getBoundingClientRect().bottom <= innerHeight && scroll.scrollHeight > scroll.clientHeight && document.documentElement.scrollHeight === ${documentHeight} && form.contains(panel) && document.activeElement.id === 'watchlist-catalog-tab'; })()`,
    ),
    'results overlay is anchored, viewport bounded and does not expand document',
  )
  check(
    await evaluate(
      page,
      `(() => { const row = document.querySelector('#watchlist-catalog-panel li'); return row.querySelector('p.text-muted')?.textContent.trim() === '1997' && !row.querySelector('time'); })()`,
    ),
    'catalog search keeps general-date year rather than saved French evidence',
  )
  check(
    await evaluate(
      page,
      `(() => { const before = scrollY; const scroll = document.querySelector('#watchlist-results .overflow-y-auto'); scroll.scrollTop = 500; return scroll.scrollTop === 500 && scrollY === before; })()`,
    ),
    'result scrolling leaves document scroll position unchanged',
  )
  await getCDP().send(
    'Input.dispatchKeyEvent',
    {
      type: 'keyDown',
      key: 'ArrowRight',
      code: 'ArrowRight',
      windowsVirtualKeyCode: 39,
    },
    page.sessionId,
  )
  check(
    await evaluate(
      page,
      `document.activeElement.id === 'watchlist-external-tab' && document.activeElement.getAttribute('aria-selected') === 'true' && !document.querySelector('#watchlist-catalog-panel').getClientRects().length && document.querySelector('#watchlist-external-panel').getClientRects().length > 0`,
    ),
    'arrow key changes source tab and visible panel',
  )
  check(
    await evaluate(
      page,
      `(() => { const row = document.querySelector('#watchlist-external-panel li'); return row.querySelector('p.text-muted')?.textContent.trim() === '2026' && !row.querySelector('time'); })()`,
    ),
    'external search keeps general-date year',
  )
  await screenshot(page, 'external-tab')
  check(
    await evaluate(page, `!document.querySelector('a[href*="999"]')`),
    'external candidate has no fictitious public URL',
  )
  await evaluate(
    page,
    `document.querySelector('#watchlist-catalog-tab').click()`,
  )
  await screenshot(page, 'catalog-panel')
  conflict = true
  await evaluate(
    page,
    `document.querySelector('button[aria-label="Ajouter Film favori à la watchlist"]').click()`,
  )
  await until(
    page,
    `!!document.querySelector('#watchlist-results [role="alert"]')`,
    'failed add error remains inside panel',
  )
  check(
    await evaluate(
      page,
      `document.querySelector('#watchlist-query').value === 'private candidate query' && document.querySelector('#watchlist-catalog-panel li') && !document.querySelector('button[aria-label="Ajouter Film favori à la watchlist"]').disabled`,
    ),
    'failed save preserves query results and retry',
  )
  hold = true
  await evaluate(
    page,
    `document.querySelector('button[aria-label="Ajouter Film favori à la watchlist"]').click()`,
  )
  await until(
    page,
    `document.querySelector('button[aria-label="Ajouter Film favori à la watchlist"]').disabled`,
    'save action locks in flight',
  )
  check(
    await evaluate(
      page,
      `document.querySelector('button[aria-label="Ajouter Film favori à la watchlist"]').textContent.trim() === 'Ajouter' && !!document.querySelector('#watchlist-results') && document.querySelector('#watchlist-query').value === 'private candidate query'`,
    ),
    'no optimistic false saved state',
  )
  for (let i = 0; !release && i < 100; i++) await delay(20)
  check(!!release, 'held save reached fixture')
  hold = false
  release()
  await until(
    page,
    `!!document.querySelector('button[aria-label="Retirer de la watchlist"]')`,
    'acknowledged saved state',
  )
  check(
    await evaluate(
      page,
      `!document.querySelector('#watchlist-results') && document.querySelector('#watchlist-query').value === '' && document.activeElement.id === 'watchlist-query'`,
    ),
    'confirmed catalog save closes clears and restores input focus',
  )
  check(
    await evaluate(
      page,
      `(() => { const row = ${savedRow('saved-film')}; return row.querySelector('time')?.getAttribute('datetime') === '1998-10-14' && row.querySelector('time').textContent.trim() === '14 octobre 1998' && !row.textContent.includes('1997'); })()`,
    ),
    'saved catalog movie displays verified full French date, not general release year',
  )
  await fill(page, 'watchlist-query', 'private candidate query')
  await click(page, 'Rechercher')
  await until(
    page,
    `!!document.querySelector('button[aria-label="Film favori déjà dans la watchlist"]')`,
    'already saved catalog state',
  )
  check(
    await evaluate(
      page,
      `document.querySelector('button[aria-label="Film favori déjà dans la watchlist"]').disabled && !document.querySelector('#watchlist-results button[aria-label="Retirer de la watchlist"]')`,
    ),
    'search cannot toggle an already saved movie off',
  )
  await evaluate(
    page,
    `document.querySelector('#watchlist-external-tab').click()`,
  )
  conflict = true
  await evaluate(
    page,
    `document.querySelector('button[aria-label="Ajouter Film externe à la watchlist"]').click()`,
  )
  await until(
    page,
    `!!document.querySelector('#watchlist-results [role="alert"]')`,
    'failed import stays visible',
  )
  check(
    await evaluate(
      page,
      `document.querySelector('#watchlist-query').value === 'private candidate query' && document.querySelector('#watchlist-external-tab').getAttribute('aria-selected') === 'true'`,
    ),
    'failed import preserves external tab and query',
  )
  await evaluate(
    page,
    `document.querySelector('button[aria-label="Ajouter Film externe à la watchlist"]').click()`,
  )
  await until(
    page,
    `!!document.querySelector('a[href="/film/external-film"]')`,
    'external result becomes canonical link',
  )
  check(
    saved.includes('external-film') && saved.includes('saved-film'),
    'import commits public movie and private saved item',
  )
  check(
    await evaluate(
      page,
      `!document.querySelector('#watchlist-results') && document.querySelector('#watchlist-query').value === ''`,
    ),
    'confirmed external import closes and clears search',
  )
  check(
    await evaluate(
      page,
      `(() => { const row = ${savedRow('external-film')}; return !!row && !row.querySelector('time, .text-muted') && !row.textContent.includes('2026'); })()`,
    ),
    'imported movie without French evidence has no date or general-year fallback',
  )
  const membershipRevision = revision
  const beforeRefreshWrites = writes.length
  for (const [evidence, label] of [
    ['1998-10-07', '7 octobre 1998'],
    [undefined, null],
    ['1998-10-14', '14 octobre 1998'],
    ['1998-02-30', null],
    ['1998-10-14', '14 octobre 1998'],
  ]) {
    if (evidence) frenchReleases.set('saved-film', evidence)
    else frenchReleases.delete('saved-film')
    await evaluate(page, `window.dispatchEvent(new Event('focus'))`)
    await until(
      page,
      `(() => { const row = ${savedRow('saved-film')}; return !!row && !document.querySelector('#watchlist-query').disabled && ${label ? `row.querySelector('time')?.textContent.trim() === ${JSON.stringify(label)}` : `!row.querySelector('time, .text-muted')`}; })()`,
      'French evidence revalidation at unchanged membership revision',
    )
    check(
      revision === membershipRevision && writes.length === beforeRefreshWrites,
      `equal-revision evidence refresh ${evidence ?? 'no evidence'} updates without membership mutation`,
    )
  }
  await screenshot(page, 'desktop')
  await route(page, '/film/external-film')
  await until(
    page,
    `!!document.querySelector('button[aria-label="Retirer de la watchlist"]')`,
    'standalone bookmark on no-trailer no-session film',
  )
  check(
    await evaluate(
      page,
      `!document.querySelector('button[aria-haspopup="dialog"]') && document.querySelector('button[aria-label="Retirer de la watchlist"]').getBoundingClientRect().width >= 44`,
    ),
    'no-trailer bookmark retains accessible target',
  )
  await screenshot(page, 'film')
  conflict = true
  await evaluate(
    page,
    `document.querySelector('button[aria-label="Retirer de la watchlist"]').click()`,
  )
  await until(
    page,
    `document.querySelector('main').textContent.includes('Votre watchlist a changé')`,
    'conflict readback and safe error',
  )
  check(saved.includes('external-film'), 'conflict does not replay removal')
  await until(
    page,
    `!!document.querySelector('button[aria-label="Retirer de la watchlist"]:not(:disabled)')`,
    'removal available after readback',
  )
  await evaluate(
    page,
    `document.querySelector('button[aria-label="Retirer de la watchlist"]').focus()`,
  )
  for (const modifiers of [0, 8]) {
    for (const type of ['keyDown', 'keyUp']) {
      await getCDP().send(
        'Input.dispatchKeyEvent',
        { type, key: 'Tab', code: 'Tab', windowsVirtualKeyCode: 9, modifiers },
        page.sessionId,
      )
    }
  }
  check(
    await evaluate(
      page,
      `document.activeElement?.getAttribute('aria-label') === 'Retirer de la watchlist' && (getComputedStyle(document.activeElement).outlineStyle !== 'none' || getComputedStyle(document.activeElement).boxShadow !== 'none')`,
    ),
    'keyboard navigation shows bookmark focus',
  )
  await getCDP().send(
    'Input.dispatchKeyEvent',
    {
      type: 'keyDown',
      key: 'Enter',
      code: 'Enter',
      windowsVirtualKeyCode: 13,
      text: '\r',
    },
    page.sessionId,
  )
  await getCDP().send(
    'Input.dispatchKeyEvent',
    { type: 'keyUp', key: 'Enter', code: 'Enter', windowsVirtualKeyCode: 13 },
    page.sessionId,
  )
  await until(
    page,
    `!!document.querySelector('button[aria-label="Ajouter à la watchlist"]:not(:disabled)')`,
    'remove acknowledged',
  )
  check(!saved.includes('external-film'), 'keyboard Enter removes saved film')
  await route(page, '/compte/watchlist')
  await until(
    page,
    `!!document.querySelector('#watchlist-query:not(:disabled)')`,
    'watchlist returns',
  )
  check(
    await evaluate(
      page,
      `document.querySelector('#watchlist-query').value === ''`,
    ),
    'search draft clears on route departure',
  )
  externalStatus = 'unavailable'
  await fill(page, 'watchlist-query', 'private candidate query')
  await click(page, 'Rechercher')
  await until(
    page,
    `!!document.querySelector('#watchlist-external-tab')`,
    'source tabs reopen',
  )
  await evaluate(
    page,
    `document.querySelector('#watchlist-external-tab').click()`,
  )
  await until(
    page,
    `document.querySelector('main').textContent.includes('La recherche externe est indisponible')`,
    'TMDB outage leaves local catalog usable',
  )
  check(
    await evaluate(
      page,
      `!!document.querySelector('a[href="/film/saved-film"]')`,
    ),
    'catalog remains available during external outage',
  )
  await getCDP().send(
    'Input.dispatchKeyEvent',
    {
      type: 'keyDown',
      key: 'Escape',
      code: 'Escape',
      windowsVirtualKeyCode: 27,
    },
    page.sessionId,
  )
  check(
    await evaluate(
      page,
      `!document.querySelector('#watchlist-results') && document.activeElement.id === 'watchlist-query' && document.querySelector('#watchlist-query').value === 'private candidate query'`,
    ),
    'Escape closes and returns focus without deleting draft',
  )
  holdSearch = true
  await click(page, 'Rechercher')
  for (let i = 0; !releaseSearch && i < 100; i++) await delay(20)
  check(!!releaseSearch, 'pending search reached fixture')
  await evaluate(
    page,
    `document.querySelector('#saved-heading').dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }))`,
  )
  holdSearch = false
  releaseSearch()
  await delay(100)
  check(
    await evaluate(
      page,
      `!document.querySelector('#watchlist-results') && document.querySelector('#watchlist-query').value === 'private candidate query'`,
    ),
    'outside dismissal fences late search results',
  )
  emptySearch = true
  externalStatus = 'ready'
  await click(page, 'Rechercher')
  await until(
    page,
    `document.querySelector('#watchlist-catalog-panel')?.textContent.includes('Aucun film du catalogue')`,
    'catalog empty state',
  )
  await evaluate(
    page,
    `document.querySelector('#watchlist-external-tab').click()`,
  )
  check(
    await evaluate(
      page,
      `document.querySelector('#watchlist-external-panel').textContent.includes('Aucun autre film trouvé')`,
    ),
    'both source tabs expose actionable empty states',
  )
  await evaluate(
    page,
    `document.querySelector('button[aria-label="Retirer de la watchlist"]').focus()`,
  )
  check(
    await evaluate(
      page,
      `!document.querySelector('#watchlist-results') && document.activeElement.getAttribute('aria-label') === 'Retirer de la watchlist'`,
    ),
    'keyboard focus can leave non-modal panel without a trap',
  )
  externalStatus = 'disabled'
  await click(page, 'Rechercher')
  await until(
    page,
    `document.querySelector('#watchlist-external-panel')?.textContent.includes('n’est pas activée')`,
    'disabled external source state',
  )
  await evaluate(
    page,
    `document.querySelector('#watchlist-external-tab').click()`,
  )
  check(
    await evaluate(
      page,
      `document.querySelector('#watchlist-external-panel').getClientRects().length > 0 && !document.querySelector('#watchlist-external-panel button')`,
    ),
    'disabled external source explains unavailable action',
  )
  emptySearch = false
  externalStatus = 'ready'
  const searchPath = `/recherche?theaters=ugc-1&date=${date}&start_after=10%3A00&finish_before=23%3A00`
  await route(page, searchPath)
  await until(
    page,
    `document.querySelector('section[aria-label="Ma watchlist"]') && document.querySelector('section[aria-label="Autres films"]')`,
    'explicit watchlist-first search sections',
  )
  for (const grouping of ['movie', 'chronological'])
    for (const layout of ['lines', 'boxes']) {
      await route(page, `${searchPath}&grouping=${grouping}&layout=${layout}`)
      await until(
        page,
        `document.querySelector('section[aria-label="Ma watchlist"]')`,
        'search layout ready',
      )
      check(
        await evaluate(
          page,
          `(() => { const saved = document.querySelector('section[aria-label="Ma watchlist"]'); const other = document.querySelector('section[aria-label="Autres films"]'); return !!(saved.compareDocumentPosition(other) & Node.DOCUMENT_POSITION_FOLLOWING) && saved.textContent.includes('Film favori') && other.textContent.includes('Autre film') })()`,
        ),
        `${grouping}/${layout} keeps later watchlist screening before earlier other movie`,
      )
    }
  const count = requests.filter(
    (request) => request.path === '/api/v1/search/slot',
  ).length
  const url = await evaluate(page, 'location.href')
  await evaluate(
    page,
    `[...document.querySelectorAll('label')].find(label => label.textContent.includes('Ma watchlist uniquement')).querySelector('input').click()`,
  )
  await until(
    page,
    `!document.querySelector('section[aria-label="Autres films"]')`,
    'private only filter hides other movies',
  )
  check(
    (await evaluate(page, 'location.href')) === url &&
      count ===
        requests.filter((request) => request.path === '/api/v1/search/slot')
          .length,
    'private filter neither changes URL nor refetches public results',
  )
  const second = await tab(page.browserContextId)
  await go(second, '/film/saved-film')
  await until(
    second,
    `!!document.querySelector('button[aria-label="Retirer de la watchlist"]:not(:disabled)')`,
    'second tab sees server snapshot',
  )
  await evaluate(
    second,
    `document.querySelector('button[aria-label="Retirer de la watchlist"]').click()`,
  )
  await until(
    page,
    `document.querySelector('main').textContent.includes('Aucune séance de votre watchlist')`,
    'tab invalidation revalidates private partition',
  )
  await evaluate(page, `window.dispatchEvent(new Event('pagehide'))`)
  check(
    await evaluate(
      page,
      `!document.querySelector('section[aria-label="Ma watchlist"]')`,
    ),
    'pagehide purges private membership immediately',
  )
  await evaluate(
    page,
    `window.dispatchEvent(new PageTransitionEvent('pageshow', { persisted: true }))`,
  )
  await until(
    page,
    `!![...document.querySelectorAll('label')].find(label => label.textContent.includes('Ma watchlist uniquement'))?.querySelector('input:not(:disabled)')`,
    'BFCache restore revalidates account',
  )
  check(
    await evaluate(
      page,
      `![...document.querySelectorAll('label')].find(label => label.textContent.includes('Ma watchlist uniquement')).querySelector('input').checked`,
    ),
    'invalidation resets private filter',
  )
  await getCDP().send(
    'Emulation.setDeviceMetricsOverride',
    { width: 390, height: 844, deviceScaleFactor: 1, mobile: true },
    page.sessionId,
  )
  await route(page, '/compte')
  await until(
    page,
    `!!document.querySelector('main a[href="/compte/watchlist"]')`,
    'mobile account hub links to watchlist',
  )
  await evaluate(
    page,
    `document.querySelector('main a[href="/compte/watchlist"]').click()`,
  )
  await until(
    page,
    `!!document.querySelector('#watchlist-query:not(:disabled)')`,
    'mobile watchlist route',
  )
  check(
    await evaluate(
      page,
      `document.documentElement.scrollWidth <= innerWidth && [...document.querySelectorAll('main a[href="/compte"]')].some(node => node.getClientRects().length)`,
    ),
    'mobile has no horizontal overflow and visible account return',
  )
  await fill(page, 'watchlist-query', 'private candidate query')
  await click(page, 'Rechercher')
  await until(
    page,
    `!!document.querySelector('#watchlist-catalog-panel li')`,
    'mobile catalog panel ready',
  )
  check(
    await evaluate(
      page,
      `(() => { const panel = document.querySelector('#watchlist-results').getBoundingClientRect(); const scroll = document.querySelector('#watchlist-results .overflow-y-auto'); return panel.left >= 0 && panel.right <= innerWidth && panel.bottom <= innerHeight && scroll.scrollHeight > scroll.clientHeight && document.documentElement.scrollWidth <= innerWidth; })()`,
    ),
    'mobile overlay fits width and scrolls inside available viewport',
  )
  await screenshot(page, 'mobile')
  await evaluate(
    page,
    `document.querySelector('button[aria-label="Fermer les résultats"]').click()`,
  )
  check(
    await evaluate(
      page,
      `!document.querySelector('#watchlist-results') && document.activeElement.id === 'watchlist-query'`,
    ),
    'close control returns focus',
  )
  await click(page, 'Rechercher')
  await until(
    page,
    `!!document.querySelector('button[aria-label="Ajouter Film favori à la watchlist"]:not(:disabled)')`,
    'mobile saved-date candidate ready',
  )
  await evaluate(
    page,
    `document.querySelector('button[aria-label="Ajouter Film favori à la watchlist"]').click()`,
  )
  await until(
    page,
    `${savedRow('saved-film')}?.querySelector('time')?.textContent.trim() === '14 octobre 1998' && !document.querySelector('#watchlist-results')`,
    'mobile saved date rendered',
  )
  check(
    await evaluate(
      page,
      `(() => { const row = ${savedRow('saved-film')}; const time = row.querySelector('time').getBoundingClientRect(); const action = row.querySelector('button').getBoundingClientRect(); return document.documentElement.scrollWidth <= innerWidth && time.right <= action.left && action.width >= 44 && action.height >= 44; })()`,
    ),
    'mobile full French date fits saved row and preserves bookmark touch target',
  )
  await screenshot(page, 'mobile-saved-date')
  check(
    await evaluate(
      page,
      `!JSON.stringify({ local: { ...localStorage }, session: { ...sessionStorage }, payload: window.__NUXT__, data: document.querySelector('#__nuxt').__vue_app__.$nuxt.payload.data, url: location.href }).match(/private_watchlist_owner|private candidate query|watchlist-only|added_at|1998-10-14|14 octobre 1998/)`,
    ),
    'private watchlist identity query membership absent from browser storage and public payload',
  )
  check(
    !page.collections.some((collection) =>
      /private_watchlist_owner|private candidate query|watchlist-only|saved-film|1998-10-14|14 octobre 1998/.test(
        JSON.stringify(collection),
      ),
    ),
    'analytics contains no watchlist membership, identity or private query',
  )
  const ssr = await (
    await fetch('http://127.0.0.1:13009/film/external-film')
  ).text()
  check(
    !/private_watchlist_owner|added_at|private candidate query|1998-10-14|14 octobre 1998/.test(
      ssr,
    ),
    'public film SSR excludes account watchlist state',
  )
  check(
    writes
      .filter((write) => write.path.endsWith('/watchlist/search'))
      .every((write) => write.body.query === 'private candidate query'),
    'private candidate queries travel only in POST bodies',
  )
  session = { enabled: true, state: 'anonymous', account: null }
  await route(page, '/film/external-film')
  await evaluate(
    second,
    `(() => { const channel = new BroadcastChannel('messeances-account'); channel.postMessage('changed'); channel.close() })()`,
  )
  await until(
    page,
    `!!document.querySelector('button[aria-label="Ajouter à la watchlist"]:not(:disabled)')`,
    'anonymous film bookmark',
  )
  const before = writes.length
  await evaluate(
    page,
    `document.querySelector('button[aria-label="Ajouter à la watchlist"]').click()`,
  )
  await until(
    page,
    `location.pathname === '/connexion'`,
    'anonymous save uses normal sign-in',
  )
  check(writes.length === before, 'anonymous action queues no mutation')
}
// Real Chrome -> Nitro -> Go -> isolated PostgreSQL. Uses the backend's shared
// canonical catalogue fixture without seeding or altering its database directly.
export async function watchlistBackendScenario({
  tab,
  go,
  evaluate,
  until,
  click,
  fill,
  check,
  request,
  mail,
  run,
}) {
  const page = await tab()
  const email = `watchlist-${run}@example.test`
  const username = `watchlist_${run}`
  await go(page, '/inscription')
  await fill(page, 'account-email', email)
  await fill(page, 'account-password', 'Synthetic cinema password 42!')
  await click(page, 'Créer mon compte')
  await until(
    page,
    `document.querySelector('main').textContent.includes('Si cette adresse peut être utilisée')`,
    'registration accepted',
  )
  const message = await mail(email, 'verification')
  await go(page, message.link)
  await click(page, 'Confirmer mon email')
  await until(
    page,
    `location.pathname === '/finaliser' && !!document.getElementById('account-username')`,
    'verified onboarding ready',
  )
  await fill(page, 'account-username', username)
  await click(page, 'Confirmer mon nom')
  await until(page, `location.pathname === '/compte'`, 'complete account ready')
  const session = await request(page, '/auth/session', undefined, 'GET')
  check(
    session.status === 200 &&
      session.body.state === 'complete' &&
      session.body.account.username === username,
    'real backend complete account created through UI',
  )
  await evaluate(
    page,
    `document.querySelector('nav[aria-label="Rubriques du compte"] a[href="/compte/watchlist"]').click()`,
  )
  await until(
    page,
    `document.querySelector('main').textContent.includes('Votre watchlist est vide')`,
    'persisted empty watchlist visible',
  )
  const empty = await request(page, '/account/watchlist', undefined, 'GET')
  check(
    empty.status === 200 &&
      empty.body.username === username &&
      empty.body.revision === '0' &&
      empty.body.items.length === 0,
    'real watchlist starts at revision zero with correct owner',
  )
  check(
    await evaluate(
      page,
      `(async () => { const response = await fetch('/api/v1/account/watchlist', {cache:'no-store'}); return response.headers.get('cache-control')?.includes('no-store') && response.headers.get('referrer-policy') === 'no-referrer'; })()`,
    ),
    'real private watchlist response retains privacy headers',
  )
  const movies = await request(page, '/movies', undefined, 'GET')
  check(
    movies.status === 200 && movies.body.items.length > 0,
    'public fixture exposes local movies',
  )
  const theaters = await request(page, '/theaters', undefined, 'GET')
  const preferences = await request(page, '/account/theaters', undefined, 'GET')
  const configured = await request(page, '/account/theaters', {
    expected_username: username,
    expected_revision: preferences.body.revision,
    theater_ids: theaters.body.map((theater) => theater.id).join(','),
  })
  check(configured.status === 200, 'real search account has configured cinemas')
  const date = theaters.body[0].available_dates[0]
  const params = new URLSearchParams({
    theaters: theaters.body.map((theater) => theater.id).join(','),
    date,
    start_after: '08:00',
    finish_before: '23:30',
  })
  const slots = await request(page, `/search/slot?${params}`, undefined, 'GET')
  check(
    slots.status === 200 &&
      new Set(slots.body.map((slot) => slot.showtime.movie.slug)).size > 1,
    'real slot fixture has distinct local films for priority checks',
  )
  const chronological = [...slots.body].sort(
    (left, right) =>
      Date.parse(left.showtime.start_time) -
      Date.parse(right.showtime.start_time),
  )
  const otherMovie = chronological[0].showtime.movie
  const movie = chronological.find(
    (slot) => slot.showtime.movie.slug !== otherMovie.slug,
  ).showtime.movie
  await fill(page, 'watchlist-query', movie.title)
  await click(page, 'Rechercher')
  await until(
    page,
    `!!document.querySelector('#watchlist-catalog-panel li') && !document.querySelector('form[aria-busy="true"]')`,
    'real catalogue search completed',
  )
  const search = await request(page, '/account/watchlist/search', {
    expected_username: username,
    query: movie.title,
  })
  check(
    search.status === 200 && search.body.username === username,
    'real private search returns admitted owner',
  )
  check(
    search.body.external_status === 'disabled' &&
      !empty.body.external_search_available,
    'external TMDB safely disabled in backend fixture',
  )
  check(
    search.body.catalog.some((item) => item.slug === movie.slug),
    'real private catalogue search resolves public canonical movie',
  )
  await go(page, `/film/${encodeURIComponent(movie.slug)}`)
  await until(
    page,
    `!!document.querySelector('button[aria-label="Ajouter à la watchlist"]:not(:disabled)')`,
    'real public film bookmark ready',
  )
  await evaluate(
    page,
    `document.querySelector('button[aria-label="Ajouter à la watchlist"]').click()`,
  )
  await until(
    page,
    `!!document.querySelector('button[aria-label="Retirer de la watchlist"]') || !!document.querySelector('main [role="alert"]')`,
    'real save response rendered',
  )
  const saved = await request(page, '/account/watchlist', undefined, 'GET')
  check(
    saved.status === 200 &&
      saved.body.items.some((item) => item.slug === movie.slug),
    'real local save persists membership',
  )
  await go(page, '/compte/watchlist')
  await until(
    page,
    `!!document.querySelector('#saved-heading') && !!document.querySelector('button[aria-label="Retirer de la watchlist"]')`,
    'saved membership survives document reload',
  )
  const router = `document.querySelector('#__nuxt').__vue_app__.config.globalProperties.$router`
  const searchPath = `/recherche?${params}`
  await go(page, searchPath)
  for (const grouping of ['movie', 'chronological'])
    for (const layout of ['lines', 'boxes']) {
      await evaluate(
        page,
        `${router}.push(${JSON.stringify(`${searchPath}&grouping=${grouping}&layout=${layout}`)})`,
      )
      await until(
        page,
        `!!document.querySelector('section[aria-label="Ma watchlist"]') && !!document.querySelector('section[aria-label="Autres films"]')`,
        'real search personalized sections ready',
      )
      check(
        await evaluate(
          page,
          `(() => { const saved = document.querySelector('section[aria-label="Ma watchlist"]'); const other = document.querySelector('section[aria-label="Autres films"]'); return !!(saved.compareDocumentPosition(other) & Node.DOCUMENT_POSITION_FOLLOWING) && saved.textContent.includes(${JSON.stringify(movie.title)}) && !saved.textContent.includes(${JSON.stringify(otherMovie.title)}) && other.textContent.includes(${JSON.stringify(otherMovie.title)}) && !other.textContent.includes(${JSON.stringify(movie.title)}); })()`,
        ),
        `real ${grouping}/${layout} puts later saved film before earlier other film`,
      )
    }
  const slotCount = () =>
    page.requests.filter((entry) => entry.path === '/api/v1/search/slot').length
  const beforeCount = slotCount()
  const publicURL = await evaluate(page, 'location.href')
  const toggle = `[...document.querySelectorAll('label')].find(label => label.textContent.includes('Ma watchlist uniquement')).querySelector('input')`
  await evaluate(page, `${toggle}.click()`)
  await until(
    page,
    `!!document.querySelector('section[aria-label="Ma watchlist"]') && !document.querySelector('section[aria-label="Autres films"]')`,
    'real watchlist-only filter hides other films',
  )
  check(
    (await evaluate(page, 'location.href')) === publicURL &&
      slotCount() === beforeCount,
    'real watchlist-only filter changes neither public URL nor slot request count',
  )
  const privateMarkers = [
    username,
    email,
    'added_at',
    'expected_username',
    'watchlistOnly',
  ]
  check(
    await evaluate(
      page,
      `!${JSON.stringify(privateMarkers)}.some(marker => JSON.stringify({local:{...localStorage}, session:{...sessionStorage}, payload:window.__NUXT__, data:document.querySelector('#__nuxt').__vue_app__.$nuxt.payload.data, url:location.href}).includes(marker))`,
    ),
    'real membership and identity absent from storage public data and URL',
  )
  const publicHTML = await (await fetch(publicURL)).text()
  check(
    !privateMarkers.some((marker) => publicHTML.includes(marker)) &&
      !publicHTML.includes('aria-label="Ma watchlist"'),
    'anonymous public SSR has no owner or saved membership',
  )
  await until(page, '!!window.umami', 'real pinned analytics fixture loaded')
  check(
    page.collections.length > 0 &&
      !privateMarkers.some((marker) =>
        JSON.stringify(page.collections).includes(marker),
      ),
    'real public analytics contain no watchlist owner or membership',
  )
  await evaluate(page, `${toggle}.click()`)
  await until(
    page,
    `!!document.querySelector('section[aria-label="Autres films"]')`,
    'real filter clear restores other films',
  )
  check(
    (await evaluate(page, 'location.href')) === publicURL &&
      slotCount() === beforeCount,
    'real filter clear restores results without refetch or URL mutation',
  )
  check(
    page.requests
      .filter((entry) => entry.path.endsWith('/watchlist/search'))
      .every(
        (entry) => entry.method === 'POST' && entry.bodyKeys.includes('query'),
      ),
    'real private catalogue query travels only in POST body',
  )
  await go(page, '/compte/watchlist')
  await until(
    page,
    `!!document.querySelector('button[aria-label="Retirer de la watchlist"]:not(:disabled)')`,
    'real saved movie ready for removal',
  )
  await evaluate(
    page,
    `document.querySelector('button[aria-label="Retirer de la watchlist"]').click()`,
  )
  await until(
    page,
    `document.querySelector('main').textContent.includes('Votre watchlist est vide')`,
    'real remove restored empty state',
  )
  const removed = await request(page, '/account/watchlist', undefined, 'GET')
  check(
    removed.status === 200 &&
      removed.body.items.length === 0 &&
      BigInt(removed.body.revision) > BigInt(saved.body.revision),
    'real remove advances revision and clears membership',
  )
  await go(page, searchPath)
  await until(
    page,
    `!!document.querySelector('section[aria-label="Autres films"]') && !document.querySelector('section[aria-label="Ma watchlist"]')`,
    'real removed movie no longer prioritized',
  )
  await evaluate(page, `${toggle}.click()`)
  await until(
    page,
    `document.querySelector('main').textContent.includes('Aucune séance de votre watchlist')`,
    'real empty watchlist-only result state',
  )
  console.log(
    'SKIP external import: TMDB intentionally disabled in backend fixture',
  )
  return true
}
