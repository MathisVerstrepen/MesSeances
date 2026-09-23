import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { runInNewContext } from 'node:vm'
import test from 'node:test'
import {
  accountFragmentBootstrap,
  accountPageRoots,
  accountPrivacyHeaders,
  isAccountPrivatePath,
} from '../shared/accountPrivacy.ts'
import {
  accountCookieHeader,
  accountRequestHeaders,
} from '../server/utils/accountApi.ts'

const token = 'a'.repeat(43)
const read = (path: string) => readFile(new URL(path, import.meta.url), 'utf8')

interface TokenBrowser {
  __takeAccountToken?: () => string
}

test('private account HTML, payloads and endpoints share no-store policy', () => {
  for (const path of accountPageRoots) {
    assert.equal(isAccountPrivatePath(path), true)
    assert.equal(isAccountPrivatePath(`${path}/_payload.json`), true)
  }
  for (const path of [
    '/api/v1/auth/session',
    '/api/v1/account',
    '/api/v1/account/username',
  ])
    assert.equal(isAccountPrivatePath(path), true)
  for (const path of [
    '/Connexion',
    '/VERIFICATION',
    '/%63ompte/_payload.json',
    '//compte//confirmer-email',
  ])
    assert.equal(isAccountPrivatePath(path), true)
  for (const path of [
    '/films',
    '/api/v1/movies',
    '/admin',
    '/compteur',
    '/connexion-extra',
  ])
    assert.equal(isAccountPrivatePath(path), false)
  assert.equal(accountPrivacyHeaders['Cache-Control'], 'private, no-store')
  assert.equal(accountPrivacyHeaders.Vary, 'Cookie')
  assert.equal(accountPrivacyHeaders['Referrer-Policy'], 'no-referrer')
})

test('SSR forwards only correctly named, bounded account cookie and fresh request ID', () => {
  const cookies = `admin=secret; __Host-messeances_session=${token}; messeances_session_dev=${'b'.repeat(43)}; __Host-messeances_registration=${'c'.repeat(43)}; messeances_registration_dev=${'d'.repeat(43)}; other=secret`
  assert.equal(
    accountCookieHeader(cookies, 'https://messeances.fr'),
    `__Host-messeances_session=${token}`,
  )
  assert.equal(
    accountCookieHeader(cookies, 'http://localhost:3000'),
    `messeances_session_dev=${'b'.repeat(43)}`,
  )
  assert.equal(
    accountCookieHeader(
      `__Host-messeances_session=${token}; __Host-messeances_session=${token}`,
      'https://messeances.fr',
    ),
    undefined,
  )
  assert.equal(
    accountCookieHeader(
      '__Host-messeances_session=bad\r\nvalue',
      'https://messeances.fr',
    ),
    undefined,
  )
  const first = accountRequestHeaders(cookies, 'https://messeances.fr')
  const second = accountRequestHeaders(cookies, 'https://messeances.fr')
  assert.deepEqual(Object.keys(first).sort(), ['Cookie', 'X-Request-ID'])
  assert.match(first['X-Request-ID']!, /^[a-f0-9]{32}$/)
  assert.notEqual(first['X-Request-ID'], second['X-Request-ID'])
  for (const siteUrl of ['https://messeances.fr', 'http://localhost:3000']) {
    assert.equal(
      accountCookieHeader(
        `__Host-messeances_registration=${token}; messeances_registration_dev=${token}`,
        siteUrl,
      ),
      undefined,
    )
  }
})

test('fragment bootstrap clears URL synchronously and exposes token once in memory', () => {
  for (const [fragment, expected] of [
    [`#token=${token}`, token],
    ['#token=invalid', ''],
    [`#token=${token}&token=${token}`, ''],
  ]) {
    const browser: TokenBrowser = {}
    const order: string[] = []
    runInNewContext(accountFragmentBootstrap, {
      window: browser,
      location: { hash: fragment, pathname: '/verification', search: '' },
      history: {
        state: null,
        replaceState: (_state: null, _title: string, path: string) => {
          order.push(path)
        },
      },
      URLSearchParams,
    })
    assert.deepEqual(order, ['/verification'])
    const consume = browser.__takeAccountToken!
    assert.equal(consume(), expected)
    assert.equal(browser.__takeAccountToken, undefined)
    assert.equal(consume(), '')
  }
})

test('privacy boundary excludes tracker, forces document transitions and prepends bootstrap', async () => {
  assert.match(
    await read('../app/app.vue'),
    /!privateDocument && umamiScriptUrl/,
  )
  assert.match(
    await read('../app/middleware/account-boundary.global.ts'),
    /external: true/,
  )
  assert.match(
    await read('../server/plugins/account-privacy.ts'),
    /html\.head\.unshift/,
  )
  const header = await read('../app/components/AppHeader.vue')
  assert.match(header, /<a\s+:href="accountHref"/)
  const config = await read('../nuxt.config.ts')
  assert.match(config, /handler: 'NetworkOnly'/)
  assert.match(config, /navigateFallback: null/)
  assert.match(config, /globIgnores:.*_payload/)
  assert.match(config, /changeOrigin: false/)
})

test('account client disables retries and caching and never uses internal authorization', async () => {
  const client = await read('../app/composables/useAccountApi.ts')
  assert.match(client, /'X-Messeances-CSRF': '1'/)
  assert.match(client, /credentials: 'same-origin'/)
  assert.match(client, /retry: false/)
  assert.match(client, /cache: 'no-store'/)
  assert.match(client, /import\.meta\.server && body/)
  assert.doesNotMatch(
    client,
    /internalApiSharedSecret|localStorage|sessionStorage/,
  )
  const verification = await read('../app/pages/verification.vue')
  assert.match(verification, /@submit\.prevent="confirm"/)
  assert.doesNotMatch(
    verification,
    /onMounted\(confirm\)|watch\([^]*confirmVerification/,
  )
  assert.doesNotMatch(
    verification,
    /AccountPasswordField|verificationMethod|verification-password|type="radio"|Mode d’inscription/,
  )
  assert.match(verification, /api\.confirmVerification\(token\.value\)/)
  assert.match(verification, /href="\/inscription"/)
  assert.match(verification, /Recommencer l’inscription/)
  assert.doesNotMatch(verification, /href="\/connexion"/)
})

test('creation forms share ten-character criteria without visible byte-limit copy', async () => {
  const field = await read('../app/components/AccountPasswordField.vue')
  assert.match(field, /Au moins 10 caractères/)
  assert.match(field, /Au plus 128 caractères/)
  assert.doesNotMatch(field, /octets/)
  for (const path of [
    '../app/components/AccountCredentialsForm.vue',
    '../app/pages/reinitialiser-mot-de-passe.vue',
    '../app/pages/compte/index.vue',
    '../app/pages/compte/confirmer-identite.vue',
  ]) {
    const source = await read(path)
    assert.match(source, /passwordCriteria\(/)
    assert.match(source, /10 à 128 caractères/)
    assert.doesNotMatch(source, /15 à|512 octets/)
  }
})
