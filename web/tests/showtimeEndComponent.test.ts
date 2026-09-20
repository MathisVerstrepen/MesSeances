import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const source = (path: string) =>
  readFile(new URL(`../app/${path}`, import.meta.url), 'utf8')

test('estimated time has a unique accessible description, actual-value formula and local hover/focus/Escape state', async () => {
  const value = await source('components/ShowtimeEndTime.vue')
  assert.match(value, /useId\(\)/)
  assert.match(value, /:aria-describedby="tooltipId"/)
  assert.match(value, /:id="tooltipId"[\s\S]*role="tooltip"/)
  assert.match(value, /underline decoration-dotted underline-offset-4/)
  assert.match(value, /tabindex="0"/)
  assert.match(value, /focus-visible:ring-2/)
  assert.match(
    value,
    /Fin estimée : début annoncé à \$\{formatParisTime\(props\.advertisedStart\)\} \+ \$\{props\.end\.adsMinutes\} min de publicités \+ \$\{props\.runtimeMinutes\} min de film\./,
  )
  assert.match(value, /@mouseenter="openTooltip\('hover'\)"/)
  assert.match(value, /@mouseleave="leaveTooltip"/)
  assert.match(
    value,
    /setTimeout\(\(\) => \{\s*hovered\.value = false\s*\}, 100\)/,
  )
  assert.match(value, /clearTimeout\(hoverCloseTimer\)/)
  assert.match(value, /@focus="openTooltip\('focus'\)"/)
  assert.match(value, /@blur="focused = false"/)
  assert.match(value, /event\.key !== 'Escape'/)
  assert.match(value, /dismissed\.value = true/)
  assert.match(value, /dismissed\.value = false/)
  assert.match(value, /@click\.stop/)
  assert.match(value, /@pointerdown\.stop/)
  assert.doesNotMatch(value, /group-hover|<a\b|<button\b|title=/)
})

test('estimated booking-card triggers are siblings, never nested inside booking links', async () => {
  for (const path of [
    'pages/film/[slug].vue',
    'components/ShowtimeResultBox.vue',
  ]) {
    const value = await source(path)
    const links = value.match(/<BookingLink\b[\s\S]*?<\/BookingLink>/g) ?? []
    for (const link of links) {
      if (link.includes('<ShowtimeEndTime'))
        assert.match(
          link,
          /v-if="(?:showtime|result)\.end && !(?:showtime|result)\.end\.estimated"/,
        )
      assert.doesNotMatch(link, /tabindex="0"/)
    }
    assert.match(
      value,
      /<\/BookingLink>\s*<span\s+v-if="(?:showtime|result)\.end\?\.estimated"[\s\S]*?<ShowtimeEndTime/,
    )
  }
})

test('frontend search default agrees with schedule default', async () => {
  const frontend = await source('pages/recherche.vue')
  const backend = await readFile(
    new URL('../../api/internal/schedule/model.go', import.meta.url),
    'utf8',
  )
  assert.match(frontend, /ADS_BUFFER_MINUTES = 15/)
  assert.match(backend, /DefaultBufferAdsMinutes\s*=\s*15/)
})
