<script setup lang="ts">
import * as maplibregl from 'maplibre-gl'
import type { FullscreenControl as MapLibreFullscreenControl, MapLayerMouseEvent, Map as MapLibreMap } from 'maplibre-gl'
import 'maplibre-gl/dist/maplibre-gl.css'
import workerUrl from 'maplibre-gl/dist/maplibre-gl-worker.mjs?worker&url'
import type { FeatureCollection, Point } from 'geojson'
import type { Theater } from '~/types/api'
import type { GeographicPoint } from '~/utils/theaterDistance'
import {
  buildTheaterFeatureCollection,
  theaterFeatureBounds,
  THEATER_PROVIDER_COLORS,
  THEATER_PROVIDER_LABELS
} from '~/utils/theaterMap'

maplibregl.setWorkerUrl(workerUrl)

const props = defineProps<{
  theaters: readonly Theater[]
  favoriteTheaterIds: readonly string[]
  userPosition: GeographicPoint | null
}>()

const emit = defineEmits<{
  'show-list': []
  'toggle-favorite': [theaterId: string]
}>()

const OPENFREEMAP_STYLE_URL = 'https://tiles.openfreemap.org/styles/liberty'
const FRANCE_BOUNDS: [[number, number], [number, number]] = [[-5.5, 41], [10, 51.5]]
const THEATER_SOURCE_ID = 'cinema-theaters'
const CLUSTER_LAYER_ID = 'cinema-clusters'
const CLUSTER_COUNT_LAYER_ID = 'cinema-cluster-count'
const FAVORITE_LAYER_ID = 'cinema-favorites'
const THEATER_LAYER_ID = 'cinema-points'
const USER_SOURCE_ID = 'cinema-user-position'
const USER_LAYER_ID = 'cinema-user-position-point'

const mapContainer = ref<HTMLDivElement | null>(null)
const mapLayout = ref<HTMLDivElement | null>(null)
const detailHeading = ref<HTMLElement | null>(null)
const selectedTheaterId = ref('')
const runtimeFailed = ref(false)
const mapReady = ref(false)
let map: MapLibreMap | null = null
let fullscreenControl: MapLibreFullscreenControl | null = null
let fullscreenResizeFrame: number | null = null
let resizeObserver: ResizeObserver | null = null
let webglCanvas: HTMLCanvasElement | null = null
let layerListenersRegistered = false
let destroyed = false

const favoriteIds = computed(() => new Set(props.favoriteTheaterIds))
const theaterData = computed(() => buildTheaterFeatureCollection(props.theaters, favoriteIds.value))
const mappedTheaters = computed(() => {
  const mappedIds = new Set(theaterData.value.features.map((feature) => feature.properties.id))
  return props.theaters.filter((theater) => mappedIds.has(theater.id))
})
const mapTheaterSummary = computed(() => {
  const mappedCount = mappedTheaters.value.length
  const resultCount = props.theaters.length
  const mappedPlural = mappedCount === 1 ? '' : 's'
  const resultPlural = resultCount === 1 ? '' : 's'
  return `${mappedCount} cinéma${mappedPlural} localisé${mappedPlural} sur ${resultCount} résultat${resultPlural}`
})
const selectedTheater = computed(() => mappedTheaters.value.find((theater) => theater.id === selectedTheaterId.value) ?? null)

function isReducedMotion(): boolean {
  return window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

function emptyFeatureCollection(): FeatureCollection<Point> {
  return { type: 'FeatureCollection', features: [] }
}

function validUserPositionData(): FeatureCollection<Point> {
  const position = props.userPosition
  if (!position || !Number.isFinite(position.latitude) || !Number.isFinite(position.longitude)
    || position.latitude < -90 || position.latitude > 90 || position.longitude < -180 || position.longitude > 180) {
    return emptyFeatureCollection()
  }
  return {
    type: 'FeatureCollection',
    features: [{
      type: 'Feature',
      geometry: { type: 'Point', coordinates: [position.longitude, position.latitude] },
      properties: {}
    }]
  }
}

function focusSelectedTheater() {
  nextTick(() => detailHeading.value?.focus())
}

function selectTheater(theaterId: string) {
  if (!mappedTheaters.value.some((theater) => theater.id === theaterId)) return
  selectedTheaterId.value = theaterId
  focusSelectedTheater()
}

function closeDetails() {
  selectedTheaterId.value = ''
  nextTick(() => {
    if (!map || runtimeFailed.value || destroyed) return
    map.getCanvas().focus({ preventScroll: true })
  })
}

function resizeMapAfterFullscreenChange() {
  if (fullscreenResizeFrame !== null) cancelAnimationFrame(fullscreenResizeFrame)
  fullscreenResizeFrame = requestAnimationFrame(() => {
    fullscreenResizeFrame = null
    if (!map || runtimeFailed.value || destroyed) return
    try {
      map.resize()
    } catch {
      failRuntime()
    }
  })
}

function fitTheaterFeatures() {
  if (!map || runtimeFailed.value || destroyed) return
  const bounds = theaterFeatureBounds(theaterData.value)
  const duration = isReducedMotion() ? 0 : 350
  try {
    if (!bounds) {
      map.fitBounds(FRANCE_BOUNDS, { duration, padding: 24 })
    } else if (bounds.count === 1) {
      const center: [number, number] = [bounds.west, bounds.south]
      if (duration === 0) map.jumpTo({ center, zoom: 12 })
      else map.easeTo({ center, zoom: 12, duration })
    } else {
      const padding = mapContainer.value && mapContainer.value.clientWidth < 640 ? 32 : 56
      map.fitBounds([[bounds.west, bounds.south], [bounds.east, bounds.north]], {
        duration,
        padding,
        maxZoom: 12
      })
    }
  } catch {
    failRuntime()
  }
}

function updateTheaterSource(refit: boolean) {
  if (!map || !mapReady.value || runtimeFailed.value || destroyed) return
  try {
    const source = map.getSource(THEATER_SOURCE_ID)
    if (!(source instanceof maplibregl.GeoJSONSource)) return
    source.setData(theaterData.value)
    if (selectedTheaterId.value && !mappedTheaters.value.some((theater) => theater.id === selectedTheaterId.value)) {
      selectedTheaterId.value = ''
    }
    if (refit) fitTheaterFeatures()
  } catch {
    failRuntime()
  }
}

function updateUserSource() {
  if (!map || !mapReady.value || runtimeFailed.value || destroyed) return
  try {
    const data = validUserPositionData()
    const source = map.getSource(USER_SOURCE_ID)
    if (source instanceof maplibregl.GeoJSONSource) {
      source.setData(data)
      return
    }
    if (data.features.length === 0) return
    map.addSource(USER_SOURCE_ID, { type: 'geojson', data })
    map.addLayer({
      id: USER_LAYER_ID,
      type: 'circle',
      source: USER_SOURCE_ID,
      paint: {
        'circle-radius': 8,
        'circle-color': '#1f6f78',
        'circle-stroke-color': '#ffffff',
        'circle-stroke-width': 3
      }
    })
  } catch {
    failRuntime()
  }
}

function onTheaterClick(event: MapLayerMouseEvent) {
  const theaterId = String(event.features?.[0]?.properties?.id ?? '')
  selectTheater(theaterId)
}

async function onClusterClick(event: MapLayerMouseEvent) {
  if (!map || runtimeFailed.value || destroyed) return
  const feature = event.features?.[0]
  const clusterId = Number(feature?.properties?.cluster_id)
  if (!Number.isSafeInteger(clusterId) || feature?.geometry.type !== 'Point') return
  const source = map.getSource(THEATER_SOURCE_ID)
  if (!(source instanceof maplibregl.GeoJSONSource)) return
  try {
    const zoom = await source.getClusterExpansionZoom(clusterId)
    if (!map || runtimeFailed.value || destroyed) return
    const [longitude = Number.NaN, latitude = Number.NaN] = feature.geometry.coordinates
    if (!Number.isFinite(longitude) || !Number.isFinite(latitude)) return
    const center: [number, number] = [longitude, latitude]
    if (isReducedMotion()) map.jumpTo({ center, zoom })
    else map.easeTo({ center, zoom, duration: 350 })
  } catch {
    failRuntime()
  }
}

function showPointer() {
  if (map) map.getCanvas().style.cursor = 'pointer'
}

function hidePointer() {
  if (map) map.getCanvas().style.cursor = ''
}

function onWebglContextLost(event: Event) {
  event.preventDefault()
  failRuntime()
}

function detachRuntime() {
  if (fullscreenResizeFrame !== null) {
    cancelAnimationFrame(fullscreenResizeFrame)
    fullscreenResizeFrame = null
  }
  fullscreenControl?.off('fullscreenstart', resizeMapAfterFullscreenChange)
  fullscreenControl?.off('fullscreenend', resizeMapAfterFullscreenChange)
  fullscreenControl = null
  resizeObserver?.disconnect()
  resizeObserver = null
  webglCanvas?.removeEventListener('webglcontextlost', onWebglContextLost)
  webglCanvas = null
  if (!map) return
  map.off('load', onMapLoad)
  map.off('error', onMapError)
  if (layerListenersRegistered) {
    map.off('click', THEATER_LAYER_ID, onTheaterClick)
    map.off('click', CLUSTER_LAYER_ID, onClusterClick)
    map.off('mouseenter', THEATER_LAYER_ID, showPointer)
    map.off('mouseleave', THEATER_LAYER_ID, hidePointer)
    map.off('mouseenter', CLUSTER_LAYER_ID, showPointer)
    map.off('mouseleave', CLUSTER_LAYER_ID, hidePointer)
    layerListenersRegistered = false
  }
  const mapToRemove = map
  map = null
  mapReady.value = false
  try {
    mapToRemove.remove()
  } catch {
    runtimeFailed.value = true
  }
}

function failRuntime() {
  if (runtimeFailed.value || destroyed) return
  runtimeFailed.value = true
  detachRuntime()
}

function onMapError() {
  failRuntime()
}

function onMapLoad() {
  if (!map || runtimeFailed.value || destroyed) return
  try {
    map.addSource(THEATER_SOURCE_ID, {
      type: 'geojson',
      data: theaterData.value,
      cluster: true,
      clusterRadius: 50,
      clusterMaxZoom: 14
    })
    map.addLayer({
      id: CLUSTER_LAYER_ID,
      type: 'circle',
      source: THEATER_SOURCE_ID,
      filter: ['has', 'point_count'],
      paint: {
        'circle-color': '#27272a',
        'circle-radius': ['step', ['get', 'point_count'], 18, 20, 22, 75, 27],
        'circle-stroke-color': '#ffffff',
        'circle-stroke-width': 2
      }
    })
    map.addLayer({
      id: CLUSTER_COUNT_LAYER_ID,
      type: 'symbol',
      source: THEATER_SOURCE_ID,
      filter: ['has', 'point_count'],
      layout: {
        'text-field': '{point_count_abbreviated}',
        'text-font': ['Noto Sans Regular'],
        'text-size': 12
      },
      paint: { 'text-color': '#ffffff' }
    })
    map.addLayer({
      id: FAVORITE_LAYER_ID,
      type: 'circle',
      source: THEATER_SOURCE_ID,
      filter: ['all', ['!', ['has', 'point_count']], ['==', ['get', 'favorite'], true]],
      paint: {
        'circle-radius': 12,
        'circle-color': 'rgba(255, 255, 255, 0)',
        'circle-stroke-color': '#27272a',
        'circle-stroke-width': 3
      }
    })
    map.addLayer({
      id: THEATER_LAYER_ID,
      type: 'circle',
      source: THEATER_SOURCE_ID,
      filter: ['!', ['has', 'point_count']],
      paint: {
        'circle-radius': 7,
        'circle-color': [
          'match', ['get', 'provider'],
          'ugc', THEATER_PROVIDER_COLORS.ugc,
          'kinepolis', THEATER_PROVIDER_COLORS.kinepolis,
          'pathe', THEATER_PROVIDER_COLORS.pathe,
          'cgr', THEATER_PROVIDER_COLORS.cgr,
          '#52525b'
        ],
        'circle-stroke-color': '#ffffff',
        'circle-stroke-width': 2
      }
    })
    map.on('click', THEATER_LAYER_ID, onTheaterClick)
    map.on('click', CLUSTER_LAYER_ID, onClusterClick)
    map.on('mouseenter', THEATER_LAYER_ID, showPointer)
    map.on('mouseleave', THEATER_LAYER_ID, hidePointer)
    map.on('mouseenter', CLUSTER_LAYER_ID, showPointer)
    map.on('mouseleave', CLUSTER_LAYER_ID, hidePointer)
    layerListenersRegistered = true
    mapReady.value = true
    updateTheaterSource(true)
    updateUserSource()
    map.resize()
  } catch {
    failRuntime()
  }
}

watch(() => props.theaters, () => updateTheaterSource(true))
watch(() => props.favoriteTheaterIds, () => updateTheaterSource(false))
watch(() => props.userPosition, updateUserSource, { deep: true })

onMounted(async () => {
  await nextTick()
  if (destroyed || !mapContainer.value) return
  try {
    const createdMap = new maplibregl.Map({
      container: mapContainer.value,
      style: OPENFREEMAP_STYLE_URL,
      bounds: FRANCE_BOUNDS,
      fitBoundsOptions: { padding: 24 },
      hash: false,
      attributionControl: false,
      cooperativeGestures: true,
      dragRotate: false,
      touchPitch: false,
      locale: {
        'Map.Title': 'Carte des cinémas',
        'NavigationControl.ZoomIn': 'Zoomer',
        'NavigationControl.ZoomOut': 'Dézoomer',
        'FullscreenControl.Enter': 'Afficher la carte en plein écran',
        'FullscreenControl.Exit': 'Quitter le plein écran',
        'CooperativeGesturesHandler.WindowsHelpText': 'Utilisez Ctrl + défilement pour zoomer sur la carte',
        'CooperativeGesturesHandler.MacHelpText': 'Utilisez ⌘ + défilement pour zoomer sur la carte',
        'CooperativeGesturesHandler.MobileHelpText': 'Utilisez deux doigts pour déplacer la carte'
      }
    })
    map = createdMap
    createdMap.addControl(new maplibregl.NavigationControl({ showCompass: false }), 'top-right')
    if (mapLayout.value) {
      const control = new maplibregl.FullscreenControl({ container: mapLayout.value })
      control.on('fullscreenstart', resizeMapAfterFullscreenChange)
      control.on('fullscreenend', resizeMapAfterFullscreenChange)
      fullscreenControl = control
      createdMap.addControl(control, 'top-right')
    }
    createdMap.on('load', onMapLoad)
    createdMap.on('error', onMapError)
    webglCanvas = createdMap.getCanvas()
    webglCanvas.addEventListener('webglcontextlost', onWebglContextLost)
    resizeObserver = new ResizeObserver(() => {
      if (!map || runtimeFailed.value || destroyed) return
      try {
        map.resize()
      } catch {
        failRuntime()
      }
    })
    if (mapLayout.value) resizeObserver.observe(mapLayout.value)
  } catch {
    failRuntime()
  }
})

onBeforeUnmount(() => {
  destroyed = true
  detachRuntime()
})
</script>

<template>
  <div class="border-2 border-ink bg-surface shadow-[6px_6px_0_#27272a]">
    <div v-if="runtimeFailed" class="flex flex-wrap items-center justify-between gap-4 border-2 border-primary bg-primary-soft p-4 text-[#7f1d1d]" role="alert">
      <strong>La carte ne peut pas être affichée.</strong>
      <button type="button" class="inline-flex min-h-11 items-center justify-center border-2 border-ink bg-ink px-[0.8rem] py-[0.6rem] font-mono text-[0.65rem] font-black tracking-[0.06em] text-surface uppercase enabled:hover:bg-primary focus-visible:outline-[3px] focus-visible:outline-offset-[3px] focus-visible:outline-accent disabled:cursor-not-allowed disabled:opacity-50" @click="emit('show-list')">Afficher la liste</button>
    </div>

    <template v-else>
      <div class="border-b-2 border-ink bg-[#f1efe8] p-[0.9rem]">
        <p class="text-[0.82rem] font-[750]" role="status" aria-live="polite">{{ mapTheaterSummary }}</p>
      </div>

      <div ref="mapLayout" class="map-layout grid min-w-0" :class="selectedTheater ? 'min-[900px]:grid-cols-[minmax(0,1fr)_minmax(17rem,22rem)]' : ''">
        <div class="map-panel relative min-w-0">
          <div ref="mapContainer" class="map-canvas h-[clamp(25rem,62vh,43rem)] w-full max-[520px]:h-[26rem]" role="region" aria-label="Carte interactive des cinémas"></div>
          <div v-if="mappedTheaters.length === 0" class="absolute top-4 left-4 z-[2] max-w-[min(22rem,calc(100%_-_5rem))] border-2 border-ink bg-surface p-3 font-extrabold shadow-[3px_3px_0_#27272a]" role="status">
            <p>Aucun cinéma de cette sélection ne peut être placé sur la carte.</p>
            <button type="button" class="mt-3 inline-flex min-h-11 items-center justify-center border-2 border-ink bg-ink px-[0.8rem] py-[0.6rem] font-mono text-[0.65rem] font-black tracking-[0.06em] text-surface uppercase enabled:hover:bg-primary focus-visible:outline-[3px] focus-visible:outline-offset-[3px] focus-visible:outline-accent disabled:cursor-not-allowed disabled:opacity-50" @click="emit('show-list')">Afficher la liste</button>
          </div>
          <div class="flex flex-wrap gap-x-[0.9rem] gap-y-2 border-t-2 border-ink bg-[#f8f7f2] px-[0.8rem] py-[0.65rem] text-xs font-extrabold" aria-label="Légende des exploitants">
            <span v-for="(label, provider) in THEATER_PROVIDER_LABELS" :key="provider" class="inline-flex items-center gap-[0.35rem]"><i class="inline-block size-[0.8rem] rounded-full border-2 border-ink" :style="{ backgroundColor: THEATER_PROVIDER_COLORS[provider] }" aria-hidden="true"></i>{{ label }}</span>
            <span class="inline-flex items-center gap-[0.35rem]"><i class="inline-block size-4 rounded-full border-3 border-double border-ink bg-transparent" aria-hidden="true"></i>Favori</span>
          </div>
          <p class="border-t border-ink bg-surface px-[0.8rem] py-[0.4rem] text-[0.7rem] leading-[1.4]">
            Carte <a class="font-[850] underline decoration-2 underline-offset-[3px] focus-visible:outline-[3px] focus-visible:outline-offset-[3px] focus-visible:outline-accent" href="https://openfreemap.org" target="_blank" rel="noopener noreferrer">OpenFreeMap</a>
            · © <a class="font-[850] underline decoration-2 underline-offset-[3px] focus-visible:outline-[3px] focus-visible:outline-offset-[3px] focus-visible:outline-accent" href="https://openmaptiles.org" target="_blank" rel="noopener noreferrer">OpenMapTiles</a>
            · © <a class="font-[850] underline decoration-2 underline-offset-[3px] focus-visible:outline-[3px] focus-visible:outline-offset-[3px] focus-visible:outline-accent" href="https://www.openstreetmap.org/copyright" target="_blank" rel="noopener noreferrer">contributeurs OpenStreetMap</a>
          </p>
        </div>

        <article v-if="selectedTheater" class="theater-details border-t-2 border-ink bg-[#f1efe8] p-4 min-[900px]:border-t-0 min-[900px]:border-l-2" aria-labelledby="selected-theater-heading">
          <div class="flex items-start justify-between gap-4">
            <div>
              <p class="font-mono text-[0.68rem] font-black uppercase tracking-[0.09em]">{{ THEATER_PROVIDER_LABELS[selectedTheater.provider] }}</p>
              <h3 id="selected-theater-heading" ref="detailHeading" tabindex="-1" class="mt-[0.35rem] text-[clamp(1.35rem,3vw,2rem)] leading-none font-black tracking-[-0.04em] focus-visible:outline-[3px] focus-visible:outline-offset-4 focus-visible:outline-accent">{{ selectedTheater.name }}</h3>
            </div>
            <button type="button" class="min-h-11 p-[0.4rem] font-mono text-[0.65rem] font-black uppercase underline underline-offset-[3px] focus-visible:outline-[3px] focus-visible:outline-offset-[3px] focus-visible:outline-accent" aria-label="Fermer les détails du cinéma" @click="closeDetails">Fermer</button>
          </div>
          <p class="mt-4 leading-[1.55] font-[650]"><template v-if="selectedTheater.address">{{ selectedTheater.address }}, </template>{{ selectedTheater.postal_code }} {{ selectedTheater.city }}</p>
          <p class="mt-4 font-mono text-[0.68rem] font-black uppercase tracking-[0.09em]">{{ favoriteIds.has(selectedTheater.id) ? 'Dans vos favoris' : 'Pas dans vos favoris' }}</p>
          <div class="mt-4 flex flex-wrap items-center gap-x-4 gap-y-3">
            <button type="button" class="inline-flex min-h-11 items-center justify-center border-2 border-ink bg-ink px-[0.8rem] py-[0.6rem] font-mono text-[0.65rem] font-black tracking-[0.06em] text-surface uppercase enabled:hover:bg-primary focus-visible:outline-[3px] focus-visible:outline-offset-[3px] focus-visible:outline-accent disabled:cursor-not-allowed disabled:opacity-50" @click="emit('toggle-favorite', selectedTheater.id)">
              {{ favoriteIds.has(selectedTheater.id) ? 'Retirer des favoris' : 'Ajouter aux favoris' }}
            </button>
            <NuxtLink :to="`/cinema/${encodeURIComponent(selectedTheater.slug)}`" class="font-[850] underline decoration-2 underline-offset-[3px] focus-visible:outline-[3px] focus-visible:outline-offset-[3px] focus-visible:outline-accent">Voir les séances</NuxtLink>
          </div>
        </article>
      </div>
    </template>
  </div>
</template>

<style scoped>
.map-layout:fullscreen,
.map-layout.maplibregl-pseudo-fullscreen {
  width: 100vw;
  height: 100vh;
  background: #fff;
}

.map-layout:fullscreen .map-panel,
.map-layout.maplibregl-pseudo-fullscreen .map-panel {
  display: grid;
  min-height: 0;
  grid-template-rows: minmax(0, 1fr) auto auto;
}

.map-layout:fullscreen .map-canvas,
.map-layout.maplibregl-pseudo-fullscreen .map-canvas {
  height: 100%;
  min-height: 0;
}

.map-layout:fullscreen .theater-details,
.map-layout.maplibregl-pseudo-fullscreen .theater-details {
  overflow: auto;
}

</style>
