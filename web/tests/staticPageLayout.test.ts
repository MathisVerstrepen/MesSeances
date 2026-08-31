import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const [layout, legal, credits] = await Promise.all([
  readFile(new URL('../app/components/StaticPageLayout.vue', import.meta.url), 'utf8'),
  readFile(new URL('../app/components/LegalPageLayout.vue', import.meta.url), 'utf8'),
  readFile(new URL('../app/pages/credits.vue', import.meta.url), 'utf8')
])

test('static shell owns shared header, decoration, grid paper, and action slot', () => {
  assert.match(layout, /defineProps<\{[\s\S]*eyebrow: string[\s\S]*title: string/)
  assert.match(layout, /<slot name="header-actions"/)
  assert.match(layout, /after:bg-highlight/)
  assert.match(layout, /background-image:linear-gradient/)
  assert.match(layout, /text-\[clamp\(3\.25rem,9vw,8\.5rem\)\]/)
})

test('legal layout composes the shell and retains legal document styling', () => {
  assert.match(legal, /<StaticPageLayout :eyebrow="eyebrow" :title="title">/)
  assert.match(legal, /class="legal-document/)
  assert.match(legal, /\.legal-document :deep\(\.legal-section h2\)/)
  assert.doesNotMatch(legal, /<main/)
})

test('credits uses shared shell without changing attribution content or crawl policy', () => {
  assert.match(credits, /<StaticPageLayout eyebrow="Attributions · Sources" title="Crédits">/)
  assert.match(credits, /<template #header-actions><ShareButton class="shrink-0"/)
  assert.match(credits, /description: pageDescription/)
  assert.match(credits, /robots: 'noindex,follow'/)
  assert.match(credits, /absoluteSiteUrl\(config\.public\.siteUrl, '\/credits'\)/)
  assert.match(credits, /rel: 'canonical'/)
  for (const destination of ['themoviedb.org', 'ugc.fr', 'kinepolis.fr', 'pathe.fr', 'cgrcinemas.fr', 'openfreemap.org', 'openmaptiles.org', 'openstreetmap.org']) {
    assert.match(credits, new RegExp(destination.replace('.', '\\.')))
  }
  assert.match(credits, /This product uses the TMDB API but is not endorsed or certified by TMDB\./)
})
