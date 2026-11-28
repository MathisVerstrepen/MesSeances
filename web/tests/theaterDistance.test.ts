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
  type TheaterCoordinates,
} from '../app/utils/theaterDistance.ts'

function theater(
  overrides: Partial<TheaterCoordinates> & Pick<TheaterCoordinates, 'id'>,
): TheaterCoordinates {
  return {
    name: overrides.id,
    city: 'Lille',
    latitude: 50.6292,
    longitude: 3.0573,
    ...overrides,
  }
}

test('validates coordinates and calculates zero and known Haversine distances', () => {
  assert.equal(isValidGeographicPoint({ latitude: -90, longitude: 180 }), true)
  assert.equal(isValidGeographicPoint({ latitude: 90, longitude: -180 }), true)
  assert.equal(isValidGeographicPoint({ latitude: 91, longitude: 0 }), false)
  assert.equal(
    isValidGeographicPoint({ latitude: 0, longitude: Number.NaN }),
    false,
  )
  assert.equal(
    haversineDistanceKm(
      { latitude: 0, longitude: 0 },
      { latitude: 0, longitude: 0 },
    ),
    0,
  )

  const oneDegreeAtEquator = haversineDistanceKm(
    { latitude: 0, longitude: 0 },
    { latitude: 0, longitude: 1 },
  )
  assert.ok(oneDegreeAtEquator !== null)
  assert.ok(Math.abs(oneDegreeAtEquator - 111.1949) < 0.001)
})

test('sorts located theaters by exact distance without mutating input', () => {
  const theaters = [
    theater({ id: 'far', latitude: 50.7 }),
    theater({ id: 'near', latitude: 50.63 }),
    theater({ id: 'middle', latitude: 50.65 }),
  ]
  const before = structuredClone(theaters)

  const rows = sortTheatersByDistance(theaters, {
    latitude: 50.6292,
    longitude: 3.0573,
  })

  assert.deepEqual(theaters, before)
  assert.deepEqual(
    rows.map((row) => row.theater.id),
    ['near', 'middle', 'far'],
  )
  assert.deepEqual(
    rows.map((row) => row.isNearest),
    [true, false, false],
  )
})

test('uses deterministic French city, name, and id ties', () => {
  const rows = sortTheatersByDistance(
    [
      theater({
        id: 'b',
        name: 'Zénith',
        city: 'Lyon',
        latitude: 0,
        longitude: 0,
      }),
      theater({
        id: 'c',
        name: 'Alpha',
        city: 'Lille',
        latitude: 0,
        longitude: 0,
      }),
      theater({
        id: 'b',
        name: 'Alpha',
        city: 'Lille',
        latitude: 0,
        longitude: 0,
      }),
      theater({
        id: 'a',
        name: 'Alpha',
        city: 'Lille',
        latitude: 0,
        longitude: 0,
      }),
    ],
    { latitude: 0, longitude: 0 },
  )

  assert.deepEqual(
    rows.map(
      (row) => `${row.theater.city}/${row.theater.name}/${row.theater.id}`,
    ),
    ['Lille/Alpha/a', 'Lille/Alpha/b', 'Lille/Alpha/c', 'Lyon/Zénith/b'],
  )
})

test('puts partial, null, non-finite, and out-of-range coordinates last deterministically', () => {
  const theaters = [
    theater({ id: 'partial', name: 'Delta', latitude: 50, longitude: null }),
    theater({
      id: 'infinite',
      name: 'Charlie',
      latitude: Number.POSITIVE_INFINITY,
    }),
    theater({ id: 'range', name: 'Bravo', latitude: 91 }),
    theater({ id: 'null', name: 'Alpha', latitude: null, longitude: null }),
    theater({ id: 'located', name: 'Zulu', latitude: 50.63 }),
  ]
  const origin = { latitude: 50.6292, longitude: 3.0573 }
  const rows = sortTheatersByDistance(theaters, origin)
  const reversedRows = sortTheatersByDistance([...theaters].reverse(), origin)

  assert.deepEqual(
    rows.map((row) => row.theater.id),
    ['located', 'null', 'range', 'infinite', 'partial'],
  )
  assert.deepEqual(
    reversedRows.map((row) => row.theater.id),
    rows.map((row) => row.theater.id),
  )
  assert.equal(rows[0]?.isNearest, true)
  assert.ok(
    rows.slice(1).every((row) => row.distanceKm === null && !row.isNearest),
  )
})

test('does not mark a nearest theater when every coordinate pair is unavailable', () => {
  const rows = sortTheatersByDistance(
    [
      theater({ id: 'b', latitude: null, longitude: null }),
      theater({ id: 'a', latitude: undefined, longitude: undefined }),
    ],
    { latitude: 50, longitude: 3 },
  )

  assert.deepEqual(
    rows.map((row) => row.theater.id),
    ['a', 'b'],
  )
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
  assert.equal(
    url,
    'https://www.openstreetmap.org/?mlat=50.6292&mlon=3.0574#map=16/50.6292/3.0574',
  )
})

test('rejects invalid coordinates instead of producing an OpenStreetMap URL', () => {
  assert.equal(
    buildOpenStreetMapPositionUrl({ latitude: Number.NaN, longitude: 3 }),
    null,
  )
  assert.equal(
    buildOpenStreetMapPositionUrl({ latitude: 91, longitude: 3 }),
    null,
  )
  assert.equal(
    buildOpenStreetMapPositionUrl({ latitude: 50, longitude: -181 }),
    null,
  )
})

const pageSource = () =>
  readFile(new URL('../app/pages/cinemas.vue', import.meta.url), 'utf8')
const locationSource = () =>
  readFile(
    new URL('../app/utils/cinemaDirectoryLocation.ts', import.meta.url),
    'utf8',
  )

test('cinemas page uses deferred browser geolocation with bounded options', async () => {
  const [page, location, search, picker] = await Promise.all([
    pageSource(),
    locationSource(),
    readFile(
      new URL('../app/components/CinemaDirectorySearch.vue', import.meta.url),
      'utf8',
    ),
    readFile(
      new URL('../app/components/CinemaPickerDialog.vue', import.meta.url),
      'utf8',
    ),
  ])

  assert.match(page, /Utiliser ma position/)
  assert.match(search, /'Autour de moi'/)
  assert.match(page, /@nearby="useCurrentPosition"/)
  assert.match(page, /@click="useCurrentPosition"/)
  for (const source of [page, picker])
    assert.match(
      source,
      /createCinemaDirectoryLocation\(\s*\(\) =>\s*import.meta.client \? navigator.geolocation : undefined,?\s*\)/,
    )
  assert.match(
    location,
    /enableHighAccuracy: false,\s*timeout: 8000,\s*maximumAge: 600000/,
  )
  assert.equal(location.match(/\.getCurrentPosition\(/g)?.length, 1)
  assert.doesNotMatch(page + picker + location, /watchPosition/)
  assert.match(picker, /location\.dispose\(\)/)
})

test('cinemas page exposes accessible pending, failure, and nearest states', async () => {
  const [page, location, row] = await Promise.all([
    pageSource(),
    locationSource(),
    readFile(
      new URL('../app/components/CinemaDirectoryRow.vue', import.meta.url),
      'utf8',
    ),
  ])

  assert.match(page, /role="status"\s*>\s*Recherche de votre position…/)
  assert.match(page, /v-if="locationError"[\s\S]*role="alert"/)
  assert.match(row, />Le plus proche</)
  assert.match(page, /isNearest: position === 0/)
  assert.match(page, /onBeforeUnmount\(\(\) => \{\s*isUnmounted = true/)
  assert.match(
    location,
    /Localisation refusée\. Autorisez l’accès à votre position dans les réglages du navigateur, puis réessayez\./,
  )
  assert.match(
    location,
    /Position indisponible\. Vérifiez que la localisation est activée, puis réessayez\./,
  )
  assert.match(location, /La localisation a pris trop de temps\. Réessayez\./)
  assert.match(
    page,
    />Position utilisée : latitude\s*\{\{\s*formatPositionCoordinate\(userPosition\.latitude\)\s*\}\}\s*·\s*longitude/,
  )
  assert.match(page, /target="_blank"/)
  assert.match(page, /rel="noopener noreferrer"/)
  assert.match(page, /\(ouvre OpenStreetMap dans un nouvel onglet\)/)
  assert.match(
    page,
    /<span>\s*·\s*\{\{\s*formatPositionAccuracy\(locationAccuracyMeters\)\s*\}\}\s*<\/span\s*>/,
  )
  assert.doesNotMatch(page, /window\.open/)
  assert.doesNotMatch(page + location, /localStorage|sessionStorage|console\./)
})

test('cinemas page lazy-loads client-only map mode while preserving list fallback and shared state', async () => {
  const page = await pageSource()

  assert.match(page, /aria-label="Mode d’affichage des cinémas"/)
  assert.match(page, /:aria-pressed="routeState\.view === 'list'"/)
  assert.match(page, /<NuxtErrorBoundary v-else>/)
  assert.match(page, /<LazyCinemaTheaterMap/)
  assert.match(page, /:theaters="resultTheaters"/)
  assert.match(page, /:favorite-theater-ids="draftFavoriteTheaterIds"/)
  assert.match(page, /:user-position="userPosition"/)
  assert.match(page, /@toggle-favorite="toggleTheater"/)
  assert.match(page, /@show-list="setView\('list'\)"/)
  assert.match(page, /recoverMapBoundary\(clearError\)/)
  assert.doesNotMatch(page, /import\s+CinemaTheaterMap/)
})

test('cinemas page keeps selection controls disabled until preferences finish loading', async () => {
  const page = await pageSource()

  assert.match(page, /const preferencesReady = ref\(false\)/)
  assert.match(
    page,
    /await loadPreferences\(\)\s*if \(!isUnmounted\) preferencesReady\.value = true/,
  )
  assert.equal(page.match(/preferencesReady\.value = true/g)?.length, 1)
  assert.match(
    page,
    /const selectionDisabled = computed\(\s*\(\) => writesBlocked\.value \|\| !preferencesReady\.value,?\s*\)/,
  )
  assert.match(page, /:disabled="selectionDisabled"/)
  assert.match(page, /:selection-disabled="selectionDisabled"/)
})

test('cinemas page keeps a zero-capable draft that only targets the toggled cinemas', async () => {
  const page = await pageSource()

  assert.match(page, /const draftFavoriteTheaterIds = ref<string\[]>\(\[\]\)/)
  assert.match(
    page,
    /const selectedIds = computed\(\(\) => new Set\(draftFavoriteTheaterIds\.value\)\)/,
  )
  assert.doesNotMatch(page, /if \(nextIds\.length === 0\)/)
  assert.match(page, /setFavoriteTheaterIds\(nextIds\)/)
  assert.match(
    page,
    /draftFavoriteTheaterIds\.value = \[\.\.\.favoriteTheaterIds\.value\]/,
  )
  assert.match(
    page,
    /updateTheaterSelection\(\s*draftFavoriteTheaterIds\.value,\s*\[theater\],\s*!selectedIds\.value\.has\(id\),?\s*\)/,
  )
  assert.match(
    page,
    /updateTheaterSelection\(draftFavoriteTheaterIds\.value, theaters, true\)/,
  )
  assert.doesNotMatch(page, /Tout sélectionner|Désélectionner/)
})

test('client map keeps individual points at every zoom, accessible Vue details, privacy, and cleanup', async () => {
  const component = await readFile(
    new URL('../app/components/CinemaTheaterMap.client.vue', import.meta.url),
    'utf8',
  )

  assert.match(component, /import 'maplibre-gl\/dist\/maplibre-gl\.css'/)
  assert.match(
    component,
    /import workerUrl from 'maplibre-gl\/dist\/maplibre-gl-worker\.mjs\?worker&url'/,
  )
  assert.match(component, /maplibregl\.setWorkerUrl\(workerUrl\)/)
  assert.equal(component.match(/setWorkerUrl\(/g)?.length, 1)
  assert.ok(
    component.indexOf('setWorkerUrl(workerUrl)') <
      component.indexOf('new maplibregl.Map'),
  )
  assert.match(component, /https:\/\/tiles\.openfreemap\.org\/styles\/liberty/)
  assert.match(
    component,
    /map\.addSource\(THEATER_SOURCE_ID, \{\s*type: 'geojson',\s*data: theaterData\.value,?\s*\}\)/,
  )
  assert.doesNotMatch(component, /cluster|point_count/i)
  const mapLoad = component.slice(
    component.indexOf('function onMapLoad()'),
    component.indexOf('\nwatch('),
  )
  assert.doesNotMatch(mapLoad, /minzoom|maxzoom/i)
  assert.match(
    component,
    /id: THEATER_LAYER_ID,\s*type: 'circle',\s*source: THEATER_SOURCE_ID,\s*paint:/,
  )
  assert.match(
    component,
    /id: FAVORITE_LAYER_ID,\s*type: 'circle',\s*source: THEATER_SOURCE_ID,\s*filter: \['==', \['get', 'favorite'\], true\]/,
  )
  assert.match(
    component,
    /map\.on\('click', THEATER_LAYER_ID, onTheaterClick\)/,
  )
  assert.match(
    component,
    /map\.off\('click', THEATER_LAYER_ID, onTheaterClick\)/,
  )
  assert.match(component, /USER_SOURCE_ID/)
  assert.doesNotMatch(component, /GeolocateControl|navigator\.geolocation/)
  assert.match(component, /<article\s+v-if="selectedTheater"/)
  assert.match(component, /tabindex="-1"/)
  assert.match(
    component,
    /map\.getCanvas\(\)\.focus\(\{ preventScroll: true \}\)/,
  )
  assert.match(component, /emit\('toggle-favorite', selectedTheater\.id\)/)
  assert.match(component, />\s*Mes cinémas\s*<\/span\s*>/)
  assert.match(component, /'Dans mes cinémas' : 'Hors de mes cinémas'/)
  assert.match(component, /'Retirer de mes cinémas' : 'Ajouter à mes cinémas'/)
  assert.match(
    component,
    /`\/cinema\/\$\{encodeURIComponent\(selectedTheater\.slug\)\}`/,
  )
  assert.doesNotMatch(component, /\bPopup\b|setHTML|setDOMContent|v-html/)
  assert.match(component, /new ResizeObserver/)
  assert.match(component, /resizeObserver\?\.disconnect\(\)/)
  assert.match(component, /mapToRemove\.remove\(\)/)
  assert.match(component, /onBeforeUnmount/)
  assert.match(component, /role="alert"/)
  assert.match(component, />\s*Afficher la liste\s*</)
  assert.match(component, /https:\/\/openfreemap\.org/)
  assert.match(component, /https:\/\/openmaptiles\.org/)
  assert.match(component, /https:\/\/www\.openstreetmap\.org\/copyright/)
  assert.doesNotMatch(
    component,
    /localStorage|sessionStorage|console\.|watchPosition|getCurrentPosition/,
  )
})

test('client map removes theater selector and uses accessible fullscreen lifecycle', async () => {
  const component = await readFile(
    new URL('../app/components/CinemaTheaterMap.client.vue', import.meta.url),
    'utf8',
  )

  assert.doesNotMatch(
    component,
    /map-selector-row|mapped-theater-selector|selectorTheaterId|selectorButton|showSelectedTheater/,
  )
  assert.match(
    component,
    /new maplibregl\.FullscreenControl\(\{\s*container: mapLayout\.value,?\s*\}\)/,
  )
  assert.match(
    component,
    /'FullscreenControl\.Enter': 'Afficher la carte en plein écran'/,
  )
  assert.match(component, /'FullscreenControl\.Exit': 'Quitter le plein écran'/)
  assert.match(
    component,
    /control\.on\('fullscreenstart', resizeMapAfterFullscreenChange\)/,
  )
  assert.match(
    component,
    /control\.on\('fullscreenend', resizeMapAfterFullscreenChange\)/,
  )
  assert.match(
    component,
    /fullscreenControl\?\.off\(\s*'fullscreenstart',\s*resizeMapAfterFullscreenChange,?\s*\)/,
  )
  assert.match(
    component,
    /fullscreenControl\?\.off\(\s*'fullscreenend',\s*resizeMapAfterFullscreenChange,?\s*\)/,
  )
  assert.match(
    component,
    /requestAnimationFrame\(\(\) => \{[\s\S]*map\.resize\(\)/,
  )
  assert.match(component, /cancelAnimationFrame\(fullscreenResizeFrame\)/)
  assert.match(component, /\.map-layout:fullscreen[\s\S]*\.theater-details/)
})

test('client map reports localized theater coverage', async () => {
  const component = await readFile(
    new URL('../app/components/CinemaTheaterMap.client.vue', import.meta.url),
    'utf8',
  )

  assert.match(component, /const mappedCount = mappedTheaters\.value\.length/)
  assert.match(component, /const resultCount = props\.theaters\.length/)
  assert.match(component, /mappedCount === 1 \? '' : 's'/)
  assert.match(component, /resultCount === 1 \? '' : 's'/)
  assert.match(
    component,
    /`\$\{mappedCount\} cinéma\$\{mappedPlural\} localisé\$\{mappedPlural\} sur \$\{resultCount\} résultat\$\{resultPlural\}`/,
  )
  assert.match(
    component,
    /<p\s+class="[^"]+"\s+role="status"\s+aria-live="polite"\s*>\s*\{\{ mapTheaterSummary \}\}\s*<\/p>/,
  )
})
