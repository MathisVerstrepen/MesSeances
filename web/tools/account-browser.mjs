// Requires fresh accounts_browser_harness_test.go + test:accounts:server.
// --overview --visual needs only test:accounts:server; owns a read-only mock API.
// Installed Chrome and Node WebSocket only. Optional --visual saves synthetic,
// token-free screenshots under /tmp/opencode. No traces, mail or secrets saved.
import { spawn } from 'node:child_process'
import { createServer } from 'node:http'
import { mkdtemp, rm, writeFile } from 'node:fs/promises'
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
let overviewServer, overviewDetails
let passed = 0
let interceptionFailure = false
const responses = []
const allPages = []
const visual = process.argv.includes('--visual')
const captured = new Set()
class HarnessError extends Error {}
class ExpiredInterception extends Error {}

function check(value, label) {
  if (!value) throw new HarnessError(label)
  passed++
  console.log(`PASS ${label}`)
}

class BrowserCDP extends CDP {
  observers = new Map()
  onMessage(data) {
    const event = JSON.parse(data)
    // Navigation can cancel a paused request before Chrome processes continue.
    // Keep this distinct from other protocol errors; the handler below still
    // requires a failed request or replaced document before accepting it.
    if (
      event.error?.code === -32602 &&
      /^Invalid InterceptionId\.?$/u.test(event.error.message)
    ) {
      const pending = this.pending.get(event.id)
      if (pending) {
        this.pending.delete(event.id)
        clearTimeout(pending.timer)
        pending.reject(new ExpiredInterception())
      }
      return
    }
    super.onMessage(data)
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
  const failedRequests = new Set()
  const requestDocuments = new Map()
  const frameDocuments = new Map()
  cdp.on('Page.frameNavigated', sessionId, ({ frame }) => {
    frameDocuments.set(frame.id, frame.loaderId)
  })
  cdp.on('Network.loadingFailed', sessionId, ({ requestId }) => {
    failedRequests.add(requestId)
  })
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
  cdp.on(
    'Network.requestWillBeSent',
    sessionId,
    ({ requestId, frameId, loaderId, redirectResponse }) => {
      if (loaderId) requestDocuments.set(requestId, { frameId, loaderId })
      if (!redirectResponse) return
      const url = new URL(redirectResponse.url)
      if (
        url.origin === origin &&
        /^\/api\/v1\/(auth|account)(\/|$)/u.test(url.pathname)
      )
        responses.push({ path: url.pathname, status: redirectResponse.status })
    },
  )
  await cdp.send('Fetch.enable', { patterns: [{ urlPattern: '*' }] }, sessionId)
  cdp.on(
    'Fetch.requestPaused',
    sessionId,
    async ({ requestId, networkId, request }) => {
      try {
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
          if (page.fault?.path === url.pathname) {
            const fault = page.fault
            page.fault = null
            if (fault.delay) await delay(fault.delay)
            await cdp.send(
              'Fetch.fulfillRequest',
              {
                requestId,
                responseCode: fault.status,
                responseHeaders: [
                  { name: 'Content-Type', value: 'application/json' },
                  { name: 'Cache-Control', value: 'no-store' },
                ],
                body: Buffer.from(
                  JSON.stringify({ error: { code: 'unavailable' } }),
                ).toString('base64'),
              },
              sessionId,
            )
          } else
            await cdp.send('Fetch.continueRequest', { requestId }, sessionId)
        } else {
          page.external++
          await cdp.send(
            'Fetch.failRequest',
            { requestId, errorReason: 'BlockedByClient' },
            sessionId,
          )
        }
      } catch (error) {
        if (!(error instanceof ExpiredInterception) || !networkId) throw error
        // Events and command replies may arrive in either order. Never retry a
        // request, and never ignore an unverified interception/protocol failure.
        const cancelled = () => {
          const document = requestDocuments.get(networkId)
          const current = document && frameDocuments.get(document.frameId)
          return (
            failedRequests.has(networkId) ||
            (current && current !== document.loaderId)
          )
        }
        if (!cancelled()) await delay(100)
        if (!cancelled()) throw error
      }
    },
  )
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
async function openEditor(page, name) {
  await until(
    page,
    `!!document.getElementById('trigger-${name}') && !document.getElementById('trigger-${name}').disabled`,
    'Disclosure ready',
  )
  await evaluate(page, `document.getElementById('trigger-${name}').click()`)
  await until(
    page,
    `document.getElementById('trigger-${name}').getAttribute('aria-expanded') === 'true' && !!document.getElementById('editor-${name}')`,
    'Disclosure opened',
  )
}

async function inspectOverview(page) {
  check(
    await evaluate(
      page,
      `!document.querySelector('main input') && [...document.querySelectorAll('main h2')].map(el => el.textContent.trim()).join('|') === 'Identité|Connexion|Sessions|Suppression' && [...document.querySelectorAll('main button')].filter(el => el.textContent.trim() === 'Se déconnecter').length === 1`,
    ),
    'overview groups four sections, no hidden required fields or duplicate logout',
  )
  await openEditor(page, 'password')
  check(
    await evaluate(page, `document.activeElement.id === 'current-password'`),
    'opening focuses first password field',
  )
  await fill(page, 'current-password', password)
  await fill(page, 'new-password', replacement)
  await evaluate(page, `window.dispatchEvent(new Event('focus'))`)
  await until(
    page,
    `document.getElementById('current-password')?.value === ${JSON.stringify(password)}`,
    'Same-session focus preserves password draft',
  )
  check(
    await evaluate(
      page,
      `document.getElementById('new-password').value === ${JSON.stringify(replacement)}`,
    ),
    'same-session transient revalidation preserves typed values',
  )
  await openEditor(page, 'email')
  check(
    await evaluate(
      page,
      `!document.getElementById('editor-password') && document.activeElement.id === 'new-email'`,
    ),
    'switch closes previous form and focuses new field',
  )
  await fill(page, 'new-email', 'draft@example.test')
  await click(page, 'Annuler')
  check(
    await evaluate(
      page,
      `!document.getElementById('editor-email') && document.activeElement.id === 'trigger-email'`,
    ),
    'cancel closes editor and restores trigger focus',
  )
  await openEditor(page, 'password')
  check(
    await evaluate(
      page,
      `!document.getElementById('current-password').value && !document.getElementById('new-password').value`,
    ),
    'switch clears previous password values',
  )
  await fill(page, 'current-password', password)
  await rejectShortPassword(page, 'new-password', 'Enregistrer le mot de passe')
  await click(page, 'Annuler')
  await openEditor(page, 'password')
  check(
    await evaluate(
      page,
      `!document.querySelector('#editor-password [role="alert"]') && !document.getElementById('new-password').value`,
    ),
    'cancel clears stale errors and secrets',
  )
  await inspectStyle(page, 'compte-password-open')
  await fill(page, 'current-password', password)
  await fill(page, 'new-password', replacement)
  page.fault = {
    path: '/api/v1/account/reauth/password',
    status: 401,
    delay: 1000,
  }
  await click(page, 'Enregistrer le mot de passe')
  check(
    await evaluate(
      page,
      `[...document.querySelectorAll('main button.account-link,main button.account-primary,main button.account-secondary,main button.account-danger,main input')].every(el => el.disabled)`,
    ),
    'in-flight proof disables cancel, switches and competing actions',
  )
  await until(
    page,
    `!!document.querySelector('#editor-password [role="alert"]')`,
    'Scoped proof error',
  )
  check(
    await evaluate(
      page,
      `document.getElementById('new-password').value === ${JSON.stringify(replacement)} && !document.getElementById('trigger-password').disabled`,
    ),
    'failed proof preserves draft and restores controls',
  )
  await inspectStyle(page, 'compte-password-error')
  await click(page, 'Annuler')
  await openEditor(page, 'delete')
  check(
    await evaluate(
      page,
      `document.getElementById('editor-delete').textContent.includes('immédiate et définitive') && !!document.getElementById('deletion-confirmation')`,
    ),
    'deletion disclosure includes full warning and typed confirmation',
  )
  await fill(page, 'deletion-password', password)
  await fill(page, 'deletion-confirmation', 'SUPPRIMER')
  await click(page, 'Annuler')
  await openEditor(page, 'delete')
  check(
    await evaluate(
      page,
      `!document.getElementById('deletion-password').value && !document.getElementById('deletion-confirmation').value`,
    ),
    'cancelling deletion clears both confirmation and password',
  )
  await inspectStyle(page, 'compte-delete-open')
  await fill(page, 'deletion-password', password)
  await fill(page, 'deletion-confirmation', 'SUPPRIMER')
  await evaluate(
    page,
    `window.dispatchEvent(new Event('pagehide')); window.dispatchEvent(new PageTransitionEvent('pageshow', { persisted: true }))`,
  )
  await until(
    page,
    `!!document.getElementById('trigger-delete')`,
    'Pagehide revalidation',
  )
  if (await evaluate(page, `!document.getElementById('editor-delete')`))
    await openEditor(page, 'delete')
  check(
    await evaluate(
      page,
      `!document.getElementById('deletion-password').value && !document.getElementById('deletion-confirmation').value`,
    ),
    'pagehide clears sensitive drafts before history revalidation',
  )
  await click(page, 'Annuler')
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
  // innerText reflects the public controls' CSS uppercase transformation.
  await until(
    page,
    `document.body.innerText.toLowerCase().includes(${JSON.stringify(fragment.toLowerCase())})`,
    'Expected UI state',
  )
}

async function inspectStyle(page, name) {
  if (!visual || captured.has(name)) return
  captured.add(name)
  await cdp.send('Page.bringToFront', {}, page.sessionId)
  await evaluate(page, 'document.fonts.ready.then(() => true)')
  check(
    await evaluate(
      page,
      `location.hash === '' && location.search === '' && !document.getElementById('original-email-link')?.value && [...document.querySelectorAll('input[autocomplete$="password"]')].every(el => el.type === 'password')`,
    ),
    `${name}: screenshot has no URL token or exposed secret field`,
  )
  for (const width of [1440, 2560, 390, 320]) {
    await cdp.send(
      'Emulation.setDeviceMetricsOverride',
      { width, height: 900, deviceScaleFactor: 1, mobile: width < 600 },
      page.sessionId,
    )
    await evaluate(page, 'window.scrollTo(0, 0)')
    await delay(100)
    check(
      await evaluate(page, `document.documentElement.scrollWidth <= ${width}`),
      `${name}: ${width}px has no horizontal overflow`,
    )
    check(
      await evaluate(
        page,
        `Array.from(document.querySelectorAll('main button,main input:not([type="radio"]),main a')).filter(el => el.getClientRects().length).every(el => { const r = el.getBoundingClientRect(); return r.height >= 44 && r.width >= 44; })`,
      ),
      `${name}: ${width}px controls keep 44px targets`,
    )
    if (
      await evaluate(page, `!!document.querySelector('.account-shell-area')`)
    ) {
      check(
        await evaluate(
          page,
          `(() => {
            const main = document.querySelector('main'), nav = main.querySelector('.account-area-navigation'), content = main.querySelector('.account-shell-content'), inner = main.querySelector('.account-area-inner');
            const m = main.getBoundingClientRect(), n = nav.getBoundingClientRect(), c = content.getBoundingClientRect(), i = inner.getBoundingClientRect();
            const header = document.querySelector('header').getBoundingClientRect(), footer = document.querySelector('footer').getBoundingClientRect();
            return m.left === 0 && m.width === document.documentElement.clientWidth && m.height >= innerHeight && m.top >= header.bottom - 1 && footer.top >= m.bottom - 1 && i.width <= 960 && Math.abs((i.left - c.left) - (c.right - i.right)) <= 1 && getComputedStyle(content).boxShadow === 'none' && getComputedStyle(content).borderTopWidth === '0px' && [main, nav, content, inner].every(el => !['auto', 'scroll', 'hidden'].includes(getComputedStyle(el).overflowY)) && (${width} >= 1024 ? n.width === 240 && c.left === n.right && c.right === m.right && n.top === c.top && i.left >= c.left + 48 && Math.abs(nav.querySelector('a').getBoundingClientRect().top - inner.querySelector('h1').getBoundingClientRect().top) <= 1 : n.width === m.width && c.top >= n.bottom && c.width === m.width);
          })()`,
        ),
        `${name}: ${width}px full-width area, responsive 240px sidebar, readable measure, header/footer and document scrolling`,
      )
      check(
        await evaluate(
          page,
          `(() => {
            const nav = document.querySelector('.account-area-navigation'), current = nav.querySelector('a'), future = [...nav.querySelectorAll('button')];
            return nav.getAttribute('aria-label') === 'Espace personnel' && nav.querySelectorAll('a').length === 1 && current.getAttribute('href') === '/compte' && current.getAttribute('aria-current') === 'page' && current.textContent.trim() === 'Paramètres' && getComputedStyle(current).textDecorationLine === 'none' && future.length === 3 && future.every((el, index) => el.disabled && !el.hasAttribute('href') && el.innerText.includes(['Films aimés', 'Watchlist', 'Amis'][index]) && el.innerText.includes('À venir')) && document.querySelectorAll('main h1').length === 1 && document.querySelector('main h1').textContent.trim() === 'Paramètres';
          })()`,
        ),
        `${name}: ${width}px current-page semantics and disabled future entries without routes`,
      )
      await inspectWorkspaceScroll(page, `${name}: ${width}px`)
    } else {
      check(
        await evaluate(
          page,
          `(() => { const content = document.querySelector('.account-shell-content'), css = getComputedStyle(content), r = content.getBoundingClientRect(); return !document.querySelector('.account-area-navigation') && css.borderTopWidth === '2px' && css.boxShadow !== 'none' && r.width <= 896 && Math.abs(r.left - (document.documentElement.clientWidth - r.right)) <= 1 && !!document.querySelector('header') && !!document.querySelector('footer'); })()`,
        ),
        `${name}: ${width}px auth page retains centered boxed layout and public chrome`,
      )
    }
    if (
      await evaluate(
        page,
        `!!document.querySelector('.account-overview-sections')?.getClientRects().length`,
      )
    ) {
      check(
        await evaluate(
          page,
          `(() => {
          const textBottom = element => {
            const text = [...element.childNodes].find(node => node.nodeType === Node.TEXT_NODE && node.textContent.trim());
            const range = document.createRange(); range.selectNodeContents(text);
            return range.getBoundingClientRect().bottom;
          };
          return [...document.querySelectorAll('.overview-row')].every(row => {
            const label = row.querySelector('.overview-label'), action = row.querySelector('button');
            if (!action) return true;
            const a = action.getBoundingClientRect(), value = row.querySelector('.overview-row-value').getBoundingClientRect();
            return Math.abs(textBottom(label) - textBottom(action)) <= 1 && value.top >= a.bottom - 1;
          });
        })()`,
        ),
        `${name}: ${width}px summary text baselines align and values sit below actions`,
      )
      check(
        await evaluate(
          page,
          `(() => {
          const panel = document.querySelector('.account-area-inner').getBoundingClientRect();
          const icon = document.querySelector('#account-google svg');
          return (${width} < 1440 || panel.width === 960) && icon.getBoundingClientRect().width === 20 && icon.getAttribute('aria-hidden') === 'true' && [...document.querySelectorAll('.overview-label,.overview-link,.overview-secondary')].every(el => {
            const css = getComputedStyle(el); return !css.fontFamily.includes('monospace') && css.fontWeight === '600' && css.textTransform === 'none';
          });
        })()`,
        ),
        `${name}: ${width}px panel proportions, sentence-case sans summaries and decorative Google mark`,
      )
      check(
        await evaluate(
          page,
          `(() => {
          const [local, global] = document.querySelectorAll('.overview-secondary');
          const l = local.getBoundingClientRect(), g = global.getBoundingClientRect();
          return (${width} < 1440 || Math.abs(l.top - g.top) <= 1) && !local.hasAttribute('aria-describedby') && global.getAttribute('aria-describedby') === 'logout-all-consequence' && [...document.querySelectorAll('[id^="editor-"]')].every(el => el.getBoundingClientRect().width <= 512) && [...document.querySelectorAll('.account-input')].every(el => ${width} < 1440 || el.getBoundingClientRect().width === 512);
        })()`,
        ),
        `${name}: ${width}px matching logout rows, scoped consequence and comfortable desktop inputs`,
      )
      check(
        await evaluate(
          page,
          `(() => { const identity = document.querySelector('[aria-labelledby="account-identity"] dl'), [username, email] = identity.children, deletion = document.getElementById('account-delete'); return email.getBoundingClientRect().top - username.getBoundingClientRect().bottom === 8 && deletion.textContent === 'Suppression' && getComputedStyle(deletion).color === 'rgb(39, 39, 42)' && document.querySelector('header a[href="/compte"]').textContent.trim() === 'Mon compte'; })()`,
        ),
        `${name}: ${width}px compact identity, neutral Suppression and unchanged public Mon compte`,
      )
    }
    check(
      await evaluate(
        page,
        `Array.from(document.querySelectorAll('main .account-input')).every(el => { const css = getComputedStyle(el); return css.borderRadius === '0px' && css.borderTopWidth === '2px'; }) && Array.from(document.querySelectorAll('main .account-primary')).every(el => getComputedStyle(el).backgroundColor === 'rgb(39, 39, 42)')`,
      ),
      `${name}: ${width}px public square controls and dark CTAs`,
    )
    check(
      await evaluate(
        page,
        `Array.from(document.querySelectorAll('.account-password-input')).filter(el => el.getClientRects().length).every(input => { const toggle = input.parentElement.querySelector('button'); const i = input.getBoundingClientRect(), t = toggle.getBoundingClientRect(); return t.left >= i.left && t.right <= i.right && t.top >= i.top && t.bottom <= i.bottom && parseFloat(getComputedStyle(input).paddingRight) >= i.right - t.left && toggle.getAttribute('aria-controls') === input.id && toggle.getAttribute('aria-label') === 'Afficher le mot de passe' && toggle.getAttribute('aria-pressed') === 'false' && !!toggle.querySelector('svg[aria-hidden="true"]'); })`,
      ),
      `${name}: ${width}px masked icon toggles inset without covering input text`,
    )
    if (name === 'connexion' || name === 'inscription') {
      check(
        await evaluate(
          page,
          `(() => { const field = document.getElementById('account-email'); const r = field.getBoundingClientRect(); const link = document.querySelector('main a[href="${name === 'connexion' ? '/inscription' : '/connexion'}"]'); const css = getComputedStyle(link); return (${width} !== 1440 || (r.width >= 440 && r.width <= 480)) && css.fontWeight === '400' && !css.fontFamily.includes('monospace') && document.querySelectorAll('main a[href="${name === 'connexion' ? '/inscription' : '/connexion'}"]').length === 1; })()`,
        ),
        `${name}: ${width}px compact form and single regular navigation link`,
      )
      check(
        await evaluate(
          page,
          name === 'inscription'
            ? `!document.querySelector('main a[href="/mot-de-passe-oublie"]')`
            : `(() => {
                const row = document.querySelector('.account-password-label-row');
                const label = row.querySelector('label'), link = row.querySelector('a');
                const baseline = element => {
                  const text = document.createElement('span');
                  const marker = document.createElement('span');
                  marker.style.cssText = 'display:inline-block;width:0;height:0;vertical-align:baseline';
                  text.append(...element.childNodes);
                  text.append(marker);
                  element.append(text);
                  const y = marker.getBoundingClientRect().top;
                  marker.remove();
                  text.replaceWith(...text.childNodes);
                  return y;
                };
                const delta = Math.abs(baseline(label) - baseline(link));
                const l = label.getBoundingClientRect(), a = link.getBoundingClientRect();
                return getComputedStyle(row).flexWrap === 'wrap' && a.height >= 44 && a.bottom <= document.getElementById('account-password').getBoundingClientRect().top && (${width} !== 320 || a.top >= l.bottom) && (${width} === 320 || delta < 1);
              })()`,
        ),
        `${name}: ${width}px login-only recovery baseline alignment and narrow fallback`,
      )
    }
    if (name === 'inscription-sent') {
      check(
        await evaluate(
          page,
          `(() => { const a = document.querySelector('main a[href="/verification"]'); const css = getComputedStyle(a.parentElement); return css.display === 'flex' && css.flexWrap === 'wrap' && css.gap === '16px'; })()`,
        ),
        `registration sent: ${width}px retains wrapping 16px action gap`,
      )
    }
    if (name === 'compte' && width === 1440) {
      check(
        await evaluate(
          page,
          `document.querySelector('.account-shell-content').getBoundingClientRect().width === document.documentElement.clientWidth - 240`,
        ),
        'settings use remaining full desktop width',
      )
    }
    await inspectPasswordKeyboard(page, `${name}: ${width}px`)
    const { cssContentSize } = await cdp.send(
      'Page.getLayoutMetrics',
      {},
      page.sessionId,
    )
    const { data } = await cdp.send(
      'Page.captureScreenshot',
      {
        format: 'png',
        captureBeyondViewport: true,
        clip: { x: 0, y: 0, width, height: cssContentSize.height, scale: 1 },
      },
      page.sessionId,
    )
    const path = `/tmp/opencode/account-style-${name}-${width}.png`
    await writeFile(path, Buffer.from(data, 'base64'))
    console.log(`SCREENSHOT ${path}`)
  }
  await cdp.send(
    'Emulation.setDeviceMetricsOverride',
    { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false },
    page.sessionId,
  )
}

async function inspectWorkspaceScroll(page, label) {
  const originalTop = await evaluate(
    page,
    `document.querySelector('.account-area-navigation a').getBoundingClientRect().top`,
  )
  await evaluate(page, 'window.scrollTo(0, 300)')
  await delay(50)
  check(
    await evaluate(
      page,
      `(() => { const header = document.querySelector('header'), nav = document.querySelector('.account-area-navigation a'); return scrollY > 0 && getComputedStyle(header).position === 'sticky' && header.getBoundingClientRect().top === 0 && Math.abs(nav.getBoundingClientRect().top + scrollY - ${originalTop}) <= 1; })()`,
    ),
    `${label} real scroll keeps public header sticky and category navigation in normal flow`,
  )
  await evaluate(
    page,
    'window.scrollTo(0, document.documentElement.scrollHeight)',
  )
  await delay(50)
  check(
    await evaluate(
      page,
      `(() => { const footer = document.querySelector('footer').getBoundingClientRect(), header = document.querySelector('header').getBoundingClientRect(), main = document.querySelector('main').getBoundingClientRect(); return footer.bottom <= innerHeight + 1 && footer.top >= header.bottom && footer.top >= main.bottom - 1; })()`,
    ),
    `${label} footer reachable without sidebar or content overlap`,
  )
  await evaluate(
    page,
    `document.querySelector('.account-area-navigation a').focus(); window.scrollTo(0, 0)`,
  )
  // Move away and back using real keyboard events, including empty/error states.
  for (const modifiers of [0, 8]) {
    for (const type of ['keyDown', 'keyUp']) {
      await cdp.send(
        'Input.dispatchKeyEvent',
        { type, key: 'Tab', code: 'Tab', windowsVirtualKeyCode: 9, modifiers },
        page.sessionId,
      )
    }
  }
  await evaluate(page, 'window.scrollTo(0, 0)')
  check(
    await evaluate(
      page,
      `(() => { const current = document.querySelector('.account-area-navigation a'), css = getComputedStyle(current), r = current.getBoundingClientRect(), nav = current.closest('nav').getBoundingClientRect(); return document.activeElement === current && current.matches(':focus-visible') && (css.outlineStyle !== 'none' || css.boxShadow !== 'none') && r.top >= document.querySelector('header').getBoundingClientRect().bottom + 6 && r.left >= nav.left + 6 && r.right <= nav.right - 6; })()`,
    ),
    `${label} current category has visible unclipped keyboard focus without underline`,
  )
  await evaluate(page, 'document.activeElement.blur(); window.scrollTo(0, 0)')
}

async function inspectPasswordKeyboard(page, label) {
  const id = await evaluate(
    page,
    `Array.from(document.querySelectorAll('.account-password-input')).find(el => el.getClientRects().length && !el.disabled)?.id`,
  )
  if (!id) return
  const before = page.requests.filter((item) => item.method !== 'GET').length
  await evaluate(page, `document.getElementById(${JSON.stringify(id)}).focus()`)
  async function key(key, code, windowsVirtualKeyCode) {
    for (const type of ['keyDown', 'keyUp']) {
      const event = { type, key, code, windowsVirtualKeyCode }
      if (key === 'Enter' && type === 'keyDown') event.text = '\r'
      await cdp.send('Input.dispatchKeyEvent', event, page.sessionId)
    }
  }
  await key('Tab', 'Tab', 9)
  check(
    await evaluate(
      page,
      `document.activeElement?.getAttribute('aria-controls') === ${JSON.stringify(id)} && getComputedStyle(document.activeElement).outlineStyle !== 'none'`,
    ),
    `${label} keyboard reaches inset toggle with visible focus`,
  )
  await key('Enter', 'Enter', 13)
  check(
    await evaluate(
      page,
      `document.getElementById(${JSON.stringify(id)}).type === 'text' && document.activeElement.getAttribute('aria-pressed') === 'true' && document.activeElement.getAttribute('aria-label') === 'Masquer le mot de passe'`,
    ),
    `${label} Enter reveals password with updated accessible state`,
  )
  await key(' ', 'Space', 32)
  check(
    await evaluate(
      page,
      `document.getElementById(${JSON.stringify(id)}).type === 'password' && document.activeElement.getAttribute('aria-pressed') === 'false' && document.activeElement.getAttribute('aria-label') === 'Afficher le mot de passe'`,
    ),
    `${label} Space remasks password with updated accessible state`,
  )
  check(
    page.requests.filter((item) => item.method !== 'GET').length === before,
    `${label} visibility toggle never submits form`,
  )
  await evaluate(page, `document.activeElement.blur(); window.scrollTo(0, 0)`)
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
    `location.pathname === '/compte' && !!document.getElementById('trigger-password')`,
    'Login complete',
  )
}
async function register(page, email, username, reserved = false) {
  await go(page, '/inscription')
  await noExplore(page)
  await inspectStyle(page, 'inscription')
  await fill(page, 'account-email', email)
  await rejectShortPassword(page, 'account-password', 'Créer mon compte')
  await fill(page, 'account-password', password)
  await click(page, 'Créer mon compte')
  await text(page, 'Si cette adresse peut être utilisée')
  await inspectStyle(page, 'inscription-sent')
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
    await inspectStyle(other, 'verification-browser-error')
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
  await inspectStyle(page, 'verification')
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
  await inspectStyle(page, 'finaliser')
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
    `location.pathname === '/compte' && !!document.getElementById('trigger-password')`,
    'Account completed',
  )
  const completeCookie = await cookie(page)
  await inspectStyle(page, 'compte')
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
  await inspectStyle(google, 'compte-google')
  for (const [editor, action] of [
    ['password', 'Continuer avec Google'],
    ['delete', 'Vérifier mon identité avant suppression'],
    ['email', 'Continuer avec Google'],
  ]) {
    await openEditor(google, editor)
    if (editor === 'email')
      await fill(google, 'new-email', 'google-next@example.test')
    google.fault = { path: '/api/v1/auth/google/start', status: 503 }
    const writesBefore = google.requests.filter(
      (item) => item.method !== 'GET',
    ).length
    await click(google, action)
    if (editor === 'email') {
      // Uncertain email actions deliberately use destructive recovery, not soft focus.
      await text(google, 'La réponse a été interrompue.')
      await until(
        google,
        `!!document.getElementById('trigger-email') && !document.querySelector('.animate-pulse')`,
        'Uncertain email recovery',
      )
      check(
        await evaluate(
          google,
          `document.getElementById('trigger-email').getAttribute('aria-expanded') === 'false' && !document.getElementById('new-email')`,
        ),
        'uncertain Google email action clears its editor during conservative recovery',
      )
      await openEditor(google, 'email')
      check(
        await evaluate(
          google,
          `document.getElementById('new-email').value === ''`,
        ),
        'uncertain Google email recovery never restores its old draft',
      )
      check(
        google.requests.filter((item) => item.method !== 'GET').length ===
          writesBefore + 1,
        'uncertain Google email action is never automatically replayed',
      )
      await click(google, 'Annuler')
      continue
    }
    await text(google, 'Le service de comptes est indisponible')
    check(
      await evaluate(
        google,
        `!!document.querySelector('${editor === 'email' ? 'section[aria-labelledby="account-identity"]' : `#editor-${editor}`} [role="alert"]') && document.getElementById('trigger-${editor}').getAttribute('aria-expanded') === 'true'`,
      ),
      `Google ${editor} proof failure is visible beside owning action`,
    )
    await inspectStyle(google, `compte-google-${editor}-error`)
    await click(google, 'Annuler')
  }
  check(
    !(await evaluate(
      google,
      `!!document.getElementById('trigger-google') || [...document.querySelectorAll('button')].some(el => el.textContent.trim() === 'Dissocier')`,
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
  await inspectStyle(google, 'confirmer-identite')
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
  await text(google, 'Confirmer mon identité avec ce lien')
  await inspectStyle(google, 'confirmer-identite-link')
  check(
    !(await session(google)).account.has_password,
    'email proof link GET does not add password',
  )
  await click(google, 'Confirmer mon identité avec ce lien')
  await rejectShortPassword(google, 'add-password', 'Ajouter mon mot de passe')
  await inspectStyle(google, 'confirmer-identite-password-error')
  await fill(google, 'add-password', replacement)
  check(
    !(await session(google)).account.has_password,
    'confirmed proof still requires explicit final action',
  )
  await click(google, 'Ajouter mon mot de passe')
  await text(google, 'Votre mot de passe a été ajouté.')
  await go(google, '/compte')
  await until(
    google,
    `!!document.getElementById('trigger-google')`,
    'Both methods ready',
  )
  await inspectStyle(google, 'compte-both')
  await openEditor(google, 'google')
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
  await fill(pending, 'account-username', `browser_g_pending_${run}`)
  await click(pending, 'Confirmer mon nom')
  await text(pending, 'Google est votre seul moyen de connexion')
  await googleRedirect(
    pending,
    {
      mode: 'reauth',
      action: 'email_change',
      target: 'google-next@example.test',
    },
    'unverified',
    '/compte/confirmer-identite',
  )
  await click(pending, 'Recevoir le lien de vérification')
  await text(pending, 'La demande a été acceptée.')
  const emailProof = await mail(
    'google-unverified@example.test',
    'email_step_up',
  )
  await fragmentVisit(pending, emailProof.link)
  await click(pending, 'Confirmer mon identité avec ce lien')
  await until(
    pending,
    `!!document.querySelector('input[value="request"]')`,
    'Google email action ready',
  )
  await evaluate(
    pending,
    `document.querySelector('input[value="request"]').click()`,
  )
  await click(pending, 'Demander le lien au nouvel email')
  await text(pending, 'Votre adresse actuelle reste inchangée')
  await go(pending, '/compte')
  await text(pending, 'Confirmation en attente')
  await inspectStyle(pending, 'compte-google-pending')
  await click(pending, 'Annuler le changement d’email')
  await text(pending, 'Le changement d’email a été annulé')
  check(
    await evaluate(
      pending,
      `!document.body.innerText.toLowerCase().includes('confirmation en attente')`,
    ),
    'Google email continuation creates pending request and explicit cancel removes it',
  )
  // Preserve the real mailbox cooldown and step limiter's 90-second refill.
  await delay(91000)
  await openEditor(pending, 'delete')
  check(
    await evaluate(
      pending,
      `document.getElementById('editor-delete').textContent.includes('immédiate et définitive')`,
    ),
    'Google deletion warning precedes provider proof',
  )
  await googleRedirect(
    pending,
    { mode: 'reauth', action: 'delete_account' },
    'unverified',
    '/compte/confirmer-identite',
  )
  await click(pending, 'Recevoir le lien de vérification')
  await text(pending, 'La demande a été acceptée.')
  const deletionProof = await mail(
    'google-unverified@example.test',
    'email_step_up',
  )
  await fragmentVisit(pending, deletionProof.link)
  await click(pending, 'Confirmer mon identité avec ce lien')
  await fill(pending, 'delete-confirmation', 'SUPPRIMER')
  await click(pending, 'Supprimer définitivement mon compte')
  await text(pending, 'Votre compte a été supprimé définitivement')
  check(
    !(await cookie(pending)),
    'Google-only deletion needs both proofs and typed confirmation then clears session',
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
  await inspectStyle(a, 'connexion')
  await click(a, 'Mot de passe oublié ?')
  await until(
    a,
    `location.pathname === '/mot-de-passe-oublie' && !!document.getElementById('reset-email')`,
    'Recovery navigation',
  )
  check(true, 'login field recovery link reaches password recovery')
  await go(a, '/connexion')
  await fill(a, 'account-email', emailA)
  await fill(a, 'account-password', replacement)
  await click(a, 'Se connecter')
  await until(
    a,
    `!!document.getElementById('account-credentials-error')`,
    'Unchosen password rejected',
  )
  await inspectStyle(a, 'connexion-error')
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
  await inspectOverview(a)
  await openEditor(a, 'password')
  await fill(a, 'current-password', password)
  await rejectShortPassword(a, 'new-password', 'Enregistrer le mot de passe')
  await fill(a, 'new-password', recovered)
  await click(a, 'Enregistrer le mot de passe')
  await text(a, 'Votre mot de passe a été modifié.')
  check(
    (await cookie(a)).value !== cookieA.value,
    'password proof/change rotates cookie',
  )
  const sibling = await tab(a.browserContextId)
  await go(sibling, '/compte')
  await text(sibling, usernameA)
  await click(a, 'Déconnecter tous les appareils')
  await text(a, 'Toutes vos sessions ont été fermées')
  await until(
    sibling,
    `!document.getElementById('trigger-password')`,
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
  // Focus assertions represent the active user tab, not a background CDP target.
  await cdp.send('Page.bringToFront', {}, a.sessionId)
  await login(a, emailA, recovered)

  phase = 'email settings'
  await openEditor(a, 'email')
  await fill(a, 'new-email', newEmail)
  await fill(a, 'email-password', recovered)
  await click(a, 'Recevoir le lien de confirmation')
  await text(a, 'La demande de changement d’email a été acceptée')
  await until(
    a,
    `!document.getElementById('editor-email') && document.activeElement.id === 'trigger-email' && document.body.innerText.toLowerCase().includes('confirmation en attente')`,
    'Pending email ready',
  ).catch(async (error) => {
    console.error(
      'Email overview state:',
      await evaluate(
        a,
        `JSON.stringify({ editorOpen: !!document.getElementById('editor-email'), focusedTrigger: document.activeElement.id === 'trigger-email', triggerPresent: !!document.getElementById('trigger-email'), triggerDisabled: document.getElementById('trigger-email')?.disabled, pendingVisible: document.body.innerText.toLowerCase().includes('confirmation en attente') })`,
      ),
    )
    throw error
  })
  await delay(150)
  check(
    await evaluate(
      a,
      `!document.getElementById('editor-email') && document.activeElement.id === 'trigger-email' && document.body.innerText.toLowerCase().includes('confirmation en attente')`,
    ),
    'email success closes editor, restores focus and retains pending status',
  )
  await inspectStyle(a, 'compte-pending-email')
  await openEditor(a, 'email')
  await click(a, 'Annuler')
  check(
    await evaluate(
      a,
      `document.body.textContent.includes('Annuler le changement d’email')`,
    ),
    'closing email editor does not cancel pending change',
  )
  const change = await mail(newEmail, 'email_change')
  await go(a, change.link)
  await until(
    a,
    `!!document.getElementById('confirm-email-password')`,
    'Email confirmation form',
  )
  await inspectStyle(a, 'confirmer-email')
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
  await inspectStyle(a, 'mot-de-passe-oublie')
  await fill(a, 'reset-email', newEmail)
  await click(a, 'Recevoir un lien')
  await text(a, 'Si cette adresse correspond à un compte avec mot de passe')
  await inspectStyle(a, 'mot-de-passe-oublie-sent')
  const reset = await mail(newEmail, 'password_reset')
  await go(a, reset.link)
  await until(a, `!!document.getElementById('reset-password')`, 'Reset form')
  await inspectStyle(a, 'reinitialiser-mot-de-passe')
  await noExplore(a)
  check(
    (await session(a)).state === 'complete',
    'reset GET leaves session unchanged',
  )
  await rejectShortPassword(a, 'reset-password', 'Modifier mon mot de passe')
  await fill(a, 'reset-password', replacement)
  await click(a, 'Modifier mon mot de passe')
  await text(a, 'Toutes vos sessions ont été fermées.')
  await inspectStyle(a, 'reinitialiser-mot-de-passe-done')
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
  await openEditor(a, 'password')
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
    `location.pathname === '/compte' && !!document.getElementById('trigger-password')`,
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
    `!document.getElementById('trigger-password')`,
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
    `!!document.getElementById('trigger-password')`,
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
    `!document.getElementById('trigger-password')`,
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
    `!document.getElementById('trigger-password')`,
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
  await openEditor(a, 'delete')
  await fill(a, 'deletion-password', replacement)
  await fill(a, 'deletion-confirmation', 'SUPPRIMER')
  await click(a, 'Supprimer définitivement mon compte')
  await text(a, 'Votre compte a été supprimé définitivement')
  await until(
    sibling,
    `!document.getElementById('trigger-password')`,
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

async function overviewScenario() {
  // Read-only presentation fixture: real Vue SSR/hydration, no DB or auth writes.
  let sessionMode = 'ready'
  let detailMode = 'ready'
  const heldDetails = new Set()
  const heldSessions = new Set()
  let sessionReads = 0
  let detailReads = 0
  let writes = 0
  const accountView = () =>
    overviewDetails &&
    Object.fromEntries(
      ['email', 'username', 'has_password', 'google_linked'].map((key) => [
        key,
        overviewDetails[key],
      ]),
    )
  overviewServer = createServer((request, response) => {
    response.setHeader('Content-Type', 'application/json')
    response.setHeader('Cache-Control', 'no-store')
    if (request.method !== 'GET') writes++
    if (request.url === '/api/v1/auth/session') sessionReads++
    if (request.url === '/api/v1/account') detailReads++
    if (request.url === '/api/v1/auth/session' && sessionMode === 'loading') {
      heldSessions.add(response)
      response.once('close', () => heldSessions.delete(response))
      return
    }
    if (request.url === '/api/v1/account' && detailMode === 'loading') {
      heldDetails.add(response)
      response.once('close', () => heldDetails.delete(response))
      return
    }
    if (
      (request.url === '/api/v1/account' && detailMode === 'error') ||
      (request.url === '/api/v1/auth/session' && sessionMode === 'error')
    ) {
      response.statusCode = 503
      response.end(JSON.stringify({ error: { code: 'unavailable' } }))
      return
    }
    const data =
      request.method !== 'GET'
        ? undefined
        : request.url === '/api/v1/auth/session'
          ? {
              enabled: sessionMode !== 'disabled',
              state: overviewDetails ? 'complete' : 'anonymous',
              account: accountView(),
            }
          : request.url === '/api/v1/account'
            ? overviewDetails
            : request.url === '/api/v1/theaters'
              ? { theaters: [] }
              : undefined
    response.statusCode = data === undefined ? 404 : 200
    response.end(JSON.stringify(data ?? { error: { code: 'unavailable' } }))
  })
  await new Promise((resolve, reject) => {
    overviewServer.once('error', reject)
    overviewServer.listen(18089, '127.0.0.1', resolve)
  })
  await launch()
  const page = await tab()
  for (const method of ['password', 'google', 'both', 'long']) {
    const long = method === 'long'
    overviewDetails = {
      email: long
        ? `${'a'.repeat(64)}@${'b'.repeat(63)}.example.test`
        : 'owner@example.test',
      username: long ? 'a'.repeat(30) : 'cinema_lover',
      has_password: method !== 'google',
      google_linked: method !== 'password',
      google_email: long
        ? `${'g'.repeat(64)}@${'d'.repeat(63)}.example.test`
        : 'google@example.test',
      pending_email: long
        ? `${'p'.repeat(64)}@${'d'.repeat(63)}.example.test`
        : null,
      allowed_methods:
        method === 'google' ? ['google'] : ['password', 'google'],
    }
    await go(page, '/compte')
    await until(
      page,
      `!!document.getElementById('trigger-password')`,
      'Overview ready',
    )
    check(
      await evaluate(
        page,
        `document.querySelectorAll('main input').length === 0 && ${method === 'google' ? '!' : '!!'}document.getElementById('trigger-google')`,
      ),
      `${method}: collapsed overview and last-method guard`,
    )
    await inspectStyle(page, `overview-${method}`)
    await evaluate(
      page,
      `document.querySelector('.account-area-navigation a').focus()`,
    )
    await cdp.send(
      'Input.dispatchKeyEvent',
      { type: 'keyDown', key: 'Tab', code: 'Tab', windowsVirtualKeyCode: 9 },
      page.sessionId,
    )
    await cdp.send(
      'Input.dispatchKeyEvent',
      { type: 'keyUp', key: 'Tab', code: 'Tab', windowsVirtualKeyCode: 9 },
      page.sessionId,
    )
    check(
      await evaluate(
        page,
        `document.activeElement.id === 'trigger-email' && document.activeElement.matches(':focus-visible') && (getComputedStyle(document.activeElement).outlineStyle !== 'none' || getComputedStyle(document.activeElement).boxShadow !== 'none')`,
      ),
      `${method}: keyboard skips unavailable categories and retains visible focus`,
    )
    for (const editor of ['email', 'password', 'google', 'delete']) {
      if (editor === 'google' && method === 'google') continue
      await openEditor(page, editor)
      check(
        await evaluate(
          page,
          `document.querySelectorAll('[id^="editor-"]').length === 1 && !!document.querySelector('#editor-${editor} :focus')`,
        ),
        `${method}/${editor}: one editor and focused field/action`,
      )
      await inspectStyle(page, `overview-${method}-${editor}`)
      await evaluate(
        page,
        `document.getElementById('trigger-${editor}').click()`,
      )
      await until(
        page,
        `!document.getElementById('editor-${editor}') && document.activeElement.id === 'trigger-${editor}'`,
        'Editor closed with restored focus',
      )
      await openEditor(page, editor)
      await evaluate(
        page,
        `document.querySelectorAll('#editor-${editor} input').forEach(input => { input.value = 'fixture-draft'; input.dispatchEvent(new Event('input', { bubbles: true })); })`,
      )
      await click(page, 'Annuler')
      await until(
        page,
        `!document.getElementById('editor-${editor}') && document.activeElement.id === 'trigger-${editor}'`,
        'Cancel restores trigger focus',
      )
      await openEditor(page, editor)
      check(
        await evaluate(
          page,
          `[...document.querySelectorAll('#editor-${editor} input')].every(input => input.value === '')`,
        ),
        `${method}/${editor}: cancel clears drafts before reopening`,
      )
      await click(page, 'Annuler')
    }
  }
  // Synthetic focus events, not native OS focus. Hold both network phases against
  // actual hydrated Vue; observe DOM identity, caret, handlers and transport.
  overviewDetails = {
    email: 'owner@example.test',
    username: 'cinema_lover',
    has_password: true,
    google_linked: false,
    google_email: null,
    pending_email: null,
    allowed_methods: ['password'],
  }
  const ownerDetails = { ...overviewDetails }
  const releaseSession = () => {
    sessionMode = 'ready'
    for (const response of heldSessions)
      response.end(
        JSON.stringify({
          enabled: true,
          state: overviewDetails ? 'complete' : 'anonymous',
          account: accountView(),
        }),
      )
  }
  const releaseDetails = () => {
    detailMode = 'ready'
    for (const response of heldDetails)
      response.end(JSON.stringify(overviewDetails))
  }
  const waitHeld = async (responses, label) => {
    for (let i = 0; i < 100 && responses.size === 0; i++) await delay(50)
    check(responses.size === 1, label)
  }
  const openDraft = async () => {
    await go(page, '/compte')
    await until(
      page,
      `!!document.getElementById('trigger-password')`,
      'Focus fixture ready',
    )
    await openEditor(page, 'password')
    await evaluate(
      page,
      `(() => {
      const input = document.getElementById('current-password');
      let component = input.__vueParentComponent;
      while (component && !component.setupState.changePassword) component = component.parent;
      if (!component) throw new Error('Missing overview setup');
      const state = component.setupState;
      input.value = 'synthetic-focus-draft'; input.dispatchEvent(new Event('input', { bubbles: true }));
      input.focus(); input.setSelectionRange(2, 7);
      const fixture = window.__focusFixture = { input, state, flashes: 0 };
      fixture.observer = new MutationObserver(() => {
        if (document.querySelector('main .animate-pulse') || !input.isConnected || !input.getClientRects().length) fixture.flashes++;
      });
      fixture.observer.observe(document.querySelector('main'), { subtree: true, childList: true, attributes: true });
    })()`,
    )
  }
  const stableDraft = async (label) =>
    check(
      await evaluate(
        page,
        `(() => {
    const { input, flashes } = window.__focusFixture;
    return flashes === 0 && input === document.getElementById('current-password') && input === document.activeElement &&
      input.value === 'synthetic-focus-draft' && input.selectionStart === 2 && input.selectionEnd === 7 &&
      !input.disabled && !!input.getClientRects().length && !document.querySelector('main .animate-pulse');
  })()`,
      ),
      label,
    )
  const blockedWrites = async (label) => {
    check(
      await evaluate(
        page,
        `(async () => {
      const { state, input } = window.__focusFixture;
      const button = input.closest('form').querySelector('button[type="submit"]');
      if (!button.disabled) return false;
      button.click(); input.closest('form').dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
      for (const key of ['changePassword','requestEmail','changeGoogle','deleteAccount','cancelEmail','logoutAll','logout']) await state[key]();
      await state.googleProof('password_add');
      let denied = false;
      try { await state.api.cancelEmailChange(); } catch { denied = true; }
      return denied && !state.busy && !state.passwordError && !state.emailError;
    })()`,
      ),
      label,
    )
    check(writes === 0, `${label}: zero mutation transport`)
  }
  await openDraft()
  sessionMode = 'loading'
  detailMode = 'loading'
  const initialSessionReads = sessionReads
  const initialDetailReads = detailReads
  await evaluate(
    page,
    `for (let i=0; i<8; i++) window.dispatchEvent(new Event('focus'))`,
  )
  await waitHeld(
    heldSessions,
    'rapid synthetic focus starts one session request',
  )
  await stableDraft(
    'held session: no skeleton, same input/draft/focus/selection',
  )
  await blockedWrites('held session: UI, handlers and shared API reject writes')
  releaseSession()
  await waitHeld(
    heldDetails,
    'focus always refreshes related details despite same session DTO',
  )
  await evaluate(
    page,
    `for (let i=0; i<8; i++) window.dispatchEvent(new Event('focus'))`,
  )
  await stableDraft(
    'held details: no skeleton, same input/draft/focus/selection',
  )
  await blockedWrites('held details: UI, handlers and shared API reject writes')
  const { data: focusImage } = await cdp.send(
    'Page.captureScreenshot',
    { format: 'png', captureBeyondViewport: true },
    page.sessionId,
  )
  await writeFile(
    '/tmp/opencode/account-focus-details-held.png',
    Buffer.from(focusImage, 'base64'),
  )
  console.log('SCREENSHOT /tmp/opencode/account-focus-details-held.png')
  overviewDetails = {
    ...overviewDetails,
    pending_email: 'updated@example.test',
    google_linked: true,
    google_email: 'linked@example.test',
    allowed_methods: ['password', 'google'],
  }
  releaseDetails()
  await until(
    page,
    `!window.__focusFixture.state.blocked`,
    'Focus barrier released',
  )
  await stableDraft(
    'completed focus: stable DOM and caret without any observed skeleton',
  )
  check(
    sessionReads === initialSessionReads + 1 &&
      detailReads === initialDetailReads + 1,
    'rapid focus deduplicates both phases',
  )
  check(
    await evaluate(
      page,
      `document.querySelector('main').innerText.includes('updated@example.test') && window.__focusFixture.state.details.allowed_methods.includes('google') && !document.querySelector('#editor-password button[type="submit"]').disabled`,
    ),
    'fresh pending email and login methods applied before writes re-enable',
  )
  await evaluate(page, 'window.__focusFixture.observer.disconnect()')
  // A second successful refresh can remove a login method; no permanent stale DTO cache.
  overviewDetails = {
    ...overviewDetails,
    has_password: false,
    allowed_methods: ['google'],
  }
  await evaluate(page, `window.dispatchEvent(new Event('focus'))`)
  await until(
    page,
    `document.getElementById('trigger-password')?.innerText === 'Ajouter'`,
    'Changed login methods rendered',
  )
  check(
    await evaluate(
      page,
      `!document.getElementById('current-password') && !document.getElementById('trigger-google')`,
    ),
    'removed password method updates editor and last-method guard',
  )

  for (const outcome of [
    'session-error',
    'details-error',
    'revoked',
    'different',
    'broadcast',
    'offline',
    'pagehide',
  ]) {
    overviewDetails = { ...ownerDetails }
    await openDraft()
    sessionMode = 'loading'
    detailMode = 'loading'
    await evaluate(page, `window.dispatchEvent(new Event('focus'))`)
    await waitHeld(heldSessions, `${outcome}: session held`)
    if (outcome === 'session-error') {
      sessionMode = 'ready'
      for (const response of heldSessions) {
        response.statusCode = 503
        response.end('{}')
      }
    } else if (outcome === 'revoked' || outcome === 'different') {
      overviewDetails =
        outcome === 'revoked'
          ? null
          : { ...ownerDetails, username: 'another_owner' }
      releaseSession()
    } else {
      releaseSession()
      await waitHeld(heldDetails, `${outcome}: details held`)
      if (outcome === 'details-error') {
        detailMode = 'ready'
        for (const response of heldDetails) {
          response.statusCode = 503
          response.end('{}')
        }
      } else {
        sessionMode = 'loading'
        await evaluate(
          page,
          outcome === 'broadcast'
            ? `(() => { const channel = new BroadcastChannel('messeances-account'); channel.postMessage('changed'); channel.close(); })()`
            : `window.dispatchEvent(new Event('${outcome}'))`,
        )
      }
    }
    await until(
      page,
      `!window.__focusFixture.state.account.session.value && !window.__focusFixture.state.email && !window.__focusFixture.state.currentPassword`,
      `${outcome}: private state and drafts cleared`,
    )
    releaseDetails()
    await delay(100)
    check(
      await evaluate(
        page,
        `window.__focusFixture.state.details === null && !window.__focusFixture.state.currentPassword`,
      ),
      `${outcome}: late details never restore old private data`,
    )
    await evaluate(page, 'window.__focusFixture.observer.disconnect()')
    overviewDetails = null
    releaseSession()
    if (outcome === 'pagehide') {
      await evaluate(
        page,
        `window.dispatchEvent(new PageTransitionEvent('pageshow', { persisted: true }))`,
      )
      await until(
        page,
        `window.__focusFixture.state.account.status.value === 'ready'`,
        'Persisted restore rechecks session',
      )
      check(
        await evaluate(
          page,
          `!window.__focusFixture.state.details && !window.__focusFixture.state.currentPassword`,
        ),
        'persisted restore never recovers old drafts',
      )
    }
    detailMode = 'ready'
  }
  overviewDetails = { ...ownerDetails }
  for (const state of ['loading', 'error']) {
    detailMode = state
    await go(page, '/compte')
    await until(
      page,
      state === 'loading'
        ? `document.querySelector('main [role="status"]')?.textContent.includes('Chargement du compte')`
        : `!!document.querySelector('main [role="alert"]')`,
      `Account details ${state}`,
    )
    await inspectStyle(page, `overview-details-${state}`)
    detailMode = 'ready'
    if (state === 'loading') {
      for (const response of heldDetails)
        response.end(JSON.stringify(overviewDetails))
    } else {
      await click(page, 'Réessayer')
    }
    await until(
      page,
      `!!document.getElementById('trigger-password')`,
      'Details recover',
    )
  }
  for (const state of ['error', 'disabled']) {
    sessionMode = state
    await go(page, '/compte')
    await until(
      page,
      state === 'error'
        ? `!!document.querySelector('main [role="alert"]')`
        : `document.querySelector('main [role="status"]')?.textContent.includes('pas encore disponibles')`,
      `Account session ${state}`,
    )
    await inspectStyle(page, `overview-session-${state}`)
  }
  sessionMode = 'ready'
  overviewDetails = null
  await go(page, '/connexion')
  await until(page, `!!document.getElementById('account-email')`, 'Login ready')
  await inspectStyle(page, 'connexion')
  check(
    await evaluate(
      page,
      `document.querySelector('button.account-secondary svg')?.getAttribute('viewBox') === '10 10 20 20'`,
    ),
    'login keeps same decorative Google artwork',
  )
  for (const route of [
    'inscription',
    'mot-de-passe-oublie',
    'reinitialiser-mot-de-passe',
    'verification',
    'compte/confirmer-email',
    'compte/confirmer-identite',
  ]) {
    await go(page, `/${route}`)
    await until(
      page,
      `!!document.querySelector('.account-shell-content h1')`,
      'Auth route ready',
    )
    await inspectStyle(page, route.replaceAll('/', '-'))
  }
  check(
    !interceptionFailure && allPages.every((page) => !page.external),
    'no unexpected external page requests',
  )
  console.log(`ACCOUNT_BROWSER_PASS scenario=overview assertions=${passed}`)
}

async function main() {
  // Run each scenario against a freshly started backend fixture. Real rate limits
  // deliberately remain enabled; neither driver nor fixture bypasses them.
  const google = process.argv.slice(2).includes('--google')
  if (
    process.argv
      .slice(2)
      .some((arg) => !['--google', '--visual', '--overview'].includes(arg))
  )
    throw new HarnessError('Unknown scenario argument')
  if (process.argv.includes('--overview')) {
    phase = 'read-only overview presentation'
    if (google || !visual)
      throw new HarnessError('Overview requires --visual and excludes --google')
    await overviewScenario()
    return
  }
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
  if (interceptionFailure || allPages.some((page) => page.external))
    console.error('Interception state:', {
      interceptionFailure,
      externalCounts: allPages.map((page) => page.external),
    })
  check(
    !interceptionFailure && allPages.every((page) => !page.external),
    'no unexpected external page requests; tracker synthetic only',
  )
  console.log(
    `ACCOUNT_BROWSER_PASS scenario=${google ? 'simulated-google' : 'email'} assertions=${passed}`,
  )
  console.log(
    'OUTSTANDING real Google button/provider, Google linking/email-confirm continuation, SES delivery, production HTTPS cookie, expiry clocks, real OS focus/BFCache/PWA install, password-manager and manual screen-reader acceptance',
  )
}

async function cleanup() {
  cdp?.close()
  await terminateProcessGroup(chrome)
  if (profile) await rm(profile, { recursive: true, force: true })
  if (overviewServer?.listening) {
    overviewServer.closeAllConnections()
    await new Promise((resolve) => overviewServer.close(resolve))
  }
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
