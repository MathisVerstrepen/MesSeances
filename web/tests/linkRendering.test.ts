import assert from 'node:assert/strict'
import test from 'node:test'
import * as booking from '../app/utils/bookingUrl.ts'
import { renderVue } from './helpers/renderVue.ts'

test('booking renderer gives safe reservations protected new tabs, truthful website labels and inert unsafe fallback', async () => {
  for (const [provider, url, label] of [
    ['megarama', 'https://roubaix.megarama.fr/', 'Site du cinéma Megarama'],
    ['cinewest', 'https://www.cine-cognac.com/', 'Site du cinéma Cinewest'],
    [
      'cgr',
      'https://achat.cgrcinemas.fr/cgr-lille/r/123',
      'Réserver sur CGR Cinémas',
    ],
  ]) {
    const html = await renderVue(
      'components/BookingLink.vue',
      { provider, url },
      { '~/utils/bookingUrl': booking },
    )
    assert.match(html, /<a\b/)
    assert.ok(html.includes(`href="${url}"`))
    assert.ok(
      html.includes(`aria-label="${label}, ouverture dans un nouvel onglet"`),
    )
    assert.match(html, /target="_blank" rel="noopener noreferrer"/)
  }
  const html = await renderVue(
    'components/BookingLink.vue',
    { provider: 'ugc', url: 'javascript:alert(1)' },
    { '~/utils/bookingUrl': booking },
  )
  assert.match(html, /<span[^>]*aria-disabled="true"/)
  assert.match(html, /Réservation indisponible/)
  assert.doesNotMatch(html, /<a\b|href=/)
})
