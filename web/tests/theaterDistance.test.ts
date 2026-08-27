import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import {
  buildOpenStreetMapPositionUrl,
  formatPositionAccuracy,
  formatPositionCoordinate,
  formatTheaterDistance,
  haversineDistanceKm,
  isValidGeographicPoint,
  sortTheatersByDistance,
  type TheaterCoordinates
} from '../app/utils/theaterDistance.ts'

function theater(overrides: Partial<TheaterCoordinates> & Pick<TheaterCoordinates, 'id'>): TheaterCoordinates {
  return {
    name: overrides.id,
    city: 'Lille',
    latitude: 50.6292,
    longitude: 3.0573,
    ...overrides
  }
}

test('validates coordinates and calculates zero and known Haversine distances', () => {
  assert.equal(isValidGeographicPoint({ latitude: -90, longitude: 180 }), true)
  assert.equal(isValidGeographicPoint({ latitude: 90, longitude: -180 }), true)
  assert.equal(isValidGeographicPoint({ latitude: 91, longitude: 0 }), false)
  assert.equal(isValidGeographicPoint({ latitude: 0, longitude: Number.NaN }), false)
  assert.equal(haversineDistanceKm({ latitude: 0, longitude: 0 }, { latitude: 0, longitude: 0 }), 0)

  const oneDegreeAtEquator = haversineDistanceKm({ latitude: 0, longitude: 0 }, { latitude: 0, longitude: 1 })
  assert.ok(oneDegreeAtEquator !== null)
  assert.ok(Math.abs(oneDegreeAtEquator - 111.1949) < 0.001)
})

test('sorts located theaters by exact distance without mutating input', () => {
  const theaters = [
    theater({ id: 'far', latitude: 50.7 }),
    theater({ id: 'near', latitude: 50.63 }),
    theater({ id: 'middle', latitude: 50.65 })
  ]
  const before = structuredClone(theaters)

  const rows = sortTheatersByDistance(theaters, { latitude: 50.6292, longitude: 3.0573 })

  assert.deepEqual(theaters, before)
  assert.deepEqual(rows.map((row) => row.theater.id), ['near', 'middle', 'far'])
  assert.deepEqual(rows.map((row) => row.isNearest), [true, false, false])
})

test('uses deterministic French city, name, and id ties', () => {
  const rows = sortTheatersByDistance([
    theater({ id: 'b', name: 'Zénith', city: 'Lyon', latitude: 0, longitude: 0 }),
    theater({ id: 'c', name: 'Alpha', city: 'Lille', latitude: 0, longitude: 0 }),
    theater({ id: 'b', name: 'Alpha', city: 'Lille', latitude: 0, longitude: 0 }),
    theater({ id: 'a', name: 'Alpha', city: 'Lille', latitude: 0, longitude: 0 })
  ], { latitude: 0, longitude: 0 })

  assert.deepEqual(rows.map((row) => `${row.theater.city}/${row.theater.name}/${row.theater.id}`), [
    'Lille/Alpha/a',
    'Lille/Alpha/b',
    'Lille/Alpha/c',
    'Lyon/Zénith/b'
  ])
})

test('puts partial, null, non-finite, and out-of-range coordinates last deterministically', () => {
  const theaters = [
    theater({ id: 'partial', name: 'Delta', latitude: 50, longitude: null }),
    theater({ id: 'infinite', name: 'Charlie', latitude: Number.POSITIVE_INFINITY }),
    theater({ id: 'range', name: 'Bravo', latitude: 91 }),
    theater({ id: 'null', name: 'Alpha', latitude: null, longitude: null }),
    theater({ id: 'located', name: 'Zulu', latitude: 50.63 })
  ]
  const origin = { latitude: 50.6292, longitude: 3.0573 }
  const rows = sortTheatersByDistance(theaters, origin)
  const reversedRows = sortTheatersByDistance([...theaters].reverse(), origin)

  assert.deepEqual(rows.map((row) => row.theater.id), ['located', 'null', 'range', 'infinite', 'partial'])
  assert.deepEqual(reversedRows.map((row) => row.theater.id), rows.map((row) => row.theater.id))
  assert.equal(rows[0]?.isNearest, true)
  assert.ok(rows.slice(1).every((row) => row.distanceKm === null && !row.isNearest))
})

test('does not mark a nearest theater when every coordinate pair is unavailable', () => {
  const rows = sortTheatersByDistance([
    theater({ id: 'b', latitude: null, longitude: null }),
    theater({ id: 'a', latitude: undefined, longitude: undefined })
  ], { latitude: 50, longitude: 3 })

  assert.deepEqual(rows.map((row) => row.theater.id), ['a', 'b'])
  assert.ok(rows.every((row) => row.distanceKm === null && !row.isNearest))
})

test('formats French distance labels around zero and 0.1 km', () => {
  assert.equal(formatTheaterDistance(0), '0 km')
  assert.equal(formatTheaterDistance(Number.MIN_VALUE), '< 0,1 km')
  assert.equal(formatTheaterDistance(0.0999), '< 0,1 km')
  assert.equal(formatTheaterDistance(0.1), '0,1 km')
  assert.equal(formatTheaterDistance(12.34), '12,3 km')
  assert.equal(formatTheaterDistance(null), null)
  assert.equal(formatTheaterDistance(-1), null)
})

test('formats the used position with explicit French coordinates and browser accuracy', () => {
  assert.equal(formatPositionCoordinate(50.62924), '50,6292')
  assert.equal(formatPositionCoordinate(3.05735), '3,0574')
  assert.equal(formatPositionCoordinate(-0.00001), '0,0000')
  assert.equal(formatPositionAccuracy(25.4), 'précision environ 25 m')
  assert.equal(formatPositionAccuracy(null), 'précision indisponible')
  assert.equal(formatPositionAccuracy(Number.NaN), 'précision indisponible')
  assert.equal(formatPositionAccuracy(-1), 'précision indisponible')
})

test('builds a fixed OpenStreetMap marker URL from the same four-decimal coordinates', () => {
  const point = { latitude: 50.62924, longitude: 3.05735 }
  const url = buildOpenStreetMapPositionUrl(point)

  assert.equal(formatPositionCoordinate(point.latitude), '50,6292')
  assert.equal(formatPositionCoordinate(point.longitude), '3,0574')
  assert.equal(url, 'https://www.openstreetmap.org/?mlat=50.6292&mlon=3.0574#map=16/50.6292/3.0574')
})

test('rejects invalid coordinates instead of producing an OpenStreetMap URL', () => {
  assert.equal(buildOpenStreetMapPositionUrl({ latitude: Number.NaN, longitude: 3 }), null)
  assert.equal(buildOpenStreetMapPositionUrl({ latitude: 91, longitude: 3 }), null)
  assert.equal(buildOpenStreetMapPositionUrl({ latitude: 50, longitude: -181 }), null)
})

test('cinemas page requests location only from explicit controls and keeps route ownership stable', async () => {
  const page = await readFile(new URL('../app/pages/cinemas.vue', import.meta.url), 'utf8')

  assert.match(page, /'Utiliser ma position'/)
  assert.match(page, />\s*Afficher par ville\s*</)
  assert.match(page, /@click="useCurrentPosition"/)
  assert.match(page, /navigator\.geolocation\.getCurrentPosition\(handleLocationSuccess, handleLocationError, \{\s*enableHighAccuracy: false,\s*timeout: 8000,\s*maximumAge: 600000\s*\}\)/)
  assert.equal(page.match(/\.getCurrentPosition\(/g)?.length, 1)
  assert.doesNotMatch(page, /watchPosition/)
  assert.match(page, /const OWNED_QUERY_KEYS = \['q'\] as const/)
})

test('cinemas page exposes accessible pending, failure, and nearest states', async () => {
  const page = await readFile(new URL('../app/pages/cinemas.vue', import.meta.url), 'utf8')

  assert.match(page, /role="status" aria-live="polite">Recherche de votre position…/)
  assert.match(page, /v-if="locationError"[\s\S]*role="alert"/)
  assert.match(page, />Le plus proche</)
  assert.match(page, /:aria-busy="locationStatus === 'requesting'"/)
  assert.match(page, /onBeforeUnmount\(\(\) => \{\s*isUnmounted = true/)
  assert.match(page, /Localisation refusée\. Autorisez l’accès à votre position dans les réglages du navigateur, puis réessayez\./)
  assert.match(page, /Position indisponible\. Vérifiez que la localisation est activée, puis réessayez\./)
  assert.match(page, /La localisation a pris trop de temps\. Réessayez\./)
  assert.match(page, /La localisation n’est pas disponible dans ce navigateur\. Continuez avec la liste par ville\./)
  assert.match(page, />Position utilisée : latitude \{\{ formatPositionCoordinate\(userPosition\.latitude\) \}\} · longitude/)
  assert.match(page, /target="_blank"/)
  assert.match(page, /rel="noopener noreferrer"/)
  assert.match(page, /class="sr-only"> \(ouvre OpenStreetMap dans un nouvel onglet\)<\/span>/)
  assert.match(page, /<span> · \{\{ formatPositionAccuracy\(locationAccuracyMeters\) \}\}<\/span>/)
  assert.doesNotMatch(page, /window\.open/)
  assert.match(page, /Number\.isFinite\(position\.coords\.accuracy\) && position\.coords\.accuracy >= 0/)
  assert.match(page, /locationAccuracyMeters\.value = null/)
  assert.doesNotMatch(page, /localStorage|sessionStorage|console\./)
})
