// DB-free browser contract: public metadata and private account ownership stay separate.
import { createServer } from 'node:http'
import { writeFile } from 'node:fs/promises'
import { setTimeout as delay } from 'node:timers/promises'

const palette = [
  ['neutral', 'Neutre', '#f4f4f5', '#3f3f46', '#71717a'],
  ['red', 'Rouge', '#fee2e2', '#991b1b', '#dc2626'],
  ['amber', 'Ambre', '#fef3c7', '#92400e', '#b45309'],
  ['green', 'Vert', '#dcfce7', '#166534', '#15803d'],
  ['teal', 'Sarcelle', '#ccfbf1', '#115e59', '#0f766e'],
  ['blue', 'Bleu', '#dbeafe', '#1e40af', '#2563eb'],
  ['violet', 'Violet', '#ede9fe', '#5b21b6', '#7c3aed'],
  ['rose', 'Rose', '#ffe4e6', '#9f1239', '#e11d48'],
]

async function checkPalette(page, evaluate, check) {
  check(
    await evaluate(
      page,
      `(() => {
    const expected = ${JSON.stringify(palette)};
    const radios = [...document.querySelectorAll('#watchlist-tag-manager form:first-of-type input[type="radio"]')];
    const rgb = hex => 'rgb(' + [1,3,5].map(i => parseInt(hex.slice(i, i+2), 16)).join(', ') + ')';
    const lum = rgb => { const c = rgb.match(/\\d+/g).map(n => { const x = Number(n)/255; return x <= .04045 ? x/12.92 : ((x+.055)/1.055)**2.4 }); return c[0]*.2126+c[1]*.7152+c[2]*.0722 };
    const contrast = (a,b) => (Math.max(lum(a),lum(b))+.05)/(Math.min(lum(a),lum(b))+.05);
    if (!radios.length) return false;
    const grid = getComputedStyle(radios[0].closest('label').parentElement);
    if (grid.gridTemplateColumns.split(' ').length !== (innerWidth >= 640 ? 4 : 2) || parseFloat(grid.gap) < 8) return false;
    return radios.length === 8 && new Set(radios.map(r => r.name)).size === 1 && radios.every((r,i) => {
      const label = r.closest('label'), style = getComputedStyle(label), marker = getComputedStyle(label.querySelector('[aria-hidden]')), rect = label.getBoundingClientRect(), token = expected[i];
      return r.value === token[0] && label.textContent.trim() === token[1] && marker.backgroundColor === rgb(token[2]) && marker.color === rgb(token[3]) && marker.borderColor === rgb(token[4]) && rect.height >= 44 && rect.width >= 44 && contrast(style.color, style.backgroundColor) >= 4.5 && contrast(style.borderColor, style.backgroundColor) >= 3 && contrast(marker.borderColor, marker.backgroundColor) >= 3;
    });
  })()`,
    ),
    'eight named native radios have compact exact-color markers, accessible tile contrast and touch targets',
  )
}

async function exerciseTagColors({
  page,
  evaluate,
  check,
  fill,
  click,
  until,
  name,
}) {
  const state = `JSON.stringify({filter:document.querySelector('#watchlist-tag-filter').value, films:[...document.querySelectorAll('section[aria-labelledby="saved-heading"] a[href^="/film/"]')].map(a=>a.pathname)})`
  const before = await evaluate(page, state)
  const chips = (tagName) =>
    `[...document.querySelectorAll('ul[aria-label="Tags associés"] li')].filter(chip => chip.textContent.trim() === ${JSON.stringify(tagName)})`
  check(
    await evaluate(
      page,
      `${chips(name)}.length === 2 && ${chips(name)}.every(chip => getComputedStyle(chip).backgroundColor === 'rgb(219, 234, 254)' && getComputedStyle(chip).color === 'rgb(30, 64, 175)' && getComputedStyle(chip).borderColor === 'rgb(37, 99, 235)')`,
    ),
    'atomic name/color update colors both assigned chips with exact blue tokens',
  )
  for (const [nextName, color, background] of [
    [name, 'violet', 'rgb(237, 233, 254)'],
    [`${name} +`, 'violet', 'rgb(237, 233, 254)'],
    [name, 'neutral', 'rgb(244, 244, 245)'],
  ]) {
    await evaluate(
      page,
      `document.querySelector(${JSON.stringify(`button[aria-label="Modifier ${name}"]`)}).click()`,
    )
    await fill(page, 'watchlist-tag-edit', nextName)
    await evaluate(
      page,
      `document.querySelector('#watchlist-tag-edit').form.querySelector('input[value="${color}"]').click()`,
    )
    await click(page, 'Enregistrer')
    await until(
      page,
      `!document.querySelector('#watchlist-tag-edit') && ${chips(nextName)}.length === 2 && ${chips(nextName)}.every(chip => getComputedStyle(chip).backgroundColor === ${JSON.stringify(background)})`,
      'color-only, name-only and neutral reset commit all assigned chips',
    )
    check(
      (await evaluate(page, state)) === before,
      'tag edits preserve filter ID and saved-film order',
    )
    name = nextName
  }
}

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
  const ownerTags = new Map()
  const ownerAssignments = new Map()
  let nextTagId = 1
  const tags = () => ownerTags.get(session.account.username) ?? []
  const assignments = () => {
    if (!ownerAssignments.has(session.account.username))
      ownerAssignments.set(session.account.username, new Map())
    return ownerAssignments.get(session.account.username)
  }
  const sorts = new Map()
  const addedTimes = new Map()
  let uncertainSort = false
  let uncertainAssignment = false
  const frenchReleases = new Map([['saved-film', '1998-10-14']])
  let externalStatus = 'ready'
  let emptySearch = false
  let conflict = false
  let hold = false
  let holdSearch = false
  let releaseSearch
  let holdRead = true
  let failRead = false
  let releaseRead
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
    sort_order: sorts.get(session.account.username) ?? 'added_desc',
    tags: tags(),
    items: saved.map((slug) => ({
      ...movie(slug),
      french_release_date: frenchReleases.get(slug),
      added_at: addedTimes.get(slug) ?? `${date}T00:00:00Z`,
      tag_ids: assignments().get(slug) ?? [],
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
      if (req.method === 'GET') {
        const value = snapshot()
        if (holdRead)
          await new Promise((resolve) => {
            releaseRead = resolve
          })
        if (failRead) {
          failRead = false
          return send({ error: { code: 'watchlist_unavailable' } }, 503)
        }
        return send(value)
      }
      if (conflict || body.expected_revision !== String(revision)) {
        conflict = false
        return send({ error: { code: 'watchlist_changed' } }, 409)
      }
      if (hold)
        await new Promise((resolve) => {
          release = resolve
        })
      if (path.endsWith('/sort')) {
        sorts.set(session.account.username, body.sort_order)
        revision++
        if (uncertainSort) {
          uncertainSort = false
          return send({ error: { code: 'watchlist_unavailable' } }, 503)
        }
        return send(snapshot())
      }
      if (path.includes('/tags')) {
        if (
          !['/tags', '/tags/update', '/tags/delete', '/tags/assign'].some(
            (suffix) => path === `/api/v1/account/watchlist${suffix}`,
          )
        )
          return send({ error: { code: 'not_found' } }, 404)
        if (
          (path.endsWith('/tags') || path.endsWith('/update')) &&
          !palette.some(([color]) => body.color === color)
        )
          return send({ error: { code: 'invalid_request' } }, 400)
        const name = body.name?.trim().replace(/\s+/gu, ' ').normalize('NFC')
        const existing = tags().find((tag) => tag.id === body.tag_id)
        if (
          body.name !== undefined &&
          (!name ||
            [...name].length > 40 ||
            /[\p{Cc}\u2028\u2029]/u.test(body.name))
        )
          return send({ error: { code: 'invalid_request' } }, 400)
        if (
          name &&
          tags().some(
            (tag) =>
              tag.id !== body.tag_id &&
              tag.name.toLocaleLowerCase('fr') === name.toLocaleLowerCase('fr'),
          )
        )
          return send({ error: { code: 'watchlist_tag_name_taken' } }, 409)
        if (!path.endsWith('/tags') && !existing)
          return send({ error: { code: 'watchlist_tag_not_found' } }, 404)
        if (path.endsWith('/tags') && tags().length >= 50)
          return send({ error: { code: 'watchlist_tag_limit_reached' } }, 409)
        if (path.endsWith('/tags'))
          ownerTags.set(session.account.username, [
            ...tags(),
            { id: String(nextTagId++), name, color: body.color },
          ])
        else if (path.endsWith('/update')) {
          existing.name = name
          existing.color = body.color
        } else if (path.endsWith('/delete')) {
          ownerTags.set(
            session.account.username,
            tags().filter((tag) => tag.id !== body.tag_id),
          )
          for (const [slug, ids] of assignments())
            assignments().set(
              slug,
              ids.filter((id) => id !== body.tag_id),
            )
        } else if (path.endsWith('/assign')) {
          if (!saved.includes(body.movie_slug))
            return send({ error: { code: 'watchlist_movie_not_saved' } }, 404)
          const ids = (assignments().get(body.movie_slug) ?? []).filter(
            (id) => id !== body.tag_id,
          )
          if (body.assigned === 'true') ids.push(body.tag_id)
          assignments().set(
            body.movie_slug,
            ids.sort((a, b) =>
              BigInt(a) < BigInt(b) ? -1 : BigInt(a) > BigInt(b) ? 1 : 0,
            ),
          )
        }
        revision++
        if (path.endsWith('/assign') && uncertainAssignment) {
          uncertainAssignment = false
          return send({ error: { code: 'watchlist_unavailable' } }, 503)
        }
        return send(snapshot())
      }
      const slug = path.endsWith('/import') ? 'external-film' : body.movie_slug
      saved = saved.filter((item) => item !== slug)
      if (body.saved === 'false') assignments().delete(slug)
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
  async function screenshot(page, name, captureBeyondViewport = true) {
    if (!process.argv.includes('--visual')) return
    const image = await getCDP().send(
      'Page.captureScreenshot',
      { format: 'png', captureBeyondViewport },
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
  const savedOrder = `[...document.querySelectorAll('section[aria-labelledby="saved-heading"] li a')].map(node => node.getAttribute('href').split('/').at(-1))`
  async function checkLoadingLayout(viewport) {
    await until(
      page,
      `!!document.querySelector('section[aria-labelledby="saved-heading"] [role="status"]')`,
      'saved-list loading status visible',
    )
    check(
      await evaluate(
        page,
        `(() => {
          const input = document.querySelector('#watchlist-query');
          const form = input.closest('form');
          const section = document.querySelector('section[aria-labelledby="saved-heading"]');
          const heading = section.querySelector('#saved-heading');
          const select = section.querySelector('#watchlist-sort');
          const status = section.querySelector('[role="status"]');
          const icon = select?.parentElement.querySelector('svg[aria-hidden="true"]');
          const nodes = [form, heading, icon, select, status];
          return nodes.every(node => node?.getBoundingClientRect().width > 0)
            && nodes.every((node, index) => !index || (nodes[index - 1].compareDocumentPosition(node) & Node.DOCUMENT_POSITION_FOLLOWING))
            && document.querySelectorAll('main [role="status"]').length === 1
            && status.textContent.trim() === 'Chargement de la watchlist…'
            && !section.querySelector('li') && !section.textContent.includes('Votre watchlist est vide')
            && input.disabled && select.disabled && select.value === ''
            && select.selectedOptions[0].disabled && select.selectedOptions[0].textContent.trim() === 'Trier par'
            && select.labels[0].textContent.trim() === 'Trier par' && select.labels[0].classList.contains('sr-only')
            && document.documentElement.scrollWidth <= innerWidth
            && form.getBoundingClientRect().bottom <= heading.getBoundingClientRect().top
            && select.getBoundingClientRect().bottom <= status.getBoundingClientRect().top;
        })()`,
      ),
      `${viewport} loading keeps search then heading/icon/disabled neutral select then one skeleton, without stale rows or empty-state flash`,
    )
  }
  async function finishRead() {
    for (let i = 0; !releaseRead && i < 100; i++) await delay(20)
    check(!!releaseRead, 'held private watchlist GET reached fixture')
    holdRead = false
    releaseRead()
    releaseRead = undefined
  }
  async function selectSort(value) {
    await until(
      page,
      `!!document.querySelector('#watchlist-sort:not(:disabled)')`,
      'sort control available',
    )
    await evaluate(
      page,
      `(() => { const select = document.querySelector('#watchlist-sort'); select.value = ${JSON.stringify(value)}; select.dispatchEvent(new Event('change', { bubbles: true })); })()`,
    )
    await until(
      page,
      `document.querySelector('#watchlist-sort:not(:disabled)')?.value === ${JSON.stringify(value)}`,
      'committed sort selection',
    )
  }
  await launch()
  const page = await tab()
  await getCDP().send('Page.bringToFront', {}, page.sessionId)
  await getCDP().send(
    'Emulation.setDeviceMetricsOverride',
    { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false },
    page.sessionId,
  )
  await go(page, '/compte/watchlist')
  await checkLoadingLayout('desktop')
  await screenshot(page, 'loading-desktop')
  const loadingAccessibility = await getCDP().send(
    'Accessibility.getFullAXTree',
    {},
    page.sessionId,
  )
  check(
    loadingAccessibility.nodes.some(
      (node) =>
        node.role?.value === 'combobox' &&
        node.name?.value === 'Trier par' &&
        node.properties?.some(
          (property) => property.name === 'disabled' && property.value?.value,
        ),
    ),
    'loading native selector exposes accessible name and disabled state',
  )
  await getCDP().send(
    'Emulation.setDeviceMetricsOverride',
    { width: 390, height: 844, deviceScaleFactor: 1, mobile: true },
    page.sessionId,
  )
  await checkLoadingLayout('mobile')
  await screenshot(page, 'loading-mobile')
  await getCDP().send(
    'Emulation.setDeviceMetricsOverride',
    { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false },
    page.sessionId,
  )
  failRead = true
  await finishRead()
  await until(
    page,
    `!!document.querySelector('main [role="alert"]')`,
    'initial read failure replaces loading feedback',
  )
  check(
    await evaluate(
      page,
      `(() => { const section = document.querySelector('section[aria-labelledby="saved-heading"]'); const select = section.querySelector('#watchlist-sort'); return !document.querySelector('main [role="status"]') && document.querySelectorAll('main [role="alert"]').length === 1 && select.disabled && select.value === '' && select.selectedOptions[0].textContent.trim() === 'Tri indisponible' && !section.querySelector('li') && !section.textContent.includes('Votre watchlist est vide'); })()`,
    ),
    'failed initial read retains heading and disabled neutral sort without skeleton or false empty state',
  )
  await click(page, 'Réessayer')
  await until(
    page,
    `document.querySelector('main').textContent.includes('Votre watchlist est vide')`,
    'watchlist empty state',
  )
  check(
    await evaluate(
      page,
      `!document.querySelector('main [role="status"], main [role="alert"], #watchlist-sort option[value=""]')`,
    ),
    'successful retry replaces loading/error placeholder with ready empty state',
  )
  check(
    await evaluate(
      page,
      `!document.querySelector('#watchlist-query').disabled && document.querySelectorAll('h1').length === 1`,
    ),
    'watchlist form enabled with single heading',
  )
  check(
    await evaluate(
      page,
      `(() => { const select = document.querySelector('#watchlist-sort'); const label = select.labels[0]; const icon = select.parentElement.querySelector('svg[aria-hidden="true"]'); const bounds = icon?.getBoundingClientRect(); return select.value === 'added_desc' && select.options.length === 6 && label.textContent.trim() === 'Trier par' && label.classList.contains('sr-only') && getComputedStyle(label).position === 'absolute' && bounds?.width === 20 && getComputedStyle(icon).pointerEvents === 'none' && getComputedStyle(select).appearance === 'auto' && document.elementFromPoint(bounds.left + bounds.width / 2, bounds.top + bounds.height / 2) === select; })()`,
    ),
    'empty watchlist has screen-reader labeled six-option native selector and click-through decorative sort icon',
  )
  const accessibility = await getCDP().send(
    'Accessibility.getFullAXTree',
    {},
    page.sessionId,
  )
  check(
    accessibility.nodes.some(
      (node) =>
        node.role?.value === 'combobox' && node.name?.value === 'Trier par',
    ),
    'native sort selector retains Trier par accessible name',
  )
  check(
    await evaluate(
      page,
      `(() => { const select = document.querySelector('#watchlist-sort'); const control = select.getBoundingClientRect(); const style = getComputedStyle(select); const icon = select.parentElement.querySelector('svg').getBoundingClientRect(); const filter = document.querySelector('#watchlist-tag-filter').getBoundingClientRect(); const trigger = document.querySelector('button[aria-controls="watchlist-tag-manager"]').getBoundingClientRect(); return control.height >= 44 && icon.left > control.left && icon.right < control.right && icon.top > control.top && icon.bottom < control.bottom && control.left + parseFloat(style.borderLeftWidth) + parseFloat(style.paddingLeft) >= icon.right + 8 && Math.abs(icon.top + icon.height / 2 - control.top - control.height / 2) < 1 && Math.abs(filter.bottom - control.bottom) < 1 && Math.abs(trigger.bottom - control.bottom) < 1; })()`,
    ),
    'desktop sort icon sits inside select with text clearance and filter/manager alignment',
  )
  await click(page, 'Gérer les tags')
  check(
    await evaluate(
      page,
      `!document.querySelector('#watchlist-tag-manager form, #watchlist-tag-manager fieldset') && document.querySelector('button[aria-controls="watchlist-tag-create"]').getAttribute('aria-expanded') === 'false'`,
    ),
    'manager opens list-first without expanded creation or color picker',
  )
  check(
    await evaluate(
      page,
      `document.querySelector('#watchlist-tag-manager').matches(':modal') && document.activeElement.getAttribute('aria-label') === 'Fermer la gestion des tags'`,
    ),
    'manager uses native modal top layer and initially focuses close control',
  )
  const modalAccessibility = await getCDP().send(
    'Accessibility.getFullAXTree',
    {},
    page.sessionId,
  )
  check(
    modalAccessibility.nodes.some(
      (node) =>
        node.role?.value === 'dialog' &&
        node.name?.value === 'Gérer les tags' &&
        node.properties?.some(
          (property) => property.name === 'modal' && property.value?.value,
        ),
    ),
    'tag modal exposes accessible title and modal state',
  )
  for (let index = 0; index < 8; index++) {
    for (const type of ['keyDown', 'keyUp']) {
      await getCDP().send(
        'Input.dispatchKeyEvent',
        { type, key: 'Tab', code: 'Tab', windowsVirtualKeyCode: 9 },
        page.sessionId,
      )
    }
    check(
      await evaluate(
        page,
        `document.querySelector('#watchlist-tag-manager').contains(document.activeElement) || document.activeElement === document.body`,
      ),
      'native modal Tab does not reach background controls',
    )
  }
  await click(page, 'Créer un tag')
  check(
    await evaluate(
      page,
      `document.activeElement.id === 'watchlist-tag-name' && document.querySelectorAll('#watchlist-tag-manager form').length === 1`,
    ),
    'create opener expands and focuses sole form',
  )
  await fill(page, 'watchlist-tag-name', '<b>Amis</b>')
  await checkPalette(page, evaluate, check)
  await click(page, 'Créer')
  await until(
    page,
    `!document.querySelector('#watchlist-tag-name') && document.querySelector('#watchlist-tag-filter').options.length === 2`,
    'create reusable tag with empty watchlist',
  )
  check(
    await evaluate(
      page,
      `document.activeElement.getAttribute('aria-controls') === 'watchlist-tag-create'`,
    ),
    'confirmed unchanged create restores focus to opener',
  )
  check(
    saved.length === 0 &&
      tags()[0]?.name === '<b>Amis</b>' &&
      tags()[0]?.color === 'neutral',
    'tag-only account does not save a film',
  )
  check(
    await evaluate(
      page,
      `!document.querySelector('#watchlist-tag-manager b') && document.querySelector('#watchlist-tag-manager').textContent.includes('<b>Amis</b>')`,
    ),
    'tag names render escaped text, not HTML',
  )
  await click(page, 'Créer un tag')
  await fill(page, 'watchlist-tag-name', '<b>amis</b>')
  await evaluate(
    page,
    `document.querySelector('#watchlist-tag-name').form.querySelector('input[value="neutral"]').focus()`,
  )
  for (const type of ['keyDown', 'keyUp']) {
    await getCDP().send(
      'Input.dispatchKeyEvent',
      {
        type,
        key: 'ArrowRight',
        code: 'ArrowRight',
        windowsVirtualKeyCode: 39,
      },
      page.sessionId,
    )
  }
  check(
    await evaluate(
      page,
      `(() => { const radio = document.activeElement; const style = getComputedStyle(radio.closest('label')); return radio.value === 'red' && radio.checked && radio.matches(':focus-visible') && style.outlineWidth === '2px' && style.outlineColor === 'rgb(39, 39, 42)' && style.outlineOffset === '2px'; })()`,
    ),
    'native arrow key selects named color and exposes 2px high-contrast separated focus outline',
  )
  await getCDP().send(
    'Emulation.setEmulatedMedia',
    { features: [{ name: 'forced-colors', value: 'active' }] },
    page.sessionId,
  )
  check(
    await evaluate(
      page,
      `(() => { const radio = document.activeElement; return radio.checked && radio.value === 'red' && getComputedStyle(radio).appearance !== 'none' && getComputedStyle(radio).forcedColorAdjust !== 'none' && getComputedStyle(radio.closest('label')).outlineStyle !== 'none'; })()`,
    ),
    'forced colors preserves native checked state and visible focus',
  )
  await getCDP().send(
    'Emulation.setEmulatedMedia',
    { features: [] },
    page.sessionId,
  )
  await click(page, 'Créer')
  await until(
    page,
    `document.querySelector('#watchlist-tag-manager [role="alert"]')?.textContent.includes('existe déjà')`,
    'duplicate tag safe error',
  )
  check(
    await evaluate(
      page,
      `document.querySelector('#watchlist-tag-name').value === '<b>amis</b>' && document.querySelector('#watchlist-tag-name').form.querySelector('input[value="red"]').checked`,
    ),
    'duplicate create retains draft after readback',
  )
  await fill(
    page,
    'watchlist-tag-name',
    'À revoir au cinéma avec tous les amis',
  )
  await click(page, 'Créer')
  await until(
    page,
    `document.querySelector('#watchlist-tag-filter').options.length === 3`,
    'second reusable tag',
  )
  check(
    tags()[1].color === 'red' &&
      (await evaluate(page, `!document.querySelector('#watchlist-tag-name')`)),
    'nonneutral create persists selected color and collapses unchanged form',
  )
  await click(page, 'Créer un tag')
  check(
    await evaluate(
      page,
      `document.querySelector('#watchlist-tag-name').value === '' && document.querySelector('#watchlist-tag-create input[value="neutral"]').checked`,
    ),
    'fresh creation resets name and color to neutral',
  )
  await click(page, 'Annuler')
  check(
    await evaluate(
      page,
      `!document.querySelector('#watchlist-tag-create') && document.activeElement.getAttribute('aria-controls') === 'watchlist-tag-create'`,
    ),
    'cancel creation returns focus to connected opener',
  )
  await evaluate(
    page,
    `document.querySelector('button[aria-label="Fermer la gestion des tags"]').click()`,
  )
  check(
    await evaluate(
      page,
      `!document.querySelector('#watchlist-tag-manager') && document.activeElement.getAttribute('aria-controls') === 'watchlist-tag-manager'`,
    ),
    'modal close restores original manager trigger',
  )
  await click(page, 'Gérer les tags')
  for (const type of ['mousePressed', 'mouseReleased']) {
    await getCDP().send(
      'Input.dispatchMouseEvent',
      { type, x: 2, y: 2, button: 'left', clickCount: 1 },
      page.sessionId,
    )
  }
  check(
    await evaluate(
      page,
      `!document.querySelector('#watchlist-tag-manager') && document.activeElement.getAttribute('aria-controls') === 'watchlist-tag-manager'`,
    ),
    'native modal backdrop click closes and restores trigger',
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
  async function toggleAssignment(slug, tagName, assigned) {
    await evaluate(
      page,
      `(() => { const row = ${savedRow(slug)}; const button = row.querySelector('button[aria-expanded]'); if (button.getAttribute('aria-expanded') !== 'true') button.click(); })()`,
    )
    await until(
      page,
      `${savedRow(slug)}?.querySelector('input[type="checkbox"]:not(:disabled)')`,
      'row editor available',
    )
    await evaluate(
      page,
      `(() => { const row = ${savedRow(slug)}; const label = [...row.querySelectorAll('label')].find(label => label.textContent.trim() === ${JSON.stringify(tagName)}); const input = label.querySelector('input'); input.focus(); input.click(); })()`,
    )
    await until(
      page,
      `!document.querySelector('#watchlist-sort').disabled && (() => { const row = ${savedRow(slug)}; if (!row) return true; const label = [...row.querySelectorAll('label')].find(label => label.textContent.trim() === ${JSON.stringify(tagName)}); return label?.querySelector('input').checked === ${assigned}; })()`,
      'committed tag assignment',
    )
  }
  const filterTag = async (id) => {
    await evaluate(
      page,
      `(() => { const select = document.querySelector('#watchlist-tag-filter'); select.value = ${JSON.stringify(id)}; select.dispatchEvent(new Event('change', { bubbles: true })); })()`,
    )
    await delay(50)
  }
  const firstTag = tags()[0].id
  const secondTag = tags()[1].id
  const picker = `${savedRow('saved-film')}.querySelector('[role="group"]')`
  const rowHeight = await evaluate(
    page,
    `${savedRow('saved-film')}.getBoundingClientRect().height`,
  )
  const listHeight = await evaluate(
    page,
    'document.documentElement.scrollHeight',
  )
  await evaluate(
    page,
    `${savedRow('saved-film')}.querySelector('button[aria-expanded]').click()`,
  )
  const firstCheckbox = `[...${savedRow('saved-film')}.querySelectorAll('label')].find(label => label.textContent.trim() === '<b>Amis</b>').querySelector('input')`
  check(
    await evaluate(
      page,
      `(() => { const p = ${picker}, r = p.getBoundingClientRect(), button = ${savedRow('saved-film')}.querySelector('button[aria-expanded]'); return getComputedStyle(p).position === 'fixed' && r.width <= 320 && r.left >= 8 && r.right <= innerWidth - 8 && r.top >= 8 && r.bottom <= innerHeight - 8 && document.activeElement === ${firstCheckbox} && button.textContent.trim() === 'Tag' && button.getBoundingClientRect().height >= 44 && ${savedRow('saved-film')}.getBoundingClientRect().height === ${rowHeight} && document.documentElement.scrollHeight === ${listHeight}; })()`,
    ),
    'compact film-anchored picker focuses first checkbox without expanding row or page',
  )
  const pickerAX = await getCDP().send(
    'Accessibility.getFullAXTree',
    {},
    page.sessionId,
  )
  check(
    pickerAX.nodes.some(
      (node) =>
        node.role?.value === 'group' &&
        node.name?.value === 'Tags de Film favori',
    ),
    'picker group exposes film-specific title',
  )
  await screenshot(page, 'tag-picker-desktop', false)
  await evaluate(
    page,
    `document.querySelector('#saved-heading').dispatchEvent(new PointerEvent('pointerdown', {bubbles:true}))`,
  )
  check(
    await evaluate(page, `!${picker}`),
    'outside pointer dismisses compact picker',
  )
  await evaluate(
    page,
    `${savedRow('saved-film')}.querySelector('button[aria-expanded]').click()`,
  )
  for (let index = 0; index < 2; index++) {
    for (const type of ['keyDown', 'keyUp'])
      await getCDP().send(
        'Input.dispatchKeyEvent',
        { type, key: 'Tab', code: 'Tab', windowsVirtualKeyCode: 9 },
        page.sessionId,
      )
  }
  check(
    await evaluate(
      page,
      `!${picker} && document.activeElement.getAttribute('aria-label') === 'Retirer de la watchlist'`,
    ),
    'Tab traverses choices then leaves nonmodal picker without focus theft',
  )
  await evaluate(
    page,
    `${savedRow('saved-film')}.querySelector('button[aria-expanded]').click()`,
  )
  await evaluate(page, `${firstCheckbox}.focus()`)
  hold = true
  release = undefined
  await getCDP().send(
    'Input.dispatchKeyEvent',
    { type: 'keyDown', key: ' ', code: 'Space', windowsVirtualKeyCode: 32 },
    page.sessionId,
  )
  await getCDP().send(
    'Input.dispatchKeyEvent',
    { type: 'keyUp', key: ' ', code: 'Space', windowsVirtualKeyCode: 32 },
    page.sessionId,
  )
  await until(page, `${firstCheckbox}.disabled`, 'keyboard tag write pending')
  check(
    await evaluate(
      page,
      `!${firstCheckbox}.checked && !document.querySelector('ul[aria-label="Tags associés"]') && document.querySelector('#watchlist-sort').disabled`,
    ),
    'pending native checkbox stays committed and blocks shared writers',
  )
  for (let i = 0; !release && i < 100; i++) await delay(20)
  check(!!release, 'held keyboard tag assignment reached fixture')
  hold = false
  release()
  await until(
    page,
    `${firstCheckbox}.checked && !${firstCheckbox}.disabled`,
    'keyboard assignment commits',
  )
  conflict = true
  holdRead = true
  releaseRead = undefined
  const assignmentsBeforeFailure = writes.filter((write) =>
    write.path.endsWith('/tags/assign'),
  ).length
  await evaluate(page, `${firstCheckbox}.click()`)
  for (let i = 0; !releaseRead && i < 100; i++) await delay(20)
  check(
    await evaluate(
      page,
      `!!${picker} && ${firstCheckbox}.checked && ${firstCheckbox}.disabled`,
    ),
    'uncertain reconciliation retains open picker and committed disabled checkbox',
  )
  failRead = true
  await finishRead()
  await until(
    page,
    `!!${picker}?.querySelector('[role="alert"]') && !${picker}.querySelector('[role="alert"] button').disabled`,
    'failed assignment and failed readback remain recoverable inside picker',
  )
  await click(page, 'Actualiser les tags')
  await until(
    page,
    `!!${picker} && !${firstCheckbox}.disabled && !${picker}.querySelector('[role="alert"]')`,
    'explicit refresh reconciles without closing picker',
  )
  check(
    writes.filter((write) => write.path.endsWith('/tags/assign')).length ===
      assignmentsBeforeFailure + 1,
    'error refresh never replays assignment',
  )
  check(
    await evaluate(page, `${firstCheckbox}.checked`),
    'rejected checkbox restores authoritative checked state',
  )
  uncertainAssignment = true
  await toggleAssignment('saved-film', tags()[1].name, true)
  check(
    await evaluate(
      page,
      `!!${picker} && !!${picker}.querySelector('[role="alert"]') && ${picker}.querySelectorAll('input:checked').length === 2`,
    ),
    'uncertain committed second choice reads back both assignments and keeps panel open with error',
  )
  await toggleAssignment('external-film', '<b>Amis</b>', true)
  check(
    await evaluate(
      page,
      `document.querySelectorAll('section[aria-labelledby="saved-heading"] input[type="checkbox"]').length === 2`,
    ),
    'only one compact row picker mounted',
  )
  const filterRequests = requests.length
  await filterTag(secondTag)
  check(
    await evaluate(page, `JSON.stringify(${savedOrder}) === '["saved-film"]'`),
    'single tag filter selects matching saved film',
  )
  check(requests.length === filterRequests, 'filter does not fetch or persist')
  await toggleAssignment('saved-film', tags()[1].name, false)
  await until(
    page,
    `document.querySelector('main').textContent.includes('Aucun film avec ce tag.')`,
    'filtered empty state after unassignment',
  )
  check(
    await evaluate(
      page,
      `document.activeElement.id === 'watchlist-tag-filter'`,
    ),
    'filtered row disappearance restores filter focus',
  )
  await click(page, 'Voir tous les films')
  await filterTag(firstTag)
  await click(page, 'Gérer les tags')
  await click(page, 'Créer un tag')
  await fill(page, 'watchlist-tag-name', 'Brouillon conservé')
  await evaluate(
    page,
    `document.querySelector('button[aria-label="Modifier <b>Amis</b>"]').click()`,
  )
  check(
    await evaluate(
      page,
      `document.activeElement.id === 'watchlist-tag-edit' && !document.querySelector('#watchlist-tag-create')`,
    ),
    'edit collapses creation and focuses name',
  )
  await click(page, 'Créer un tag')
  check(
    await evaluate(
      page,
      `!document.querySelector('#watchlist-tag-edit') && document.querySelector('#watchlist-tag-name').value === 'Brouillon conservé'`,
    ),
    'creation closes edit and preserves same-owner create draft',
  )
  await evaluate(
    page,
    `document.querySelector('button[aria-label="Supprimer <b>Amis</b>"]').click()`,
  )
  check(
    await evaluate(
      page,
      `!document.querySelector('#watchlist-tag-create') && !document.querySelector('#watchlist-tag-edit') && document.activeElement.id === 'watchlist-tag-delete-cancel'`,
    ),
    'delete closes creation and focuses safe cancellation',
  )
  await click(page, 'Annuler')
  check(
    await evaluate(
      page,
      `document.activeElement.getAttribute('aria-label') === 'Supprimer <b>Amis</b>' && !document.querySelector('#watchlist-tag-delete-cancel')`,
    ),
    'cancel deletion restores row action without mutation',
  )
  await evaluate(
    page,
    `document.querySelector('button[aria-label="Supprimer <b>Amis</b>"]').click()`,
  )
  await click(page, 'Créer un tag')
  check(
    await evaluate(
      page,
      `!document.querySelector('#watchlist-tag-delete-cancel') && document.querySelectorAll('#watchlist-tag-manager form').length === 1`,
    ),
    'creation closes delete confirmation',
  )
  await click(page, 'Annuler')
  await evaluate(
    page,
    `document.querySelector('button[aria-label="Modifier <b>Amis</b>"]').click()`,
  )
  await fill(page, 'watchlist-tag-edit', 'Soirée cinéma')
  await evaluate(
    page,
    `document.querySelector('#watchlist-tag-edit').form.querySelector('input[value="blue"]').click()`,
  )
  check(
    await evaluate(
      page,
      `new Set([...document.querySelectorAll('#watchlist-tag-manager input[type="radio"]')].map(input => input.name)).size === 1 && document.querySelectorAll('#watchlist-tag-manager form').length === 1 && !document.querySelector('#watchlist-tag-create')`,
    ),
    'only active edit palette and its single native radio group are mounted',
  )
  await screenshot(page, 'tag-color-edit-desktop', false)
  await click(page, 'Enregistrer')
  await until(
    page,
    `!document.querySelector('#watchlist-tag-edit') && document.querySelector('#watchlist-tag-filter').selectedOptions[0].textContent === 'Soirée cinéma'`,
    'rename preserves selected ID',
  )
  check(
    await evaluate(
      page,
      `document.querySelectorAll('ul[aria-label="Tags associés"]').length === 2 && [...document.querySelectorAll('ul[aria-label="Tags associés"]')].every(list => list.textContent.includes('Soirée cinéma'))`,
    ),
    'rename updates every film chip',
  )
  await exerciseTagColors({
    page,
    evaluate,
    check,
    fill,
    click,
    until,
    name: 'Soirée cinéma',
  })
  // Leave a committed nonneutral chip visible for desktop/mobile captures.
  await evaluate(
    page,
    `document.querySelector('button[aria-label="Modifier Soirée cinéma"]').click()`,
  )
  await evaluate(
    page,
    `document.querySelector('#watchlist-tag-edit').form.querySelector('input[value="blue"]').click()`,
  )
  await click(page, 'Enregistrer')
  await until(
    page,
    `!document.querySelector('#watchlist-tag-edit')`,
    'blue color-only update committed',
  )
  const beforeCancel = writes.length
  await evaluate(
    page,
    `document.querySelector('button[aria-label="Modifier Soirée cinéma"]').click()`,
  )
  await evaluate(
    page,
    `document.querySelector('#watchlist-tag-edit').form.querySelector('input[value="amber"]').click()`,
  )
  await click(page, 'Annuler')
  check(
    await evaluate(
      page,
      `document.activeElement.getAttribute('aria-label') === 'Modifier Soirée cinéma' && !document.querySelector('#watchlist-tag-manager form')`,
    ),
    'cancel edit restores row action without opening another form',
  )
  check(
    writes.length === beforeCancel &&
      tags().find((tag) => tag.id === firstTag).color === 'blue',
    'cancel discards draft color without a mutation',
  )
  check(
    await evaluate(
      page,
      `!['Soirée cinéma', '<b>Amis</b>', 'tag_ids', 'selectedTag', 'editDraft', 'editColor'].some(marker => JSON.stringify({url:location.href,local:{...localStorage},session:{...sessionStorage},payload:window.__NUXT__,data:document.querySelector('#__nuxt').__vue_app__.$nuxt.payload.data}).includes(marker))`,
    ),
    'active private tags filter and drafts stay out of URL storage and Nuxt payload',
  )
  check(
    !['Soirée cinéma', '<b>Amis</b>', 'tag_ids'].some((marker) =>
      JSON.stringify(page.collections).includes(marker),
    ),
    'tag values never enter analytics',
  )
  await evaluate(page, 'window.scrollTo(0, 0)')
  await delay(50)
  await screenshot(page, 'tags-desktop', false)
  await getCDP().send(
    'Emulation.setDeviceMetricsOverride',
    { width: 320, height: 844, deviceScaleFactor: 1, mobile: true },
    page.sessionId,
  )
  check(
    await evaluate(
      page,
      `(() => { const dialog = document.querySelector('#watchlist-tag-manager'); const body = dialog.querySelector('.overflow-y-auto'); const rect = body.getBoundingClientRect(); return dialog.matches(':modal') && rect.top >= 0 && rect.bottom <= innerHeight && dialog.scrollWidth <= innerWidth && body.scrollWidth <= body.clientWidth; })()`,
    ),
    '320px modal body stays viewport bounded without horizontal overflow',
  )
  await screenshot(page, 'tag-manager-mobile', false)
  await click(page, 'Créer un tag')
  await checkPalette(page, evaluate, check)
  await screenshot(page, 'tag-create-mobile', false)
  await getCDP().send(
    'Emulation.setDeviceMetricsOverride',
    { width: 320, height: 320, deviceScaleFactor: 1, mobile: true },
    page.sessionId,
  )
  check(
    await evaluate(
      page,
      `(() => { const modal = document.querySelector('#watchlist-tag-manager'); const body = modal.querySelector('.overflow-y-auto'); const close = modal.querySelector('button[aria-label="Fermer la gestion des tags"]').getBoundingClientRect(); return body.scrollHeight > body.clientHeight && body.getBoundingClientRect().bottom <= innerHeight && close.top >= 0 && close.bottom <= innerHeight; })()`,
    ),
    'short viewport scrolls modal body while close control remains visible',
  )
  await screenshot(page, 'tag-manager-short-viewport', false)
  await getCDP().send(
    'Emulation.setDeviceMetricsOverride',
    { width: 320, height: 844, deviceScaleFactor: 1, mobile: true },
    page.sessionId,
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
  await getCDP().send(
    'Input.dispatchKeyEvent',
    { type: 'keyUp', key: 'Escape', code: 'Escape', windowsVirtualKeyCode: 27 },
    page.sessionId,
  )
  check(
    await evaluate(
      page,
      `!document.querySelector('#watchlist-tag-manager') && document.activeElement.getAttribute('aria-controls') === 'watchlist-tag-manager'`,
    ),
    'Escape dismisses native modal and restores manager trigger',
  )
  await evaluate(page, `window.scrollTo(0, 0)`)
  await screenshot(page, 'saved-tags-mobile')
  await toggleAssignment('saved-film', 'Soirée cinéma', false)
  await evaluate(
    page,
    `(() => { const button = document.querySelector('button[aria-label="Modifier les tags de Film externe"]'); button.focus(); button.click(); })()`,
  )
  check(
    await evaluate(page, `document.documentElement.scrollWidth <= innerWidth`),
    '320px long tags and native checkbox editor do not overflow',
  )
  await evaluate(
    page,
    `document.querySelector('#saved-heading').scrollIntoView({block:'start'})`,
  )
  await delay(50)
  await screenshot(page, 'tags-mobile', false)
  await getCDP().send(
    'Input.dispatchKeyEvent',
    { type: 'keyDown', key: 'Escape', code: 'Escape' },
    page.sessionId,
  )
  await getCDP().send(
    'Input.dispatchKeyEvent',
    { type: 'keyUp', key: 'Escape', code: 'Escape' },
    page.sessionId,
  )
  check(
    await evaluate(
      page,
      `document.activeElement.getAttribute('aria-label') === 'Modifier les tags de Film externe' && document.activeElement.getAttribute('aria-expanded') === 'false' && !document.querySelector('section[aria-labelledby="saved-heading"] input[type="checkbox"]')`,
    ),
    'Escape closes tag checkbox disclosure and restores its trigger focus',
  )
  const ordinaryTags = [...tags()]
  ownerTags.set(owner.username, [
    ...ordinaryTags,
    ...Array.from({ length: 12 }, (_, index) => ({
      id: String(nextTagId++),
      name: `Sélection cinéma ${index + 1}`,
      color: palette[index % palette.length][0],
    })),
  ])
  revision++
  await evaluate(page, `window.dispatchEvent(new Event('focus'))`)
  await until(
    page,
    `document.querySelector('#watchlist-tag-filter:not(:disabled)')?.options.length === 15`,
    'long picker fixture revalidated',
  )
  for (const [name, width, height] of [
    ['mobile', 320, 844],
    ['short-viewport', 320, 320],
  ]) {
    await getCDP().send(
      'Emulation.setDeviceMetricsOverride',
      { width, height, deviceScaleFactor: 1, mobile: true },
      page.sessionId,
    )
    await evaluate(
      page,
      `(() => { const button = document.querySelector('button[aria-label="Modifier les tags de Film externe"]'); button.scrollIntoView({block:'end'}); if(button.getAttribute('aria-expanded') !== 'true') button.click(); })()`,
    )
    await delay(100)
    check(
      await evaluate(
        page,
        `(() => { const p = document.querySelector('section[aria-labelledby="saved-heading"] [role="group"]'), r = p.getBoundingClientRect(), scroll = p.querySelector('.overflow-y-auto'), close = p.querySelector('button'), c = close.getBoundingClientRect(); return r.left >= 8 && r.right <= innerWidth - 8 && r.top >= 8 && r.bottom <= innerHeight - 8 && scroll.scrollHeight > scroll.clientHeight && scroll.clientHeight > 0 && p.scrollWidth <= p.clientWidth && [...p.querySelectorAll('label')].every(label=>label.getBoundingClientRect().height >= 44) && close.contains(document.elementFromPoint(c.left+c.width/2,c.top+c.height/2)); })()`,
      ),
      `${name} picker clamps all edges, scrolls internally and retains reachable close control`,
    )
    await screenshot(page, `tag-picker-${name}`, false)
    await evaluate(
      page,
      `document.querySelector('button[aria-label="Fermer les tags"]').click()`,
    )
  }
  ownerTags.set(owner.username, ordinaryTags)
  revision++
  await evaluate(
    page,
    `document.querySelector('button[aria-label="Modifier les tags de Film externe"]').click()`,
  )
  await evaluate(page, `window.dispatchEvent(new Event('pagehide'))`)
  check(
    await evaluate(
      page,
      `!document.querySelector('section[aria-labelledby="saved-heading"] [role="group"]') && !document.querySelector('button[aria-label="Modifier les tags de Film externe"]')`,
    ),
    'pagehide removes picker and its private options immediately',
  )
  await getCDP().send(
    'Emulation.setDeviceMetricsOverride',
    { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false },
    page.sessionId,
  )
  await go(page, '/compte/watchlist')
  await until(
    page,
    `!!document.querySelector('#watchlist-tag-filter:not(:disabled)')`,
    'tags survive reload',
  )
  await evaluate(
    page,
    `${savedRow('saved-film')}.querySelector('button[aria-expanded]').click()`,
  )
  const priorSaved = saved
  session = {
    enabled: true,
    state: 'complete',
    account: { ...owner, username: 'picker_replacement_owner' },
  }
  saved = []
  await evaluate(page, `window.dispatchEvent(new Event('focus'))`)
  await until(
    page,
    `!document.querySelector('#watchlist-sort')`,
    'picker owner invalidation',
  )
  check(
    await evaluate(
      page,
      `!document.querySelector('section[aria-labelledby="saved-heading"] [role="group"]') && !document.activeElement.getAttribute('aria-label')?.startsWith('Modifier les tags de')`,
    ),
    'owner change removes picker and never restores old-owner focus',
  )
  session = { enabled: true, state: 'complete', account: owner }
  saved = priorSaved
  await go(page, '/compte/watchlist')
  await until(
    page,
    `!!document.querySelector('#watchlist-tag-filter:not(:disabled)')`,
    'original owner picker data reloaded',
  )
  await evaluate(
    page,
    `${savedRow('saved-film')}.querySelector('button[aria-expanded]').click()`,
  )
  await route(page, '/compte')
  check(
    await evaluate(
      page,
      `!document.querySelector('section[aria-labelledby="saved-heading"] [role="group"]')`,
    ),
    'route departure unmounts picker',
  )
  await route(page, '/compte/watchlist')
  await until(
    page,
    `!!document.querySelector('#watchlist-tag-filter:not(:disabled)')`,
    'watchlist reentry after picker dismissal',
  )
  check(
    await evaluate(
      page,
      `document.querySelector('#watchlist-tag-filter').value === '' && ${savedRow('external-film')}.textContent.includes('Soirée cinéma') && [...document.querySelectorAll('ul[aria-label="Tags associés"] li')].filter(chip => chip.textContent.trim() === 'Soirée cinéma').every(chip => getComputedStyle(chip).backgroundColor === 'rgb(219, 234, 254)')`,
    ),
    'reload recovers committed assignments but resets filter',
  )
  await screenshot(page, 'saved-tags-desktop')
  await filterTag(firstTag)
  await click(page, 'Gérer les tags')
  await evaluate(
    page,
    `document.querySelector('button[aria-label="Supprimer Soirée cinéma"]').click()`,
  )
  check(
    await evaluate(
      page,
      `document.querySelector('#watchlist-tag-manager').textContent.includes('Les films seront conservés.')`,
    ),
    'inline tag deletion explains retained films',
  )
  await click(page, 'Supprimer le tag')
  await until(
    page,
    `document.querySelector('#watchlist-tag-filter').value === '' && document.querySelector('#watchlist-tag-filter').options.length === 2`,
    'deleted selected tag resets filter',
  )
  check(
    saved.length === 2 && assignments().get('external-film').length === 0,
    'deleting tag removes associations but keeps films',
  )
  await evaluate(
    page,
    `document.querySelector('#watchlist-tag-manager button[aria-label^="Supprimer "]').click()`,
  )
  await click(page, 'Supprimer le tag')
  await until(
    page,
    `document.querySelector('#watchlist-tag-manager').textContent.includes('Aucun tag.')`,
    'last reusable tag removed',
  )
  await evaluate(
    page,
    `document.querySelector('button[aria-label="Fermer la gestion des tags"]').click()`,
  )
  await evaluate(
    page,
    `${savedRow('saved-film')}.querySelector('button[aria-expanded]').click()`,
  )
  check(
    await evaluate(
      page,
      `!!${picker} && ${picker}.textContent.includes('Créez un tag dans') && document.activeElement.getAttribute('aria-label') === 'Fermer les tags'`,
    ),
    'empty picker points to existing manager and focuses close control',
  )
  await evaluate(page, `${picker}.querySelector('button').click()`)
  addedTimes.set('external-film', `${date}T00:00:00.000000001Z`)
  await evaluate(page, `window.dispatchEvent(new Event('focus'))`)
  await until(
    page,
    `!!document.querySelector('#watchlist-sort:not(:disabled)')`,
    'sort snapshot revalidated',
  )
  for (const [order, expected] of [
    ['added_desc', ['external-film', 'saved-film']],
    ['added_asc', ['saved-film', 'external-film']],
    ['title_asc', ['external-film', 'saved-film']],
    ['title_desc', ['saved-film', 'external-film']],
    ['release_desc', ['saved-film', 'external-film']],
    ['release_asc', ['saved-film', 'external-film']],
  ]) {
    await selectSort(order)
    check(
      await evaluate(
        page,
        `JSON.stringify(${savedOrder}) === ${JSON.stringify(JSON.stringify(expected))}`,
      ),
      `${order} commits matching saved-row order`,
    )
  }
  const sortWrites = () =>
    writes.filter((write) => write.path.endsWith('/watchlist/sort'))
  check(
    sortWrites().every(
      (write) =>
        Object.keys(write.body).sort().join(',') ===
        'expected_revision,expected_username,sort_order',
    ),
    'sort mutation uses exact narrow revision/owner request',
  )
  const beforeConflictSort = sortWrites().length
  conflict = true
  await evaluate(
    page,
    `(() => { const select = document.querySelector('#watchlist-sort'); select.value = 'title_asc'; select.dispatchEvent(new Event('change', { bubbles: true })); })()`,
  )
  await until(
    page,
    `document.querySelector('#watchlist-sort:not(:disabled)')?.value === 'release_asc' && !!document.querySelector('[role="alert"]')`,
    'rejected sort restores unchanged native selection',
  )
  check(
    sortWrites().length === beforeConflictSort + 1 &&
      (await evaluate(
        page,
        `JSON.stringify(${savedOrder}) === '["saved-film","external-film"]'`,
      )),
    'rejected sort keeps committed rows and is never replayed',
  )
  hold = true
  release = undefined
  await evaluate(
    page,
    `(() => { const select = document.querySelector('#watchlist-sort'); select.value = 'title_asc'; select.dispatchEvent(new Event('change', { bubbles: true })); })()`,
  )
  await until(
    page,
    `document.querySelector('#watchlist-sort')?.disabled`,
    'pending sort disables selector',
  )
  check(
    await evaluate(
      page,
      `document.querySelector('#watchlist-sort').value === 'release_asc' && JSON.stringify(${savedOrder}) === '["saved-film","external-film"]' && [...document.querySelectorAll('section[aria-labelledby="saved-heading"] li button')].every(button => button.disabled)`,
    ),
    'pending sort restores committed control immediately and locks membership without optimistic reorder',
  )
  for (let i = 0; !release && i < 100; i++) await delay(20)
  check(!!release, 'held sort reached fixture')
  hold = false
  release()
  await until(
    page,
    `document.querySelector('#watchlist-sort:not(:disabled)')?.value === 'title_asc'`,
    'held sort commits',
  )
  uncertainSort = true
  await selectSort('release_desc')
  check(
    await evaluate(
      page,
      `!!document.querySelector('[role="alert"]') && JSON.stringify(${savedOrder}) === '["saved-film","external-film"]'`,
    ),
    'uncertain committed sort read-back updates native selection and rows',
  )
  holdRead = true
  await go(page, '/compte/watchlist')
  await checkLoadingLayout('reload with persisted release sort')
  await finishRead()
  await until(
    page,
    `document.querySelector('#watchlist-sort:not(:disabled)')?.value === 'release_desc'`,
    'sort persists after full reload',
  )
  check(
    await evaluate(
      page,
      `JSON.stringify(${savedOrder}) === '["saved-film","external-film"]'`,
    ),
    'reloaded account retains committed release order',
  )
  const sortTab = await tab(page.browserContextId)
  await go(sortTab, '/compte/watchlist')
  await until(
    sortTab,
    `document.querySelector('#watchlist-sort:not(:disabled)')?.value === 'release_desc'`,
    'another tab reads account preference',
  )
  await evaluate(
    sortTab,
    `(() => { const select = document.querySelector('#watchlist-sort'); select.value = 'title_asc'; select.dispatchEvent(new Event('change', { bubbles: true })); })()`,
  )
  await until(
    page,
    `document.querySelector('#watchlist-sort:not(:disabled)')?.value === 'title_asc'`,
    'another tab sort invalidation revalidates committed preference',
  )
  check(
    await evaluate(
      page,
      `JSON.stringify(${savedOrder}) === '["external-film","saved-film"]'`,
    ),
    'cross-tab preference and saved rows refresh together',
  )
  await getCDP().send('Page.close', {}, sortTab.sessionId)
  await getCDP().send('Page.bringToFront', {}, page.sessionId)
  await selectSort('release_desc')
  await click(page, 'Gérer les tags')
  await click(page, 'Créer un tag')
  await fill(page, 'watchlist-tag-name', 'private modal draft')
  const ownerSaved = saved
  session = {
    enabled: true,
    state: 'complete',
    account: { ...owner, username: 'other_sort_owner' },
  }
  saved = []
  await evaluate(page, `window.dispatchEvent(new Event('focus'))`)
  await until(
    page,
    `!document.querySelector('#watchlist-sort') && !document.querySelector('section[aria-labelledby="saved-heading"] li')`,
    'owner change purges previous private selection',
  )
  check(
    await evaluate(
      page,
      `!document.querySelector('#watchlist-tag-manager') && !document.querySelector('main').textContent.includes('private modal draft') && document.activeElement.getAttribute('aria-controls') !== 'watchlist-tag-manager'`,
    ),
    'owner replacement removes modal and never focuses replacement-owner trigger',
  )
  holdRead = true
  await go(page, '/compte/watchlist')
  await checkLoadingLayout('replacement owner')
  await finishRead()
  await until(
    page,
    `document.querySelector('#watchlist-sort:not(:disabled)')?.value === 'added_desc' && document.querySelector('main').textContent.includes('Votre watchlist est vide')`,
    'second account has own default',
  )
  await selectSort('title_desc')
  session = { enabled: true, state: 'anonymous', account: null }
  await evaluate(page, `window.dispatchEvent(new Event('focus'))`)
  await until(
    page,
    `!document.querySelector('#watchlist-sort')`,
    'logout removes private sort selection',
  )
  session = { enabled: true, state: 'complete', account: owner }
  saved = ownerSaved
  await go(page, '/compte/watchlist')
  await until(
    page,
    `document.querySelector('#watchlist-sort:not(:disabled)')?.value === 'release_desc'`,
    'original account preference restored after logout/login',
  )
  check(
    sorts.get('other_sort_owner') === 'title_desc',
    'second account preference remains isolated',
  )
  await evaluate(page, `document.querySelector('#watchlist-sort').focus()`)
  await getCDP().send(
    'Input.dispatchKeyEvent',
    {
      type: 'keyDown',
      key: 'ArrowUp',
      code: 'ArrowUp',
      windowsVirtualKeyCode: 38,
    },
    page.sessionId,
  )
  await getCDP().send(
    'Input.dispatchKeyEvent',
    {
      type: 'keyUp',
      key: 'ArrowUp',
      code: 'ArrowUp',
      windowsVirtualKeyCode: 38,
    },
    page.sessionId,
  )
  await until(
    page,
    `document.querySelector('#watchlist-sort:not(:disabled)')?.value === 'title_desc'`,
    'keyboard changes native selector',
  )
  check(
    await evaluate(
      page,
      `document.activeElement.id === 'watchlist-sort' && (getComputedStyle(document.activeElement).outlineStyle !== 'none' || getComputedStyle(document.activeElement).boxShadow !== 'none')`,
    ),
    'native sort selector supports keyboard with visible focus',
  )
  await selectSort('release_asc')
  frenchReleases.set('external-film', '1998-10-10')
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
    const expectedOrder =
      evidence === '1998-10-07'
        ? ['saved-film', 'external-film']
        : ['external-film', 'saved-film']
    check(
      await evaluate(
        page,
        `JSON.stringify(${savedOrder}) === ${JSON.stringify(JSON.stringify(expectedOrder))}`,
      ),
      'equal-revision evidence arrival replacement or removal reorders release-sorted saved rows',
    )
  }
  await screenshot(page, 'desktop')
  check(
    await evaluate(
      page,
      `!document.querySelector('section[aria-labelledby="saved-heading"] input[type="checkbox"]') && [...document.querySelectorAll('section[aria-labelledby="saved-heading"] li')].filter(row => row.querySelector('a')).every(row => { const poster = row.firstElementChild.getBoundingClientRect(); const title = row.querySelector('a').getBoundingClientRect(); return Math.abs(poster.top - title.top) <= 4; })`,
    ),
    'resting saved list has collapsed editors and top-aligned poster/title',
  )
  await click(page, 'Gérer les tags')
  await click(page, 'Créer un tag')
  await fill(page, 'watchlist-tag-name', 'route-private-draft')
  await route(page, '/film/external-film')
  await until(
    page,
    `!!document.querySelector('button[aria-label="Retirer de la watchlist"]') && !document.querySelector('#watchlist-tag-filter')`,
    'standalone bookmark on no-trailer no-session film',
  )
  check(
    await evaluate(
      page,
      `!document.querySelector('#watchlist-tag-manager') && !document.body.textContent.includes('route-private-draft')`,
    ),
    'page departure detaches modal and private draft',
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
  check(
    await evaluate(
      page,
      `(() => { const control = document.querySelector('#watchlist-sort'); const select = control.getBoundingClientRect(); const style = getComputedStyle(control); const icon = control.parentElement.querySelector('svg').getBoundingClientRect(); const heading = document.querySelector('#saved-heading').getBoundingClientRect(); const filter = document.querySelector('#watchlist-tag-filter').getBoundingClientRect(); const trigger = document.querySelector('button[aria-controls="watchlist-tag-manager"]').getBoundingClientRect(); return select.height >= 44 && select.left >= 0 && icon.left > select.left && icon.right < select.right && icon.top > select.top && icon.bottom < select.bottom && select.left + parseFloat(style.borderLeftWidth) + parseFloat(style.paddingLeft) >= icon.right + 8 && select.right <= innerWidth && select.top >= heading.bottom && select.top >= filter.bottom && trigger.top >= select.bottom && Math.abs(select.left - filter.left) < 1 && Math.abs(select.right - filter.right) < 1 && Math.abs(icon.top + icon.height / 2 - select.top - select.height / 2) < 1; })()`,
    ),
    'mobile sort icon stays inside select with text clearance, stacked toolbar alignment and 44px target',
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
      `(() => { const row = ${savedRow('saved-film')}; const time = row.querySelector('time').getBoundingClientRect(); const action = row.querySelector('button[aria-label="Retirer de la watchlist"]').getBoundingClientRect(); return document.documentElement.scrollWidth <= innerWidth && time.right <= action.left && action.width >= 44 && action.height >= 44; })()`,
    ),
    'mobile full French date fits saved row and preserves bookmark touch target',
  )
  await screenshot(page, 'mobile-saved-date')
  await click(page, 'Gérer les tags')
  await click(page, 'Créer un tag')
  await fill(page, 'watchlist-tag-name', 'pagehide-private-draft')
  await evaluate(
    page,
    `window.dispatchEvent(new PageTransitionEvent('pagehide', { persisted: true }))`,
  )
  check(
    await evaluate(
      page,
      `!document.querySelector('#watchlist-tag-manager') && !document.body.textContent.includes('pagehide-private-draft')`,
    ),
    'pagehide removes tag modal and private draft',
  )
  await evaluate(
    page,
    `window.dispatchEvent(new PageTransitionEvent('pageshow', { persisted: true }))`,
  )
  await until(
    page,
    `!!document.querySelector('#watchlist-tag-filter:not(:disabled)')`,
    'restored watchlist revalidates before manager reopens',
  )
  await click(page, 'Gérer les tags')
  check(
    await evaluate(
      page,
      `!document.querySelector('#watchlist-tag-name') && !document.querySelector('#watchlist-tag-edit')`,
    ),
    'restored manager opens list-first',
  )
  await click(page, 'Créer un tag')
  check(
    await evaluate(
      page,
      `document.querySelector('#watchlist-tag-name').value === '' && document.querySelector('#watchlist-tag-create input[value="neutral"]').checked`,
    ),
    'restored creation never resurrects private name or color drafts',
  )
  await evaluate(
    page,
    `document.querySelector('button[aria-label="Fermer la gestion des tags"]').click()`,
  )
  check(
    await evaluate(
      page,
      `!JSON.stringify({ local: { ...localStorage }, session: { ...sessionStorage }, payload: window.__NUXT__, data: document.querySelector('#__nuxt').__vue_app__.$nuxt.payload.data, url: location.href }).match(/private_watchlist_owner|private candidate query|watchlist-only|added_at|sort_order|release_asc|1998-10-14|14 octobre 1998/)`,
    ),
    'private watchlist identity query membership absent from browser storage and public payload',
  )
  check(
    !page.collections.some((collection) =>
      /private_watchlist_owner|private candidate query|watchlist-only|saved-film|sort_order|release_asc|1998-10-14|14 octobre 1998/.test(
        JSON.stringify(collection),
      ),
    ),
    'analytics contains no watchlist membership, identity or private query',
  )
  const ssr = await (
    await fetch('http://127.0.0.1:13009/film/external-film')
  ).text()
  check(
    !/private_watchlist_owner|added_at|sort_order|release_asc|private candidate query|1998-10-14|14 octobre 1998/.test(
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
  const email = `watchlist-${run}@example.test`
  const username = `watchlist_${run}`
  async function register(ownerEmail, ownerName) {
    const page = await tab()
    await go(page, '/inscription')
    await fill(page, 'account-email', ownerEmail)
    await fill(page, 'account-password', 'Synthetic cinema password 42!')
    await click(page, 'Créer mon compte')
    await until(
      page,
      `document.querySelector('main').textContent.includes('Si cette adresse peut être utilisée')`,
      'registration accepted',
    )
    const message = await mail(ownerEmail, 'verification')
    await go(page, message.link)
    await click(page, 'Confirmer mon email')
    await until(
      page,
      `location.pathname === '/finaliser' && !!document.getElementById('account-username')`,
      'verified onboarding ready',
    )
    await fill(page, 'account-username', ownerName)
    await click(page, 'Confirmer mon nom')
    await until(
      page,
      `location.pathname === '/compte'`,
      'complete account ready',
    )
    return page
  }
  const page = await register(email, username)
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
      empty.body.sort_order === 'added_desc' &&
      Array.isArray(empty.body.tags) &&
      empty.body.tags.length === 0 &&
      empty.body.items.length === 0,
    'real watchlist starts at revision zero with correct owner and default sort',
  )
  check(
    await evaluate(
      page,
      `document.getElementById('watchlist-sort')?.value === 'added_desc' && !document.getElementById('watchlist-sort').disabled`,
    ),
    'real empty watchlist exposes enabled default sort',
  )
  check(
    await evaluate(
      page,
      `(async () => { const response = await fetch('/api/v1/account/watchlist', {cache:'no-store'}); return response.headers.get('cache-control')?.includes('no-store') && response.headers.get('referrer-policy') === 'no-referrer'; })()`,
    ),
    'real private watchlist response retains privacy headers',
  )
  const movies = await request(page, '/movies', undefined, 'GET')
  await click(page, 'Gérer les tags')
  await click(page, 'Créer un tag')
  await fill(page, 'watchlist-tag-name', 'Soirée privée <b>amis</b>')
  await click(page, 'Créer')
  await until(
    page,
    `document.querySelector('#watchlist-tag-filter').options.length === 2 && !document.querySelector('#watchlist-tag-name')`,
    'real tag created with no films',
  )
  await click(page, 'Créer un tag')
  await fill(page, 'watchlist-tag-name', 'À revoir')
  await evaluate(
    page,
    `document.querySelector('#watchlist-tag-name').form.querySelector('input[value="rose"]').click()`,
  )
  await click(page, 'Créer')
  await until(
    page,
    `document.querySelector('#watchlist-tag-filter').options.length === 3 && !document.querySelector('#watchlist-tag-name')`,
    'real second reusable tag created',
  )
  const tagOnly = await request(page, '/account/watchlist', undefined, 'GET')
  check(
    tagOnly.status === 200 &&
      tagOnly.body.tags.length === 2 &&
      tagOnly.body.items.length === 0 &&
      tagOnly.body.revision === '2',
    'real tag-only state persists without membership',
  )
  check(
    await evaluate(page, `!document.querySelector('#watchlist-tag-manager b')`),
    'real HTML-like tag name is escaped',
  )
  const firstTag = tagOnly.body.tags.find(
    (tag) => tag.name === 'Soirée privée <b>amis</b>',
  ).id
  const secondTag = tagOnly.body.tags.find((tag) => tag.name === 'À revoir').id
  check(
    tagOnly.body.tags.find((tag) => tag.id === firstTag).color === 'neutral' &&
      tagOnly.body.tags.find((tag) => tag.id === secondTag).color === 'rose',
    'real neutral default and nonneutral create persist required colors',
  )
  await evaluate(
    page,
    `document.querySelector('button[aria-label="Fermer la gestion des tags"]').click()`,
  )
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
  const pair = await request(page, '/account/watchlist', {
    expected_username: username,
    expected_revision: saved.body.revision,
    movie_slug: otherMovie.slug,
    saved: 'true',
  })
  check(
    pair.status === 200 && pair.body.items.length === 2,
    'real second saved film provides sortable rows',
  )
  await go(page, '/compte/watchlist')
  const rows = `Array.from(document.querySelectorAll('[aria-labelledby="saved-heading"] li a')).map(link => decodeURIComponent(link.pathname.split('/').pop()))`
  let committed = pair.body
  check(
    committed.tags.length === 2 &&
      committed.items.every(
        (item) => Array.isArray(item.tag_ids) && item.tag_ids.length === 0,
      ),
    'real membership responses preserve tags and add untagged films',
  )
  const orders = [
    'added_desc',
    'added_asc',
    'title_asc',
    'title_desc',
    'release_desc',
    'release_asc',
  ]
  for (const order of orders) {
    await until(
      page,
      `!!document.querySelector('#watchlist-sort:not(:disabled)')`,
      'real sort ready',
    )
    await evaluate(
      page,
      `(() => { const select = document.getElementById('watchlist-sort'); select.value = ${JSON.stringify(order)}; select.dispatchEvent(new Event('change', {bubbles:true})); })()`,
    )
    await until(
      page,
      `document.querySelector('#watchlist-sort:not(:disabled)')?.value === ${JSON.stringify(order)}`,
      'real sort committed',
    )
    const snapshot = await request(page, '/account/watchlist', undefined, 'GET')
    check(
      snapshot.status === 200 &&
        snapshot.body.sort_order === order &&
        snapshot.body.username === username &&
        BigInt(snapshot.body.revision) ===
          BigInt(committed.revision) +
            (committed.sort_order === order ? 0n : 1n),
      `real ${order} persisted with shared revision`,
    )
    committed = snapshot.body
    const direction = order.endsWith('_asc') ? 1 : -1
    const expected = [...committed.items]
      .sort((left, right) => {
        let compared = 0
        if (order.startsWith('title_'))
          compared = left.title.localeCompare(right.title, 'fr-FR', {
            sensitivity: 'base',
            numeric: true,
          })
        else if (order.startsWith('added_'))
          compared = left.added_at.localeCompare(right.added_at)
        else {
          const leftDate = left.french_release_date
          const rightDate = right.french_release_date
          if (!leftDate || !rightDate)
            return leftDate
              ? -1
              : rightDate
                ? 1
                : left.slug.localeCompare(right.slug)
          compared = leftDate.localeCompare(rightDate)
        }
        return compared * direction || left.slug.localeCompare(right.slug)
      })
      .map((item) => item.slug)
    await until(
      page,
      `JSON.stringify(${rows}) === ${JSON.stringify(JSON.stringify(expected))}`,
      `real ${order} rows reordered`,
    )
    check(true, `real ${order} selector and rows match persisted preference`)
  }
  await go(page, '/compte/watchlist')
  await until(
    page,
    `document.querySelector('#watchlist-sort:not(:disabled)')?.value === 'release_asc'`,
    'real nondefault sort survives reload',
  )
  check(
    (await request(page, '/account/watchlist', undefined, 'GET')).body
      .sort_order === 'release_asc',
    'real nondefault preference survives document reload',
  )
  check(
    page.requests
      .filter((entry) => entry.path.endsWith('/watchlist/sort'))
      .every(
        (entry) =>
          entry.method === 'POST' &&
          JSON.stringify([...entry.bodyKeys].sort()) ===
            JSON.stringify([
              'expected_revision',
              'expected_username',
              'sort_order',
            ]),
      ),
    'real sort uses exact private POST body',
  )
  async function assignReal(slug, name, assigned) {
    const row = `document.querySelector('section[aria-labelledby="saved-heading"] a[href="/film/${slug}"]').closest('li')`
    await evaluate(
      page,
      `(() => { const button = ${row}.querySelector('button[aria-expanded]'); if (button.getAttribute('aria-expanded') !== 'true') button.click(); })()`,
    )
    const checkbox = `[...${row}.querySelectorAll('label')].find(label => label.textContent.trim() === ${JSON.stringify(name)}).querySelector('input')`
    await until(page, `!${checkbox}.disabled`, 'real tag assignment ready')
    await evaluate(page, `${checkbox}.click()`)
    await until(
      page,
      `!document.querySelector('#watchlist-sort').disabled && ${checkbox}.checked === ${assigned}`,
      'real tag assignment committed',
    )
  }
  await assignReal(movie.slug, 'Soirée privée <b>amis</b>', true)
  await assignReal(movie.slug, 'À revoir', true)
  await assignReal(otherMovie.slug, 'Soirée privée <b>amis</b>', true)
  committed = (await request(page, '/account/watchlist', undefined, 'GET')).body
  check(
    committed.items.find((item) => item.slug === movie.slug).tag_ids.length ===
      2 &&
      committed.items.find((item) => item.slug === otherMovie.slug).tag_ids
        .length === 1,
    'real multiple assignments on multiple saved films persist',
  )
  await evaluate(
    page,
    `(() => { const filter = document.querySelector('#watchlist-tag-filter'); filter.value = ${JSON.stringify(secondTag)}; filter.dispatchEvent(new Event('change', {bubbles:true})); })()`,
  )
  await until(
    page,
    `${rows}.length === 1`,
    'real filter shows matching film only',
  )
  await go(page, '/compte/watchlist')
  await until(
    page,
    `!!document.querySelector('#watchlist-tag-filter:not(:disabled)') && document.querySelectorAll('ul[aria-label="Tags associés"]').length === 2`,
    'real assignments survive document reload',
  )
  check(
    await evaluate(
      page,
      `document.querySelector('#watchlist-tag-filter').value === ''`,
    ),
    'real reload clears page-local tag filter',
  )
  await click(page, 'Gérer les tags')
  await evaluate(
    page,
    `document.querySelector('button[aria-label="Modifier Soirée privée <b>amis</b>"]').click()`,
  )
  await fill(page, 'watchlist-tag-edit', 'Ensemble privé')
  await evaluate(
    page,
    `document.querySelector('#watchlist-tag-edit').form.querySelector('input[value="blue"]').click()`,
  )
  await click(page, 'Enregistrer')
  await until(
    page,
    `!document.querySelector('#watchlist-tag-edit') && [...document.querySelectorAll('ul[aria-label="Tags associés"]')].every(list => list.textContent.includes('Ensemble privé'))`,
    'real rename changes both chips',
  )
  const coloredSnapshot = (
    await request(page, '/account/watchlist', undefined, 'GET')
  ).body
  check(
    coloredSnapshot.tags.find((tag) => tag.id === firstTag).name ===
      'Ensemble privé' &&
      coloredSnapshot.tags.find((tag) => tag.id === firstTag).color === 'blue',
    'real atomic name and color stored in same definition',
  )
  await go(page, '/compte/watchlist')
  await until(
    page,
    `document.querySelectorAll('ul[aria-label="Tags associés"]').length === 2 && !document.querySelector('#watchlist-sort').disabled`,
    'real colored tags survive document reload',
  )
  await click(page, 'Gérer les tags')
  await exerciseTagColors({
    page,
    evaluate,
    check,
    fill,
    click,
    until,
    name: 'Ensemble privé',
  })
  check(
    (
      await request(page, '/account/watchlist', undefined, 'GET')
    ).body.tags.find((tag) => tag.id === firstTag).color === 'neutral',
    'real color-only/name-only edits and neutral reset persisted',
  )
  await evaluate(
    page,
    `document.querySelector('button[aria-label="Fermer la gestion des tags"]').click()`,
  )
  await assignReal(movie.slug, 'À revoir', false)
  await evaluate(
    page,
    `(() => { const filter = document.querySelector('#watchlist-tag-filter'); filter.value = ${JSON.stringify(secondTag)}; filter.dispatchEvent(new Event('change', {bubbles:true})); })()`,
  )
  await until(
    page,
    `document.querySelector('main').textContent.includes('Aucun film avec ce tag.')`,
    'real filtered-empty state after unassignment',
  )
  await click(page, 'Voir tous les films')
  await click(page, 'Gérer les tags')
  await evaluate(
    page,
    `document.querySelector('button[aria-label="Supprimer Ensemble privé"]').click()`,
  )
  await click(page, 'Supprimer le tag')
  await until(
    page,
    `document.querySelector('#watchlist-tag-filter').options.length === 2 && !document.querySelector('ul[aria-label="Tags associés"]')`,
    'real tag deleted from every film',
  )
  committed = (await request(page, '/account/watchlist', undefined, 'GET')).body
  check(
    committed.items.length === 2 &&
      committed.tags.length === 1 &&
      !committed.tags.some((tag) => tag.id === firstTag) &&
      committed.items.every((item) => item.tag_ids.length === 0),
    'real deletion preserves both films and removes associations',
  )
  await evaluate(
    page,
    `document.querySelector('button[aria-label="Fermer la gestion des tags"]').click()`,
  )
  const restore = await request(page, '/account/watchlist', {
    expected_username: username,
    expected_revision: committed.revision,
    movie_slug: otherMovie.slug,
    saved: 'false',
  })
  check(
    restore.status === 200 &&
      restore.body.items.length === 1 &&
      restore.body.sort_order === 'release_asc',
    'real membership removal preserves sort',
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
    'sort_order',
    'release_asc',
    'expected_username',
    'watchlistOnly',
    'tag_ids',
    'selectedTag',
    'editDraft',
    'editColor',
    'Soirée privée',
    'Ensemble privé',
    'À revoir',
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
      removed.body.sort_order === 'release_asc' &&
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
  const second = await register(
    `watchlist-b-${run}@example.test`,
    `watchlist_b_${run}`,
  )
  await go(second, '/compte/watchlist')
  await until(
    second,
    `document.querySelector('#watchlist-sort:not(:disabled)')?.value === 'added_desc'`,
    'real second owner starts with default sort',
  )
  const secondSnapshot = await request(
    second,
    '/account/watchlist',
    undefined,
    'GET',
  )
  check(
    secondSnapshot.status === 200 &&
      secondSnapshot.body.sort_order === 'added_desc' &&
      secondSnapshot.body.revision === '0' &&
      secondSnapshot.body.tags.length === 0 &&
      secondSnapshot.body.items.length === 0,
    'real second owner inherits neither preference nor membership',
  )
  await evaluate(
    second,
    `(() => { const select = document.getElementById('watchlist-sort'); select.value = 'title_desc'; select.dispatchEvent(new Event('change', {bubbles:true})); })()`,
  )
  await until(
    second,
    `document.querySelector('#watchlist-sort:not(:disabled)')?.value === 'title_desc'`,
    'real second owner saves independent preference on empty list',
  )
  await go(second, '/compte/watchlist')
  await until(
    second,
    `document.querySelector('#watchlist-sort:not(:disabled)')?.value === 'title_desc'`,
    'real second owner preference survives reload',
  )
  check(
    (await request(page, '/account/watchlist', undefined, 'GET')).body
      .sort_order === 'release_asc',
    'real second owner preference does not alter first owner',
  )
  console.log(
    'SKIP external import: TMDB intentionally disabled in backend fixture',
  )
  return true
}
