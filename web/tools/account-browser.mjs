// Requires fresh accounts_browser_harness_test.go + test:accounts:server.
// Installed Chrome and Node WebSocket only. No screenshots, traces, mail or secrets saved.
import { spawn } from 'node:child_process'
import { mkdtemp, rm } from 'node:fs/promises'
import { setTimeout as delay } from 'node:timers/promises'
import { CDP, CaptureError, terminateProcessGroup } from './screenshot.mjs'

const origin = 'http://127.0.0.1:13009'
const api = 'http://127.0.0.1:18089'
const tracker = 'https://analytics.example.test/script.js'
const password = 'Cinéma🎬42!'
const replacement = 'Autre🎬42!x'
const recovered = 'Après🎬42!x'
const shortPassword = 'Cinéma🎬42'
const run = Date.now().toString(36)
const emailA = `browser-a-${run}@example.test`
const emailB = `browser-b-${run}@example.test`
const newEmail = `browser-new-${run}@example.test`
const usernameA = `browser_a_${run}`
const usernameB = `browser_b_${run}`
let phase = 'prerequisites'
let chrome, cdp, profile
let passed = 0
let interceptionFailure = false
const responses = []
const allPages = []
class HarnessError extends Error {}

function check(value, label) {
  if (!value) throw new HarnessError(label)
  passed++
  console.log(`PASS ${label}`)
}

class BrowserCDP extends CDP {
  observers = new Map()
  onMessage(data) {
    super.onMessage(data)
    const event = JSON.parse(data)
    if (!event.method) return
    const handler = this.observers.get(
      `${event.sessionId ?? ''}:${event.method}`,
    )
    if (handler)
      Promise.resolve(handler(event.params)).catch(() => {
        interceptionFailure = true
      })
  }
  on(method, session, handler) {
    this.observers.set(`${session}:${method}`, handler)
  }
}

async function launch() {
  profile = await mkdtemp('/tmp/opencode/account-browser-')
  chrome = spawn(
    process.env.CHROME_BIN || 'google-chrome',
    [
      '--headless=new',
      '--no-sandbox',
      '--disable-gpu',
      '--disable-background-networking',
      '--disable-component-update',
      '--disable-sync',
      '--disable-default-apps',
      '--no-first-run',
      '--no-default-browser-check',
      '--disable-quic',
      '--proxy-server=http://127.0.0.1:9',
      '--proxy-bypass-list=127.0.0.1',
      '--host-resolver-rules=MAP * ~NOTFOUND, EXCLUDE 127.0.0.1',
      '--remote-debugging-address=127.0.0.1',
      '--remote-debugging-port=0',
      `--user-data-dir=${profile}`,
      'about:blank',
    ],
    { detached: true, stdio: ['ignore', 'ignore', 'pipe'] },
  )
  const endpoint = await new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('Chrome readiness')), 15000)
    let buffer = ''
    chrome.stderr.on('data', (chunk) => {
      buffer = (buffer + chunk.toString()).slice(-4096)
      const match = buffer.match(
        /DevTools listening on (ws:\/\/127\.0\.0\.1:[^\s]+)/u,
      )
      if (match) {
        clearTimeout(timer)
        resolve(match[1])
        buffer = ''
      }
    })
    chrome.once('error', () => {
      clearTimeout(timer)
      reject(new Error('Chrome launch'))
    })
  })
  cdp = new BrowserCDP(endpoint)
  await cdp.open()
}

async function tab(context) {
  const browserContextId =
    context || (await cdp.send('Target.createBrowserContext')).browserContextId
  const { targetId } = await cdp.send('Target.createTarget', {
    url: 'about:blank',
    browserContextId,
  })
  const { sessionId } = await cdp.send('Target.attachToTarget', {
    targetId,
    flatten: true,
  })
  const page = {
    sessionId,
    browserContextId,
    requests: [],
    trackerCount: 0,
    external: 0,
  }
  allPages.push(page)
  await cdp.send('Page.enable', {}, sessionId)
  await cdp.send('Runtime.enable', {}, sessionId)
  await cdp.send('Network.enable', {}, sessionId)
  cdp.on('Network.responseReceived', sessionId, ({ response }) => {
    const url = new URL(response.url)
    if (
      url.origin === origin &&
      /^\/api\/v1\/(auth|account)(\/|$)/u.test(url.pathname)
    )
      responses.push({
        path: url.pathname,
        status: response.status,
        retryAfter: Number(
          Object.entries(response.headers).find(
            ([name]) => name.toLowerCase() === 'retry-after',
          )?.[1],
        ),
      })
  })
  cdp.on('Network.requestWillBeSent', sessionId, ({ redirectResponse }) => {
    if (!redirectResponse) return
    const url = new URL(redirectResponse.url)
    if (
      url.origin === origin &&
      /^\/api\/v1\/(auth|account)(\/|$)/u.test(url.pathname)
    )
      responses.push({ path: url.pathname, status: redirectResponse.status })
  })
  await cdp.send('Fetch.enable', { patterns: [{ urlPattern: '*' }] }, sessionId)
  cdp.on('Fetch.requestPaused', sessionId, async ({ requestId, request }) => {
    const url = new URL(request.url)
    if (request.url === tracker) {
      page.trackerCount++
      await cdp.send(
        'Fetch.fulfillRequest',
        {
          requestId,
          responseCode: 200,
          responseHeaders: [
            { name: 'Content-Type', value: 'application/javascript' },
          ],
          body: Buffer.from('window.__syntheticTracker = true;').toString(
            'base64',
          ),
        },
        sessionId,
      )
    } else if (url.origin === origin) {
      if (url.pathname.startsWith('/api/v1/')) {
        const headers = Object.fromEntries(
          Object.entries(request.headers).map(([key, value]) => [
            key.toLowerCase(),
            value,
          ]),
        )
        page.requests.push({
          path: url.pathname,
          method: request.method,
          origin: headers.origin,
          csrf: headers['x-messeances-csrf'],
          bodyKeys: request.postData
            ? Object.keys(JSON.parse(request.postData)).sort()
            : [],
        })
      }
      await cdp.send('Fetch.continueRequest', { requestId }, sessionId)
    } else {
      page.external++
      await cdp.send(
        'Fetch.failRequest',
        { requestId, errorReason: 'BlockedByClient' },
        sessionId,
      )
    }
  })
  return page
}

async function evaluate(page, expression) {
  const result = await cdp.send(
    'Runtime.evaluate',
    { expression, awaitPromise: true, returnByValue: true },
    page.sessionId,
  )
  if (result.exceptionDetails) throw new Error('Browser evaluation failed')
  return result.result.value
}
async function until(page, expression, label, timeout = 20000) {
  const deadline = Date.now() + timeout
  while (Date.now() < deadline) {
    try {
      if (await evaluate(page, expression)) return
    } catch {
      /* Document navigation. */
    }
    await delay(100)
  }
  throw new HarnessError(label)
}
async function go(page, path) {
  const url = new URL(path, origin)
  if (url.origin !== origin) throw new Error('Nonlocal navigation refused')
  // Also wait for navigation away from the old document, including hash-only
  // account links that the privacy boundary explicitly reloads.
  await evaluate(page, 'window.__navigationProbe = true')
  await cdp.send('Page.navigate', { url: url.href }, page.sessionId)
  await until(
    page,
    `!window.__navigationProbe && location.pathname === ${JSON.stringify(url.pathname)} && document.readyState === 'complete' && !!document.querySelector('main')`,
    'Page readiness',
  )
  await until(
    page,
    `!!document.querySelector('#__nuxt')?.__vue_app__`,
    'Vue hydration',
  )
}
async function fill(page, id, value) {
  await until(
    page,
    `!!document.querySelector('#__nuxt')?.__vue_app__`,
    'Vue hydration',
  )
  await until(
    page,
    `!!document.getElementById(${JSON.stringify(id)}) && !document.getElementById(${JSON.stringify(id)}).disabled`,
    'Input ready',
  )
  await evaluate(
    page,
    `(() => { const input = document.getElementById(${JSON.stringify(id)}); input.focus(); input.value = ${JSON.stringify(value)}; input.dispatchEvent(new Event('input', { bubbles: true })); input.dispatchEvent(new Event('change', { bubbles: true })); })()`,
  )
}
async function click(page, text) {
  await until(
    page,
    `!!document.querySelector('#__nuxt')?.__vue_app__`,
    'Vue hydration',
  )
  const expression = `Array.from(document.querySelectorAll('button,a')).find(el => el.textContent.trim() === ${JSON.stringify(text)} && !el.disabled)`
  await until(page, `!!(${expression})`, 'Action ready')
  await evaluate(page, `(${expression}).click()`)
}
async function text(page, fragment) {
  await until(
    page,
    `document.body.innerText.includes(${JSON.stringify(fragment)})`,
    'Expected UI state',
  )
}
async function request(page, path, body, method = 'POST', csrf = true) {
  return evaluate(
    page,
    `(async () => { const response = await fetch(${JSON.stringify(`/api/v1${path}`)}, { method: ${JSON.stringify(method)}, headers: {'Content-Type': 'application/json', ${csrf ? "'X-Messeances-CSRF': '1'" : ''}}, ${method === 'GET' ? '' : `body: JSON.stringify(${JSON.stringify(body)}),`} cache: 'no-store' }); const text = await response.text(); return {status: response.status, body: text ? JSON.parse(text) : null}; })()`,
  )
}
async function session(page) {
  return (await request(page, '/auth/session', undefined, 'GET')).body
}
async function cookie(page, name = 'messeances_session_dev') {
  const { cookies } = await cdp.send(
    'Network.getCookies',
    { urls: [origin] },
    page.sessionId,
  )
  return cookies.find((item) => item.name === name)
}
async function mail(email, purpose) {
  const response = await fetch(
    `${api}/api/__browser/mailbox?email=${encodeURIComponent(email)}`,
    {
      headers: { 'X-Browser-Harness': '1' },
      signal: AbortSignal.timeout(10000),
    },
  )
  if (!response.ok) throw new Error('Mailbox fixture unavailable')
  const message = (await response.json()).messages.find(
    (item) => item.purpose === purpose,
  )
  if (!message) throw new Error('Synthetic mail missing')
  return message
}
async function fragmentVisit(page, link) {
  // Observe the real parser-blocking bootstrap, retaining booleans only.
  const { identifier } = await cdp.send(
    'Page.addScriptToEvaluateOnNewDocument',
    {
      source: `(() => { const replace = history.replaceState; history.replaceState = function(...args) { const hadFragment = location.hash.length > 0; const first = !!document.currentScript && document.scripts[0] === document.currentScript && document.scripts.length === 1; const result = replace.apply(this, args); if (hadFragment) window.__fragmentProbe = { first, cleared: location.hash === '' }; return result; }; })()`,
    },
    page.sessionId,
  )
  try {
    await go(page, link)
  } finally {
    await cdp.send(
      'Page.removeScriptToEvaluateOnNewDocument',
      { identifier },
      page.sessionId,
    )
  }
  check(
    await evaluate(
      page,
      `window.__fragmentProbe?.first && window.__fragmentProbe.cleared`,
    ),
    'first parser-blocking application script clears fragment before later scripts',
  )
}
async function login(page, email, secret) {
  await go(page, '/connexion')
  await fill(page, 'account-email', email)
  await fill(page, 'account-password', secret)
  const before = responses.length
  await click(page, 'Se connecter')
  await until(
    page,
    `location.pathname === '/compte' || !!document.getElementById('account-credentials-error')`,
    'Login response',
  )
  const limited = responses
    .slice(before)
    .find((item) => item.path === '/api/v1/auth/login' && item.status === 429)
  if (limited) {
    check(
      Number.isFinite(limited.retryAfter) &&
        limited.retryAfter > 0 &&
        limited.retryAfter <= 60,
      'browser scenario respects bounded real login Retry-After',
    )
    // New negative-path checks share the real per-IP login bucket. Wait for its
    // stated refill, then simulate one explicit retry; never disable rate limits.
    await delay((limited.retryAfter + 1) * 1000)
    await click(page, 'Se connecter')
  }
  await until(
    page,
    `location.pathname === '/compte' && !!document.getElementById('new-password')`,
    'Login complete',
  )
}
async function register(page, email, username, reserved = false) {
  await go(page, '/inscription')
  await noExplore(page)
  await fill(page, 'account-email', email)
  await rejectShortPassword(page, 'account-password', 'Créer mon compte')
  await fill(page, 'account-password', password)
  await click(page, 'Créer mon compte')
  await text(page, 'Si cette adresse peut être utilisée')
  const registeredAt = Date.now()
  check(!(await cookie(page)), 'registration does not establish a session')
  const proof = await cookie(page, 'messeances_registration_dev')
  check(
    proof?.httpOnly &&
      proof.sameSite === 'Lax' &&
      proof.path === '/' &&
      !(await evaluate(
        page,
        `document.cookie.includes('messeances_registration_dev')`,
      )),
    'registration proof remains HttpOnly and unavailable to scripts',
  )
  let message = await mail(email, 'verification')
  if (reserved) {
    const other = await tab()
    await fragmentVisit(other, message.link)
    await verificationForm(other)
    await click(other, 'Confirmer mon email')
    await text(
      other,
      'Rouvrez ce lien dans le navigateur où vous avez commencé votre inscription',
    )
    check(
      await evaluate(
        other,
        `document.querySelector('a[href="/inscription"]')?.textContent.includes('Recommencer') && !document.getElementById('verification-email')`,
      ),
      'cross-browser failure offers fresh registration, not ineffective resend',
    )
    check(
      (await request(other, '/auth/login', { email, password })).body.state ===
        'pending_email',
      'tentative login grants only pending session',
    )
    await go(other, message.link)
    await click(other, 'Confirmer mon email')
    await text(
      other,
      'Rouvrez ce lien dans le navigateur où vous avez commencé votre inscription',
    )
    check(
      (await session(other)).state === 'pending_email',
      'tentative password login cannot substitute for browser proof',
    )
    await click(other, 'Recommencer l’inscription')
    await until(
      other,
      `location.pathname === '/inscription' && !!document.getElementById('account-password')`,
      'Pending session can restart registration',
    )
    check(true, 'restart link remains usable with pending password session')
  } else {
    await cdp.send(
      'Network.deleteCookies',
      { name: 'messeances_registration_dev', url: origin },
      page.sessionId,
    )
    await go(page, message.link)
    await click(page, 'Confirmer mon email')
    await text(
      page,
      'Rouvrez ce lien dans le navigateur où vous avez commencé votre inscription',
    )
    await click(page, 'Recommencer l’inscription')
    await fill(page, 'account-email', email)
    await fill(page, 'account-password', replacement)
    // Respect real resend/registration cooldown, never weaken fixture quotas.
    await delay(Math.max(0, registeredAt + 61000 - Date.now()))
    await click(page, 'Créer mon compte')
    await text(page, 'Si cette adresse peut être utilisée')
    const previous = message
    message = await mail(email, 'verification')
    check(
      message.token !== previous.token,
      'lost proof recovery creates fresh registration and link',
    )
    await go(page, previous.link)
    await click(page, 'Confirmer mon email')
    await text(page, 'Ce lien est invalide ou expiré')
    check(
      (await session(page)).state === 'anonymous',
      'fresh registration invalidates previous verification link',
    )
  }
  const before = page.requests.length
  await fragmentVisit(page, message.link)
  await verificationForm(page)
  check(
    await evaluate(page, `location.hash === ''`),
    'verification fragment removed',
  )
  check(
    !page.requests.slice(before).some((item) => item.method !== 'GET'),
    'verification GET does not consume token',
  )
  check(
    (await session(page)).state === 'anonymous',
    'verification requires explicit confirmation, without a second password',
  )
  check(
    await evaluate(
      page,
      `![...Object.values(localStorage), ...Object.values(sessionStorage)].some(value => value.includes(${JSON.stringify(message.token)}))`,
    ),
    'verification token absent from browser storage',
  )
  await click(page, 'Confirmer mon email')
  await until(
    page,
    `location.pathname === '/finaliser' && !!document.getElementById('account-username')`,
    'Verified onboarding',
  )
  const pendingCookie = await cookie(page)
  await noExplore(page)
  check(
    !(await cookie(page, 'messeances_registration_dev')),
    'verification clears originating registration proof',
  )
  check(
    page.requests
      .filter((item) => item.path === '/api/v1/auth/verification/confirm')
      .every(
        (item) => item.method === 'POST' && item.bodyKeys.join(',') === 'token',
      ),
    'verification submits only token with explicit POST',
  )
  if (reserved) {
    await fill(page, 'account-username', 'ADMIN')
    await click(page, 'Confirmer mon nom')
    await until(
      page,
      `!!document.querySelector('[role="alert"]')`,
      'Reserved username error',
    )
    check(
      (await session(page)).state === 'pending_username',
      'reserved username cannot complete onboarding',
    )
  }
  await fill(page, 'account-username', username.toUpperCase())
  await click(page, 'Confirmer mon nom')
  await until(
    page,
    `location.pathname === '/compte' && !!document.getElementById('new-password')`,
    'Account completed',
  )
  const completeCookie = await cookie(page)
  check(
    completeCookie?.value !== pendingCookie?.value,
    'onboarding rotates session cookie',
  )
  check(
    completeCookie?.httpOnly &&
      completeCookie.sameSite === 'Lax' &&
      !completeCookie.secure &&
      completeCookie.domain === '127.0.0.1' &&
      completeCookie.path === '/' &&
      completeCookie.expires > Date.now() / 1000 + 29 * 86400,
    'development persistent host-only HttpOnly cookie',
  )
  check(
    (await session(page)).account.username === username,
    'uppercase username normalized and account complete',
  )
}

async function noExplore(page) {
  check(
    !(await evaluate(
      page,
      `Array.from(document.querySelectorAll('a,button')).some(el => el.textContent.trim() === 'Explorer les séances')`,
    )),
    'login and registration flow omit exploration action',
  )
}

async function verificationForm(page) {
  await text(page, 'Confirmer mon email')
  await noExplore(page)
  check(
    await evaluate(
      page,
      `!document.querySelector('input[type="password"],input[type="radio"]') && !document.body.innerText.includes('Mode d’inscription') && !document.body.innerText.includes('Choisir mon mot de passe')`,
    ),
    'verification has no password or signup-method selector',
  )
}

async function rejectShortPassword(page, id, submit) {
  await fill(page, id, shortPassword)
  const before = page.requests.length
  await click(page, submit)
  await text(page, 'Choisissez un mot de passe de 10 à 128 caractères.')
  check(
    !page.requests.slice(before).some((item) => item.method !== 'GET'),
    'nine Unicode characters rejected before API mutation',
  )
  check(
    !(await evaluate(page, `document.body.innerText.includes('512 octets')`)),
    'password criteria omit byte-limit copy',
  )
}

async function googleRedirect(page, body, identity, destination) {
  // Deliberately bypasses production Google-button allowlist for the local fixture.
  const response = await request(page, '/auth/google/start', body)
  check(response.status === 200, 'simulated provider start accepted')
  const url = new URL(response.body.authorization_url)
  if (
    url.origin !== origin ||
    url.pathname !== '/api/__browser/google/authorize'
  )
    throw new HarnessError('Only local simulated provider redirect allowed')
  url.searchParams.set('identity', identity)
  await cdp.send('Page.navigate', { url: url.href }, page.sessionId)
  await until(
    page,
    `location.pathname === ${JSON.stringify(destination)} && !!document.querySelector('#__nuxt')?.__vue_app__`,
    'Simulated provider callback',
  )
  check(
    await evaluate(page, `!location.search && !location.hash`),
    'simulated callback strips provider state from destination',
  )
}

async function simulatedGoogle() {
  let google = await tab()
  await go(google, '/connexion')
  await googleRedirect(google, { mode: 'login' }, 'verified', '/finaliser')
  check(
    (await session(google)).state === 'pending_username',
    'simulated verified Google skips mailbox verification',
  )
  await fill(google, 'account-username', `browser_google_${run}`)
  await click(google, 'Confirmer mon nom')
  await until(
    google,
    `location.pathname === '/compte'`,
    'Simulated Google account',
  )
  await text(google, 'Google est votre seul moyen de connexion')
  check(
    !(await evaluate(
      google,
      `document.body.innerText.includes('Dissocier Google')`,
    )),
    'Google-only UI has no unlink-last-method action',
  )
  await googleRedirect(
    google,
    { mode: 'reauth', action: 'password_add' },
    'verified',
    '/compte/confirmer-identite',
  )
  const before = google.requests.length
  await text(google, 'Recevoir le lien de vérification')
  check(
    !google.requests.slice(before).some((item) => item.method !== 'GET'),
    'Google continuation GET does not automatically request proof',
  )
  await click(google, 'Recevoir le lien de vérification')
  await text(google, 'La demande a été acceptée.')
  const proof = await mail('google-verified@example.test', 'email_step_up')
  // Mail clients commonly open a new tab. Preserve browser/session binding.
  google = await tab(google.browserContextId)
  await fragmentVisit(google, proof.link)
  check(
    !(await session(google)).account.has_password,
    'email proof link GET does not add password',
  )
  await click(google, 'Confirmer mon identité avec ce lien')
  await rejectShortPassword(google, 'add-password', 'Ajouter mon mot de passe')
  await fill(google, 'add-password', replacement)
  check(
    !(await session(google)).account.has_password,
    'confirmed proof still requires explicit final action',
  )
  await click(google, 'Ajouter mon mot de passe')
  await text(google, 'Votre mot de passe a été ajouté.')
  await go(google, '/compte')
  await fill(google, 'google-password', replacement)
  await click(google, 'Dissocier Google')
  await text(google, 'Google a été dissocié.')
  check(
    !(await session(google)).account.google_linked,
    'simulated Google unlink preserves added password',
  )

  let pending = await tab()
  await go(pending, '/connexion')
  await googleRedirect(
    pending,
    { mode: 'login' },
    'unverified',
    '/verification',
  )
  check(
    (await session(pending)).state === 'pending_email',
    'simulated unverified Google requires mailbox proof',
  )
  const verification = await mail(
    'google-unverified@example.test',
    'verification',
  )
  const missing = await tab()
  await fragmentVisit(missing, verification.link)
  await verificationForm(missing)
  await click(missing, 'Confirmer mon email')
  await text(
    missing,
    'Connectez-vous avec le même compte Google, puis rouvrez ce lien.',
  )
  check(
    await evaluate(
      missing,
      `Array.from(document.querySelectorAll('button')).some(el => el.textContent.trim() === 'Se connecter avec Google') && !document.querySelector('a[href="/inscription"]')`,
    ),
    'Google missing-session recovery offers Google sign-in without method selector',
  )
  // Local redirect adapter substitutes only provider transport, never session proof.
  await googleRedirect(
    missing,
    { mode: 'login' },
    'unverified',
    '/verification',
  )
  await go(missing, verification.link)
  await verificationForm(missing)
  pending = await tab(missing.browserContextId)
  await fragmentVisit(pending, verification.link)
  await verificationForm(pending)
  await click(pending, 'Confirmer mon email')
  await until(
    pending,
    `location.pathname === '/finaliser'`,
    'Unverified Google mailbox confirmation',
  )
  check(
    (await session(pending)).state === 'pending_username',
    'simulated unverified Google advances only after explicit confirmation',
  )
  check(
    google.trackerCount === 0 && pending.trackerCount === 0,
    'simulated Google sensitive routes load no trackers',
  )
}

async function emailScenario() {
  const a = await tab()
  const b = await tab()
  phase = 'email registration A'
  await register(a, emailA, usernameA, true)
  phase = 'email registration B'
  await register(b, emailB, usernameB)

  phase = 'SSR and CSRF'
  const cookieA = await cookie(a),
    cookieB = await cookie(b)
  const pages = await Promise.all(
    [cookieA, cookieB].map(async (item) => {
      const response = await fetch(`${origin}/compte`, {
        headers: { Cookie: `${item.name}=${item.value}` },
        signal: AbortSignal.timeout(10000),
      })
      const html = await response.text()
      return {
        html,
        cache: response.headers.get('cache-control'),
        vary: response.headers.get('vary'),
        referrer: response.headers.get('referrer-policy'),
      }
    }),
  )
  check(
    pages[0].html.includes(usernameA) &&
      !pages[0].html.includes(usernameB) &&
      pages[1].html.includes(usernameB) &&
      !pages[1].html.includes(usernameA),
    'concurrent two-user SSR HTML and payload isolation',
  )
  check(
    pages.every(
      (item) =>
        item.cache?.includes('no-store') &&
        item.vary?.toLowerCase().includes('cookie') &&
        item.referrer === 'no-referrer',
    ),
    'sensitive SSR privacy headers',
  )
  check(
    (await request(a, '/auth/logout', {}, 'POST', false)).status === 403,
    'browser mutation without CSRF rejected through Nitro',
  )
  for (const badOrigin of [undefined, 'null', 'http://127.0.0.1:13010']) {
    const headers = {
      'Content-Type': 'application/json',
      'X-Messeances-CSRF': '1',
    }
    if (badOrigin) headers.Origin = badOrigin
    const response = await fetch(`${origin}/api/v1/auth/logout`, {
      method: 'POST',
      headers,
      body: '{}',
      signal: AbortSignal.timeout(10000),
    })
    check(
      response.status === 403,
      'missing/null/foreign Origin rejected through Nitro',
    )
  }
  check(
    a.requests
      .filter((item) => item.method === 'POST' && item.csrf)
      .every((item) => item.origin === origin),
    'real browser writes carry exact Origin and CSRF',
  )
  check(
    !(await evaluate(a, `document.cookie.includes('messeances_session_dev')`)),
    'session cookie inaccessible to document scripts',
  )

  phase = 'ordinary logout and original registration password'
  await click(a, 'Se déconnecter')
  await until(
    a,
    `location.pathname === '/connexion' && !!document.querySelector('input#account-email')`,
    'Ordinary logout',
  )
  check(!(await cookie(a)), 'ordinary logout clears cookie')
  await noExplore(a)
  await fill(a, 'account-email', emailA)
  await fill(a, 'account-password', replacement)
  await click(a, 'Se connecter')
  await until(
    a,
    `!!document.getElementById('account-credentials-error')`,
    'Unchosen password rejected',
  )
  check(
    (await session(a)).state === 'anonymous',
    'unchosen replacement password cannot authenticate',
  )
  await login(a, emailA, password)
  check(
    (await session(a)).state === 'complete',
    'original ten-character Unicode registration password authenticates after verification',
  )

  phase = 'password settings'
  await fill(a, 'current-password', password)
  await rejectShortPassword(a, 'new-password', 'Modifier mon mot de passe')
  await fill(a, 'new-password', recovered)
  await click(a, 'Modifier mon mot de passe')
  await text(a, 'Votre mot de passe a été modifié.')
  check(
    (await cookie(a)).value !== cookieA.value,
    'password proof/change rotates cookie',
  )
  const sibling = await tab(a.browserContextId)
  await go(sibling, '/compte')
  await text(sibling, usernameA)
  await click(a, 'Se déconnecter de tous les appareils')
  await text(a, 'Toutes vos sessions ont été fermées')
  await until(
    sibling,
    `!document.getElementById('new-password')`,
    'BroadcastChannel logout',
  )
  check(!(await cookie(a)), 'logout-all clears actual browser cookie')
  check(
    !(await evaluate(
      sibling,
      `document.body.innerText.includes(${JSON.stringify(emailA)})`,
    )),
    'BroadcastChannel removes private identity in sibling tab',
  )
  await login(a, emailA, recovered)

  phase = 'email settings'
  await fill(a, 'new-email', newEmail)
  await fill(a, 'email-password', recovered)
  await click(a, 'Recevoir le lien de confirmation')
  await text(a, 'La demande de changement d’email a été acceptée')
  const change = await mail(newEmail, 'email_change')
  await go(a, change.link)
  await until(
    a,
    `!!document.getElementById('confirm-email-password')`,
    'Email confirmation form',
  )
  check(
    (await session(a)).account.email === emailA,
    'email confirmation GET leaves old email active',
  )
  await fill(a, 'confirm-email-password', recovered)
  await click(a, 'Confirmer mon nouvel email')
  await text(a, 'Votre email a été modifié.')
  check(!(await cookie(a)), 'email confirmation clears session cookie')
  await login(a, newEmail, recovered)

  phase = 'recovery'
  await go(a, '/mot-de-passe-oublie')
  await fill(a, 'reset-email', newEmail)
  await click(a, 'Recevoir un lien')
  await text(a, 'Si cette adresse correspond à un compte avec mot de passe')
  const reset = await mail(newEmail, 'password_reset')
  await go(a, reset.link)
  await until(a, `!!document.getElementById('reset-password')`, 'Reset form')
  await noExplore(a)
  check(
    (await session(a)).state === 'complete',
    'reset GET leaves session unchanged',
  )
  await rejectShortPassword(a, 'reset-password', 'Modifier mon mot de passe')
  await fill(a, 'reset-password', replacement)
  await click(a, 'Modifier mon mot de passe')
  await text(a, 'Toutes vos sessions ont été fermées.')
  check(!(await cookie(a)), 'reset clears browser session and requires login')
  await login(a, newEmail, replacement)

  phase = 'privacy and mobile'
  check(
    a.trackerCount === 0 && b.trackerCount === 0,
    'direct sensitive documents load no tracker',
  )
  await cdp.send(
    'Emulation.setDeviceMetricsOverride',
    { width: 390, height: 844, deviceScaleFactor: 1, mobile: true },
    a.sessionId,
  )
  check(
    await evaluate(a, `document.documentElement.scrollWidth <= 390`),
    '390px account layout has no horizontal overflow',
  )
  check(
    await evaluate(
      a,
      `Array.from(document.querySelectorAll('main button,main input')).filter(el => el.getClientRects().length).every(el => {const r=el.getBoundingClientRect(); return r.height >= 44 && r.width >= 44})`,
    ),
    'account form mobile targets at least 44px',
  )
  await cdp.send('Page.bringToFront', {}, a.sessionId)
  await until(
    a,
    `!!document.getElementById('current-password')`,
    'Focused account ready',
  )
  await evaluate(a, `document.getElementById('current-password').focus()`)
  await cdp.send(
    'Input.dispatchKeyEvent',
    { type: 'keyDown', key: 'Tab', code: 'Tab', windowsVirtualKeyCode: 9 },
    a.sessionId,
  )
  await cdp.send(
    'Input.dispatchKeyEvent',
    { type: 'keyUp', key: 'Tab', code: 'Tab', windowsVirtualKeyCode: 9 },
    a.sessionId,
  )
  check(
    await evaluate(
      a,
      `document.activeElement?.getAttribute('aria-controls') === 'current-password' && (getComputedStyle(document.activeElement).outlineStyle !== 'none' || getComputedStyle(document.activeElement).boxShadow !== 'none')`,
    ),
    'keyboard Tab reaches password visibility toggle with focus indicator',
  )
  await go(a, '/confidentialite')
  await until(
    a,
    `window.__syntheticTracker === true`,
    'synthetic public tracker loaded',
  )
  await evaluate(a, `window.__publicDocumentMarker = true`)
  await click(a, 'Mon compte')
  await until(
    a,
    `location.pathname === '/compte' && !!document.getElementById('new-password')`,
    'Public to sensitive entry',
  )
  check(
    await evaluate(
      a,
      `!window.__publicDocumentMarker && !window.__syntheticTracker`,
    ),
    'public-to-account uses clean full document without surviving tracker',
  )
  await cdp.send(
    'Network.emulateNetworkConditions',
    { offline: true, latency: 0, downloadThroughput: 0, uploadThroughput: 0 },
    a.sessionId,
  )
  await until(
    a,
    `!document.getElementById('new-password')`,
    'Offline private UI cleared',
  )
  check(
    !(await evaluate(
      a,
      `document.body.innerText.includes(${JSON.stringify(newEmail)})`,
    )),
    'offline transition hides private identity',
  )
  await cdp.send(
    'Network.emulateNetworkConditions',
    {
      offline: false,
      latency: 0,
      downloadThroughput: -1,
      uploadThroughput: -1,
    },
    a.sessionId,
  )
  await until(
    a,
    `!!document.getElementById('new-password')`,
    'Online revalidation',
  )
  check(
    await evaluate(
      a,
      `(async () => { for (const name of await caches.keys()) { for (const request of await (await caches.open(name)).keys()) { if (/\\/(compte|connexion|verification|api\\/v1\\/(auth|account))(\\/|$)/.test(new URL(request.url).pathname)) return false; } } return true; })()`,
    ),
    'browser CacheStorage contains no private routes or account API responses',
  )

  phase = 'focus and back navigation'
  await cdp.send('Page.bringToFront', {}, b.sessionId)
  // Revoke via actual endpoint without application BroadcastChannel notification.
  check(
    (await request(a, '/auth/logout', {})).status === 204,
    'out-of-band session revoked',
  )
  await cdp.send('Page.bringToFront', {}, a.sessionId)
  // Headless contexts do not reliably receive native desktop focus events.
  await evaluate(a, `window.dispatchEvent(new Event('focus'))`)
  await until(
    a,
    `!document.getElementById('new-password')`,
    'Focus revalidation removes stale session',
  )
  check(
    !(await evaluate(
      a,
      `document.body.innerText.includes(${JSON.stringify(newEmail)})`,
    )),
    'browser focus-event handler hides identity after out-of-band logout (synthetic focus)',
  )
  await login(a, newEmail, replacement)
  await go(a, '/confidentialite')
  check(
    (await request(a, '/auth/logout', {})).status === 204,
    'session revoked before history restore',
  )
  await evaluate(a, 'history.back()')
  await until(
    a,
    `location.pathname === '/compte' || location.pathname === '/connexion'`,
    'History restoration',
  )
  await until(
    a,
    `!document.getElementById('new-password')`,
    'History restore hides revoked account',
  )
  check(
    !(await evaluate(
      a,
      `document.body.innerText.includes(${JSON.stringify(newEmail)})`,
    )),
    'back navigation cannot restore revoked private identity',
  )
  await login(a, newEmail, replacement)

  phase = 'deletion'
  await go(sibling, '/compte')
  await fill(a, 'deletion-password', replacement)
  await fill(a, 'deletion-confirmation', 'SUPPRIMER')
  await click(a, 'Supprimer définitivement mon compte')
  await text(a, 'Votre compte a été supprimé définitivement')
  await until(
    sibling,
    `!document.getElementById('new-password')`,
    'BroadcastChannel deletion',
  )
  check(!(await cookie(a)), 'deletion clears browser cookie')
  check(
    !(await evaluate(
      sibling,
      `document.body.innerText.includes(${JSON.stringify(newEmail)})`,
    )),
    'BroadcastChannel deletion removes sibling identity',
  )
  check(
    (await session(b)).account.username === usernameB,
    'other browser context remains independent after deletion',
  )
}

async function main() {
  // Run each scenario against a freshly started backend fixture. Real rate limits
  // deliberately remain enabled; neither driver nor fixture bypasses them.
  const google = process.argv.slice(2).includes('--google')
  if (process.argv.slice(2).some((arg) => arg !== '--google'))
    throw new HarnessError('Unknown scenario argument')
  check(
    (await fetch(`${api}/healthz`, { signal: AbortSignal.timeout(10000) })).ok,
    'local backend fixture ready',
  )
  const mailboxProbe = await fetch(
    `${api}/api/__browser/mailbox?email=probe@example.test`,
    {
      headers: { 'X-Browser-Harness': '1' },
      signal: AbortSignal.timeout(10000),
    },
  )
  check(
    mailboxProbe.ok &&
      mailboxProbe.headers.get('cache-control')?.includes('no-store') &&
      Array.isArray((await mailboxProbe.json()).messages),
    'test-only synthetic mailbox boundary ready',
  )
  check(
    (
      await fetch(`${origin}/api/v1/auth/session`, {
        signal: AbortSignal.timeout(10000),
      })
    ).ok,
    'Nitro same-origin proxy ready',
  )
  await launch()
  if (google) {
    phase =
      'simulated Google continuation (not real provider/button acceptance)'
    await simulatedGoogle()
  } else await emailScenario()
  check(
    !interceptionFailure && allPages.every((page) => !page.external),
    'no unexpected external page requests; tracker synthetic only',
  )
  console.log(
    `ACCOUNT_BROWSER_PASS scenario=${google ? 'simulated-google' : 'email'} assertions=${passed}`,
  )
  console.log(
    'OUTSTANDING real Google button/provider, Google linking/email/deletion continuations, SES delivery, production HTTPS cookie, expiry clocks, real OS focus/BFCache/PWA install, password-manager and manual screen-reader acceptance',
  )
}

async function cleanup() {
  cdp?.close()
  await terminateProcessGroup(chrome)
  if (profile) await rm(profile, { recursive: true, force: true })
}
for (const signal of ['SIGHUP', 'SIGINT', 'SIGTERM'])
  process.once(signal, () => {
    void cleanup().finally(() => process.exit(1))
  })
try {
  await main()
} catch (error) {
  // Never serialize CDP/network errors, DOM, URLs, request bodies or assertions containing values.
  const safe = new Set([
    'Browser evaluation failed',
    'Page readiness',
    'Vue hydration',
    'Input ready',
    'Action ready',
    'Expected UI state',
    'Synthetic mail missing',
    'Mailbox fixture unavailable',
  ])
  console.error(
    `ACCOUNT_BROWSER_FAIL phase=${phase}${error instanceof HarnessError || error instanceof CaptureError || safe.has(error.message) ? ` check=${error.message}` : ''}`,
  )
  console.error(
    'Recent account response statuses:',
    JSON.stringify(responses.slice(-6)),
  )
  process.exitCode = 1
} finally {
  await cleanup()
}
