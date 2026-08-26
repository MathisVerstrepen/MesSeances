import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { parseAdminTheaterLocationCoordinates } from '../app/utils/adminTheaterLocations.ts'

test('parses trimmed decimal dots and commas into JSON-ready numbers', () => {
  assert.deepEqual(parseAdminTheaterLocationCoordinates({ latitude: ' 48.8566 ', longitude: ' 2,3522 ' }), {
    coordinates: { latitude: 48.8566, longitude: 2.3522 },
    errors: {}
  })
  assert.deepEqual(parseAdminTheaterLocationCoordinates({ latitude: '-.5', longitude: '+,75' }).coordinates, {
    latitude: -0.5,
    longitude: 0.75
  })
})

test('requires both coordinate values', () => {
  assert.deepEqual(parseAdminTheaterLocationCoordinates({ latitude: '', longitude: ' ' }), {
    coordinates: null,
    errors: {
      latitude: 'Indiquez la latitude.',
      longitude: 'Indiquez la longitude.'
    }
  })
  assert.deepEqual(parseAdminTheaterLocationCoordinates({ latitude: '48.8', longitude: '' }), {
    coordinates: null,
    errors: { longitude: 'Indiquez la longitude.' }
  })
  assert.deepEqual(parseAdminTheaterLocationCoordinates({ latitude: '', longitude: '2.3' }), {
    coordinates: null,
    errors: { latitude: 'Indiquez la latitude.' }
  })
})

test('rejects non-numbers and Infinity-like input', () => {
  for (const latitude of ['cinquante', 'NaN', 'Infinity', '-Infinity', '1e2', '1,2.3']) {
    const result = parseAdminTheaterLocationCoordinates({ latitude, longitude: '2' })
    assert.equal(result.coordinates, null)
    assert.equal(result.errors.latitude, 'Utilisez un nombre décimal valide.')
  }
})

test('accepts inclusive coordinate boundaries and rejects values outside them', () => {
  assert.deepEqual(parseAdminTheaterLocationCoordinates({ latitude: '-90', longitude: '180' }).coordinates, {
    latitude: -90,
    longitude: 180
  })
  assert.deepEqual(parseAdminTheaterLocationCoordinates({ latitude: '90', longitude: '-180' }).coordinates, {
    latitude: 90,
    longitude: -180
  })
  assert.equal(parseAdminTheaterLocationCoordinates({ latitude: '90.0001', longitude: '0' }).errors.latitude, 'La latitude doit être comprise entre -90 et 90.')
  assert.equal(parseAdminTheaterLocationCoordinates({ latitude: '0', longitude: '-180.0001' }).errors.longitude, 'La longitude doit être comprise entre -180 et 180.')
})

test('keeps theater location page authenticated and renders API text literally', async () => {
  const page = await readFile(new URL('../app/pages/admin/theater-locations.vue', import.meta.url), 'utf8')

  assert.match(page, /definePageMeta\(\{ middleware: 'admin-auth' \}\)/)
  assert.match(page, /\{\{ item\.suggestion\.label \}\}/)
  assert.match(page, />Géocodage IGN</)
  assert.match(page, />\s*Lancer le géocodage\s*</)
  assert.match(page, /\{\{ geocodingJob\.summary\.selected \}\}/)
  assert.match(page, /\{\{ geocodingJob\.summary\.skipped \}\}/)
  assert.match(page, /\{\{ geocodingJob\.summary\.matched \}\}/)
  assert.match(page, /\{\{ geocodingJob\.summary\.ambiguous \}\}/)
  assert.match(page, /\{\{ geocodingJob\.summary\.not_found \}\}/)
  assert.match(page, /\{\{ geocodingJob\.summary\.failed \}\}/)
  assert.match(page, /\{\{ geocodingJob\.summary\.written \}\}/)
  assert.doesNotMatch(page, /\bv-html\b/)
})

test('uses authenticated geocoding endpoints and cleans up two-second polling', async () => {
  const [page, api] = await Promise.all([
    readFile(new URL('../app/pages/admin/theater-locations.vue', import.meta.url), 'utf8'),
    readFile(new URL('../app/composables/useMesSeancesApi.ts', import.meta.url), 'utf8')
  ])

  assert.match(api, /adminTheaterGeocodingStatus\(\)/)
  assert.match(api, /adminStartTheaterGeocoding\(\)/)
  assert.match(api, /\/api\/v1\/admin\/theater-locations\/geocoding-runs/)
  assert.match(api, /credentials: 'include'/)
  assert.match(api, /code === 'theater_geocoding_in_progress'/)
  assert.match(api, /code === 'theater_geocoding_unavailable'/)
  assert.match(api, /code === 'theater_geocoding_failed'/)
  assert.match(page, /const POLL_DELAY = 2000/)
  assert.match(page, /setTimeout\(\(\) =>/)
  assert.match(page, /onBeforeUnmount\(\(\) => \{[\s\S]*clearGeocodingPolling\(\)/)
  assert.match(page, /nextJob\?\.state === 'succeeded'[\s\S]*refreshLocationsAfterGeocoding\(\)/)
  assert.match(page, /getApiErrorStatus\(error\) === 409[\s\S]*loadGeocodingStatus\(\)/)
})
