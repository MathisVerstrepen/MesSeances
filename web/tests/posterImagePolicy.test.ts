import assert from 'node:assert/strict'
import test from 'node:test'
import * as safeImages from '../app/utils/safeImageUrl.ts'
import { renderVue } from './helpers/renderVue.ts'

test('real poster renderer rejects unsafe images and protects lazy responsive attributes from fallthrough', async () => {
  const imports = { '~/utils/safeImageUrl': safeImages }
  for (const src of [
    null,
    'javascript:alert(1)',
    'https://evil.test/poster.jpg',
  ]) {
    const html = await renderVue(
      'components/PosterImage.vue',
      { src, alt: 'Film', sizes: '96px' },
      imports,
    )
    assert.doesNotMatch(html, /<img\b/)
    assert.match(html, /Affiche indisponible/)
  }
  const html = await renderVue(
    'components/PosterImage.vue',
    {
      src: 'https://image.tmdb.org/t/p/w500/poster.jpg',
      alt: '<Film>',
      sizes: ' 96px ',
      loading: 'eager',
      srcset: 'https://evil.test/x 500w',
      fetchpriority: 'high',
      width: '1',
    },
    imports,
  )
  assert.match(html, /src="https:\/\/image.tmdb.org\/t\/p\/w500\/poster.jpg"/)
  assert.match(html, /srcset="[^"<>]*w342[^"<>]*w500/)
  assert.match(html, /sizes="96px"/)
  assert.match(html, /loading="lazy"/)
  assert.match(html, /width="500"/)
  assert.match(html, /alt="&lt;Film&gt;"/)
  assert.doesNotMatch(html, /evil.test|fetchpriority|eager/)
  const provider = await renderVue(
    'components/PosterImage.vue',
    {
      src: 'https://storage.googleapis.com/cineville-files-prod/images/event.jpg',
      alt: '',
      sizes: '96px',
    },
    imports,
  )
  assert.match(provider, /<img\b/)
  assert.doesNotMatch(provider, /srcset=/)
})
