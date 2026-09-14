import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const source = (path: string) => readFile(new URL(`../app/${path}`, import.meta.url), 'utf8')

test('Grand Ecran reuses exact supplied WebP for accessible inline and display logos with contrast', async () => {
  const bytes = await readFile(new URL('../app/assets/imgs/grand_ecran_logo_small.webp', import.meta.url))
  assert.equal(createHash('sha256').update(bytes).digest('hex'), '5b483a298244f8acae3be4ffe54b448529a32d96e475aeb1ddb7fa3871a653cf')
  const logo = await source('components/BrandLogo.vue')
  assert.match(logo, /grand_ecran_logo_small\.webp\?no-inline/)
  assert.match(logo, /'Grand Ecran': \{ inline: grandEcranLogoSmall, display: grandEcranLogoSmall \}/)
  assert.match(logo, /'Grand Ecran': 'Grand Ecran'/)
  assert.match(logo, /brand === 'Grand Ecran' \? 'bg-ink'/)
  assert.match(logo, /:alt="decorative \? '' : accessibleNames\[brand\]"/)
  const credits = await source('pages/credits.vue')
  assert.match(credits, /brand: 'Grand Ecran', name: 'Grand Ecran', url: 'https:\/\/www\.grandecran\.fr\/'/)
  assert.equal((credits.match(/credit\.brand\.replaceAll\(' ', '-'\)/g) ?? []).length, 2)
})

test('Grand Ecran branding matches accents and case without consuming cinema location text', async () => {
  const component = await source('components/BrandedText.vue')
  const pattern = /\.split\(\/(.+)\/(giu)\)/.exec(component)!
  const brands = new RegExp(pattern[1]!, pattern[2]!)
  for (const brand of ['Grand Ecran', 'Grand Écran', 'grand écran', 'GRAND ECRAN']) assert.deepEqual(`Cinéma ${brand} Vichy`.split(brands).filter(Boolean), ['Cinéma ', brand, ' Vichy'])
  assert.deepEqual('Grand Ecranville'.split(brands), ['Grand Ecranville'])
  assert.match(component, /brand === 'GRAND ECRAN'.+brand: 'Grand Ecran'/)
})

test('Grand Ecran follows existing MK2 and Cinewest order in manual, all and scheduled sync and admin labels', async () => {
  for (const page of ['sync', 'sync-schedules', 'tmdb-matches', 'theater-locations']) {
    const value = await source(`pages/admin/${page}.vue`)
    assert.match(value, /grandecran: 'Grand Ecran'/)
    if (page.startsWith('sync')) assert.match(value, /const providers = \['ugc', 'kinepolis', 'pathe', 'cgr', 'megarama', 'cineville', 'mk2', 'cinewest', 'grandecran'\] as const/)
  }
  assert.match(await source('pages/admin/sync-schedules.vue'), /grandecran: selectLatestProviderRun\('grandecran'/)
  assert.match(await source('components/CinemaTheaterMap.client.vue'), /'grandecran', THEATER_PROVIDER_COLORS\.grandecran/)
  assert.match(await source('components/BookingLink.vue'), /grandecran: 'Réserver sur Grand Ecran'/)
})
