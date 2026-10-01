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
    if (!radios.length || radios.filter(r => r.checked).length !== 1) return false;
    const grid = getComputedStyle(radios[0].closest('label').parentElement);
    if (grid.gridTemplateColumns.split(' ').length !== (innerWidth >= 640 ? 4 : 2) || parseFloat(grid.gap) < 8) return false;
    return radios.length === 8 && new Set(radios.map(r => r.name)).size === 1 && radios.every((r,i) => {
      const label = r.closest('label'), style = getComputedStyle(label), rect = label.getBoundingClientRect(), token = expected[i], checks = [...label.querySelectorAll('svg')];
      return r.value === token[0] && label.textContent.trim() === token[1] && r.classList.contains('sr-only') && !r.disabled && style.backgroundColor === rgb(token[2]) && style.color === rgb(token[3]) && style.borderColor === rgb(token[4]) && style.borderWidth === '2px' && style.borderRadius === '0px' && style.fontFamily.includes('monospace') && style.fontSize === '12px' && style.fontWeight === '700' && rect.height >= 44 && rect.width >= 44 && label.scrollWidth <= label.clientWidth && checks.length === (r.checked ? 1 : 0) && checks.every(check => check.getAttribute('aria-hidden') === 'true' && check.getAttribute('focusable') === 'false' && check.getBoundingClientRect().width === 20) && contrast(style.color, style.backgroundColor) >= 4.5 && contrast(style.borderColor, style.backgroundColor) >= 3;
    });
  })()`,
    ),
    'eight square mono color tiles retain exact palette contrast, native radio names, one selected check and 44px targets',
  )
}

async function checkChips(page, evaluate, check, context) {
  check(
    await evaluate(
      page,
      `(() => {
    const expected = ${JSON.stringify(palette)};
    const chips = [...document.querySelectorAll('.watchlist-tag-chip')].filter(chip => chip.checkVisibility());
    const rgb = hex => 'rgb(' + [1,3,5].map(i => parseInt(hex.slice(i, i+2), 16)).join(', ') + ')';
    return chips.length > 0 && chips.every(chip => {
      const style = getComputedStyle(chip), rect = chip.getBoundingClientRect(), token = expected.find(token => style.backgroundColor === rgb(token[2]));
      return token && style.color === rgb(token[3]) && style.borderColor === rgb(token[4]) && style.borderWidth === '2px' && style.borderRadius === '0px' && style.fontFamily.includes('monospace') && style.fontSize === '12px' && style.fontWeight === '700' && style.overflowWrap === 'anywhere' && chip.scrollWidth <= chip.clientWidth && rect.width <= chip.parentElement.clientWidth + .5 && rect.left >= 0 && rect.right <= innerWidth + .5;
    });
  })()`,
    ),
    `${context} manager, assigned and assignment-option chips share square mono borders, palette and safe long-name wrapping`,
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
  origin,
  apiPort,
  getCDP,
  launch,
  tab,
  go,
  evaluate,
  until,
  click: rawClick,
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
  const preferences = new Map()
  const preference = () =>
    preferences.get(session.account.username) ?? {
      view_mode: 'list',
      filter_tag_id: null,
    }
  const addedTimes = new Map()
  let uncertainSort = false
  let uncertainAssignment = false
  let uncertainPreferences = false
  let uncertainRemoval = false
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
  let externalTitle = 'Film externe'
  const movie = (slug) => ({
    slug,
    title:
      slug === 'external-film'
        ? externalTitle
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
    ...preference(),
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
      if (path.endsWith('/preferences')) {
        if (
          Object.keys(body).sort().join(',') !==
            'expected_revision,expected_username,filter_tag_id,view_mode' ||
          // oxlint-disable-next-line anti-slop/no-runtime-typeof -- Mock HTTP boundary rejects non-string JSON fields before interpreting the preference request.
          !Object.values(body).every((value) => typeof value === 'string') ||
          !['list', 'tags'].includes(body.view_mode)
        )
          return send({ error: { code: 'invalid_request' } }, 400)
        if (
          body.filter_tag_id &&
          !tags().some((tag) => tag.id === body.filter_tag_id)
        )
          return send({ error: { code: 'watchlist_tag_not_found' } }, 404)
        if (
          body.view_mode !== preference().view_mode ||
          (body.filter_tag_id || null) !== preference().filter_tag_id
        )
          revision++
        preferences.set(session.account.username, {
          view_mode: body.view_mode,
          filter_tag_id: body.filter_tag_id || null,
        })
        if (uncertainPreferences) {
          uncertainPreferences = false
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
          if (preference().filter_tag_id === body.tag_id)
            preferences.set(session.account.username, {
              ...preference(),
              filter_tag_id: null,
            })
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
      if (body.saved === 'false' && uncertainRemoval) {
        uncertainRemoval = false
        return send({ error: { code: 'watchlist_unavailable' } }, 503)
      }
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
    server.listen(apiPort, '127.0.0.1', resolve)
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
  async function checkTagModalStyle(viewport) {
    await screenshot(
      page,
      `tag-manager-style-${viewport.replaceAll(' ', '-')}`,
      false,
    )
    const presentation = await evaluate(
      page,
      `(() => {
        const dialog = document.querySelector('#watchlist-tag-manager'), rect = dialog.getBoundingClientRect(), style = getComputedStyle(dialog), header = dialog.querySelector('header'), title = dialog.querySelector('h2'), close = dialog.querySelector('button[aria-label="Fermer la gestion des tags"]'), body = dialog.querySelector('.overflow-y-auto');
        const headerStyle = getComputedStyle(header), closeStyle = getComputedStyle(close), bodyStyle = getComputedStyle(body), closeRect = close.getBoundingClientRect(), icon = close.querySelector('svg').getBoundingClientRect();
        const viewport = window.visualViewport, height = viewport?.height ?? innerHeight, top = viewport?.offsetTop ?? 0;
        // Tailwind color-mix may serialize as oklab rather than rgba in Chromium.
        const reference = document.createElement('span'); reference.className = 'bg-black/60'; document.body.append(reference);
        const expectedBackdrop = getComputedStyle(reference).backgroundColor; reference.remove();
        const actualBackdrop = getComputedStyle(dialog, '::backdrop').backgroundColor;
        const matches = dialog.matches(':modal') && Math.abs(rect.width - Math.min(672, innerWidth - 32)) < 1 && Math.abs(rect.left + rect.width / 2 - innerWidth / 2) < 1 && Math.abs(rect.top + rect.height / 2 - top - height / 2) < 1 && rect.top >= top + 15.5 && rect.bottom <= top + height - 15.5 && style.borderWidth === '2px' && style.borderColor === 'rgb(39, 39, 42)' && style.borderRadius === '0px' && style.backgroundColor === 'rgb(255, 255, 255)' && style.padding === '0px' && style.boxShadow !== 'none' && actualBackdrop === expectedBackdrop && headerStyle.padding === '16px' && headerStyle.borderBottomWidth === '1px' && title.classList.contains('account-heading') && closeRect.width === 44 && closeRect.height === 44 && closeStyle.borderRadius === '0px' && icon.width === 20 && icon.height === 20 && bodyStyle.padding === '16px' && bodyStyle.overflowY === 'auto' && body.scrollWidth <= body.clientWidth && document.body.style.overflow === 'hidden';
        return { matches, geometry: { left: rect.left, top: rect.top, width: rect.width, height: rect.height, viewportHeight: height, viewportTop: top }, style: { border: style.borderWidth, borderColor: style.borderColor, radius: style.borderRadius, background: style.backgroundColor, padding: style.padding, backdrop: actualBackdrop, expectedBackdrop, headerPadding: headerStyle.padding, divider: headerStyle.borderBottomWidth, closeWidth: closeRect.width, closeHeight: closeRect.height, closeRadius: closeStyle.borderRadius, iconWidth: icon.width, iconHeight: icon.height, bodyPadding: bodyStyle.padding, bodyOverflow: bodyStyle.overflowY, scrollWidth: body.scrollWidth, clientWidth: body.clientWidth, lock: document.body.style.overflow } };
      })()`,
    )
    check(
      presentation.matches,
      `${viewport} tag modal matches centered square add-dialog style, viewport margins, header/close and locked scroll ${JSON.stringify(presentation)}`,
    )
  }
  async function checkClock(page, variant) {
    if (
      variant === 'remove' &&
      (await evaluate(
        page,
        `!!document.querySelector('[data-watchlist-remove]')`,
      ))
    ) {
      check(
        await evaluate(
          page,
          `(() => { const buttons = [...document.querySelectorAll('[data-watchlist-remove]')]; return buttons.length > 0 && buttons.every(button => { const style = getComputedStyle(button), rect = button.getBoundingClientRect(), icon = button.querySelector('svg'); return button.getAttribute('aria-label') === 'Retirer de la watchlist' && button.getAttribute('aria-haspopup') === 'dialog' && !button.textContent.trim() && style.borderWidth === '0px' && rect.width >= 44 && rect.height >= 44 && icon?.getAttribute('aria-hidden') === 'true' && icon?.getAttribute('focusable') === 'false' && icon.getBoundingClientRect().width === 20 && icon.querySelectorAll('path').length === 2 && !icon.querySelector('circle') }) })()`,
        ),
        'watchlist-only removal uses named borderless decorative X and 44px target; film clock unchanged',
      )
      return
    }
    check(
      await evaluate(
        page,
        `(() => {
          const variant = ${JSON.stringify(variant)};
          const label = variant === 'remove' ? 'Retirer de la watchlist' : 'Ajouter à la watchlist';
          const buttons = [...document.querySelectorAll('button[aria-label="' + label + '"]')];
          return buttons.length > 0 && buttons.every(button => {
            const icon = button.querySelector('svg[data-watchlist-icon="' + variant + '"]');
            if (!icon || icon.getAttribute('aria-hidden') !== 'true' || icon.getAttribute('focusable') !== 'false') return false;
            const paths = [...icon.querySelectorAll('path')];
            const stroke = 'rgb(39, 39, 42)';
            const hovered = button.matches(':enabled:hover');
            const reference = document.createElement('span');
            reference.style.backgroundColor = hovered ? 'var(--color-subtle)' : '#fff';
            document.body.append(reference);
            const background = getComputedStyle(reference).backgroundColor;
            reference.remove();
            const rect = button.getBoundingClientRect();
            return rect.width >= 48 && rect.height >= 48 && button.getAttribute('aria-pressed') === String(variant === 'remove') &&
              icon.getBoundingClientRect().width === 24 && icon.getBoundingClientRect().height === 24 &&
              getComputedStyle(button).backgroundColor === background &&
              icon.getAttribute('viewBox') === '0 0 24 24' && icon.getAttribute('stroke-width') === '2' &&
              paths.length === (variant === 'add' ? 4 : 3) &&
              !!icon.querySelector('path[d="M21.92 13.267a10 10 0 1 0-8.653 8.653"]') &&
              !!icon.querySelector('path[d="M12 6v6l3.644 1.822"]') &&
              !!icon.querySelector('path[d="M16 19h6"]') &&
              !!icon.querySelector('path[d="M19 16v6"]') === (variant === 'add') &&
              paths.every(path => getComputedStyle(path).fill === 'none' && getComputedStyle(path).stroke === stroke) &&
              !icon.classList.contains('text-primary') &&
              !icon.querySelector('circle, g, path[d="M7.5 10 12 12 17 6.5"]') && !icon.classList.contains('text-accent') &&
              !button.querySelector('[class*="bookmark"]');
          });
        })()`,
      ),
      `${variant} clock has dark 24px plus/minus strokes, white surface with subtle hover, decorative semantics and 48px target`,
    )
  }
  async function checkTagRows(viewport) {
    const rows = await evaluate(
      page,
      `(() => {
      const body = document.querySelector('#watchlist-tag-manager .overflow-y-auto'), list = body.querySelector(':scope > ul'), style = getComputedStyle(list), before = list.previousElementSibling.getBoundingClientRect();
      const rows = [...list.children].map(li => {
        const row = li.firstElementChild, tag = row.firstElementChild, badge = tag.firstElementChild, name = badge.textContent.trim(), buttons = [...row.querySelectorAll('button')], rect = row.getBoundingClientRect(), tagRect = tag.getBoundingClientRect();
        return { name, wrapped: badge.getBoundingClientRect().height > 32, valid: getComputedStyle(row).flexWrap === 'nowrap' && tagRect.width > 0 && tagRect.right <= buttons[0].getBoundingClientRect().left && row.scrollWidth <= row.clientWidth && buttons.length === 2 && buttons.every((button, i) => { const b = button.getBoundingClientRect(), icon = button.querySelector('svg'); return !button.textContent.trim() && button.getAttribute('aria-label') === (i ? 'Supprimer ' : 'Modifier ') + name && icon?.getAttribute('aria-hidden') === 'true' && icon?.getAttribute('focusable') === 'false' && b.width >= 44 && b.height >= 44 && b.left >= rect.left && b.right <= rect.right && Math.abs(b.top + b.height / 2 - rect.top - rect.height / 2) < 1 && (i === 0 || b.left - buttons[0].getBoundingClientRect().right >= 8) }) };
      });
      return { rows, divider: style.borderTopWidth === '1px' && style.borderTopStyle === 'solid' && parseFloat(style.paddingTop) >= 8 && list.getBoundingClientRect().top - before.bottom >= 16 && [...list.children].slice(1).every((li, i) => parseFloat(getComputedStyle(li).borderTopWidth) > 0 || parseFloat(getComputedStyle(list.children[i]).borderBottomWidth) > 0), noOverflow: body.scrollWidth <= body.clientWidth };
    })()`,
    )
    check(
      rows.rows.length > 0 &&
        rows.rows.every((row) => row.valid) &&
        rows.divider &&
        rows.noOverflow,
      `${viewport} tag names and 44px named icon actions stay inline with creation/list divider and row separators ${JSON.stringify(rows)}`,
    )
    if (viewport === '320px')
      check(
        rows.rows.some((row) => row.name.length > 30 && row.wrapped),
        '320px long tag name wraps only inside flexible tag region',
      )
  }
  async function checkGroupDots(viewport) {
    const expected = tags().map((tag) => ({
      ...tag,
      dotColor: palette.find(([color]) => color === tag.color)[4],
    }))
    check(
      await evaluate(
        page,
        `(() => {
          const tags = ${JSON.stringify(expected)};
          const headings = [...document.querySelectorAll('h3[id^="watchlist-group-"]')];
          const rgb = hex => 'rgb(' + [1,3,5].map(i => parseInt(hex.slice(i, i+2), 16)).join(', ') + ')';
          const baseline = element => {
            const probe = document.createElement('span');
            probe.style.cssText = 'display:inline-block;width:0;height:0;vertical-align:baseline';
            element.prepend(probe);
            const y = probe.getBoundingClientRect().top;
            probe.remove();
            return y;
          };
          return headings.length > 0 && headings.every(heading => {
            const dot = heading.querySelector('[aria-hidden="true"]');
            if (heading.id === 'watchlist-group-untagged') return !dot && heading.firstElementChild.textContent === 'Sans tag';
            const tag = tags.find(tag => heading.id === 'watchlist-group-tag-' + tag.id);
            if (!tag || !dot) return false;
            const rect = dot.getBoundingClientRect(), label = dot.nextElementSibling, nameRect = label.getBoundingClientRect();
            const textBaseline = baseline(label), context = document.createElement('canvas').getContext('2d');
            context.font = getComputedStyle(label).font;
            const opticalCenter = textBaseline - context.measureText('x').actualBoundingBoxAscent / 2;
            return heading.firstElementChild === dot && dot.textContent === '' && !dot.hasAttribute('tabindex')
              && label.textContent === tag.name && rect.width === 10 && rect.height === 10
              && getComputedStyle(dot).backgroundColor === rgb(tag.dotColor) && parseFloat(getComputedStyle(dot).borderRadius) >= 5
              && rect.right + 7 <= nameRect.left && Math.abs(rect.top + rect.height / 2 - opticalCenter) <= 1
              && Math.abs(baseline(heading.lastElementChild) - textBaseline) < 1
              && nameRect.right <= innerWidth && heading.lastElementChild.getBoundingClientRect().right <= innerWidth;
          }) && document.documentElement.scrollWidth <= innerWidth;
        })()`,
      ),
      `${viewport} palette dots align with first-line x-height center and count baseline, including wrapped names; synthetic Sans tag has none`,
    )
    const names = await evaluate(
      page,
      `[...document.querySelectorAll('h3[id^="watchlist-group-"]')].map(h => [...h.children].filter(child => !child.hasAttribute('aria-hidden')).map(child => child.textContent.trim()).join(' '))`,
    )
    const tree = await getCDP().send(
      'Accessibility.getFullAXTree',
      {},
      page.sessionId,
    )
    check(
      names.every((name) =>
        tree.nodes.some(
          (node) => node.role?.value === 'heading' && node.name?.value === name,
        ),
      ),
      `${viewport} group accessible names remain tag name and count only`,
    )
    if (!process.argv.includes('--visual')) return
    const clips = await evaluate(
      page,
      `[...document.querySelectorAll('h3[id^="watchlist-group-"]')].map(h => {
        const rect = h.getBoundingClientRect();
        return { id: h.id, x: Math.max(0, rect.left + scrollX - 4), y: Math.max(0, rect.top + scrollY - 4), width: rect.width + 8, height: rect.height + 8, scale: 3 };
      })`,
    )
    for (const { id, ...clip } of clips) {
      const image = await getCDP().send(
        'Page.captureScreenshot',
        { format: 'png', captureBeyondViewport: true, clip },
        page.sessionId,
      )
      await writeFile(
        `/tmp/opencode/${id}-${viewport.replaceAll(' ', '-')}-closeup.png`,
        Buffer.from(image.data, 'base64'),
      )
    }
  }
  async function captureClockCloseup(page, variant, viewport) {
    if (!process.argv.includes('--visual')) return
    const clip = await evaluate(
      page,
      `(() => {
        const rect = document.querySelector('button svg[data-watchlist-icon="${variant}"]').closest('button').getBoundingClientRect();
        return { x: Math.max(0, rect.left + scrollX - 8), y: Math.max(0, rect.top + scrollY - 8), width: rect.width + 16, height: rect.height + 16, scale: 4 };
      })()`,
    )
    const image = await getCDP().send(
      'Page.captureScreenshot',
      { format: 'png', captureBeyondViewport: true, clip },
      page.sessionId,
    )
    await writeFile(
      `/tmp/opencode/watchlist-clock-${variant}-${viewport}-closeup.png`,
      Buffer.from(image.data, 'base64'),
    )
  }
  async function captureClockOnBackdrop(page, variant, viewport) {
    if (!process.argv.includes('--visual')) return
    // Synthetic poster-like backdrop, local CSS only: no image/provider request.
    const previous = await evaluate(
      page,
      `document.querySelector('.movie-hero').style.backgroundImage`,
    )
    try {
      await evaluate(
        page,
        `document.querySelector('.movie-hero').style.backgroundImage = 'linear-gradient(135deg, #17211c, #494439 45%, #111817 70%, #3b4a40)'`,
      )
      await checkClock(page, variant)
      await captureClockCloseup(page, variant, `${viewport}-poster-background`)
    } finally {
      await evaluate(
        page,
        `document.querySelector('.movie-hero').style.backgroundImage = ${JSON.stringify(previous)}`,
      )
    }
  }
  async function captureFilmClock(page, variant) {
    await checkClock(page, variant)
    await screenshot(page, `clock-${variant}-desktop`)
    await captureClockCloseup(page, variant, 'desktop')
    await captureClockOnBackdrop(page, variant, 'desktop')
    await getCDP().send(
      'Emulation.setDeviceMetricsOverride',
      { width: 390, height: 844, deviceScaleFactor: 1, mobile: true },
      page.sessionId,
    )
    await checkClock(page, variant)
    await screenshot(page, `clock-${variant}-mobile`)
    await captureClockCloseup(page, variant, 'mobile')
    await captureClockOnBackdrop(page, variant, 'mobile')
    await getCDP().send(
      'Emulation.setDeviceMetricsOverride',
      { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false },
      page.sessionId,
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
  async function click(target, label) {
    if (
      label === 'Gérer les tags' &&
      (await evaluate(target, `innerWidth < 1024`))
    )
      label = 'Tags'
    const sheetAction =
      ['Liste', 'Par tag'].includes(label) &&
      (await evaluate(target, `innerWidth < 1024`))
    if (
      sheetAction &&
      !(await evaluate(
        target,
        `!!document.querySelector('#watchlist-configuration')`,
      ))
    ) {
      await click(target, 'Configuration')
      await until(
        target,
        `document.querySelector('#watchlist-configuration')?.matches(':modal')`,
        'mobile configuration opens',
      )
    }
    if (
      label === 'Rechercher' &&
      !(await evaluate(target, `!!document.querySelector('#watchlist-add')`))
    ) {
      await rawClick(target, 'Ajouter')
      await until(
        target,
        `document.querySelector('#watchlist-add')?.matches(':modal')`,
        'add modal opens before search',
      )
    }
    if (sheetAction)
      await evaluate(
        target,
        `(() => { const button = [...document.querySelectorAll('#watchlist-configuration button')].find(button=>button.textContent.trim() === ${JSON.stringify(label)}); button.focus(); button.click() })()`,
      )
    else if (label === 'Configuration' || label.startsWith('Fermer'))
      await evaluate(
        target,
        `document.querySelector('button[aria-label=${JSON.stringify(label)}]').click()`,
      )
    else await rawClick(target, label)
    if (label === 'Voir tous les films')
      await until(
        target,
        `document.querySelector('#watchlist-tag-filter:not(:disabled)')?.value === ''`,
        'all-films preference committed',
      )
    if (label === 'Liste' || label === 'Par tag')
      await until(
        target,
        `document.querySelector('[aria-label="Affichage des films"] button[aria-pressed="true"]:not(:disabled)')?.textContent.trim() === ${JSON.stringify(label)}`,
        'display preference committed',
      )
    if (sheetAction)
      await evaluate(
        target,
        `document.querySelector('button[aria-label="Fermer la configuration"]').click()`,
      )
  }
  async function checkSegmentedDisplay(viewport, selected, disabled = false) {
    check(
      await evaluate(
        page,
        `(() => {
           const group = [...document.querySelectorAll('[role="group"][aria-label="Affichage des films"]')].find(node => node.checkVisibility());
          const buttons = [...group.querySelectorAll('button')];
          const outer = group.getBoundingClientRect(), style = getComputedStyle(group);
          const rects = buttons.map(button => button.getBoundingClientRect());
          const close = (a, b) => Math.abs(a - b) < 1;
          return buttons.length === 2 && style.borderRadius === '0px'
            && ['Top', 'Right', 'Bottom', 'Left'].every(side => style['border' + side + 'Width'] === '2px' && style['border' + side + 'Color'] === 'rgb(39, 39, 42)')
            && close(rects[0].right, rects[1].left) && close(rects[0].top, rects[1].top)
            && close(rects[0].left, outer.left + 2) && close(rects[1].right, outer.right - 2)
            && outer.left >= 0 && outer.right <= innerWidth
            && buttons.every((button, index) => {
              const css = getComputedStyle(button), rect = rects[index], icon = button.querySelector('svg');
              const active = index === ${selected};
              return button.textContent.trim() === ['Liste', 'Par tag'][index]
                && button.type === 'button' && button.disabled === ${disabled}
                && button.getAttribute('aria-pressed') === String(active)
                && rect.width >= 44 && rect.height >= 44 && close(rect.top, outer.top + 2) && close(rect.bottom, outer.bottom - 2)
                && css.borderRadius === '0px' && css.borderLeftWidth === (index ? '2px' : '0px')
                && (!index || css.borderLeftColor === 'rgb(39, 39, 42)')
                && ['Top', 'Right', 'Bottom'].every(side => css['border' + side + 'Width'] === '0px')
                && css.color === (active ? 'rgb(255, 255, 255)' : 'rgb(39, 39, 42)')
                && css.backgroundColor === (active ? 'rgb(39, 39, 42)' : 'rgb(255, 255, 255)')
                && (active ? css.boxShadow.endsWith('rgb(168, 191, 163) 0px -4px 0px 0px inset') : css.boxShadow === 'none')
                && icon?.getAttribute('aria-hidden') === 'true' && icon.getAttribute('focusable') === 'false'
                && icon.classList.contains(index ? 'lucide-tags' : 'lucide-list')
                && icon.getBoundingClientRect().width === 18 && icon.getBoundingClientRect().height === 18;
            });
        })()`,
      ),
      `${viewport} segmented display has shared rectangular border, single divider, no gap, decorative icons, 44px targets and selected sage underline`,
    )
  }
  async function escapeOverlay() {
    for (const type of ['keyDown', 'keyUp'])
      await getCDP().send(
        'Input.dispatchKeyEvent',
        { type, key: 'Escape', code: 'Escape', windowsVirtualKeyCode: 27 },
        page.sessionId,
      )
  }
  async function backdropClick() {
    for (const type of ['mousePressed', 'mouseReleased'])
      await getCDP().send(
        'Input.dispatchMouseEvent',
        { type, x: 8, y: 8, button: 'left', clickCount: 1 },
        page.sessionId,
      )
  }
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
           const add = document.querySelector('button[aria-controls="watchlist-add"]');
          const section = document.querySelector('section[aria-labelledby="saved-heading"]');
          const heading = section.querySelector('#saved-heading');
          const select = section.querySelector('#watchlist-sort');
          const filter = section.querySelector('#watchlist-tag-filter');
          const status = section.querySelector('[role="status"]');
          const icon = select?.parentElement.querySelector('svg[aria-hidden="true"]');
           const mobile = innerWidth < 1024;
           const config = document.querySelector('button[aria-controls="watchlist-configuration"]');
           return add.disabled && add.checkVisibility() && !document.querySelector('#watchlist-query')
             && (mobile ? config.checkVisibility() && !select.checkVisibility() : select.checkVisibility())
            && document.querySelectorAll('main [role="status"]').length === 1
            && status.textContent.trim() === 'Chargement de la watchlist…'
            && !section.querySelector('li') && !section.textContent.includes('Votre watchlist est vide')
             && select.disabled && select.value === ''
            && [...section.querySelectorAll('[aria-label="Affichage des films"] button')].every(button => button.disabled)
            && select.selectedOptions[0].disabled && select.selectedOptions[0].textContent.trim() === 'Trier par'
            && select.labels[0].textContent.trim() === 'Trier par' && select.labels[0].classList.contains('sr-only')
            && filter.disabled && filter.value === '' && filter.selectedOptions[0].disabled
            && filter.selectedOptions[0].textContent.trim() === 'Filtrer par tag'
            && filter.labels[0].textContent.trim() === 'Filtrer par tag' && filter.labels[0].classList.contains('sr-only')
            && document.documentElement.scrollWidth <= innerWidth
             && (mobile || select.getBoundingClientRect().bottom <= status.getBoundingClientRect().top);
        })()`,
      ),
      `${viewport} loading keeps disabled header add, responsive controls and one skeleton, without inline search or false empty state`,
    )
    if (viewport === 'desktop')
      await checkSegmentedDisplay(`${viewport} loading`, -1, true)
  }
  async function checkTagFilterPresentation(viewport) {
    check(
      await evaluate(
        page,
        `(() => {
           const select = document.querySelector(innerWidth < 1024 ? '#watchlist-mobile-tag-filter' : '#watchlist-tag-filter');
          const label = select.labels[0], control = select.getBoundingClientRect(), style = getComputedStyle(select);
          const icon = select.parentElement.querySelector('svg[aria-hidden="true"]');
          const bounds = icon?.getBoundingClientRect(), labelBounds = label.getBoundingClientRect();
          return label.textContent.trim() === 'Filtrer par tag' && label.classList.contains('sr-only')
            && getComputedStyle(label).position === 'absolute' && labelBounds.width <= 1 && labelBounds.height <= 1
            && icon?.classList.contains('lucide-list-filter') && bounds?.width === 20 && bounds.height === 20
            && getComputedStyle(icon).pointerEvents === 'none' && style.appearance === 'auto'
            && control.height >= 44 && control.left >= 0 && control.right <= innerWidth
            && bounds.left > control.left && bounds.right < control.right && bounds.top > control.top && bounds.bottom < control.bottom
            && control.left + parseFloat(style.borderLeftWidth) + parseFloat(style.paddingLeft) >= bounds.right + 8
            && Math.abs(bounds.top + bounds.height / 2 - control.top - control.height / 2) < 1
            && document.elementFromPoint(bounds.left + bounds.width / 2, bounds.top + bounds.height / 2) === select;
        })()`,
      ),
      `${viewport} native tag filter keeps hidden label, inset 20px click-through icon, text clearance and 44px target`,
    )
  }
  async function checkCompactLayout(width) {
    const mobile = width < 1024
    const sortId = mobile ? 'watchlist-mobile-sort' : 'watchlist-sort'
    if (mobile) {
      check(
        await evaluate(
          page,
          `!document.querySelector('#watchlist-sort').checkVisibility() && !document.querySelector('#watchlist-tag-filter').checkVisibility() && !document.querySelector('#watchlist-query') && document.querySelector('button[aria-controls="watchlist-configuration"]').checkVisibility() && document.querySelector('button[aria-controls="watchlist-tag-manager"]').checkVisibility()`,
        ),
        `${width}px base hides preferences and search, keeps separate tag manager`,
      )
      check(
        await evaluate(
          page,
          `(() => {
        const heading = document.querySelector('#saved-heading'), config = document.querySelector('button[aria-controls="watchlist-configuration"]'), tags = document.querySelector('button[aria-controls="watchlist-tag-manager"]');
        const h = heading.getBoundingClientRect(), c = config.getBoundingClientRect(), t = tags.getBoundingClientRect();
        const center = r => r.top + r.height / 2;
        return heading.textContent.trim() === 'Mes films' && h.height < 30 && config.parentElement.contains(tags)
          && config.textContent.trim() === '' && config.getAttribute('aria-label') === 'Configuration' && config.querySelector('svg[aria-hidden="true"]')
          && tags.textContent.trim() === 'Tags' && Math.abs(center(h) - center(c)) < 1 && Math.abs(center(c) - center(t)) < 1
          && [c,t].every(r => r.width >= 44 && r.height >= 44) && c.left >= h.right + 8 && t.left >= c.right + 8
          && h.left >= 0 && t.right <= innerWidth && document.documentElement.scrollWidth <= innerWidth;
      })()`,
        ),
        `${width}px Mes films, icon-only accessible Configuration and Tags share one row with 44px targets`,
      )
      await evaluate(
        page,
        `document.querySelector('button[aria-label="Configuration"]').focus()`,
      )
      for (const type of ['keyDown', 'keyUp'])
        await getCDP().send(
          'Input.dispatchKeyEvent',
          { type, key: 'Tab', code: 'Tab', windowsVirtualKeyCode: 9 },
          page.sessionId,
        )
      check(
        await evaluate(
          page,
          `document.activeElement.getAttribute('aria-controls') === 'watchlist-tag-manager' && document.activeElement.textContent.trim() === 'Tags'`,
        ),
        `${width}px toolbar keyboard order is Configuration then Tags`,
      )
      await evaluate(
        page,
        'document.activeElement.blur(); window.scrollTo(0, 0)',
      )
      await screenshot(page, `toolbar-${width}`, false)
      await click(page, 'Configuration')
      await until(
        page,
        `document.querySelector('#watchlist-configuration')?.matches(':modal')`,
        'configuration enters top layer',
      )
    }
    check(
      await evaluate(
        page,
        `(() => {
        const section = document.querySelector('section[aria-labelledby="saved-heading"]');
         const filter = document.querySelector('${mobile ? '#watchlist-mobile-tag-filter' : '#watchlist-tag-filter'}'), sort = document.querySelector('#${sortId}');
        const manager = document.querySelector('button[aria-controls="watchlist-tag-manager"]');
        const f = filter.getBoundingClientRect(), s = sort.getBoundingClientRect(), m = manager.getBoundingClientRect();
         const toggle = [...document.querySelectorAll('[aria-label="Affichage des films"]')].find(node=>node.checkVisibility()).getBoundingClientRect();
        const close = (a, b) => Math.abs(a - b) < 1;
        const full = r => close(r.left, section.getBoundingClientRect().left) && close(r.right, section.getBoundingClientRect().right);
         const sheet = document.querySelector('#watchlist-configuration')?.getBoundingClientRect();
        return document.documentElement.scrollWidth <= innerWidth
          && [f,s,m].every(r => r.height >= 44 && r.width >= 44 && r.left >= 0 && r.right <= innerWidth)
           && manager.textContent.trim() === '${mobile ? 'Tags' : 'Gérer les tags'}'
           && (${width} >= 1024 ? close(f.bottom,s.bottom) && close(s.bottom,m.bottom)
             : close(sheet.bottom,innerHeight) && sheet.top >= 16 && f.bottom + 8 <= s.top && toggle.bottom <= f.top && document.body.style.overflow === 'hidden' && document.activeElement.getAttribute('aria-label') === 'Fermer la configuration' && !document.querySelector('#watchlist-configuration button[aria-controls="watchlist-tag-manager"]'));
      })()`,
      ),
      `${width}px toolbar keeps readable control geometry, adaptive heading/toggle and compact mobile vertical gaps`,
    )
    for (const value of [
      'added_desc',
      'added_asc',
      'title_asc',
      'title_desc',
      'release_desc',
      'release_asc',
    ]) {
      await selectSort(value)
      check(
        await evaluate(
          page,
          `(() => {
           const select = document.querySelector('#${sortId}'), option = select.selectedOptions[0], css = getComputedStyle(select);
          const canvas = document.createElement('canvas'), ctx = canvas.getContext('2d');
          ctx.font = css.font;
          const available = select.clientWidth - parseFloat(css.paddingLeft) - parseFloat(css.paddingRight) - 20;
          const releaseLabels = { release_desc: 'Sortie FR récente', release_asc: 'Sortie FR ancienne' };
          return option.textContent.trim().length > 0 && !!option.getAttribute('aria-label')
            && (!releaseLabels[select.value] || option.textContent.trim() === releaseLabels[select.value])
            && ctx.measureText(option.textContent.trim()).width <= available && css.appearance === 'auto';
        })()`,
        ),
        `${width}px ${value} selected label fits beside inset icon and reserved native arrow`,
      )
      if (value.startsWith('release_') && width < 640) {
        await evaluate(
          page,
          'document.activeElement.blur(); window.scrollTo(0, 0)',
        )
        await screenshot(page, `compact-${value}-${width}`)
      }
    }
    await evaluate(
      page,
      `document.querySelector('${mobile ? '#watchlist-mobile-tag-filter' : '#watchlist-tag-filter'}').focus()`,
    )
    for (const id of mobile
      ? [sortId]
      : ['watchlist-sort', 'watchlist-tag-manager']) {
      for (const type of ['keyDown', 'keyUp'])
        await getCDP().send(
          'Input.dispatchKeyEvent',
          { type, key: 'Tab', code: 'Tab', windowsVirtualKeyCode: 9 },
          page.sessionId,
        )
      check(
        await evaluate(
          page,
          `(() => { const active = document.activeElement, css = getComputedStyle(active); return (active.id === '${id}' || active.getAttribute('aria-controls') === '${id}') && active.matches(':focus-visible') && ((css.outlineStyle !== 'none' && parseFloat(css.outlineWidth) > 0) || css.boxShadow !== 'none'); })()`,
        ),
        `${width}px native tab order reaches ${id} with visible focus`,
      )
    }
    if (mobile) {
      await checkSegmentedDisplay(`${width}px sheet`, 1)
      await checkTagFilterPresentation(`${width}px sheet`)
      await screenshot(page, `configuration-${width}`, false)
      await click(page, 'Fermer la configuration')
      await until(
        page,
        `!document.querySelector('#watchlist-configuration') && document.activeElement.getAttribute('aria-controls') === 'watchlist-configuration' && document.body.style.overflow !== 'hidden'`,
        'configuration close restores opener and scroll',
      )
    }
    await evaluate(page, 'document.activeElement.blur(); window.scrollTo(0, 0)')
    await screenshot(page, `compact-grouped-${width}`)
  }
  async function finishRead() {
    for (let i = 0; !releaseRead && i < 100; i++) await delay(20)
    check(!!releaseRead, 'held private watchlist GET reached fixture')
    holdRead = false
    releaseRead()
    releaseRead = undefined
  }
  async function selectSort(value) {
    const id = await evaluate(
      page,
      `document.querySelector('#watchlist-configuration') ? 'watchlist-mobile-sort' : 'watchlist-sort'`,
    )
    await until(
      page,
      `!!document.querySelector('#${id}:not(:disabled)')`,
      'sort control available',
    )
    await evaluate(
      page,
      `(() => { const select = document.querySelector('#${id}'); select.value = ${JSON.stringify(value)}; select.dispatchEvent(new Event('change', { bubbles: true })); })()`,
    )
    await until(
      page,
      `document.querySelector('#${id}:not(:disabled)')?.value === ${JSON.stringify(value)}`,
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
      `(() => { const section = document.querySelector('section[aria-labelledby="saved-heading"]'); const select = section.querySelector('#watchlist-sort'); const filter = section.querySelector('#watchlist-tag-filter'); return !document.querySelector('main [role="status"]') && document.querySelectorAll('main [role="alert"]').length === 1 && select.disabled && select.value === '' && select.selectedOptions[0].textContent.trim() === 'Tri indisponible' && filter.disabled && filter.value === '' && filter.selectedOptions[0].textContent.trim() === 'Filtre indisponible' && !section.querySelector('li') && !section.textContent.includes('Votre watchlist est vide'); })()`,
    ),
    'failed initial read retains heading and disabled neutral sort/filter without skeleton or false empty state',
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
      `!document.querySelector('button[aria-controls="watchlist-add"]').disabled && !document.querySelector('#watchlist-query') && document.querySelectorAll('h1').length === 1`,
    ),
    'watchlist form enabled with single heading',
  )
  check(
    !writes.some((write) => write.path.endsWith('/preferences')),
    'initial hydration and retry never POST default preferences',
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
    ['Trier par', 'Filtrer par tag'].every((name) =>
      accessibility.nodes.some(
        (node) => node.role?.value === 'combobox' && node.name?.value === name,
      ),
    ),
    'native sort and filter selectors retain accessible names',
  )
  check(
    ['Liste', 'Par tag'].every((label, index) =>
      accessibility.nodes.some(
        (node) =>
          node.role?.value === 'button' &&
          node.name?.value.toLocaleLowerCase('fr') ===
            label.toLocaleLowerCase('fr') &&
          node.properties?.some(
            (property) =>
              property.name === 'pressed' &&
              String(property.value?.value) === String(index === 0),
          ),
      ),
    ),
    'display buttons expose only Liste and Par tag names with exclusive pressed state',
  )
  check(
    await evaluate(
      page,
      `(() => { const select = document.querySelector('#watchlist-sort'); const control = select.getBoundingClientRect(); const style = getComputedStyle(select); const icon = select.parentElement.querySelector('svg').getBoundingClientRect(); const filter = document.querySelector('#watchlist-tag-filter').getBoundingClientRect(); const trigger = document.querySelector('button[aria-controls="watchlist-tag-manager"]').getBoundingClientRect(); return control.height >= 44 && icon.left > control.left && icon.right < control.right && icon.top > control.top && icon.bottom < control.bottom && control.left + parseFloat(style.borderLeftWidth) + parseFloat(style.paddingLeft) >= icon.right + 8 && Math.abs(icon.top + icon.height / 2 - control.top - control.height / 2) < 1 && Math.abs(filter.bottom - control.bottom) < 1 && Math.abs(trigger.bottom - control.bottom) < 1; })()`,
    ),
    'desktop sort icon sits inside select with text clearance and filter/manager alignment',
  )
  await checkTagFilterPresentation('desktop')
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
  await checkTagModalStyle('desktop')
  await screenshot(page, 'tag-manager-desktop', false)
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
  await screenshot(page, 'editorial-palette-create-desktop', false)
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
  await evaluate(
    page,
    `document.querySelector('#watchlist-tag-create input[value="amber"]').focus()`,
  )
  for (const type of ['keyDown', 'keyUp'])
    await getCDP().send(
      'Input.dispatchKeyEvent',
      { type, key: ' ', code: 'Space', windowsVirtualKeyCode: 32 },
      page.sessionId,
    )
  check(
    await evaluate(
      page,
      `(() => { const r = document.activeElement; return r.value === 'amber' && r.checked && r.closest('label').querySelectorAll('svg').length === 1 && document.querySelectorAll('#watchlist-tag-create svg').length === 1 && getComputedStyle(r.closest('label')).outlineWidth === '2px' })()`,
    ),
    'native Space selects unchecked color with one non-hue marker and visible keyboard focus',
  )
  await checkPalette(page, evaluate, check)
  await screenshot(page, 'editorial-palette-keyboard-desktop', false)
  for (const type of ['keyDown', 'keyUp'])
    await getCDP().send(
      'Input.dispatchKeyEvent',
      { type, key: 'ArrowLeft', code: 'ArrowLeft', windowsVirtualKeyCode: 37 },
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
  // Let the previous native close event finish before reopening another dialog.
  await evaluate(
    page,
    `new Promise(resolve => {
      document.querySelector('#watchlist-tag-manager').addEventListener('close', () => requestAnimationFrame(resolve), { once: true });
      document.querySelector('button[aria-label="Fermer la gestion des tags"]').click();
    })`,
  )
  check(
    await evaluate(
      page,
      `!document.querySelector('#watchlist-tag-manager') && document.activeElement.getAttribute('aria-controls') === 'watchlist-tag-manager' && document.body.style.overflow !== 'hidden'`,
    ),
    'modal close restores original manager trigger',
  )
  await click(page, 'Gérer les tags')
  await until(
    page,
    `document.querySelector('#watchlist-tag-manager')?.matches(':modal')`,
    'modal top layer ready before physical backdrop click',
  )
  await evaluate(
    page,
    `new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))`,
  )
  for (const type of ['mousePressed', 'mouseReleased']) {
    await getCDP().send(
      'Input.dispatchMouseEvent',
      { type, x: 2, y: 2, button: 'left', clickCount: 1 },
      page.sessionId,
    )
  }
  await until(
    page,
    `!document.querySelector('#watchlist-tag-manager') && document.activeElement.getAttribute('aria-controls') === 'watchlist-tag-manager'`,
    'backdrop close and focus restoration have rendered',
  )
  check(
    await evaluate(
      page,
      `!document.querySelector('#watchlist-tag-manager') && document.activeElement.getAttribute('aria-controls') === 'watchlist-tag-manager'`,
    ),
    'native modal backdrop click closes and restores trigger',
  )
  check(
    await evaluate(page, `document.body.style.overflow !== 'hidden'`),
    'tag modal backdrop releases background scroll lock',
  )
  await click(page, 'Ajouter')
  await until(
    page,
    `document.querySelector('#watchlist-add')?.matches(':modal') && document.activeElement.id === 'watchlist-query' && document.body.style.overflow === 'hidden'`,
    'header add opens focused search dialog and locks background',
  )
  check(
    await evaluate(
      page,
      `(() => { const h = document.querySelector('h1').getBoundingClientRect(), b = document.querySelector('button[aria-controls="watchlist-add"]').getBoundingClientRect(); return b.left > h.right && b.top < h.bottom && b.bottom > h.top })()`,
    ),
    'Ajouter sits at end of Watchlist title row',
  )
  await fill(page, 'watchlist-query', 'private candidate query')
  check(
    await evaluate(
      page,
      `!document.querySelector('#watchlist-catalog-panel li')`,
    ),
    'typing alone never submits search',
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
      `(() => { const dialog = document.querySelector('#watchlist-add'), scroll = dialog.querySelector('.overflow-y-auto'), rect = dialog.getBoundingClientRect(); return dialog.matches(':modal') && rect.top >= 16 && rect.bottom <= innerHeight - 16 && scroll.scrollHeight > scroll.clientHeight && document.documentElement.scrollHeight === ${documentHeight} && document.activeElement.id === 'watchlist-catalog-tab'; })()`,
    ),
    'search modal is viewport bounded, scrollable and does not expand document',
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
      `(() => { const before = scrollY; const scroll = document.querySelector('#watchlist-add .overflow-y-auto'); scroll.scrollTop = 500; return scroll.scrollTop === 500 && scrollY === before; })()`,
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
    await evaluate(
      page,
      `(() => { const link = document.querySelector('#watchlist-external-panel li a'); return link?.getAttribute('href') === 'https://www.themoviedb.org/movie/999' && link.getAttribute('target') === '_blank' && link.getAttribute('rel') === 'noopener noreferrer' && link.getAttribute('referrerpolicy') === 'no-referrer' && !document.querySelector('#watchlist-external-panel a[href^="/film/"]'); })()`,
    ),
    'external title links to fixed TMDB detail in protected new tab, without fictitious public URL',
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
      `!document.querySelector('#watchlist-add') && document.activeElement.getAttribute('aria-controls') === 'watchlist-add' && document.body.style.overflow !== 'hidden'`,
    ),
    'confirmed catalog save closes and restores header focus and scroll',
  )
  check(
    await evaluate(
      page,
      `(() => { const row = ${savedRow('saved-film')}; return row.querySelector('time')?.getAttribute('datetime') === '1998-10-14' && row.querySelector('time').textContent.trim() === '14 octobre 1998' && !row.textContent.includes('1997'); })()`,
    ),
    'saved catalog movie displays verified full French date, not general release year',
  )
  await click(page, 'Ajouter')
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
      `!document.querySelector('#watchlist-add') && document.activeElement.getAttribute('aria-controls') === 'watchlist-add'`,
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
  const removalWrites = () =>
    writes.filter(
      (write) =>
        write.path === '/api/v1/account/watchlist' &&
        write.body.saved === 'false',
    )
  const removePanel = `document.querySelector('#watchlist-remove')`
  async function openRemove(slug = 'external-film') {
    await until(
      page,
      `${savedRow(slug)}?.querySelector('[data-watchlist-remove]:not(:disabled)')`,
      'removal opener ready',
    )
    await evaluate(
      page,
      `${savedRow(slug)}.querySelector('[data-watchlist-remove]').click()`,
    )
    await until(
      page,
      `${removePanel}?.matches(':modal') && document.activeElement.textContent.trim() === 'Annuler'`,
      'confirmation opens with safe Cancel focus',
    )
  }
  async function checkRemovalSurface(name) {
    const geometry = await evaluate(
      page,
      `(() => { const dialog = ${removePanel}, rect = dialog.getBoundingClientRect(), style = getComputedStyle(dialog), header = dialog.firstElementChild, title = document.querySelector('#watchlist-remove-title'), close = header.querySelector('button'), body = header.nextElementSibling; return {left: rect.left, top: rect.top, width: rect.width, bottom: rect.bottom, title: title.textContent, valid: dialog.matches(':modal') && style.borderWidth === '2px' && style.borderColor === 'rgb(39, 39, 42)' && style.borderRadius === '0px' && style.backgroundColor === 'rgb(255, 255, 255)' && style.padding === '0px' && style.boxShadow !== 'none' && rect.width === Math.min(672, innerWidth - 32) && rect.top >= 15.5 && rect.bottom <= innerHeight - 15.5 && getComputedStyle(header).padding === '16px' && getComputedStyle(header).borderBottomWidth === '1px' && close.getBoundingClientRect().width === 44 && close.querySelector('svg').getBoundingClientRect().width === 20 && getComputedStyle(body).padding === '16px' && getComputedStyle(body).overflowY === 'auto' && body.scrollWidth <= body.clientWidth && title.scrollWidth <= title.clientWidth && document.body.style.overflow === 'hidden' } })()`,
    )
    check(
      geometry.valid,
      `${name} confirmation matches square native modal surface, bounded title/internal scroll and fixed close ${JSON.stringify(geometry)}`,
    )
    const tree = await getCDP().send(
      'Accessibility.getFullAXTree',
      {},
      page.sessionId,
    )
    check(
      tree.nodes.some(
        (node) =>
          node.role?.value === 'dialog' &&
          node.name?.value === 'Retirer de la watchlist' &&
          node.description?.value === geometry.title,
      ),
      `${name} native confirmation exposes selected movie as full accessible description`,
    )
    await screenshot(page, `remove-confirmation-${name}`, false)
  }
  await getCDP().send('Page.bringToFront', {}, page.sessionId)
  await checkClock(page, 'remove')
  const beforeRemoveCancel = removalWrites().length
  for (const channel of ['cancel', 'close', 'escape', 'backdrop']) {
    await openRemove()
    check(
      removalWrites().length === beforeRemoveCancel,
      `opening ${channel} confirmation sends no removal`,
    )
    if (channel === 'cancel') {
      await checkRemovalSurface('desktop')
      for (let i = 0; i < 4; i++) {
        for (const type of ['keyDown', 'keyUp'])
          await getCDP().send(
            'Input.dispatchKeyEvent',
            { type, key: 'Tab', code: 'Tab', windowsVirtualKeyCode: 9 },
            page.sessionId,
          )
        check(
          await evaluate(
            page,
            `${removePanel}.contains(document.activeElement) || document.activeElement === document.body`,
          ),
          'confirmation Tab never focuses background control',
        )
      }
      await evaluate(
        page,
        `document.querySelector('[aria-controls="watchlist-add"]').focus()`,
      )
      check(
        await evaluate(
          page,
          `document.activeElement.getAttribute('aria-controls') !== 'watchlist-add'`,
        ),
        'confirmation rejects background programmatic focus',
      )
      await evaluate(
        page,
        `document.querySelector('#watchlist-remove-heading').click()`,
      )
      check(
        await evaluate(page, `${removePanel}.matches(':modal')`),
        'inside confirmation header is not backdrop',
      )
      // Chromium can cycle Tab through browser chrome, triggering account revalidation.
      await getCDP().send('Page.bringToFront', {}, page.sessionId)
      await evaluate(
        page,
        `${removePanel}.querySelector('.account-secondary').focus()`,
      )
      await until(
        page,
        `document.activeElement === ${removePanel}.querySelector('.account-secondary') && !document.querySelector('[aria-controls="watchlist-add"]').disabled && !!${savedRow('external-film')}?.querySelector('[data-watchlist-remove]:not(:disabled)')`,
        'foreground account snapshot ready before cancellation',
      )
      await click(page, 'Annuler')
    } else if (channel === 'close') await click(page, 'Fermer la confirmation')
    else if (channel === 'escape') await escapeOverlay()
    else await backdropClick()
    try {
      await until(
        page,
        `!${removePanel} && document.body.style.overflow !== 'hidden' && document.activeElement.hasAttribute('data-watchlist-remove')`,
        `${channel} closes confirmation, restores same opener and scroll`,
      )
    } catch (cause) {
      const focusState = await evaluate(
        page,
        `({pageFocused: document.hasFocus(), activeTag: document.activeElement.tagName, activeBody: document.activeElement === document.body, activeRemove: document.activeElement.hasAttribute('data-watchlist-remove'), activeConnected: document.activeElement.isConnected, activeDisabled: !!document.activeElement.disabled, activeControls: document.activeElement.getAttribute('aria-controls'), dialogExists: !!${removePanel}, scrollLocked: document.body.style.overflow === 'hidden', enabledRemovals: document.querySelectorAll('[data-watchlist-remove]:not(:disabled)').length, addDisabled: !!document.querySelector('[aria-controls="watchlist-add"]').disabled})`,
      )
      throw new Error(
        `${channel} cancellation focus diagnostic ${JSON.stringify(focusState)}`,
        { cause },
      )
    }
    check(
      removalWrites().length === beforeRemoveCancel &&
        saved.includes('external-film'),
      `${channel} never mutates membership`,
    )
  }
  for (const type of ['keyDown', 'keyUp'])
    await getCDP().send(
      'Input.dispatchKeyEvent',
      { type, key: 'Tab', code: 'Tab', windowsVirtualKeyCode: 9 },
      page.sessionId,
    )
  await until(
    page,
    `!!document.querySelector('[data-watchlist-remove]:not(:disabled)') && !document.querySelector('[aria-controls="watchlist-add"]').disabled`,
    'committed row ready for keyboard focus styling',
  )
  // Native dialog dismissal changes the Tab starting point; inspect the current cross.
  const crossFocus = await evaluate(
    page,
    `(() => { const cross = document.querySelector('[data-watchlist-remove]:not(:disabled)'); cross.focus(); const style = getComputedStyle(cross); return {active: document.activeElement === cross, focusVisible: cross.matches(':focus-visible'), width: style.outlineWidth, line: style.outlineStyle, offset: style.outlineOffset} })()`,
  )
  check(
    crossFocus.active &&
      crossFocus.focusVisible &&
      crossFocus.width === '3px' &&
      crossFocus.line === 'solid' &&
      crossFocus.offset === '3px',
    `borderless removal cross retains separated keyboard focus outline ${JSON.stringify(crossFocus)}`,
  )
  externalTitle =
    'Un très long titre de cinéma pour identifier clairement le film à retirer sans dépasser la fenêtre de confirmation'
  await evaluate(page, `window.dispatchEvent(new Event('focus'))`)
  await until(
    page,
    `${savedRow('external-film')}?.textContent.includes(${JSON.stringify(externalTitle)}) && !document.querySelector('[aria-controls="watchlist-add"]').disabled`,
    'long removal title snapshot ready',
  )
  await openRemove()
  await checkRemovalSurface('desktop-long-title')
  await click(page, 'Annuler')
  externalTitle = 'Film externe'
  await evaluate(page, `window.dispatchEvent(new Event('focus'))`)
  await until(
    page,
    `${savedRow('external-film')}?.querySelector('a').textContent.trim() === 'Film externe' && !document.querySelector('[aria-controls="watchlist-add"]').disabled`,
    'original title restored before conflict',
  )
  conflict = true
  await openRemove()
  await evaluate(
    page,
    `${removePanel}.querySelector('.account-primary').click()`,
  )
  await until(
    page,
    `${removePanel}?.querySelector('[role="alert"]') && ${removePanel}.querySelector('.account-primary').disabled && !document.querySelector('[aria-controls="watchlist-add"]').disabled`,
    'removal conflict completes readback with local recovery and consumed intent',
  )
  check(
    removalWrites().length === beforeRemoveCancel + 1 &&
      saved.includes('external-film'),
    'conflict dispatches once without false removal',
  )
  await click(page, 'Réessayer')
  await until(
    page,
    `!${removePanel}.querySelector('[role="alert"]') && !document.querySelector('[aria-controls="watchlist-add"]').disabled`,
    'confirmation retry only refreshes authoritative state',
  )
  await evaluate(
    page,
    `${removePanel}.querySelector('.account-primary').click()`,
  )
  check(
    removalWrites().length === beforeRemoveCancel + 1,
    'readback never replays removal or reuses consumed confirmation',
  )
  await click(page, 'Annuler')
  uncertainRemoval = true
  await openRemove()
  await evaluate(
    page,
    `${removePanel}.querySelector('.account-primary').click()`,
  )
  await until(
    page,
    `${removePanel}?.querySelector('[role="alert"]') && !document.querySelector('[aria-controls="watchlist-add"]').disabled`,
    'uncertain committed removal stays open with readback error',
  )
  check(
    !saved.includes('external-film') &&
      removalWrites().length === beforeRemoveCancel + 2,
    'uncertain committed removal neither reports success nor replays',
  )
  await click(page, 'Réessayer')
  await until(
    page,
    `!${removePanel}.querySelector('[role="alert"]') && !document.querySelector('[aria-controls="watchlist-add"]').disabled`,
    'removed target readback recovers without a new removal',
  )
  check(
    await evaluate(
      page,
      `${removePanel}.querySelector('.account-primary').disabled`,
    ),
    'externally absent target cannot be reconfirmed',
  )
  await click(page, 'Annuler')
  await until(
    page,
    `!${removePanel} && document.activeElement.hasAttribute('data-watchlist-remove') && document.body.style.overflow !== 'hidden'`,
    'removed opener falls back to surviving list cross',
  )
  // Restore synthetic fixture membership for the existing preference/tag acceptance lane.
  saved.unshift('external-film')
  revision++
  await evaluate(page, `window.dispatchEvent(new Event('focus'))`)
  await until(
    page,
    `${savedRow('external-film')}?.querySelector('[data-watchlist-remove]:not(:disabled)')`,
    'restored fixture snapshot ready',
  )
  await openRemove()
  await evaluate(
    page,
    `window.dispatchEvent(new PageTransitionEvent('pagehide', { persisted: true }))`,
  )
  check(
    await evaluate(
      page,
      `!${removePanel} && !document.querySelector('#watchlist-remove-title') && document.body.style.overflow !== 'hidden'`,
    ),
    'pagehide synchronously purges removal target/title and scroll lock',
  )
  await evaluate(
    page,
    `window.dispatchEvent(new PageTransitionEvent('pageshow', { persisted: true }))`,
  )
  await until(
    page,
    `${savedRow('external-film')}?.querySelector('[data-watchlist-remove]:not(:disabled)')`,
    'pageshow restores membership without removal intent',
  )
  check(
    await evaluate(page, `!${removePanel}`),
    'pageshow does not restore private confirmation',
  )
  await openRemove()
  session = {
    enabled: true,
    state: 'complete',
    account: { ...owner, username: 'removal_replacement_owner' },
  }
  await evaluate(page, `window.dispatchEvent(new Event('focus'))`)
  await until(
    page,
    `!${removePanel} && document.body.style.overflow !== 'hidden'`,
    'owner replacement clears confirmation and lock',
  )
  check(
    await evaluate(
      page,
      `!document.querySelector('#watchlist-remove-title') && document.activeElement.getAttribute('aria-controls') !== 'watchlist-remove'`,
    ),
    'owner replacement never restores old private title or opener',
  )
  session = { enabled: true, state: 'complete', account: owner }
  await go(page, '/compte/watchlist')
  await openRemove()
  hold = true
  release = undefined
  const beforeLate = removalWrites().length
  await evaluate(
    page,
    `${removePanel}.querySelector('.account-primary').click(); ${removePanel}.querySelector('.account-primary').click()`,
  )
  for (let i = 0; !release && i < 100; i++) await delay(20)
  check(
    !!release && removalWrites().length === beforeLate + 1,
    'confirmed held removal dispatches exactly once despite duplicate click',
  )
  // Account navigation revalidation waits for the held writer. Start navigation,
  // observe synchronous page cleanup, then release the write before awaiting arrival.
  await evaluate(page, `(() => { void ${router}.push('/compte') })()`)
  await until(
    page,
    `!${removePanel} && document.body.style.overflow !== 'hidden' && !document.querySelector('#watchlist-remove-title')`,
    'navigation purges pending confirmation and private title',
  )
  hold = false
  release()
  await until(
    page,
    `${router}?.currentRoute.value.path === '/compte'`,
    'departed page remains active',
  )
  for (let i = 0; saved.includes('external-film') && i < 100; i++)
    await delay(20)
  check(
    !saved.includes('external-film') &&
      (await evaluate(
        page,
        `!${removePanel} && document.body.style.overflow !== 'hidden'`,
      )),
    'late committed removal cannot resurrect overlay or scroll lock after navigation',
  )
  saved.unshift('external-film')
  revision++
  await go(page, '/compte/watchlist')
  await until(
    page,
    `${savedRow('external-film')}?.querySelector('[data-watchlist-remove]:not(:disabled)')`,
    'existing tag lane restored after late removal',
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
    await until(
      page,
      `!!document.querySelector('#watchlist-tag-filter:not(:disabled)')`,
      'filter available',
    )
    await evaluate(
      page,
      `(() => { const select = document.querySelector('#watchlist-tag-filter'); select.value = ${JSON.stringify(id)}; select.dispatchEvent(new Event('change', { bubbles: true })); })()`,
    )
    await until(
      page,
      `document.querySelector('#watchlist-tag-filter:not(:disabled)')?.value === ${JSON.stringify(id)}`,
      'filter preference committed',
    )
  }
  const firstTag = tags()[0].id
  const secondTag = tags()[1].id
  const preferenceDevice = await tab()
  await go(preferenceDevice, '/compte/watchlist')
  await until(
    preferenceDevice,
    `!!document.querySelector('#watchlist-tag-filter:not(:disabled)')`,
    'separate device reads preferences',
  )
  check(
    preferenceDevice.browserContextId !== page.browserContextId,
    'device fixture uses isolated storage and BroadcastChannel context',
  )
  const preferenceCount = () =>
    writes.filter((write) => write.path.endsWith('/preferences')).length
  const pair = (mode, tag) =>
    `document.querySelector('[aria-label="Affichage des films"] button[aria-pressed="true"]:not(:disabled)')?.textContent.trim() === '${mode}' && document.querySelector('#watchlist-tag-filter:not(:disabled)')?.value === ${JSON.stringify(tag)}`
  const beforePreference = preferenceCount()
  const preferenceRevision = String(revision)
  hold = true
  release = undefined
  await evaluate(
    page,
    `document.querySelector('[aria-label="Affichage des films"] button:last-child').click()`,
  )
  for (let i = 0; !release && i < 100; i++) await delay(20)
  check(!!release, 'preference write reaches held fixture')
  check(
    await evaluate(
      page,
      `document.querySelector('[aria-label="Affichage des films"] button[aria-pressed="true"]').textContent.trim() === 'Liste' && document.querySelector('#watchlist-tag-filter').value === '' && ['#watchlist-sort', '#watchlist-tag-filter', '[aria-label="Affichage des films"] button'].every(selector => [...document.querySelectorAll(selector)].every(control => control.disabled))`,
    ),
    'pending display keeps committed pair and blocks filter, display and sort',
  )
  await evaluate(
    page,
    `(() => { const select = document.querySelector('#watchlist-tag-filter'); select.value = '${firstTag}'; select.dispatchEvent(new Event('change', {bubbles:true})); })()`,
  )
  check(
    await evaluate(
      page,
      `document.querySelector('#watchlist-tag-filter').value === ''`,
    ),
    'disabled synthetic filter change rolls native selection back without write',
  )
  hold = false
  release()
  release = undefined
  await until(page, pair('Par tag', ''), 'held display commits')
  check(
    preferenceCount() === beforePreference + 1 &&
      JSON.stringify(
        writes.filter((write) => write.path.endsWith('/preferences')).at(-1)
          .body,
      ) ===
        JSON.stringify({
          expected_username: owner.username,
          expected_revision: preferenceRevision,
          view_mode: 'tags',
          filter_tag_id: '',
        }),
    'one exact owner/revision preference POST carries request-only all-films sentinel',
  )
  await filterTag(firstTag)
  await go(page, '/compte/watchlist')
  await until(
    page,
    pair('Par tag', firstTag),
    'reload restores both committed preferences',
  )
  check(
    await evaluate(preferenceDevice, pair('Liste', '')),
    'separate device receives no live push',
  )
  await evaluate(preferenceDevice, `window.dispatchEvent(new Event('focus'))`)
  await until(
    preferenceDevice,
    pair('Par tag', firstTag),
    'separate device focus reads committed pair',
  )
  await evaluate(
    preferenceDevice,
    `(() => { const select = document.querySelector('#watchlist-tag-filter'); select.value = '${secondTag}'; select.dispatchEvent(new Event('change', {bubbles:true})); })()`,
  )
  await until(
    preferenceDevice,
    pair('Par tag', secondTag),
    'second device commits its filter',
  )
  check(
    await evaluate(page, pair('Par tag', firstTag)),
    'first device retains last snapshot until reconnect',
  )
  await evaluate(page, `window.dispatchEvent(new Event('online'))`)
  await until(
    page,
    pair('Par tag', secondTag),
    'reconnect updates filter without preference replay',
  )
  await click(preferenceDevice, 'Liste')
  const beforeConflict = preferenceCount()
  await evaluate(
    page,
    `(() => { const select = document.querySelector('#watchlist-tag-filter'); select.value = '${firstTag}'; select.dispatchEvent(new Event('change', {bubbles:true})); })()`,
  )
  await until(
    page,
    `${pair('Liste', secondTag)} && !!document.querySelector('main [role="alert"]')`,
    'stale preference CAS reads back entire current pair',
  )
  check(
    preferenceCount() === beforeConflict + 1,
    'stale preference write is never replayed',
  )
  uncertainPreferences = true
  const beforeUncertain = preferenceCount()
  await evaluate(
    page,
    `document.querySelector('[aria-label="Affichage des films"] button:last-child').click()`,
  )
  await until(
    page,
    `${pair('Par tag', secondTag)} && !!document.querySelector('main [role="alert"]')`,
    'lost preference response reconciles committed pair',
  )
  check(
    preferenceCount() === beforeUncertain + 1,
    'uncertain committed preference has no automatic replay',
  )
  uncertainPreferences = true
  failRead = true
  await evaluate(
    page,
    `document.querySelector('[aria-label="Affichage des films"] button:first-child').click()`,
  )
  await until(
    page,
    `!!document.querySelector('main [role="alert"]') && document.querySelector('#watchlist-sort').disabled`,
    'failed preference readback blocks all writers',
  )
  await click(page, 'Réessayer')
  await until(
    page,
    pair('Liste', secondTag),
    'explicit retry recovers committed pair',
  )
  check(
    !requests.some(
      (request) =>
        request.query.includes('filter_tag_id') ||
        request.query.includes('view_mode'),
    ),
    'preferences never travel in URL query',
  )
  await filterTag('')
  await getCDP().send('Target.disposeBrowserContext', {
    browserContextId: preferenceDevice.browserContextId,
  })
  const preferenceTab = await tab(page.browserContextId)
  await go(preferenceTab, '/compte/watchlist')
  await until(preferenceTab, pair('Liste', ''), 'same-context tab ready')
  await click(preferenceTab, 'Par tag')
  await until(
    page,
    pair('Par tag', ''),
    'ID-free cross-tab signal refreshes authoritative mode',
  )
  await click(page, 'Liste')
  await until(
    preferenceTab,
    pair('Liste', ''),
    'cross-tab preference updates in both directions',
  )
  check(true, 'same-browser preference refresh uses existing BroadcastChannel')
  await getCDP().send('Page.close', {}, preferenceTab.sessionId)
  await getCDP().send('Page.bringToFront', {}, page.sessionId)
  await until(page, pair('Liste', ''), 'native filter ready')
  const keyboardTag = await evaluate(
    page,
    `document.querySelector('#watchlist-tag-filter').options[1].value`,
  )
  await evaluate(
    page,
    `document.querySelector('#watchlist-tag-filter').focus()`,
  )
  for (const type of ['keyDown', 'keyUp'])
    await getCDP().send(
      'Input.dispatchKeyEvent',
      { type, key: 'ArrowDown', code: 'ArrowDown', windowsVirtualKeyCode: 40 },
      page.sessionId,
    )
  await until(
    page,
    pair('Liste', keyboardTag),
    'native keyboard filter commits',
  )
  check(
    await evaluate(
      page,
      `document.activeElement.id === 'watchlist-tag-filter' && document.activeElement.matches(':focus-visible') && (getComputedStyle(document.activeElement).outlineStyle !== 'none' || getComputedStyle(document.activeElement).boxShadow !== 'none')`,
    ),
    'native preference select restores visible keyboard focus after commit',
  )
  await filterTag('')
  const picker = `${savedRow('saved-film')}.querySelector('[role="group"]')`
  const floatingHeaderHeights = new Map()
  async function checkFloatingStyle(name, placement) {
    const geometry = await evaluate(
      page,
      `(() => {
      const p = document.querySelector('section[aria-labelledby="saved-heading"] li [role="group"]'), r = p.getBoundingClientRect(), s = getComputedStyle(p), header = p.firstElementChild, h = getComputedStyle(header), title = header.querySelector('h3'), label = title.firstElementChild, movie = title.lastElementChild, l = getComputedStyle(label), m = getComputedStyle(movie), lr = label.getBoundingClientRect(), mr = movie.getBoundingClientRect(), close = header.querySelector('button'), c = close.getBoundingClientRect(), body = p.lastElementChild, b = getComputedStyle(body), anchor = p.parentElement.querySelector('button[aria-expanded]').getBoundingClientRect(), original = p.closest('li').querySelector('a').textContent.trim();
      const square = [p, close, ...body.querySelectorAll('label')].every(node => getComputedStyle(node).borderRadius === '0px');
      return { left:r.left, right:r.right, top:r.top, bottom:r.bottom, width:r.width, height:r.height, anchorTop:anchor.top, anchorBottom:anchor.bottom, headerHeight:header.getBoundingClientRect().height, movieHeight:mr.height, movieLineHeight:parseFloat(m.lineHeight), titleFont:parseFloat(l.fontSize), movieFont:parseFloat(m.fontSize), titleWidth:movie.clientWidth, titleScrollWidth:movie.scrollWidth, original, surface:s.position === 'fixed' && s.borderTopWidth === '2px' && s.borderColor === 'rgb(39, 39, 42)' && s.backgroundColor === 'rgb(255, 255, 255)' && s.color === 'rgb(39, 39, 42)' && s.padding === '0px' && s.boxShadow !== 'none' && square, header:h.padding === '16px' && h.gap === '12px' && h.borderBottomWidth === '1px' && label.classList.contains('account-heading') && label.textContent === 'Tags' && movie.textContent === original && l.display === 'block' && m.display === 'block' && parseFloat(m.fontSize) < parseFloat(l.fontSize) && m.fontWeight === '400' && m.whiteSpace === 'nowrap' && m.overflowX === 'hidden' && m.textOverflow === 'ellipsis' && Math.abs(mr.height-parseFloat(m.lineHeight)) < 1 && mr.top >= lr.bottom && mr.right <= c.left-12+1 && title.classList.contains('min-w-0') && !title.hasAttribute('aria-hidden'), close:c.width === 44 && c.height === 44 && close.querySelector('svg').getAttribute('width') === '20' && c.right <= r.right && c.top >= r.top && c.bottom <= r.bottom, body:b.padding === '16px' && b.overflowY === 'auto' && b.overscrollBehaviorY === 'contain' && body.clientHeight > 0 && p.scrollWidth <= p.clientWidth, anchored:p.getAttribute('role') === 'group' && !p.hasAttribute('aria-modal') && !document.querySelector('dialog:modal') && document.body.style.overflow !== 'hidden', bound:r.left >= 8 && r.right <= innerWidth-8 && r.top >= 8 && r.bottom <= innerHeight-8 && r.width <= 320 && r.height <= 320 };
    })()`,
    )
    const previousHeight = floatingHeaderHeights.get(geometry.width)
    check(
      previousHeight === undefined ||
        Math.abs(previousHeight - geometry.headerHeight) < 1,
      `${name} short and long movie titles retain equal header height`,
    )
    floatingHeaderHeights.set(geometry.width, geometry.headerHeight)
    check(
      geometry.original.length > 60
        ? geometry.titleScrollWidth > geometry.titleWidth
        : geometry.titleScrollWidth === geometry.titleWidth,
      `${name} one-line movie title ${geometry.original.length > 60 ? 'ellipsizes long text' : 'fits short text'}`,
    )
    const floatingAX = await getCDP().send(
      'Accessibility.getFullAXTree',
      {},
      page.sessionId,
    )
    check(
      floatingAX.nodes.some(
        (node) =>
          node.role?.value === 'group' &&
          node.name?.value === `Tags ${geometry.original}`,
      ),
      `${name} floating group retains full accessible movie title`,
    )
    check(
      geometry.surface &&
        geometry.header &&
        geometry.close &&
        geometry.body &&
        geometry.anchored &&
        geometry.bound,
      `${name} floating picker matches square surface/header/body without modal behavior ${JSON.stringify(geometry)}`,
    )
    if (placement)
      check(
        Math.abs(
          placement === 'below'
            ? geometry.top - geometry.anchorBottom - 8
            : geometry.anchorTop - geometry.bottom - 8,
        ) < 1,
        `${name} floating picker remains ${placement} trigger with 8px gap`,
      )
    await screenshot(page, `tag-floating-style-${name}`, false)
  }
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
      `(() => { const p = ${picker}, r = p.getBoundingClientRect(), button = ${savedRow('saved-film')}.querySelector('button[aria-expanded]'); return getComputedStyle(p).position === 'fixed' && r.width <= 320 && r.left >= 8 && r.right <= innerWidth - 8 && r.top >= 8 && r.bottom <= innerHeight - 8 && document.activeElement === ${firstCheckbox} && button.textContent.trim() === 'Tag' && button.getBoundingClientRect().height >= 28 && ${savedRow('saved-film')}.getBoundingClientRect().height === ${rowHeight} && document.documentElement.scrollHeight === ${listHeight}; })()`,
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
        node.role?.value === 'group' && node.name?.value === 'Tags Film favori',
    ),
    'picker group exposes film-specific title',
  )
  await screenshot(page, 'tag-picker-desktop', false)
  await checkFloatingStyle('desktop', 'below')
  await checkChips(page, evaluate, check, 'desktop assignment picker')
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
  check(
    requests
      .slice(filterRequests)
      .filter((request) => request.path.endsWith('/preferences')).length === 1,
    'filter commits one preference write',
  )
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
  await checkPalette(page, evaluate, check)
  await checkChips(page, evaluate, check, 'desktop editor and saved rows')
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
  await until(
    page,
    `!document.querySelector('#watchlist-tag-manager')`,
    'breakpoint closes tag manager without stale focus restoration',
  )
  await click(page, 'Gérer les tags')
  await until(
    page,
    `document.querySelector('#watchlist-tag-manager')?.matches(':modal')`,
    'mobile manager remains separately accessible',
  )
  check(
    await evaluate(
      page,
      `(() => { const dialog = document.querySelector('#watchlist-tag-manager'); const body = dialog.querySelector('.overflow-y-auto'); const rect = body.getBoundingClientRect(); return dialog.matches(':modal') && rect.top >= 0 && rect.bottom <= innerHeight && dialog.scrollWidth <= innerWidth && body.scrollWidth <= body.clientWidth; })()`,
    ),
    '320px modal body stays viewport bounded without horizontal overflow',
  )
  await screenshot(page, 'tag-manager-mobile', false)
  await checkTagModalStyle('320px')
  await getCDP().send(
    'Emulation.setDeviceMetricsOverride',
    { width: 390, height: 844, deviceScaleFactor: 1, mobile: true },
    page.sessionId,
  )
  await evaluate(page, `window.dispatchEvent(new Event('resize'))`)
  await checkTagModalStyle('390px')
  await screenshot(page, 'tag-manager-390', false)
  await getCDP().send(
    'Emulation.setDeviceMetricsOverride',
    { width: 320, height: 844, deviceScaleFactor: 1, mobile: true },
    page.sessionId,
  )
  await evaluate(page, `window.dispatchEvent(new Event('resize'))`)
  await click(page, 'Créer un tag')
  await checkPalette(page, evaluate, check)
  await checkChips(page, evaluate, check, '320px creation and saved rows')
  await screenshot(page, 'tag-create-mobile', false)
  await checkTagRows('320px expanded creation')
  await click(page, 'Annuler')
  await evaluate(
    page,
    `document.querySelector('button[aria-label="Modifier À revoir au cinéma avec tous les amis"]').click()`,
  )
  await checkPalette(page, evaluate, check)
  await checkChips(page, evaluate, check, '320px long-name editor')
  await screenshot(page, 'editorial-palette-edit-320', false)
  await click(page, 'Annuler')
  await click(page, 'Créer un tag')
  await getCDP().send(
    'Emulation.setDeviceMetricsOverride',
    { width: 320, height: 320, deviceScaleFactor: 1, mobile: true },
    page.sessionId,
  )
  await evaluate(page, `window.dispatchEvent(new Event('resize'))`)
  await until(
    page,
    `parseFloat(getComputedStyle(document.querySelector('#watchlist-tag-manager')).maxHeight) <= 288`,
    'short tag modal applies visual viewport bound',
  )
  await checkTagModalStyle('320px short-height')
  check(
    await evaluate(
      page,
      `(() => { const modal = document.querySelector('#watchlist-tag-manager'); const body = modal.querySelector('.overflow-y-auto'); const close = modal.querySelector('button[aria-label="Fermer la gestion des tags"]').getBoundingClientRect(); return body.scrollHeight > body.clientHeight && body.getBoundingClientRect().bottom <= innerHeight && close.top >= 0 && close.bottom <= innerHeight; })()`,
    ),
    'short viewport scrolls modal body while close control remains visible',
  )
  await screenshot(page, 'tag-manager-short-viewport', false)
  check(
    await evaluate(
      page,
      `(() => { const body = document.querySelector('#watchlist-tag-manager .overflow-y-auto'), close = document.querySelector('button[aria-label="Fermer la gestion des tags"]'), top = close.getBoundingClientRect().top, page = scrollY; body.scrollTop = body.scrollHeight; return body.scrollTop > 0 && close.getBoundingClientRect().top === top && scrollY === page })()`,
    ),
    'short tag modal scrolls body without moving close control or background',
  )
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
      `!document.querySelector('#watchlist-tag-manager') && document.activeElement.getAttribute('aria-controls') === 'watchlist-tag-manager' && document.body.style.overflow !== 'hidden'`,
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
  const longPickerTitle =
    'Un très long titre de cinéma pour vérifier le panneau flottant et son bouton de fermeture'
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
  for (const [name, width, height, title] of [
    ['desktop-long', 1440, 900, longPickerTitle],
    ['desktop-short', 1440, 900, 'Film externe'],
    ['mobile', 320, 844, longPickerTitle],
    ['mobile-short', 320, 844, 'Film externe'],
    ['short-viewport', 320, 320, longPickerTitle],
  ]) {
    await getCDP().send(
      'Emulation.setDeviceMetricsOverride',
      { width, height, deviceScaleFactor: 1, mobile: true },
      page.sessionId,
    )
    externalTitle = title
    revision++
    await evaluate(page, `window.dispatchEvent(new Event('focus'))`)
    await until(
      page,
      `${savedRow('external-film')}.querySelector('a')?.textContent.trim() === ${JSON.stringify(title)} && !${savedRow('external-film')}.querySelector('button[aria-expanded]').disabled`,
      `${name} movie title fixture revalidated`,
    )
    await evaluate(
      page,
      `(() => { const button = ${savedRow('external-film')}.querySelector('button[aria-expanded]'); button.scrollIntoView({block:'end'}); if(button.getAttribute('aria-expanded') !== 'true') button.click(); })()`,
    )
    await delay(100)
    check(
      await evaluate(
        page,
        `(() => { const p = document.querySelector('section[aria-labelledby="saved-heading"] li [role="group"]'), r = p.getBoundingClientRect(), scroll = p.querySelector('.overflow-y-auto'), close = p.querySelector('button'), c = close.getBoundingClientRect(); return r.left >= 8 && r.right <= innerWidth - 8 && r.top >= 8 && r.bottom <= innerHeight - 8 && scroll.scrollHeight > scroll.clientHeight && scroll.clientHeight > 0 && p.scrollWidth <= p.clientWidth && [...p.querySelectorAll('label')].every(label=>label.getBoundingClientRect().height >= 44) && close.contains(document.elementFromPoint(c.left+c.width/2,c.top+c.height/2)); })()`,
      ),
      `${name} picker clamps all edges, scrolls internally and retains reachable close control`,
    )
    await screenshot(page, `tag-picker-${name}`, false)
    await checkFloatingStyle(
      name,
      name === 'mobile'
        ? 'above'
        : name === 'mobile-short'
          ? 'below'
          : undefined,
    )
    const beforeScroll = await evaluate(
      page,
      `({page:scrollY, close:document.querySelector('button[aria-label="Fermer les tags"]').getBoundingClientRect().top})`,
    )
    await evaluate(
      page,
      `document.querySelector('section[aria-labelledby="saved-heading"] li [role="group"] .overflow-y-auto').scrollTop = 9999`,
    )
    check(
      await evaluate(
        page,
        `(() => { const p = document.querySelector('section[aria-labelledby="saved-heading"] li [role="group"]'); return p.querySelector('.overflow-y-auto').scrollTop > 0 && scrollY === ${beforeScroll.page} && p.querySelector('button').getBoundingClientRect().top === ${beforeScroll.close} })()`,
      ),
      `${name} floating options scroll without moving close control or document`,
    )
    await checkChips(page, evaluate, check, `320px ${name} assignment picker`)
    await evaluate(
      page,
      `document.querySelector('button[aria-label="Fermer les tags"]').click()`,
    )
  }
  externalTitle = 'Film externe'
  ownerTags.set(owner.username, ordinaryTags)
  revision++
  await evaluate(page, `window.dispatchEvent(new Event('focus'))`)
  await until(
    page,
    `document.querySelector('#watchlist-tag-filter:not(:disabled)')?.options.length === 3`,
    'ordinary tags restored',
  )
  await click(page, 'Par tag')
  await evaluate(
    page,
    `document.querySelector('button[aria-label="Modifier les tags de Film externe"]').click()`,
  )
  await evaluate(page, `window.dispatchEvent(new Event('pagehide'))`)
  check(
    await evaluate(
      page,
      `!document.querySelector('section[aria-labelledby="saved-heading"] li [role="group"]') && !document.querySelector('button[aria-label="Modifier les tags de Film externe"]')`,
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
  check(
    await evaluate(
      page,
      `document.querySelector('[aria-label="Affichage des films"] button[aria-pressed="true"]').textContent.trim() === 'Par tag'`,
    ),
    'reload after pagehide recovers committed grouped mode',
  )
  await click(page, 'Par tag')
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
      `!document.querySelector('section[aria-labelledby="saved-heading"] li [role="group"]') && !document.querySelector('[id^="watchlist-group-"]') && !document.activeElement.getAttribute('aria-label')?.startsWith('Modifier les tags de')`,
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
  check(
    await evaluate(
      page,
      `document.querySelector('[aria-label="Affichage des films"] button[aria-pressed="true"]').textContent.trim() === 'Par tag'`,
    ),
    'returning owner recovers own committed grouped mode',
  )
  await click(page, 'Par tag')
  await evaluate(
    page,
    `${savedRow('saved-film')}.querySelector('button[aria-expanded]').click()`,
  )
  await route(page, '/compte')
  check(
    await evaluate(
      page,
      `!document.querySelector('section[aria-labelledby="saved-heading"] li [role="group"]')`,
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
      `document.querySelector('#watchlist-tag-filter').value === '${firstTag}' && !!${savedRow('external-film')} && document.querySelector('#watchlist-group-tag-${firstTag}').textContent.includes('Soirée cinéma') && ![...document.querySelectorAll('ul[aria-label="Tags associés"] li')].some(chip => chip.textContent.trim() === 'Soirée cinéma')`,
    ),
    'route reentry recovers committed assignments and selected filter',
  )
  await screenshot(page, 'saved-tags-desktop')
  await click(page, 'Liste')
  await filterTag('')
  // A complete private snapshot with duplicate memberships, an empty tag and an
  // untagged film exercises grouping independently of persistent sort state.
  const groupingBefore = {
    saved: [...saved],
    tags: [...tags()],
    assignments: new Map(assignments()),
    sort: sorts.get(owner.username) ?? 'added_desc',
  }
  saved = ['saved-film', 'external-film', 'other-film']
  const emptyTag = String(nextTagId++)
  ownerTags.set(owner.username, [
    ...tags(),
    { id: emptyTag, name: 'Vide', color: 'neutral' },
  ])
  assignments().set('saved-film', [firstTag, secondTag])
  assignments().set('external-film', [firstTag])
  assignments().set('other-film', [])
  addedTimes.set('external-film', `${date}T00:00:00.000000001Z`)
  revision++
  await evaluate(page, `window.dispatchEvent(new Event('focus'))`)
  await until(
    page,
    `${savedOrder}.length === 3 && !document.querySelector('#watchlist-sort').disabled`,
    'grouping source snapshot ready',
  )
  const viewButtons = `document.querySelector('[aria-label="Affichage des films"]')`
  const group = (id) =>
    `document.querySelector('section[aria-labelledby="watchlist-group-${id}"]')`
  const groupRow = (id, slug) =>
    `${group(id)}?.querySelector('a[href="/film/${slug}"]')?.closest('li')`
  const groupHeadings = `[...document.querySelectorAll('h3[id^="watchlist-group-"]')].map(node => node.id)`
  const openPickers = `document.querySelectorAll('section[aria-labelledby="saved-heading"] li [role="group"]')`
  async function openGroupPicker(id, slug) {
    await evaluate(
      page,
      `${groupRow(id, slug)}.querySelector('button[aria-expanded]').click()`,
    )
    await until(
      page,
      `${groupRow(id, slug)}?.querySelector('input:not(:disabled)')`,
      'group instance picker opens',
    )
  }
  async function assignGroup(id, slug, tagId) {
    const name = tags().find((tag) => tag.id === tagId).name
    await evaluate(
      page,
      `(() => { const input = [...${groupRow(id, slug)}.querySelectorAll('label')].find(label => label.textContent.trim() === ${JSON.stringify(name)}).querySelector('input'); input.focus(); input.click(); })()`,
    )
    await until(
      page,
      `!document.querySelector('#watchlist-sort').disabled`,
      'group assignment settled',
    )
  }
  check(
    await evaluate(
      page,
      `${viewButtons}.querySelector('button[aria-pressed="true"]').textContent.trim() === 'Liste' && !document.querySelector('h3[id^="watchlist-group-"]')`,
    ),
    'explicit committed Liste renders ungrouped rows',
  )
  const beforeGroupingRequests = requests.length
  await checkSegmentedDisplay('desktop list', 0)
  await screenshot(page, 'segmented-list-desktop')
  check(
    await evaluate(
      page,
      `${savedRow('saved-film')}.querySelectorAll('ul[aria-label="Tags associés"] li').length === 2 && ${savedRow('external-film')}.querySelectorAll('ul[aria-label="Tags associés"] li').length === 1`,
    ),
    'Liste retains every assigned chip including active filter membership',
  )
  await evaluate(
    page,
    `${viewButtons}.querySelector('button:first-child').focus()`,
  )
  for (const type of ['keyDown', 'keyUp'])
    await getCDP().send(
      'Input.dispatchKeyEvent',
      { type, key: 'Tab', code: 'Tab', windowsVirtualKeyCode: 9 },
      page.sessionId,
    )
  check(
    await evaluate(
      page,
      `document.activeElement === ${viewButtons}.querySelector('button:last-child') && document.activeElement.matches(':focus-visible') && getComputedStyle(document.activeElement).outlineStyle === 'solid' && getComputedStyle(document.activeElement).outlineColor === 'rgb(39, 39, 42)' && parseFloat(getComputedStyle(document.activeElement).outlineOffset) >= 2 && parseFloat(getComputedStyle(document.activeElement).zIndex) > 0`,
    ),
    'Tab reaches adjacent segment with separated high-contrast focus above shared border',
  )
  for (const type of ['keyDown', 'keyUp'])
    await getCDP().send(
      'Input.dispatchKeyEvent',
      { type, key: ' ', code: 'Space', windowsVirtualKeyCode: 32 },
      page.sessionId,
    )
  const groupedTagIds = [...tags()]
    .filter((tag) => tag.id !== emptyTag)
    .sort((a, b) =>
      new Intl.Collator('fr', { sensitivity: 'base', numeric: true }).compare(
        a.name,
        b.name,
      ),
    )
    .map((tag) => `watchlist-group-tag-${tag.id}`)
  await until(
    page,
    `${viewButtons}.querySelector('button[aria-pressed="true"]:not(:disabled)')?.textContent.trim() === 'Par tag'`,
    'keyboard mode committed',
  )
  check(
    await evaluate(
      page,
      `JSON.stringify(${groupHeadings}) === ${JSON.stringify(JSON.stringify([...groupedTagIds, 'watchlist-group-untagged']))} && ${group(`tag-${firstTag}`)}.querySelector('h3').textContent.includes('(2)') && ${group(`tag-${secondTag}`)}.querySelector('h3').textContent.includes('(1)') && ${group('untagged')}.querySelector('h3').textContent.includes('Sans tag')`,
    ),
    'group headings follow French order with counts, no empty tags and Sans tag last',
  )
  check(
    await evaluate(
      page,
      `document.activeElement.textContent.trim() === 'Par tag' && document.activeElement.getAttribute('aria-pressed') === 'true' && document.activeElement.matches(':focus-visible') && parseFloat(getComputedStyle(document.activeElement).outlineWidth) >= 2`,
    ),
    'native keyboard display toggle retains visible focus and pressed state',
  )
  await checkSegmentedDisplay('desktop grouped', 1)
  check(
    requests
      .slice(beforeGroupingRequests)
      .filter((request) => request.path.endsWith('/preferences')).length === 1,
    'display toggle commits exactly one preference request',
  )
  check(
    await evaluate(
      page,
      `document.querySelectorAll('section[aria-labelledby="saved-heading"] a[href="/film/saved-film"]').length === 2 && ${groupRow(`tag-${firstTag}`, 'saved-film')}.querySelector('time').textContent.trim() === '14 octobre 1998'`,
    ),
    'multi-tag film renders in both sections with preserved French release date',
  )
  await screenshot(page, 'grouped-desktop')
  await checkGroupDots('desktop')
  const originalTag = { ...tags().find((tag) => tag.id === firstTag) }
  for (const [name, color] of [
    [originalTag.name, 'blue'],
    ['Un très long nom de tag pour les amis', 'violet'],
    [originalTag.name, 'neutral'],
    [originalTag.name, originalTag.color],
  ]) {
    await click(page, 'Gérer les tags')
    await evaluate(
      page,
      `document.querySelector(${JSON.stringify(`button[aria-label="Modifier ${tags().find((tag) => tag.id === firstTag).name}"]`)}).click()`,
    )
    await fill(page, 'watchlist-tag-edit', name)
    await evaluate(
      page,
      `document.querySelector('#watchlist-tag-edit').form.querySelector('input[value="${color}"]').click()`,
    )
    await click(page, 'Enregistrer')
    await until(
      page,
      `!document.querySelector('#watchlist-tag-edit') && !document.querySelector('#watchlist-sort').disabled`,
      'group tag edit committed',
    )
    await evaluate(
      page,
      `new Promise(resolve => {
        document.querySelector('#watchlist-tag-manager').addEventListener('close', () => requestAnimationFrame(resolve), { once: true });
        document.querySelector('button[aria-label="Fermer la gestion des tags"]').click();
      })`,
    )
    await checkGroupDots(`committed ${color} edit`)
    if (color === 'neutral') await screenshot(page, 'grouped-neutral-desktop')
  }
  check(
    await evaluate(
      page,
      `(() => {
      const chips = row => [...row.querySelectorAll('ul[aria-label="Tags associés"] li')].map(chip => chip.textContent.trim());
      return JSON.stringify(chips(${groupRow(`tag-${firstTag}`, 'saved-film')})) === ${JSON.stringify(JSON.stringify([tags().find((tag) => tag.id === secondTag).name]))}
        && JSON.stringify(chips(${groupRow(`tag-${secondTag}`, 'saved-film')})) === ${JSON.stringify(JSON.stringify([tags().find((tag) => tag.id === firstTag).name]))}
        && chips(${groupRow(`tag-${firstTag}`, 'external-film')}).length === 0
        && chips(${groupRow('untagged', 'other-film')}).length === 0;
    })()`,
    ),
    'grouped summaries omit only section membership, retain other chips and leave Sans tag unchanged',
  )
  for (const width of [1440, 400, 390, 320]) {
    await getCDP().send(
      'Emulation.setDeviceMetricsOverride',
      { width, height: 900, deviceScaleFactor: 1, mobile: width < 640 },
      page.sessionId,
    )
    await evaluate(
      page,
      `window.scrollTo(0, 0); new Promise(resolve => requestAnimationFrame(resolve))`,
    )
    await checkCompactLayout(width)
    await checkGroupDots(`${width}px`)
    await click(page, 'Gérer les tags')
    await until(
      page,
      `document.querySelector('#watchlist-tag-manager')?.matches(':modal')`,
      'tag row geometry modal opens',
    )
    await checkTagRows(`${width}px`)
    for (let i = 0; i < 2; i++) {
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
        `(() => { const button = document.querySelector('#watchlist-tag-manager ul button'); return document.activeElement === button && button.matches(':focus-visible') && parseFloat(getComputedStyle(button).outlineWidth) >= 2 })()`,
      ),
      `${width}px icon edit action is keyboard reachable with visible focus`,
    )
    const actionPoint = await evaluate(
      page,
      `(() => { const r = document.querySelector('#watchlist-tag-manager ul button').getBoundingClientRect(); return { x: r.left + r.width / 2, y: r.top + r.height / 2 } })()`,
    )
    await getCDP().send(
      'Input.dispatchMouseEvent',
      { type: 'mouseMoved', ...actionPoint },
      page.sessionId,
    )
    check(
      await evaluate(
        page,
        `(() => { if (!matchMedia('(hover: hover)').matches) return true; const button = document.querySelector('#watchlist-tag-manager ul button'), reference = document.createElement('span'); reference.className = 'bg-subtle'; document.body.append(reference); const color = getComputedStyle(reference).backgroundColor; reference.remove(); return button.matches(':hover') && getComputedStyle(button).backgroundColor === color })()`,
      ),
      `${width}px icon action has visible subtle hover surface on hover-capable devices`,
    )
    await screenshot(page, `tag-row-actions-${width}`, false)
    await evaluate(
      page,
      `document.querySelector('button[aria-label="Fermer la gestion des tags"]').click()`,
    )
    await until(
      page,
      `!document.querySelector('#watchlist-tag-manager') && document.body.style.overflow !== 'hidden'`,
      'tag row inspection closes and unlocks modal',
    )
  }
  await getCDP().send(
    'Emulation.setDeviceMetricsOverride',
    { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false },
    page.sessionId,
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
        `JSON.stringify([...${group(`tag-${firstTag}`)}.querySelectorAll('a')].map(a=>a.pathname.split('/').at(-1))) === ${JSON.stringify(JSON.stringify(expected))} && !!${groupRow(`tag-${secondTag}`, 'saved-film')} && !!${groupRow('untagged', 'other-film')}`,
      ),
      `${order} orders each group without interleaving sections`,
    )
  }
  await openGroupPicker(`tag-${firstTag}`, 'saved-film')
  check(
    await evaluate(
      page,
      `${groupRow(`tag-${firstTag}`, 'saved-film')}.querySelectorAll('input:checked').length === 2`,
    ),
    'picker still checks both actual memberships including hidden section chip',
  )
  await openGroupPicker(`tag-${secondTag}`, 'saved-film')
  check(
    await evaluate(
      page,
      `${openPickers}.length === 1 && ${groupRow(`tag-${firstTag}`, 'saved-film')}.querySelector('button[aria-expanded]').getAttribute('aria-expanded') === 'false' && ${groupRow(`tag-${secondTag}`, 'saved-film')}.querySelector('button[aria-expanded]').getAttribute('aria-expanded') === 'true'`,
    ),
    'duplicate rows share membership but only selected picker instance opens',
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
      `${openPickers}.length === 0 && document.activeElement === ${groupRow(`tag-${secondTag}`, 'saved-film')}.querySelector('button[aria-expanded]')`,
    ),
    'Escape restores exact duplicate-row trigger',
  )
  await openGroupPicker(`tag-${firstTag}`, 'saved-film')
  await assignGroup(`tag-${firstTag}`, 'saved-film', firstTag)
  check(
    await evaluate(
      page,
      `!${groupRow(`tag-${firstTag}`, 'saved-film')} && !!${groupRow(`tag-${secondTag}`, 'saved-film')} && ${openPickers}.length === 0 && document.activeElement.id === 'watchlist-tag-filter'`,
    ),
    'removing active duplicate row closes picker and focuses connected filter while other copy remains',
  )
  await openGroupPicker(`tag-${secondTag}`, 'saved-film')
  await assignGroup(`tag-${secondTag}`, 'saved-film', firstTag)
  check(
    await evaluate(
      page,
      `!!${groupRow(`tag-${firstTag}`, 'saved-film')} && ${openPickers}.length === 1 && ${groupRow(`tag-${secondTag}`, 'saved-film')}.contains(document.activeElement) && document.activeElement.type === 'checkbox' && [...document.querySelectorAll('section[aria-labelledby="saved-heading"] a[href="/film/saved-film"]')].every(a=>a.closest('li').querySelectorAll('ul[aria-label="Tags associés"] li').length === 1)`,
    ),
    'adding membership creates copy, updates each contextual summary and retains originating checkbox focus',
  )
  await assignGroup(`tag-${secondTag}`, 'saved-film', secondTag)
  check(
    await evaluate(
      page,
      `!${group(`tag-${secondTag}`)} && ${openPickers}.length === 0 && document.activeElement.id === 'watchlist-tag-filter'`,
    ),
    'removing last row omits section and safely restores focus',
  )
  await openGroupPicker('untagged', 'other-film')
  await assignGroup('untagged', 'other-film', secondTag)
  check(
    await evaluate(
      page,
      `!${group('untagged')} && !!${groupRow(`tag-${secondTag}`, 'other-film')} && ${openPickers}.length === 0 && document.activeElement.id === 'watchlist-tag-filter'`,
    ),
    'first confirmed assignment moves Sans tag film and restores focus after category disappears',
  )
  await openGroupPicker(`tag-${secondTag}`, 'other-film')
  await assignGroup(`tag-${secondTag}`, 'other-film', secondTag)
  await openGroupPicker(`tag-${firstTag}`, 'saved-film')
  await assignGroup(`tag-${firstTag}`, 'saved-film', secondTag)
  await filterTag(secondTag)
  check(
    await evaluate(
      page,
      `${openPickers}.length === 0 && JSON.stringify(${groupHeadings}) === ' ["watchlist-group-tag-${secondTag}"]'.trim() && ${savedOrder}.length === 1 && document.querySelector('#watchlist-sort').value === 'release_asc'`,
    ),
    'filter closes picker, restricts grouped view to one section and keeps sort',
  )
  await filterTag(emptyTag)
  check(
    await evaluate(
      page,
      `!${groupHeadings}.length && document.querySelector('main').textContent.includes('Aucun film avec ce tag.')`,
    ),
    'selected empty tag retains existing filtered-empty message',
  )
  await filterTag(firstTag)
  await click(page, 'Liste')
  check(
    await evaluate(
      page,
      `!${groupHeadings}.length && ${savedOrder}.length === 2 && document.querySelector('#watchlist-tag-filter').value === '${firstTag}' && document.querySelector('#watchlist-sort').value === 'release_asc'`,
    ),
    'return to Liste retains tag filter and sort without duplicate rows',
  )
  await click(page, 'Par tag')
  await filterTag('')
  check(
    await evaluate(
      page,
      `!['view_mode', 'filter_tag_id', 'displayMode', 'savedSections', 'tag_ids', 'Soirée cinéma'].some(marker => JSON.stringify({url:location.href,local:{...localStorage},session:{...sessionStorage},payload:window.__NUXT__,data:document.querySelector('#__nuxt').__vue_app__.$nuxt.payload.data}).includes(marker))`,
    ),
    'grouped view and private tag identities absent from URL, storage and public Nuxt payload',
  )
  await getCDP().send(
    'Emulation.setDeviceMetricsOverride',
    { width: 320, height: 844, deviceScaleFactor: 1, mobile: true },
    page.sessionId,
  )
  await evaluate(page, `window.scrollTo(0, 0)`)
  check(
    await evaluate(
      page,
      `document.documentElement.scrollWidth <= innerWidth && document.querySelector('button[aria-controls="watchlist-configuration"]').getBoundingClientRect().height >= 44 && [...document.querySelectorAll('h3[id^="watchlist-group-"]')].every(h=>h.getBoundingClientRect().right <= innerWidth)`,
    ),
    '320px grouped toolbar and long headings fit viewport with 44px display controls',
  )
  await screenshot(page, 'grouped-mobile')
  await click(page, 'Configuration')
  await checkSegmentedDisplay('320px grouped', 1)
  await escapeOverlay()
  await until(
    page,
    `!document.querySelector('#watchlist-configuration') && document.body.style.overflow !== 'hidden' && document.activeElement.getAttribute('aria-controls') === 'watchlist-configuration'`,
    'configuration Escape restores opener and scroll',
  )
  await openGroupPicker(`tag-${secondTag}`, 'saved-film')
  await screenshot(page, 'grouped-picker-mobile', false)
  await click(page, 'Liste')
  check(
    await evaluate(
      page,
      `${openPickers}.length === 0 && ${savedOrder}.length === 3`,
    ),
    'view change closes duplicate picker and restores unique list rows',
  )
  await click(page, 'Configuration')
  await checkSegmentedDisplay('320px list', 0)
  await screenshot(page, 'segmented-list-mobile')
  await backdropClick()
  await until(
    page,
    `!document.querySelector('#watchlist-configuration') && document.body.style.overflow !== 'hidden' && document.activeElement.getAttribute('aria-controls') === 'watchlist-configuration'`,
    'configuration backdrop restores opener and scroll',
  )
  await click(page, 'Par tag')
  await go(page, '/compte/watchlist')
  await until(
    page,
    `!!document.querySelector('#watchlist-sort:not(:disabled)')`,
    'grouped reload ready',
  )
  check(
    await evaluate(
      page,
      `${viewButtons}.querySelector('button[aria-pressed="true"]').textContent.trim() === 'Par tag' && ${groupHeadings}.length > 0 && document.querySelector('#watchlist-sort').value === 'release_asc'`,
    ),
    'reload restores committed display and account sort',
  )
  await click(page, 'Par tag')
  const beforeGroupedRemove = removalWrites().length
  await evaluate(
    page,
    `${groupRow(`tag-${secondTag}`, 'saved-film')}.querySelector('button[aria-label="Retirer de la watchlist"]').click()`,
  )
  await until(
    page,
    `${removePanel}?.matches(':modal')`,
    'grouped duplicate opens one central confirmation',
  )
  check(
    removalWrites().length === beforeGroupedRemove,
    'grouped opener preserves every duplicate until confirmation',
  )
  await evaluate(
    page,
    `${removePanel}.querySelector('.account-primary').click()`,
  )
  await until(
    page,
    `!document.querySelector('#watchlist-sort').disabled && !${savedRow('saved-film')}`,
    'grouped bookmark removal committed',
  )
  check(
    await evaluate(
      page,
      `!${group(`tag-${secondTag}`)} && ${savedOrder}.length === 2 && !document.querySelector('section[aria-labelledby="saved-heading"] a[href="/film/saved-film"]')`,
    ),
    'one grouped bookmark removal removes every copy and resulting empty section',
  )
  check(
    removalWrites().length === beforeGroupedRemove + 1 &&
      (await evaluate(
        page,
        `!${removePanel} && document.body.style.overflow !== 'hidden' && (document.activeElement.hasAttribute('data-watchlist-remove') || document.activeElement.getAttribute('aria-controls') === 'watchlist-add')`,
      )),
    'grouped confirmed removal dispatches once and restores surviving focus',
  )
  await click(page, 'Liste')
  saved = groupingBefore.saved
  ownerTags.set(owner.username, groupingBefore.tags)
  ownerAssignments.set(owner.username, groupingBefore.assignments)
  sorts.set(owner.username, groupingBefore.sort)
  addedTimes.delete('external-film')
  revision++
  await click(page, 'Configuration')
  await getCDP().send(
    'Emulation.setDeviceMetricsOverride',
    { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false },
    page.sessionId,
  )
  await until(
    page,
    `!document.querySelector('#watchlist-configuration') && document.body.style.overflow !== 'hidden' && document.activeElement.getAttribute('aria-controls') !== 'watchlist-configuration'`,
    'desktop breakpoint closes sheet without focusing hidden trigger',
  )
  await evaluate(page, `window.dispatchEvent(new Event('focus'))`)
  await until(
    page,
    `${savedOrder}.length === 2 && !document.querySelector('#watchlist-sort').disabled`,
    'original fixture restored after grouping',
  )
  await filterTag(firstTag)
  const filteredBefore = {
    saved: [...saved],
    assignments: new Map(
      [...assignments()].map(([slug, ids]) => [slug, [...ids]]),
    ),
  }
  const filteredSlug = await evaluate(page, `${savedOrder}[0]`)
  await openRemove(filteredSlug)
  await evaluate(
    page,
    `${removePanel}.querySelector('.account-primary').click()`,
  )
  await until(
    page,
    `!${removePanel} && !document.querySelector('#watchlist-tag-filter').disabled`,
    'filtered removal committed',
  )
  check(
    await evaluate(
      page,
      `document.querySelector('#watchlist-tag-filter').value === ${JSON.stringify(firstTag)} && !${savedRow(filteredSlug)} && document.body.style.overflow !== 'hidden'`,
    ),
    'filtered removal keeps committed filter and removes target only',
  )
  saved = filteredBefore.saved
  ownerAssignments.set(owner.username, filteredBefore.assignments)
  revision++
  await evaluate(page, `window.dispatchEvent(new Event('focus'))`)
  await until(
    page,
    `${savedRow(filteredSlug)}?.querySelector('[data-watchlist-remove]:not(:disabled)')`,
    'filtered fixture restored for tag deletion',
  )
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
      `(() => { const row = ${savedRow('saved-film')}; return !!row && !document.querySelector('button[aria-controls="watchlist-add"]').disabled && ${label ? `row.querySelector('time')?.textContent.trim() === ${JSON.stringify(label)}` : `!row.querySelector('time, .text-muted')`}; })()`,
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
  await checkClock(page, 'remove')
  check(
    await evaluate(
      page,
      `(() => { const link = document.querySelector('nav[aria-label="Espace personnel"] a[href="/compte/watchlist"]'); const icon = link?.querySelector('svg[data-watchlist-icon="navigation"]'); return link?.textContent.trim() === 'Watchlist' && link.getAttribute('aria-current') === 'page' && icon?.getAttribute('aria-hidden') === 'true' && icon.getAttribute('focusable') === 'false' && icon.querySelectorAll('circle').length === 1 && icon.querySelectorAll('path').length === 1 && !icon.querySelector('g'); })()`,
    ),
    'active watchlist navigation keeps its name and decorative plain clock without action badge',
  )
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
    'standalone watchlist clock on no-trailer no-session film',
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
    'no-trailer watchlist clock retains accessible target',
  )
  await screenshot(page, 'film')
  await captureFilmClock(page, 'remove')
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
    'keyboard navigation shows watchlist focus',
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
  await captureFilmClock(page, 'add')
  await route(page, '/compte/watchlist')
  await until(
    page,
    `!!document.querySelector('button[aria-controls="watchlist-add"]:not(:disabled)')`,
    'watchlist returns',
  )
  check(
    await evaluate(page, `!document.querySelector('#watchlist-query')`),
    'search draft clears on route departure',
  )
  externalStatus = 'unavailable'
  await click(page, 'Ajouter')
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
      `!document.querySelector('#watchlist-add') && document.activeElement.getAttribute('aria-controls') === 'watchlist-add' && document.body.style.overflow !== 'hidden'`,
    ),
    'Escape closes and returns focus without deleting draft',
  )
  holdSearch = true
  await click(page, 'Rechercher')
  for (let i = 0; !releaseSearch && i < 100; i++) await delay(20)
  check(!!releaseSearch, 'pending search reached fixture')
  await backdropClick()
  holdSearch = false
  releaseSearch()
  await delay(100)
  check(
    await evaluate(
      page,
      `!document.querySelector('#watchlist-add') && document.body.style.overflow !== 'hidden'`,
    ),
    'outside dismissal fences late search results',
  )
  emptySearch = true
  externalStatus = 'ready'
  await click(page, 'Ajouter')
  check(
    await evaluate(
      page,
      `document.querySelector('#watchlist-query').value === 'private candidate query'`,
    ),
    'dismissal preserves draft for reopening',
  )
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
      `document.querySelector('#watchlist-add').matches(':modal') && document.activeElement.getAttribute('aria-label') !== 'Retirer de la watchlist'`,
    ),
    'native search modal prevents background focus',
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
  check(
    await evaluate(
      page,
      `!document.querySelector('#watchlist-add') && document.body.style.overflow !== 'hidden'`,
    ),
    'navigation removes open search dialog and scroll lock',
  )
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
    `!!document.querySelector('button[aria-controls="watchlist-add"]:not(:disabled)')`,
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
      `!document.querySelector('#watchlist-sort').checkVisibility() && !document.querySelector('#watchlist-tag-filter').checkVisibility() && document.querySelector('button[aria-controls="watchlist-configuration"]').checkVisibility() && !document.querySelector('#watchlist-query')`,
    ),
    'mobile base hides display/filter/sort behind Configuration',
  )
  await click(page, 'Ajouter')
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
      `(() => { const panel = document.querySelector('#watchlist-add').getBoundingClientRect(); const scroll = document.querySelector('#watchlist-add .overflow-y-auto'); return panel.left >= 0 && panel.right <= innerWidth && panel.bottom <= innerHeight && scroll.scrollHeight > scroll.clientHeight && document.documentElement.scrollWidth <= innerWidth; })()`,
    ),
    'mobile overlay fits width and scrolls inside available viewport',
  )
  await screenshot(page, 'mobile')
  await getCDP().send('Page.bringToFront', {}, page.sessionId)
  await getCDP().send(
    'Emulation.setDeviceMetricsOverride',
    { width: 390, height: 320, deviceScaleFactor: 1, mobile: true },
    page.sessionId,
  )
  // CDP device metrics can defer resize notification while this tab is backgrounded.
  await evaluate(page, `window.dispatchEvent(new Event('resize'))`)
  await until(
    page,
    `parseFloat(getComputedStyle(document.querySelector('#watchlist-add')).maxHeight) <= 288`,
    'short mobile visual viewport applies modal height',
  )
  await screenshot(page, 'add-short-viewport', false)
  const shortGeometry = await evaluate(
    page,
    `(() => { const d = document.querySelector('#watchlist-add').getBoundingClientRect(), close = document.querySelector('button[aria-label="Fermer l’ajout de film"]').getBoundingClientRect(), scroll = document.querySelector('#watchlist-add .overflow-y-auto'); return {top:d.top,bottom:d.bottom,height:innerHeight,vh:visualViewport.height,offset:visualViewport.offsetTop,closeTop:close.top,closeBottom:close.bottom,client:scroll.clientHeight,content:scroll.scrollHeight} })()`,
  )
  check(
    shortGeometry.top >= 15.5 &&
      shortGeometry.bottom <= shortGeometry.height - 15.5 &&
      shortGeometry.closeTop >= shortGeometry.top &&
      shortGeometry.closeBottom <= shortGeometry.bottom &&
      shortGeometry.client > 0 &&
      shortGeometry.content > shortGeometry.client,
    `short mobile search keeps close visible and results scrollable ${JSON.stringify(shortGeometry)}`,
  )
  await getCDP().send(
    'Emulation.setDeviceMetricsOverride',
    { width: 390, height: 844, deviceScaleFactor: 1, mobile: true },
    page.sessionId,
  )
  await evaluate(
    page,
    `document.querySelector('button[aria-label="Fermer l’ajout de film"]').click()`,
  )
  check(
    await evaluate(
      page,
      `!document.querySelector('#watchlist-add') && document.activeElement.getAttribute('aria-controls') === 'watchlist-add' && document.body.style.overflow !== 'hidden'`,
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
    'mobile full French date fits saved row and preserves watchlist touch target',
  )
  await checkClock(page, 'remove')
  await screenshot(page, 'mobile-saved-date')
  const mobileRemovalSaved = [...saved]
  saved.unshift('external-film')
  externalTitle =
    'Un très long titre de cinéma pour identifier clairement le film à retirer sans dépasser la fenêtre de confirmation'
  revision++
  await getCDP().send(
    'Emulation.setDeviceMetricsOverride',
    { width: 320, height: 844, deviceScaleFactor: 1, mobile: true },
    page.sessionId,
  )
  await evaluate(page, `window.dispatchEvent(new Event('focus'))`)
  await until(
    page,
    `${savedRow('external-film')}?.textContent.includes(${JSON.stringify(externalTitle)}) && !document.querySelector('[aria-controls="watchlist-add"]').disabled`,
    '320px long removal fixture ready',
  )
  await checkClock(page, 'remove')
  await screenshot(page, 'remove-cross-mobile-320', false)
  const beforeMobileCancel = removalWrites().length
  await openRemove()
  await checkRemovalSurface('mobile-320')
  await getCDP().send(
    'Emulation.setDeviceMetricsOverride',
    { width: 320, height: 320, deviceScaleFactor: 1, mobile: true },
    page.sessionId,
  )
  await evaluate(page, `window.dispatchEvent(new Event('resize'))`)
  await until(
    page,
    `parseFloat(getComputedStyle(${removePanel}).maxHeight) <= 288`,
    'short confirmation viewport bounds applied',
  )
  await checkRemovalSurface('short-viewport')
  check(
    await evaluate(
      page,
      `(() => { const body = ${removePanel}.querySelector('.overflow-y-auto'), close = ${removePanel}.querySelector('button[aria-label="Fermer la confirmation"]'), before = scrollY, top = close.getBoundingClientRect().top; body.scrollTop = 9999; return body.scrollTop > 0 && scrollY === before && close.getBoundingClientRect().top === top && close.getBoundingClientRect().bottom <= innerHeight })()`,
    ),
    'short confirmation scrolls internally with close fixed and background still',
  )
  await escapeOverlay()
  await until(
    page,
    `!${removePanel} && document.body.style.overflow !== 'hidden' && document.activeElement.hasAttribute('data-watchlist-remove')`,
    'mobile Escape restores cross and scrolling',
  )
  check(
    removalWrites().length === beforeMobileCancel,
    'mobile/short confirmation inspection never dispatches removal',
  )
  await getCDP().send(
    'Emulation.setDeviceMetricsOverride',
    { width: 390, height: 844, deviceScaleFactor: 1, mobile: true },
    page.sessionId,
  )
  saved = mobileRemovalSaved
  externalTitle = 'Film externe'
  revision++
  await evaluate(page, `window.dispatchEvent(new Event('focus'))`)
  await until(
    page,
    `${savedRow('saved-film')}?.querySelector('[data-watchlist-remove]:not(:disabled)') && !${savedRow('external-film')}`,
    'mobile fixture restored after nonmutating confirmation',
  )
  await click(page, 'Configuration')
  await evaluate(
    page,
    `window.dispatchEvent(new PageTransitionEvent('pagehide', { persisted: true }))`,
  )
  check(
    await evaluate(
      page,
      `!document.querySelector('#watchlist-configuration') && document.body.style.overflow !== 'hidden'`,
    ),
    'pagehide closes configuration and unlocks scroll',
  )
  await evaluate(
    page,
    `window.dispatchEvent(new PageTransitionEvent('pageshow', { persisted: true }))`,
  )
  await until(
    page,
    `!!document.querySelector('button[aria-controls="watchlist-add"]:not(:disabled)')`,
    'pageshow readies add without reopening sheet',
  )
  await click(page, 'Ajouter')
  await fill(page, 'watchlist-query', 'private candidate query')
  holdSearch = true
  releaseSearch = undefined
  await click(page, 'Rechercher')
  for (let i = 0; !releaseSearch && i < 100; i++) await delay(20)
  check(!!releaseSearch, 'late search reaches fixture before pagehide')
  await evaluate(
    page,
    `window.dispatchEvent(new PageTransitionEvent('pagehide', { persisted: true }))`,
  )
  holdSearch = false
  releaseSearch()
  await delay(100)
  check(
    await evaluate(
      page,
      `!document.querySelector('#watchlist-add') && !document.querySelector('#watchlist-query') && document.body.style.overflow !== 'hidden'`,
    ),
    'pagehide fences late search and unlocks modal scroll',
  )
  await evaluate(
    page,
    `window.dispatchEvent(new PageTransitionEvent('pageshow', { persisted: true }))`,
  )
  await until(
    page,
    `!!document.querySelector('button[aria-controls="watchlist-add"]:not(:disabled)')`,
    'pageshow revalidates private watchlist',
  )
  await click(page, 'Ajouter')
  check(
    await evaluate(
      page,
      `document.querySelector('#watchlist-query').value === ''`,
    ),
    'pageshow never restores private modal query',
  )
  await escapeOverlay()
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
      `!JSON.stringify({ local: { ...localStorage }, session: { ...sessionStorage }, payload: window.__NUXT__, data: document.querySelector('#__nuxt').__vue_app__.$nuxt.payload.data, url: location.href }).match(/private_watchlist_owner|private candidate query|watchlist-only|added_at|sort_order|view_mode|filter_tag_id|release_asc|1998-10-14|14 octobre 1998/)`,
    ),
    'private watchlist identity query membership absent from browser storage and public payload',
  )
  check(
    !page.collections.some((collection) =>
      /private_watchlist_owner|private candidate query|watchlist-only|saved-film|sort_order|view_mode|filter_tag_id|release_asc|1998-10-14|14 octobre 1998/.test(
        JSON.stringify(collection),
      ),
    ),
    'analytics contains no watchlist membership, identity or private query',
  )
  const ssr = await (await fetch(`${origin}/film/external-film`)).text()
  check(
    !/private_watchlist_owner|added_at|sort_order|view_mode|filter_tag_id|release_asc|private candidate query|1998-10-14|14 octobre 1998/.test(
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
  check(
    !page.external && !second.external,
    'watchlist tabs requested no unexpected external resources',
  )
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
      empty.body.view_mode === 'list' &&
      empty.body.filter_tag_id === null &&
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
  await click(page, 'Ajouter')
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
  await click(page, 'Par tag')
  await until(
    page,
    `document.querySelector('[aria-label="Affichage des films"] button[aria-pressed="true"]:not(:disabled)')?.textContent.trim() === 'Par tag'`,
    'real grouped preference committed',
  )
  await go(page, '/compte/watchlist')
  await until(
    page,
    `!!document.querySelector('#watchlist-tag-filter:not(:disabled)') && document.querySelectorAll('ul[aria-label="Tags associés"]').length === 1`,
    'real assignments survive document reload',
  )
  check(
    await evaluate(
      page,
      `document.querySelector('#watchlist-tag-filter').value === '${secondTag}' && document.querySelector('[aria-label="Affichage des films"] button[aria-pressed="true"]').textContent.trim() === 'Par tag'`,
    ),
    'real reload restores committed filter and grouped view',
  )
  const preferenceSnapshot = (
    await request(page, '/account/watchlist', undefined, 'GET')
  ).body
  check(
    preferenceSnapshot.view_mode === 'tags' &&
      preferenceSnapshot.filter_tag_id === secondTag,
    'real backend stores both preference fields',
  )
  const device = await tab()
  check(
    device.browserContextId !== page.browserContextId,
    'real device uses independent cookie/storage/BroadcastChannel context',
  )
  await go(device, '/connexion')
  await fill(device, 'account-email', email)
  await fill(device, 'account-password', 'Synthetic cinema password 42!')
  await click(device, 'Se connecter')
  await until(
    device,
    `location.pathname === '/compte'`,
    'same account authenticates in isolated device context',
  )
  await go(device, '/compte/watchlist')
  await until(
    device,
    `document.querySelector('#watchlist-tag-filter:not(:disabled)')?.value === '${secondTag}' && document.querySelector('[aria-label="Affichage des films"] button[aria-pressed="true"]').textContent.trim() === 'Par tag'`,
    'separate device restores same committed pair',
  )
  await click(device, 'Liste')
  await until(
    device,
    `document.querySelector('[aria-label="Affichage des films"] button[aria-pressed="true"]:not(:disabled)')?.textContent.trim() === 'Liste'`,
    'separate device commits list mode',
  )
  check(
    await evaluate(
      page,
      `document.querySelector('[aria-label="Affichage des films"] button[aria-pressed="true"]').textContent.trim() === 'Par tag'`,
    ),
    'real separate device does not push preferences into already-open page',
  )
  const stalePreference = await request(
    page,
    '/account/watchlist/preferences',
    {
      expected_username: username,
      expected_revision: preferenceSnapshot.revision,
      view_mode: 'tags',
      filter_tag_id: '',
    },
  )
  check(
    stalePreference.status === 409,
    'real stale preference CAS fails without clobbering other device',
  )
  const beforeStalePreference = page.requests.filter((entry) =>
    entry.path.endsWith('/watchlist/preferences'),
  ).length
  await evaluate(
    page,
    `(() => { const filter = document.querySelector('#watchlist-tag-filter'); filter.value = ''; filter.dispatchEvent(new Event('change', {bubbles:true})); })()`,
  )
  await until(
    page,
    `document.querySelector('[aria-label="Affichage des films"] button[aria-pressed="true"]:not(:disabled)')?.textContent.trim() === 'Liste' && document.querySelector('#watchlist-tag-filter').value === '${secondTag}'`,
    'real stale UI write reconciles other device pair',
  )
  check(
    page.requests.filter((entry) =>
      entry.path.endsWith('/watchlist/preferences'),
    ).length ===
      beforeStalePreference + 1 &&
      (await evaluate(page, `!!document.querySelector('[role="alert"]')`)),
    'real stale UI preference is rejected and read back without replay',
  )
  await evaluate(
    page,
    `(() => { const filter = document.querySelector('#watchlist-tag-filter'); filter.value = ''; filter.dispatchEvent(new Event('change', {bubbles:true})); })()`,
  )
  await until(
    page,
    `document.querySelector('#watchlist-tag-filter:not(:disabled)')?.value === '' && ${rows}.length === 2`,
    'real explicit all-films preference committed',
  )
  await evaluate(device, `window.dispatchEvent(new Event('focus'))`)
  await until(
    device,
    `document.querySelector('#watchlist-tag-filter:not(:disabled)')?.value === ''`,
    'real other device focus refreshes filter',
  )
  check(
    page.requests
      .filter((entry) => entry.path.endsWith('/watchlist/preferences'))
      .every(
        (entry) =>
          entry.method === 'POST' &&
          JSON.stringify([...entry.bodyKeys].sort()) ===
            JSON.stringify([
              'expected_revision',
              'expected_username',
              'filter_tag_id',
              'view_mode',
            ]),
      ),
    'real preferences use exact private four-field POST body',
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
  await until(
    page,
    `document.querySelector('#watchlist-tag-filter:not(:disabled)')?.value === ''`,
    'real empty-filter reset committed',
  )
  await evaluate(
    page,
    `(() => { const filter = document.querySelector('#watchlist-tag-filter'); filter.value = '${firstTag}'; filter.dispatchEvent(new Event('change', {bubbles:true})); })()`,
  )
  await until(
    page,
    `document.querySelector('#watchlist-tag-filter:not(:disabled)')?.value === '${firstTag}'`,
    'real selected tag prepared for deletion',
  )
  await click(page, 'Par tag')
  await until(
    page,
    `document.querySelector('[aria-label="Affichage des films"] button[aria-pressed="true"]:not(:disabled)')?.textContent.trim() === 'Par tag'`,
    'real deletion uses persisted nondefault mode',
  )
  await evaluate(device, `window.dispatchEvent(new Event('focus'))`)
  await until(
    device,
    `document.querySelector('#watchlist-tag-filter:not(:disabled)')?.value === '${firstTag}' && document.querySelector('[aria-label="Affichage des films"] button[aria-pressed="true"]').textContent.trim() === 'Par tag'`,
    'real device sees selected tag before deletion',
  )
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
      committed.view_mode === 'tags' &&
      committed.filter_tag_id === null &&
      committed.sort_order === 'release_asc' &&
      committed.tags.length === 1 &&
      !committed.tags.some((tag) => tag.id === firstTag) &&
      committed.items.every((item) => item.tag_ids.length === 0),
    'real deletion preserves both films and removes associations',
  )
  check(
    await evaluate(
      device,
      `document.querySelector('#watchlist-tag-filter').value === '${firstTag}'`,
    ),
    'real deletion waits for independent device revalidation',
  )
  await evaluate(device, `window.dispatchEvent(new Event('online'))`)
  await until(
    device,
    `document.querySelector('#watchlist-tag-filter:not(:disabled)')?.value === '' && document.querySelector('#watchlist-sort').value === 'release_asc' && document.querySelector('[aria-label="Affichage des films"] button[aria-pressed="true"]').textContent.trim() === 'Par tag' && ${rows}.length === 2 && document.querySelector('main').textContent.includes('Sans tag')`,
    'real reconnect clears deleted tag while preserving grouped mode and sort',
  )
  const deviceAfterDeletion = await request(
    device,
    '/account/watchlist',
    undefined,
    'GET',
  )
  check(
    deviceAfterDeletion.body.revision === committed.revision &&
      deviceAfterDeletion.body.filter_tag_id === null &&
      deviceAfterDeletion.body.view_mode === 'tags',
    'real devices converge on one committed deletion revision without repair POST',
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
    'view_mode',
    'filter_tag_id',
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
    `document.querySelector('#watchlist-remove')?.matches(':modal')`,
    'real removal confirmation opens',
  )
  await evaluate(
    page,
    `document.querySelector('#watchlist-remove .account-primary').click()`,
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
      secondSnapshot.body.view_mode === 'list' &&
      secondSnapshot.body.filter_tag_id === null &&
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
