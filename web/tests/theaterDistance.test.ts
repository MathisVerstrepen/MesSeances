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

test('cinemas page lazy-loads client-only map mode while preserving list fallback and shared state', async () => {
  const page = await readFile(new URL('../app/pages/cinemas.vue', import.meta.url), 'utf8')

  assert.match(page, /const viewMode = ref<ViewMode>\('list'\)/)
  assert.match(page, /aria-label="Mode d’affichage des cinémas"/)
  assert.match(page, /:aria-pressed="viewMode === 'list'"/)
  assert.match(page, /<NuxtErrorBoundary v-else>/)
  assert.match(page, /<LazyCinemaTheaterMap/)
  assert.match(page, /:theaters="displayedTheaters"/)
  assert.match(page, /:favorite-theater-ids="draftFavoriteTheaterIds"/)
  assert.match(page, /:user-position="userPosition"/)
  assert.match(page, /@toggle-favorite="toggleTheater"/)
  assert.match(page, /@show-list="showList"/)
  assert.doesNotMatch(page, /import\s+CinemaTheaterMap/)
  assert.doesNotMatch(page, /viewMode[\s\S]{0,100}(?:route|localStorage|sessionStorage)/)
})

test('cinemas page keeps favorite group controls deterministic until preferences finish loading', async () => {
  const page = await readFile(new URL('../app/pages/cinemas.vue', import.meta.url), 'utf8')

  assert.match(page, /const preferencesReady = ref\(false\)/)
  assert.match(page, /await loadPreferences\(\)\s*if \(!isUnmounted\) preferencesReady\.value = true/)
  assert.match(page, /<ClientOnly>[\s\S]*<template #fallback>/)
  assert.equal(page.match(/<ClientOnly>/g)?.length, 2)
  assert.equal(page.match(/:disabled="!preferencesReady \|\| group\.theaters\.every/g)?.length, 2)
  assert.equal(page.match(/class="group-action(?: group-action--secondary)?" disabled/g)?.length, 4)
  assert.equal(page.match(/preferencesReady\.value = true/g)?.length, 1)
})

test('cinemas page keeps a zero-capable draft and filters search results before selection', async () => {
  const page = await readFile(new URL('../app/pages/cinemas.vue', import.meta.url), 'utf8')

  assert.match(page, /const selectedOnly = ref\(false\)/)
  assert.match(page, /const draftFavoriteTheaterIds = ref<string\[]>\(\[\]\)/)
  assert.match(page, /const selectedIds = computed\(\(\) => new Set\(draftFavoriteTheaterIds\.value\)\)/)
  assert.match(page, /const searchResults = computed\(\(\) => directoryTheaters\.value\.filter/)
  assert.match(page, /const displayedTheaters = computed\(\(\) => selectedOnly\.value\s*\? searchResults\.value\.filter/)
  assert.match(page, /groupTheatersByCityIdentity\(displayedTheaters\.value\)/)
  assert.match(page, /sortTheatersByDistance\(displayedTheaters\.value, userPosition\.value\)/)
  assert.match(page, /const visibleTheaterCount = computed\(\(\) => displayedTheaters\.value\.length\)/)
  assert.match(page, /if \(nextIds\.length === 0\) \{[\s\S]*Vos cinémas enregistrés restent inchangés\.[\s\S]*return[\s\S]*setFavoriteTheaterIds\(nextIds\)/)
  assert.match(page, /draftFavoriteTheaterIds\.value = \[\.\.\.favoriteTheaterIds\.value\]/)
  assert.match(page, /updateTheaterSelection\(draftFavoriteTheaterIds\.value, displayedTheaters\.value, select\)/)
  assert.match(page, /:aria-pressed="selectedOnly"/)
  assert.match(page, /<span>Sélectionnés uniquement<\/span>/)
  assert.match(page, /selectedOnly && visibleTheaterCount === 0/)
  assert.match(page, />Afficher tous les cinémas<\/button>/)
  assert.match(page, /const theaters = searchResults\.value/)
  assert.doesNotMatch(page, /selectedOnly[\s\S]{0,100}(?:route|localStorage|sessionStorage)/)
  assert.doesNotMatch(page, /Conservez au moins un cinéma/)
})

test('cinemas page scopes city and global actions to displayed draft results', async () => {
  const page = await readFile(new URL('../app/pages/cinemas.vue', import.meta.url), 'utf8')
  const toolbarStart = page.indexOf('class="selection-toolbar mt-7"')
  const toolbarEnd = page.indexOf('<p class="sr-only" aria-live="polite">', toolbarStart)
  const selectionControlsStart = page.indexOf('class="selection-controls"', toolbarStart)
  const filterActionStart = page.indexOf('class="filter-action"', selectionControlsStart)
  const bulkActionsStart = page.indexOf('class="bulk-actions"', selectionControlsStart)

  assert.match(page, /function updateGroup\(groupTheaters: readonly Theater\[], select: boolean\)/)
  assert.match(page, /function updateDisplayedSelection\(select: boolean\)/)
  assert.match(page, /aria-label="Modifier les cinémas affichés"/)
  assert.match(page, /displayedTheaters\.length === 0 \|\| displayedTheaters\.every\(\(theater\) => selectedIds\.has\(theater\.id\)\)/)
  assert.match(page, /:key="group\.citySlug"/)
  assert.match(page, /encodeURIComponent\(group\.citySlug\)/)
  assert.match(page, /\{\{ draftFavoriteTheaterIds\.length \}\}/)
  assert.ok(toolbarStart >= 0)
  assert.ok(toolbarEnd > toolbarStart)
  assert.ok(page.indexOf('class="view-switch"', toolbarStart) < toolbarEnd)
  assert.ok(selectionControlsStart > page.indexOf('class="view-switch"', toolbarStart))
  assert.ok(filterActionStart > selectionControlsStart)
  assert.ok(bulkActionsStart > filterActionStart)
  assert.ok(page.indexOf('<span>Sélectionnés uniquement</span>', selectionControlsStart) < toolbarEnd)
  assert.ok(page.indexOf('aria-label="Modifier les cinémas affichés"', selectionControlsStart) < toolbarEnd)
  assert.doesNotMatch(page, /class="view-switch mb-7"/)
  assert.match(page, /\.selection-toolbar \{[\s\S]*?display: flex;[\s\S]*?flex-wrap: wrap;/)
  assert.match(page, /<List :size="16" aria-hidden="true" \/> Liste/)
  assert.match(page, /<MapIcon :size="16" aria-hidden="true" \/> Carte/)
  assert.match(page, /<ListFilter :size="17" aria-hidden="true" \/>/)
  assert.match(page, /class="filter-action__state" aria-hidden="true"><Check v-if="selectedOnly"/)
  assert.match(page, /\.filter-action\[aria-pressed="true"\] \{[\s\S]*?box-shadow: 4px 4px 0 #27272a;/)
  assert.match(page, /class="bulk-actions" role="group" aria-label="Modifier les cinémas affichés"/)
  assert.equal(page.match(/<CheckCheck :size="16" aria-hidden="true" \/> Tout sélectionner/g)?.length, 2)
  assert.equal(page.match(/<X :size="16" aria-hidden="true" \/> Désélectionner/g)?.length, 2)
  assert.match(page, /\.bulk-actions \{[\s\S]*?border: 2px dashed #27272a;/)
  assert.match(page, /\.view-switch,\s*\.filter-action,\s*\.bulk-actions \{[\s\S]*?box-sizing: border-box;[\s\S]*?height: 2\.75rem;/)
  assert.match(page, /\.selection-controls \{[\s\S]*?display: inline-flex;[\s\S]*?flex-wrap: wrap;[\s\S]*?justify-content: flex-end;[\s\S]*?gap: 0\.75rem;/)
  assert.match(page, /\.bulk-actions \.group-action \{[\s\S]*?height: 100%;[\s\S]*?min-height: 0;/)
  assert.match(page, /\.view-switch button \{[\s\S]*?height: 100%;[\s\S]*?min-height: 0;/)
  assert.match(page, /@media \(max-width: 639px\) \{[\s\S]*?\.selection-controls \{[\s\S]*?width: 100%;[\s\S]*?\.view-switch,[\s\S]*?\.filter-action,[\s\S]*?\.bulk-actions \{[\s\S]*?width: 100%;/)
})

test('client map source contract covers clustering, accessible Vue details, privacy, and cleanup', async () => {
  const component = await readFile(new URL('../app/components/CinemaTheaterMap.client.vue', import.meta.url), 'utf8')

  assert.match(component, /import 'maplibre-gl\/dist\/maplibre-gl\.css'/)
  assert.match(component, /import workerUrl from 'maplibre-gl\/dist\/maplibre-gl-worker\.mjs\?worker&url'/)
  assert.match(component, /maplibregl\.setWorkerUrl\(workerUrl\)/)
  assert.equal(component.match(/setWorkerUrl\(/g)?.length, 1)
  assert.ok(component.indexOf('setWorkerUrl(workerUrl)') < component.indexOf('new maplibregl.Map'))
  assert.match(component, /https:\/\/tiles\.openfreemap\.org\/styles\/liberty/)
  assert.match(component, /cluster:\s*true/)
  assert.match(component, /clusterRadius:\s*50/)
  assert.match(component, /clusterMaxZoom:\s*14/)
  assert.match(component, /getClusterExpansionZoom/)
  assert.match(component, /USER_SOURCE_ID/)
  assert.doesNotMatch(component, /GeolocateControl|navigator\.geolocation/)
  assert.match(component, /<article v-if="selectedTheater"/)
  assert.match(component, /tabindex="-1"/)
  assert.match(component, /map\.getCanvas\(\)\.focus\(\{ preventScroll: true \}\)/)
  assert.match(component, /emit\('toggle-favorite', selectedTheater\.id\)/)
  assert.match(component, /`\/cinema\/\$\{encodeURIComponent\(selectedTheater\.slug\)\}`/)
  assert.doesNotMatch(component, /\bPopup\b|setHTML|setDOMContent|v-html/)
  assert.match(component, /new ResizeObserver/)
  assert.match(component, /resizeObserver\?\.disconnect\(\)/)
  assert.match(component, /mapToRemove\.remove\(\)/)
  assert.match(component, /onBeforeUnmount/)
  assert.match(component, /role="alert"/)
  assert.match(component, />Afficher la liste</)
  assert.match(component, /https:\/\/openfreemap\.org/)
  assert.match(component, /https:\/\/openmaptiles\.org/)
  assert.match(component, /https:\/\/www\.openstreetmap\.org\/copyright/)
  assert.doesNotMatch(component, /localStorage|sessionStorage|console\.|watchPosition|getCurrentPosition/)
})

test('client map removes theater selector and uses accessible fullscreen lifecycle', async () => {
  const component = await readFile(new URL('../app/components/CinemaTheaterMap.client.vue', import.meta.url), 'utf8')

  assert.doesNotMatch(component, /map-selector-row|mapped-theater-selector|selectorTheaterId|selectorButton|showSelectedTheater/)
  assert.match(component, /new maplibregl\.FullscreenControl\(\{ container: mapLayout\.value \}\)/)
  assert.match(component, /'FullscreenControl\.Enter': 'Afficher la carte en plein écran'/)
  assert.match(component, /'FullscreenControl\.Exit': 'Quitter le plein écran'/)
  assert.match(component, /control\.on\('fullscreenstart', resizeMapAfterFullscreenChange\)/)
  assert.match(component, /control\.on\('fullscreenend', resizeMapAfterFullscreenChange\)/)
  assert.match(component, /fullscreenControl\?\.off\('fullscreenstart', resizeMapAfterFullscreenChange\)/)
  assert.match(component, /fullscreenControl\?\.off\('fullscreenend', resizeMapAfterFullscreenChange\)/)
  assert.match(component, /requestAnimationFrame\(\(\) => \{[\s\S]*map\.resize\(\)/)
  assert.match(component, /cancelAnimationFrame\(fullscreenResizeFrame\)/)
  assert.match(component, /\.map-layout:fullscreen[\s\S]*\.map-attribution/)
})

test('client map uses supported cluster glyphs and reports localized theater coverage', async () => {
  const component = await readFile(new URL('../app/components/CinemaTheaterMap.client.vue', import.meta.url), 'utf8')

  assert.match(component, /'text-font': \['Noto Sans Regular'\]/)
  assert.doesNotMatch(component, /Open Sans Regular|Arial Unicode MS Regular/)
  assert.match(component, /const mappedCount = mappedTheaters\.value\.length/)
  assert.match(component, /const resultCount = props\.theaters\.length/)
  assert.match(component, /mappedCount === 1 \? '' : 's'/)
  assert.match(component, /resultCount === 1 \? '' : 's'/)
  assert.match(component, /`\$\{mappedCount\} cinéma\$\{mappedPlural\} localisé\$\{mappedPlural\} sur \$\{resultCount\} résultat\$\{resultPlural\}`/)
  assert.match(component, /<p class="map-summary" role="status" aria-live="polite">\{\{ mapTheaterSummary \}\}<\/p>/)
})
