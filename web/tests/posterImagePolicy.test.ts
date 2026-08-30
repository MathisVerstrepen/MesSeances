import assert from 'node:assert/strict'
import { readdir, readFile } from 'node:fs/promises'
import test from 'node:test'

const appRoot = new URL('../app/', import.meta.url)
const posterImageSource = await readFile(new URL('../app/components/PosterImage.vue', import.meta.url), 'utf8')
const resultBoxSource = await readFile(new URL('../app/components/ShowtimeResultBox.vue', import.meta.url), 'utf8')
const filmPagePath = '/pages/film/[slug].vue'

async function readVueSources(directory: URL): Promise<Array<{ path: string, source: string }>> {
  const entries = await readdir(directory, { withFileTypes: true })
  const sources = await Promise.all(entries.map(async (entry) => {
    const url = new URL(entry.name + (entry.isDirectory() ? '/' : ''), directory)
    if (entry.isDirectory()) return readVueSources(url)
    if (!entry.name.endsWith('.vue')) return []
    return [{ path: url.pathname, source: await readFile(url, 'utf8') }]
  }))
  return sources.flat()
}

const vueSources = await readVueSources(appRoot)
const combinedAppSource = vueSources.map(({ source }) => source).join('\n')

test('PosterImage centrally owns responsive lazy image policy and protects it from fallthrough attributes', () => {
  assert.match(posterImageSource, /sizes: string/)
  assert.match(posterImageSource, /props\.sizes\.trim\(\)/)
  assert.match(posterImageSource, /posterImageSources\(props\.src\)/)
  assert.match(posterImageSource, /:srcset="imageSources\.srcset \?\? undefined"/)
  assert.match(posterImageSource, /:sizes="normalizedSizes"/)
  assert.match(posterImageSource, /width="500"/)
  assert.match(posterImageSource, /height="750"/)
  assert.equal((posterImageSource.match(/loading="lazy"/g) ?? []).length, 1)
  assert.match(posterImageSource, /decoding="async"/)

  for (const attribute of ['src', 'srcset', 'sizes', 'width', 'height', 'loading', 'decoding', 'fetchpriority', 'fetch-priority']) {
    assert.match(posterImageSource, new RegExp(`'${attribute}'`), attribute)
  }
  assert.match(posterImageSource, /protectedImageAttrs\.has\(key\.toLowerCase\(\)\)/)
})

test('every PosterImage consumer supplies an explicit layout size', () => {
  const tags = vueSources.flatMap(({ path, source }) => [...source.matchAll(/<PosterImage\b[\s\S]*?\/>/g)].map((match) => ({ path, tag: match[0] })))
  assert.equal(tags.length, 12)
  for (const { path, tag } of tags) assert.match(tag, /\s:?sizes=/, path)

  const requiredSizes = [
    '(min-width: 1280px) calc((min(100vw, 1440px) - 12.5rem) / 6), (min-width: 1024px) calc((100vw - 9.5rem) / 4), (min-width: 640px) calc((100vw - 6rem) / 3), calc((100vw - 3rem) / 2)',
    '(min-width: 1024px) calc((min(100vw, 1440px) - 9rem) / 5), (min-width: 640px) calc((100vw - 5rem) / 3), calc((100vw - 3rem) / 2)',
    '(min-width: 1024px) 220px, (min-width: 640px) 180px, 160px',
    '(min-width: 640px) 120px, 96px',
    '(min-width: 640px) 72px, 64px',
    '(min-width: 640px) 52px, 48px',
    'sizes="108px"',
    '(min-width: 1024px) 80px, (min-width: 640px) 96px, 80px',
    'sizes="32px"'
  ]
  for (const sizes of requiredSizes) assert.ok(combinedAppSource.includes(sizes), sizes)

  assert.match(combinedAppSource, /\(max-width: 639px\) calc\(\(100vw - 3\.25rem\) \/ 2\), \(max-width: 767px\) calc\(\(100vw - 4\.25rem\) \/ 2\)/)
  for (const desktopWidth of ['10.5rem', '8.5rem', '13rem', '8rem', '10rem', '7rem']) assert.ok(combinedAppSource.includes(desktopWidth), desktopWidth)
})

test('raw result-box posters use responsive candidates without changing backdrop sources', () => {
  assert.match(resultBoxSource, /posterImageSources\(props\.result\.posterUrl\)/)
  assert.equal((resultBoxSource.match(/:srcset="posterSources\.srcset \?\? undefined"/g) ?? []).length, 3)
  assert.equal((resultBoxSource.match(/sizes="auto, 100vw"/g) ?? []).length, 3)

  const imageTags = [...resultBoxSource.matchAll(/<img\b[^>]*>/g)].map((match) => match[0])
  assert.equal(imageTags.length, 5)
  for (const tag of imageTags) {
    assert.match(tag, /loading="lazy"/)
    assert.match(tag, /decoding="async"/)
    assert.match(tag, /width="320"/)
    assert.match(tag, /height="96"/)
  }

  const backdropTags = imageTags.filter((tag) => tag.includes("mediaKind === 'backdrop'"))
  assert.equal(backdropTags.length, 2)
  for (const tag of backdropTags) {
    assert.doesNotMatch(tag, /srcset/)
    assert.doesNotMatch(tag, /sizes=/)
  }
})

test('posters stay lazy while the measured film backdrop alone receives high fetch priority', () => {
  assert.doesNotMatch(combinedAppSource, /loading="eager"|:loading=/)
  assert.doesNotMatch(posterImageSource, /fetchpriority\s*=\s*["']high["']/i)
  assert.doesNotMatch(resultBoxSource, /fetchpriority\s*=\s*["']high["']/i)

  const highPriorityImages = vueSources.flatMap(({ path, source }) => [...source.matchAll(/<img\b[^>]*>/g)]
    .filter((match) => /fetchpriority\s*=\s*["']high["']/i.test(match[0]))
    .map((match) => ({ path, tag: match[0] })))

  assert.equal(highPriorityImages.length, 1)
  const [filmBackdrop] = highPriorityImages
  assert.ok(filmBackdrop)
  assert.ok(filmBackdrop.path.endsWith(filmPagePath), filmBackdrop.path)
  assert.match(filmBackdrop.tag, /v-if="backdropAvailable"/)
  assert.match(filmBackdrop.tag, /:src="backdropUrl \?\? undefined"/)
  assert.doesNotMatch(filmBackdrop.tag, /loading=/)
})
