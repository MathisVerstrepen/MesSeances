// DB-free, stateful contract fixture. Synthetic values never leave intercepted transport.
import { createServer } from 'node:http'
import { readFile } from 'node:fs/promises'
import { createHash } from 'node:crypto'
import { setTimeout as delay } from 'node:timers/promises'
import ts from 'typescript'

export async function trackerFixture(host) {
  const source = await readFile(
    new URL('../tests/fixtures/umami-3.3.1.ts.txt', import.meta.url),
    'utf8',
  )
  if (
    createHash('sha256').update(source).digest('hex') !==
    '52b3310b7afa3fb7a7c472a7c09c578e70f602b840b57cb2ece11429a0bdea6d'
  )
    throw new Error('Pinned tracker checksum')
  return ts
    .transpileModule(
      source
        .replace('__COLLECT_API_HOST__', host)
        .replace('__COLLECT_API_ENDPOINT__', '/api/send'),
      {
        compilerOptions: {
          target: ts.ScriptTarget.ES2022,
          module: ts.ModuleKind.ESNext,
        },
      },
    )
    .outputText.replace(/^export \{\};?$/gm, '')
}

export async function spaScenario({
  launch,
  tab,
  go,
  evaluate,
  until,
  click,
  fill,
  check,
  setServer,
  getCDP,
}) {
  const enabled = !process.argv.includes('--no-analytics')
  const owner = {
    email: 'spa@example.test',
    username: 'spa_owner',
    has_password: true,
    google_linked: false,
  }
  const anonymous = { enabled: true, state: 'anonymous', account: null }
  let session = anonymous
  const contexts = new Map()
  let sessionReads = 0
  let hold = ''
  const held = new Set()
  const writes = []
  const server = createServer(async (req, res) => {
    res.setHeader('Content-Type', 'application/json')
    res.setHeader('Cache-Control', 'no-store')
    const path = new URL(req.url, 'http://fixture').pathname
    // The isolated devProxy bypasses Nitro's private API middleware. Model the
    // existing API header contract here; this is not live-backend verification.
    if (/^\/api\/v1\/(auth|account)(\/|$)/.test(path)) {
      res.setHeader('Referrer-Policy', 'no-referrer')
      res.setHeader('X-Robots-Tag', 'noindex, nofollow')
      res.setHeader('Vary', 'Cookie')
    }
    const context = /messeances_session_dev=([A-Za-z0-9_-]{43})/.exec(
      req.headers.cookie || '',
    )?.[1]
    const currentSession = () => contexts.get(context) ?? session
    const setSession = (value) =>
      context ? contexts.set(context, value) : (session = value)
    let raw = ''
    for await (const chunk of req) raw += chunk
    const body = raw ? JSON.parse(raw) : {}
    if (req.method !== 'GET') writes.push({ path, body })
    if (path === '/api/v1/auth/session') sessionReads++
    const send = () => {
      let value = {}
      switch (path) {
        case '/api/v1/auth/session':
          value = currentSession()
          break
        case '/api/v1/account':
          value = {
            ...(currentSession().account ?? owner),
            google_email: null,
            pending_email: 'next@example.test',
            avatar_url: null,
            allowed_methods: ['password'],
          }
          break
        case '/api/v1/theaters':
          value = { theaters: [] }
          break
        case '/api/v1/account/theaters':
          if (req.method === 'GET') {
            value = {
              username: currentSession().account?.username ?? '',
              revision: '0',
              theater_ids: [],
            }
          }
          break
        case '/api/v1/auth/login':
          setSession({ enabled: true, state: 'complete', account: owner })
          value = currentSession()
          break
        case '/api/v1/auth/logout':
        case '/api/v1/auth/password/reset/confirm':
        case '/api/v1/account/email/confirm':
          setSession(anonymous)
          break
        case '/api/v1/auth/verification/confirm':
          setSession({
            enabled: true,
            state: 'pending_username',
            account: { ...owner, username: null },
          })
          value = currentSession()
          break
        case '/api/v1/account/username':
          setSession({
            enabled: true,
            state: 'complete',
            account: { ...owner, username: body.username },
          })
          value = currentSession()
          break
        case '/api/v1/account/reauth/continuation':
          value = {
            action: 'password_add',
            target: null,
            expires_at: new Date(Date.now() + 600000).toISOString(),
          }
          break
        case '/api/v1/account/reauth/email/confirm':
          value = { grant: 'synthetic-proof' }
          break
        case '/api/v1/account/reauth/password':
          value = { grant: 'synthetic-proof' }
          break
        case '/api/v1/auth/google/start':
          value = {
            authorization_url:
              'https://accounts.google.com/o/oauth2/v2/auth?client_id=synthetic',
          }
          break
        default:
          break
      }
      res.end(JSON.stringify(value))
    }
    if (path === hold) {
      held.add(send)
      res.once('close', () => held.delete(send))
    } else send()
  })
  setServer(server)
  await new Promise((resolve, reject) => {
    server.once('error', reject)
    server.listen(18089, '127.0.0.1', resolve)
  })
  const release = () => {
    hold = ''
    for (const send of held) send()
    held.clear()
  }
  const waitHeld = async () => {
    for (let i = 0; !held.size && i < 100; i++) await delay(50)
    check(held.size > 0, 'mock reached controlled async boundary')
  }
  const router = `document.querySelector('#__nuxt').__vue_app__.config.globalProperties.$router`
  const route = async (page, path) => {
    await evaluate(page, `${router}.push(${JSON.stringify(path)})`)
    await until(
      page,
      `location.pathname === ${JSON.stringify(path.split(/[?#]/)[0])} && ${path.startsWith('/credits') ? 'true' : '!location.hash'}`,
      'SPA sanitized arrival',
    )
    await delay(100)
  }
  const probe = async (page) =>
    evaluate(
      page,
      `(() => { window.__spaDocument = document; window.__spaHeader = document.querySelector('header'); window.__spaNavigation = performance.getEntriesByType('navigation')[0]; return true })()`,
    )
  const theaterRequests = (page) =>
    page.requests.filter((request) => request.path === '/api/v1/theaters')
      .length
  const sameDocument = async (page, documents, theaterBudget) => {
    check(
      await evaluate(
        page,
        `window.__spaDocument === document && window.__spaHeader === document.querySelector('header') && window.__spaNavigation === performance.getEntriesByType('navigation')[0]`,
      ),
      'SPA preserves document and header identity',
    )
    check(
      page.documents === documents && theaterRequests(page) === theaterBudget,
      'SPA has no extra document or theaters request',
    )
  }
  const privacy = async (page, privatePage) => {
    await until(
      page,
      `document.querySelectorAll('meta[name="referrer"]').length === 1 && document.querySelector('meta[name="referrer"]').content === '${privatePage ? 'no-referrer' : 'strict-origin'}'`,
      'Reactive referrer policy',
    )
    check(
      await evaluate(
        page,
        privatePage
          ? `document.querySelector('meta[name="robots"]')?.content.includes('noindex')`
          : `!document.querySelector('meta[name="robots"]')?.content.includes('nofollow')`,
      ),
      'reactive robots follows committed privacy boundary',
    )
  }
  const tokenPaths = [
    '/verification',
    '/reinitialiser-mot-de-passe',
    '/compte/confirmer-email',
    '/compte/confirmer-identite',
  ]
  const token = 'A'.repeat(43)
  const token2 = 'B'.repeat(43)
  await launch()
  const page = await tab()
  await go(page, '/credits?arrival=private#ignored')
  await delay(300)
  if (enabled) await until(page, '!!window.umami', 'Pinned tracker loaded')
  check(
    page.collections.length === (enabled ? 1 : 0),
    'initial public visit emitted exactly once or disabled',
  )
  await probe(page)
  const documents = page.documents
  const theaterBudget = theaterRequests(page)
  const firstReads = sessionReads
  await evaluate(
    page,
    `document.querySelector('header a[href="/connexion"]').click()`,
  )
  await until(
    page,
    `location.pathname === '/connexion' && !!document.getElementById('account-email')`,
    'header account SPA click',
  )
  await privacy(page, true)
  await sameDocument(page, documents, theaterBudget)
  check(
    sessionReads === firstReads + 1,
    'account route admission makes one fresh session check',
  )
  await evaluate(
    page,
    `(async () => { const remove = ${router}.beforeEach(to => to.path === '/credits' ? false : undefined); await ${router}.push('/credits'); remove() })()`,
  )
  await privacy(page, true)
  check(
    await evaluate(page, `location.pathname === '/connexion'`),
    'aborted public navigation retains private referrer policy even without analytics',
  )
  const collectionBudget = page.collections.length
  await fill(page, 'account-email', owner.email)
  await fill(page, 'account-password', 'Synthetic passphrase 42!')
  if (enabled) {
    await evaluate(
      page,
      `(async () => { await umami.track(); await umami.track('private-click', { email: 'secret@example.test' }); await umami.identify('private-identity'); document.body.dataset.umamiEvent = 'private'; document.body.click(); window.dispatchEvent(new PageTransitionEvent('pagehide', { persisted: true })); document.dispatchEvent(new Event('visibilitychange')); })()`,
    )
    await delay(450)
    check(
      page.collections.length === collectionBudget,
      'resident real tracker blocks pageview, event, identity, click and lifecycle on private route',
    )
  }
  await evaluate(
    page,
    `document.querySelector('main a[href="/inscription"]').click()`,
  )
  await until(
    page,
    `location.pathname === '/inscription'`,
    'actual registration link',
  )
  await evaluate(
    page,
    `document.querySelector('main a[href="/connexion"]').click()`,
  )
  await until(page, `location.pathname === '/connexion'`, 'actual login return')
  await evaluate(
    page,
    `document.querySelector('main a[href="/mot-de-passe-oublie"]').click()`,
  )
  await until(
    page,
    `location.pathname === '/mot-de-passe-oublie'`,
    'actual recovery link',
  )
  await route(page, '/connexion')
  await fill(page, 'account-email', owner.email)
  await fill(page, 'account-password', 'Synthetic passphrase 42!')
  await click(page, 'Se connecter')
  await until(
    page,
    `location.pathname === '/compte' && !!document.querySelector('nav[aria-label="Rubriques du compte"] a')`,
    'login SPA account home',
  )
  check(
    await evaluate(
      page,
      `!document.querySelector('main input,main form,#trigger-password')`,
    ),
    'home has no settings editors',
  )
  await evaluate(
    page,
    `document.querySelector('nav[aria-label="Rubriques du compte"] a').click()`,
  )
  await until(
    page,
    `location.pathname === '/compte/parametres' && !!document.getElementById('trigger-password')`,
    'home opens settings',
  )
  await getCDP().send(
    'Emulation.setDeviceMetricsOverride',
    { width: 390, height: 900, deviceScaleFactor: 1, mobile: true },
    page.sessionId,
  )
  await evaluate(
    page,
    `document.querySelector('.account-area-inner > a[href="/compte"]').click()`,
  )
  await until(
    page,
    `location.pathname === '/compte' && !!document.querySelector('nav[aria-label="Rubriques du compte"] a')`,
    'settings return opens home',
  )
  await evaluate(page, 'history.back()')
  await until(
    page,
    `location.pathname === '/compte/parametres' && !!document.getElementById('trigger-password')`,
    'browser back restores settings route',
  )
  await evaluate(page, `document.getElementById('trigger-password').click()`)
  await until(
    page,
    `!!document.getElementById('current-password')`,
    'settings draft opens',
  )
  await fill(page, 'current-password', 'Synthetic departing draft 42!')
  await evaluate(
    page,
    `document.querySelector('.account-area-inner > a[href="/compte"]').click()`,
  )
  await until(
    page,
    `location.pathname === '/compte'`,
    'settings draft departs to home',
  )
  await evaluate(
    page,
    `document.querySelector('nav[aria-label="Rubriques du compte"] a').click()`,
  )
  await until(
    page,
    `!!document.getElementById('trigger-password')`,
    'settings reopened',
  )
  check(
    await evaluate(page, `!document.getElementById('current-password')`),
    'departed settings editor stays closed',
  )
  await evaluate(page, `document.getElementById('trigger-password').click()`)
  await until(
    page,
    `!!document.getElementById('current-password')`,
    'fresh settings editor',
  )
  check(
    await evaluate(
      page,
      `document.getElementById('current-password').value === ''`,
    ),
    'home departure clears private settings drafts',
  )
  await getCDP().send(
    'Emulation.clearDeviceMetricsOverride',
    {},
    page.sessionId,
  )
  await sameDocument(page, documents, theaterBudget)
  await click(page, 'Se déconnecter')
  await until(page, `location.pathname === '/connexion'`, 'logout SPA return')
  await sameDocument(page, documents, theaterBudget)
  check(
    page.collections.length === collectionBudget,
    'login logout and internal account links produce no analytics',
  )
  await evaluate(
    page,
    `document.querySelector('a[href="/"]').closest('main') ? document.querySelector('main a[href="/"]').click() : ${router}.push('/credits')`,
  )
  // Public return uses an actual shell link to '/', then a content-only page avoids catalog mocks.
  await route(page, '/credits?secret=never-sent#never-sent')
  await privacy(page, false)
  await delay(300)
  check(
    page.collections.length > collectionBudget || !enabled,
    'public tracking resumes after private visit',
  )
  check(
    page.collections.every(({ body, headers }) => {
      const p = body.payload
      const ref =
        Object.entries(headers).find(
          ([name]) => name.toLowerCase() === 'referer',
        )?.[1] || ''
      return (
        body.type === 'event' &&
        !/[?#]/.test(p.url + p.referrer) &&
        !/connexion|compte|token|secret|private|@/.test(JSON.stringify(p)) &&
        (!ref || ref === 'http://127.0.0.1:13009/') &&
        !('id' in p) &&
        !('data' in p) &&
        !('name' in p)
      )
    }),
    'actual collector payloads and HTTP Referer contain only sanitized public data',
  )

  for (const path of tokenPaths) {
    session = path.startsWith('/compte')
      ? { enabled: true, state: 'complete', account: owner }
      : anonymous
    const direct = await tab()
    const count = writes.length
    const reads = sessionReads
    await go(direct, `${path}#token=${token}`)
    await until(
      direct,
      `!location.hash && !window.__takeAccountToken`,
      'direct token bootstrap consumed',
    )
    await delay(200)
    check(
      sessionReads === reads + 1 &&
        !direct.requests.some(
          (request) => request.path === '/api/v1/auth/session',
        ),
      'direct SSR session check has no successful hydration duplicate',
    )
    const componentToken = `(() => { for (const element of document.querySelectorAll('main input, main form, main button')) { for (let instance = element.__vueParentComponent; instance; instance = instance.parent) { if (Object.hasOwn(instance.setupState, 'token')) return instance.setupState.token } } return '' })()`
    check(
      await evaluate(direct, `${componentToken} === ${JSON.stringify(token)}`),
      'direct token reaches local mounted component once',
    )
    check(
      writes.length === count && direct.trackerCount === 0,
      'direct private token GET neither consumes API token nor loads tracker',
    )
    await probe(direct)
    const directDocuments = direct.documents
    const directTheaters = theaterRequests(direct)
    const reusedReads = sessionReads
    await route(direct, `${path}#token=${token2}`)
    check(
      await evaluate(direct, `${componentToken} === ${JSON.stringify(token2)}`),
      'same-path SPA token replaces local token without reload',
    )
    await sameDocument(direct, directDocuments, directTheaters)
    check(
      sessionReads === reusedReads + 1,
      'same-path sanitized admission performs one fresh session check',
    )
    check(
      await evaluate(
        direct,
        `![history.state, window.__NUXT__, document.querySelector('#__nuxt').__vue_app__.$nuxt.payload, localStorage, sessionStorage].some(value => (JSON.stringify(value) ?? '').includes(${JSON.stringify(token2)}))`,
      ),
      'token never serialized into Nuxt history or browser storage',
    )
    for (const hash of ['#token=invalid', `#token=${token}&token=${token2}`]) {
      await route(direct, path + hash)
      check(
        await evaluate(direct, `${componentToken} === ''`),
        'invalid and duplicate token arrivals fail closed',
      )
    }
    await route(direct, '/credits')
    await evaluate(direct, 'history.back()')
    await until(
      direct,
      `location.pathname === ${JSON.stringify(path)} && !location.hash`,
      'sanitized browser back',
    )
    check(
      await evaluate(direct, `${componentToken} === ''`),
      'history cannot resurrect consumed token',
    )
    await evaluate(direct, 'history.forward()')
    await until(direct, `location.pathname === '/credits'`, 'browser forward')
    check(
      writes.length === count,
      'all token arrival and history operations remain GET-only',
    )
  }

  session = anonymous
  await route(page, '/verification#token=' + token)
  await evaluate(page, `document.querySelector('main form').requestSubmit()`)
  await until(
    page,
    `location.pathname === '/finaliser' && !!document.getElementById('account-username')`,
    'verification submit SPA onboarding',
  )
  await fill(page, 'account-username', 'spa_owner')
  await evaluate(page, `document.querySelector('main form').requestSubmit()`)
  await until(
    page,
    `location.pathname === '/compte'`,
    'username submit SPA home',
  )
  await evaluate(
    page,
    `document.querySelector('nav[aria-label="Rubriques du compte"] a').click()`,
  )
  await until(
    page,
    `location.pathname === '/compte/parametres' && !!document.getElementById('trigger-password')`,
    'onboarding home opens settings',
  )
  await sameDocument(page, documents, theaterRequests(page))
  await click(page, 'Se déconnecter')
  await until(
    page,
    `location.pathname === '/connexion' && !document.querySelector('button[type="submit"]')?.disabled`,
    'supported logout before held login',
  )
  hold = '/api/v1/auth/login'
  await fill(page, 'account-email', owner.email)
  await fill(page, 'account-password', 'Synthetic passphrase 42!')
  await click(page, 'Se connecter')
  await waitHeld()
  await route(page, '/credits')
  release()
  await delay(300)
  check(
    await evaluate(page, `location.pathname === '/credits'`),
    'late successful login cannot redirect abandoned page',
  )
  session = anonymous
  hold = '/api/v1/auth/session'
  await evaluate(page, `void ${router}.push('/verification#token=${token}')`)
  await waitHeld()
  await route(page, '/credits')
  release()
  await delay(300)
  check(
    await evaluate(page, `location.pathname === '/credits' && !location.hash`),
    'cancelled token admission cannot replace newer public navigation',
  )
  if (enabled) {
    for (const sameOrigin of [false, true]) {
      const delayed = await tab()
      delayed.scriptDelay = 800
      delayed.sameOriginCollector = sameOrigin
      await go(delayed, '/credits')
      await route(delayed, '/connexion')
      await until(
        delayed,
        '!!window.umami',
        'delayed tracker executed on private page',
      )
      await delay(200)
      check(
        delayed.collections.length === 0,
        'delayed real tracker suppresses stale public event on private arrival',
      )
      await route(delayed, '/credits?ignored=secret#ignored')
      await delay(250)
      check(
        delayed.collections.length === 1 &&
          delayed.collections[0].body.payload.referrer === '',
        'delayed tracker resumes only current public page without private referrer',
      )
      check(
        delayed.collections.every(({ headers }) => {
          const ref = Object.entries(headers).find(
            ([name]) => name.toLowerCase() === 'referer',
          )?.[1]
          return !ref || ref === 'http://127.0.0.1:13009/'
        }),
        'same/cross-origin collector HTTP Referer strips private document URL',
      )
      const initial = delayed.collections.length
      await route(delayed, '/confidentialite?email=secret@example.test')
      await until(
        delayed,
        `location.pathname === '/confidentialite'`,
        'public pathname view',
      )
      await delay(200)
      check(
        delayed.collections.length === initial + 1 &&
          delayed.collections.at(-1).body.payload.url === '/confidentialite',
        'canonical public pathname produces exactly one pageview',
      )
      await evaluate(
        delayed,
        `(async () => { await umami.track(); await umami.track('custom', { email: 'secret@example.test' }); await umami.identify('secret-id') })()`,
      )
      await route(delayed, '/confidentialite?other=secret@example.test')
      await delay(150)
      check(
        delayed.collections.length === initial + 1,
        'public arbitrary calls and query-only changes cannot emit extra events',
      )
      await evaluate(delayed, `history.back()`)
      await until(
        delayed,
        `location.search.includes('email=')`,
        'query history back',
      )
      await evaluate(delayed, `history.back()`)
      await until(
        delayed,
        `location.pathname === '/credits'`,
        'public pathname history back',
      )
      await delay(200)
      check(
        delayed.collections.length === initial + 2 &&
          delayed.collections.at(-1).body.payload.url === '/credits',
        'popstate emits one canonical public pageview',
      )
      await evaluate(
        delayed,
        `(async () => { window.__cancelSpa = ${router}.beforeEach(to => to.path === '/connexion' ? false : undefined); await ${router}.push('/connexion'); window.__cancelSpa(); delete window.__cancelSpa })()`,
      )
      await delay(150)
      check(
        delayed.collections.length === initial + 2,
        'aborted private navigation does not duplicate public pageview',
      )
      check(
        delayed.collections.every(({ url, body, headers }) => {
          const ref = Object.entries(headers).find(
            ([name]) => name.toLowerCase() === 'referer',
          )?.[1]
          return (
            url ===
              `${sameOrigin ? 'http://127.0.0.1:13009' : 'https://analytics.example.test'}/api/send` &&
            Object.keys(body.payload).sort().join(',') ===
              'hostname,language,referrer,screen,title,url,website' &&
            !/[?#@]/.test(JSON.stringify(body.payload)) &&
            (!ref || ref === 'http://127.0.0.1:13009/')
          )
        }),
        'complete same/cross-origin collector envelope and endpoint remain sanitized',
      )
    }
    const failed = await tab()
    failed.scriptFailure = true
    await go(failed, '/credits')
    await route(failed, '/connexion')
    await route(failed, '/credits')
    check(
      failed.collections.length === 0 &&
        failed.trackerCount === 1 &&
        (await evaluate(failed, `location.pathname === '/credits'`)),
      'tracker load failure remains optional and never retries on private navigation',
    )
  }

  // Isolated fixture cookies exercise request-scoped SSR and separate browser stores.
  const isolated = []
  for (const [index, email] of [
    'first@example.test',
    'second@example.test',
  ].entries()) {
    const key = String(index + 1).repeat(43)
    contexts.set(key, {
      enabled: true,
      state: 'complete',
      account: { ...owner, email, username: `owner_${index}` },
    })
    const contextPage = await tab()
    await go(contextPage, '/credits')
    await evaluate(
      contextPage,
      `document.cookie = 'messeances_session_dev=${key}; path=/'`,
    )
    isolated.push({ page: contextPage, key, email })
  }
  await Promise.all(
    isolated.map(({ page: contextPage }) =>
      go(contextPage, '/compte/parametres'),
    ),
  )
  const ssr = await Promise.all(
    isolated.map(async ({ key }) => {
      const response = await fetch('http://127.0.0.1:13009/compte/parametres', {
        headers: { Cookie: `messeances_session_dev=${key}` },
      })
      return { response, html: await response.text() }
    }),
  )
  check(
    ssr.every(
      ({ response, html }, index) =>
        response.status === 200 &&
        html.includes(isolated[index].email) &&
        !html.includes(isolated[1 - index].email) &&
        response.headers.get('cache-control')?.includes('no-store') &&
        response.headers.get('referrer-policy') === 'no-referrer' &&
        response.headers.get('x-robots-tag')?.includes('noindex'),
    ),
    'concurrent private SSR HTML and serialized payloads isolate cookies and preserve privacy headers',
  )
  for (const path of [
    '/compte',
    '/compte/parametres',
    '/compte/_payload.json',
    '/compte/parametres/_payload.json',
    '/compte/unknown',
    '/api/v1/auth/session',
  ]) {
    const response = await fetch(`http://127.0.0.1:13009${path}`, {
      headers: {
        Accept: path === '/compte/unknown' ? 'text/html' : 'application/json',
      },
    })
    await response.arrayBuffer()
    check(
      response.headers.get('cache-control')?.includes('no-store') &&
        response.headers.get('referrer-policy') === 'no-referrer' &&
        response.headers.get('x-robots-tag')?.includes('noindex'),
      'private HTML, payload and modeled API responses retain privacy headers including errors',
    )
  }
  for (const { page: contextPage, email } of isolated) {
    await until(
      contextPage,
      `document.querySelector('main').textContent.includes(${JSON.stringify(email)})`,
      'isolated SSR account identity',
    )
    check(
      await evaluate(
        contextPage,
        `!document.querySelector('main').textContent.includes(${JSON.stringify(email === isolated[0].email ? isolated[1].email : isolated[0].email)})`,
      ),
      'concurrent distinct-cookie SSR and browser contexts isolate identity',
    )
  }
  const sameContext = await tab(isolated[0].page.browserContextId)
  await go(sameContext, '/compte/parametres')
  await click(isolated[0].page, 'Se déconnecter')
  await until(
    sameContext,
    `!document.querySelector('main').textContent.includes('first@example.test')`,
    'same-context BroadcastChannel removes predecessor details',
  )
  check(
    await evaluate(
      isolated[1].page,
      `document.querySelector('main').textContent.includes('second@example.test')`,
    ),
    'logout broadcast is isolated from other browser context',
  )

  const disabledKey = 'D'.repeat(43)
  contexts.set(disabledKey, {
    enabled: false,
    state: 'anonymous',
    account: null,
  })
  const disabled = await tab(undefined, disabledKey)
  await go(disabled, '/credits')
  await probe(disabled)
  const disabledDocuments = disabled.documents
  const disabledTheaters = theaterRequests(disabled)
  for (const path of ['/connexion', '/inscription', '/mot-de-passe-oublie']) {
    await route(disabled, path)
    check(
      await evaluate(
        disabled,
        `[...document.querySelectorAll('main input')].every(input => input.getClientRects().length === 0) && document.querySelector('main').textContent.includes('Les comptes ne sont pas encore disponibles')`,
      ),
      'accounts-disabled route does not expose credential form',
    )
  }
  await route(disabled, '/credits')
  await sameDocument(disabled, disabledDocuments, disabledTheaters)

  const continuationPage = isolated[1].page
  await probe(continuationPage)
  const continuationDocuments = continuationPage.documents
  const continuationTheaters = theaterRequests(continuationPage)
  await route(continuationPage, '/credits')
  // App-lifetime cinema preferences remain subscribed after settings unmounts.
  const detailBaseline = await evaluate(
    continuationPage,
    `document.querySelector('#__nuxt').__vue_app__.$nuxt._accountRevalidation.details.size`,
  )
  hold = '/api/v1/account'
  await route(continuationPage, '/compte/parametres')
  await waitHeld()
  check(
    await evaluate(
      continuationPage,
      `document.querySelector('#__nuxt').__vue_app__.$nuxt._accountRevalidation.details.size === ${detailBaseline + 1}`,
    ),
    'settings adds one page-local detail callback above app baseline',
  )
  await route(continuationPage, '/compte')
  release()
  await delay(200)
  check(
    await evaluate(
      continuationPage,
      `document.querySelector('#__nuxt').__vue_app__.$nuxt._accountRevalidation.details.size === ${detailBaseline} && !document.querySelector('main').textContent.includes('second@example.test')`,
    ),
    'late details cannot restore departed UI or subscription',
  )
  await route(continuationPage, '/compte/confirmer-identite#token=' + token)
  await until(
    continuationPage,
    `!!document.querySelector('main form')`,
    'identity proof form',
  )
  const proofWrites = writes.length
  hold = '/api/v1/account/reauth/email/confirm'
  await evaluate(
    continuationPage,
    `document.querySelector('main form').requestSubmit()`,
  )
  await waitHeld()
  await route(continuationPage, '/credits')
  release()
  await delay(200)
  check(
    writes.length === proofWrites + 1 &&
      (await evaluate(
        continuationPage,
        `location.pathname === '/credits' && !document.getElementById('add-password')`,
      )),
    'late proof cannot trigger obsolete write or restore action form',
  )
  await route(continuationPage, '/compte/confirmer-identite#token=' + token2)
  await until(
    continuationPage,
    `!!document.querySelector('main form')`,
    'fresh identity proof form',
  )
  await evaluate(
    continuationPage,
    `document.querySelector('main form').requestSubmit()`,
  )
  await until(
    continuationPage,
    `!!document.getElementById('add-password')`,
    'explicit proof enables action',
  )
  await fill(continuationPage, 'add-password', 'Synthetic updated password 42!')
  await evaluate(
    continuationPage,
    `document.querySelector('main form').requestSubmit()`,
  )
  await until(
    continuationPage,
    `document.querySelector('main').textContent.includes('Votre mot de passe a été ajouté')`,
    'identity action success',
  )
  await evaluate(
    continuationPage,
    `document.querySelector('main a[href="/compte"]').click()`,
  )
  await until(
    continuationPage,
    `location.pathname === '/compte'`,
    'identity action SPA return',
  )
  await route(continuationPage, '/compte/confirmer-email#token=' + token)
  await until(
    continuationPage,
    `!!document.getElementById('confirm-email-password')`,
    'email confirmation form',
  )
  await fill(
    continuationPage,
    'confirm-email-password',
    'Synthetic passphrase 42!',
  )
  await evaluate(
    continuationPage,
    `document.querySelector('main form').requestSubmit()`,
  )
  await until(
    continuationPage,
    `document.querySelector('main').textContent.includes('Votre email a été modifié')`,
    'email confirmation success',
  )
  await evaluate(
    continuationPage,
    `document.querySelector('main a[href="/connexion"]').click()`,
  )
  await until(
    continuationPage,
    `location.pathname === '/connexion'`,
    'email confirmation SPA login',
  )
  await route(continuationPage, '/reinitialiser-mot-de-passe#token=' + token)
  await fill(
    continuationPage,
    'reset-password',
    'Synthetic updated password 42!',
  )
  await evaluate(
    continuationPage,
    `document.querySelector('main form').requestSubmit()`,
  )
  await until(
    continuationPage,
    `document.querySelector('main').textContent.includes('Votre mot de passe a été modifié')`,
    'reset password explicit success',
  )
  await evaluate(
    continuationPage,
    `document.querySelector('main a[href="/connexion"]').click()`,
  )
  await until(
    continuationPage,
    `location.pathname === '/connexion'`,
    'reset SPA login',
  )
  await sameDocument(
    continuationPage,
    continuationDocuments,
    continuationTheaters,
  )

  await route(continuationPage, '/credits')
  const admissionReads = sessionReads
  hold = '/api/v1/auth/session'
  await evaluate(continuationPage, `void ${router}.push('/connexion')`)
  await waitHeld()
  await evaluate(continuationPage, `window.dispatchEvent(new Event('focus'))`)
  await delay(150)
  check(
    sessionReads === admissionReads + 1,
    'concurrent focus shares in-flight route admission',
  )
  release()
  await until(
    continuationPage,
    `location.pathname === '/connexion'`,
    'shared session admission',
  )
  const subscriptions = await evaluate(
    continuationPage,
    `(() => { const app = document.querySelector('#__nuxt').__vue_app__.$nuxt; return [app._accountNavigation.starts.size, app._accountNavigation.arrivals.size, app._accountRevalidation.details.size] })()`,
  )
  for (let i = 0; i < 3; i++) {
    await route(continuationPage, '/mot-de-passe-oublie')
    await route(continuationPage, '/connexion')
  }
  await delay(400)
  check(
    await evaluate(
      continuationPage,
      `(() => { const app = document.querySelector('#__nuxt').__vue_app__.$nuxt; return JSON.stringify([app._accountNavigation.starts.size, app._accountNavigation.arrivals.size, app._accountRevalidation.details.size]) === ${JSON.stringify(JSON.stringify(subscriptions))} })()`,
    ),
    'repeated navigation does not accumulate local subscriptions or detail callbacks',
  )
  await click(continuationPage, 'Continuer avec Google')
  for (let i = 0; !continuationPage.googleStarts && i < 50; i++) await delay(50)
  check(
    continuationPage.googleStarts === 1,
    'allowlisted Google authorization alone leaves SPA and is intercepted before provider',
  )
  check(
    writes.filter(({ path }) => path.endsWith('/verification/confirm'))
      .length === 1,
    'verification POST only occurs on explicit confirmation',
  )
}
