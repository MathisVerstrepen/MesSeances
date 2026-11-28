import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { renderLlmsTxt } from '../server/utils/llms.ts'

const read = (path: string) =>
  readFile(new URL(`../app/${path}`, import.meta.url), 'utf8')

test('date tabs can render as real links while plain clicks stay in-page', async () => {
  const bar = await read('components/ShowtimeDateBar.vue')
  assert.match(bar, /dateHref\?: \(date: string\) => string/)
  assert.match(bar, /:is="dateHref \? 'a' : 'button'"/)
  assert.match(bar, /:href="dateHref \? dateHref\(option\) : undefined"/)
  assert.match(bar, /@click="selectDateFromTab\(\$event, option\)"/)
  assert.match(
    bar,
    /event\.button !== 0 \|\|\s*event\.metaKey \|\|\s*event\.ctrlKey \|\|\s*event\.shiftKey \|\|\s*event\.altKey/,
  )
  for (const [page, helper] of [
    ['pages/cinema/[slug].vue', 'cinemaDateHref'],
    ['pages/film/[slug].vue', 'filmDateHref'],
    ['pages/ville/[slug]/index.vue', 'cityDateHref'],
  ] as const) {
    const source = await read(page)
    assert.match(source, new RegExp(`:date-href="${helper}"`), page)
    assert.match(source, /formatLongDateWithYear\(/, page)
  }
})

test('city day page is indexable only in its default view and links its Markdown version', async () => {
  const page = await read('pages/ville/[slug]/index.vue')
  assert.match(
    page,
    /isDefaultView\.value\s*\? 'index,follow'\s*: 'noindex,follow'/,
  )
  assert.match(
    page,
    /rel: 'alternate',\s*type: 'text\/markdown',\s*href: `\$\{canonicalUrl\.value\}\.md`/,
  )
  const directory = await read('pages/ville/[slug]/cinemas.vue')
  assert.match(
    directory,
    /:to="`\/ville\/\$\{encodeURIComponent\(detail\.city\.slug\)\}`"/,
  )
})

test('the agents page documents every public API path from llms.txt', async () => {
  const page = await read('pages/agents.vue')
  const site = 'https://site.test'
  const apiPaths = new Set(
    [
      ...renderLlmsTxt(site).matchAll(
        /\]\((https:\/\/site\.test\/api\/v1\/[^)\s?]+)/g,
      ),
    ]
      .map((match) => match[1]!.slice(site.length))
      .filter((path) => path !== '/api/v1/openapi.json'),
  )
  assert.ok(apiPaths.size >= 8)
  for (const path of apiPaths)
    assert.ok(page.includes(`path: '${path}'`), `${path} missing from /agents`)
  for (const link of [
    '/llms.txt',
    '/llms-full.txt',
    '/api/v1/openapi.json',
    '/ville/lille.md',
  ])
    assert.ok(page.includes(link), link)
  assert.match(page, /robots: 'index,follow'/)
})

test('every page footer and the homepage lead agents and visitors to cities and the guide', async () => {
  const footer = await read('components/AppFooter.vue')
  assert.match(footer, /to="\/agents"[^>]*>Agents IA et API</)
  const home = await read('pages/index.vue')
  assert.match(home, /<HomeCityLinks \/>/)
  const cities = await read('components/HomeCityLinks.vue')
  assert.match(
    cities,
    /:to="`\/ville\/\$\{encodeURIComponent\(city\.slug\)\}`"/,
  )
  assert.match(cities, /'paris',\s*'marseille',\s*'lyon'/)
  assert.match(cities, /'lille'/)
  assert.match(
    renderLlmsTxt('https://site.test'),
    /\[Guide en HTML\]\(https:\/\/site\.test\/agents\)/,
  )
})
