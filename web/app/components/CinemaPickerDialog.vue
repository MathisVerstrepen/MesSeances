<script setup lang="ts">
import { ArrowLeft, LoaderCircle, LocateFixed, X } from '@lucide/vue'
import type { Theater } from '~/types/api'
import { publicChain } from '~/utils/cinemaChains'
import { cinemaChainLabel } from '~/utils/cinemaDirectoryChains'
import {
  normalizeDirectoryText,
  type CinemaDirectoryIndex,
  type DirectoryCity,
} from '~/utils/cinemaDirectoryIndex'
import { createCinemaDirectoryLocation } from '~/utils/cinemaDirectoryLocation'
import {
  formatTheaterDistance,
  sortTheatersByDistance,
  type GeographicPoint,
} from '~/utils/theaterDistance'
import { theaterDisplayName } from '~/utils/theaterDisplayName'

const props = defineProps<{
  open: 'where' | 'manage' | null
  index: CinemaDirectoryIndex | null
  selectedIds: readonly string[]
  disabled: boolean
}>()
const emit = defineEmits<{
  close: []
  add: [ids: string[]]
  remove: [id: string]
}>()

const PICKER_RADII = [5, 10, 25, 50] as const
const MAX_ROWS = 40

const dialog = ref<HTMLDialogElement | null>(null)
const step = ref<'where' | 'pick' | 'manage'>('where')
const cityQuery = ref('')
const center = ref<(GeographicPoint & { label: string }) | null>(null)
// A chosen city keeps its cinemas even when they have no coordinates yet.
const chosenCity = ref<DirectoryCity | null>(null)
const radius = ref<(typeof PICKER_RADII)[number]>(10)
const picked = ref<string[]>([])
const location = createCinemaDirectoryLocation(() =>
  import.meta.client ? navigator.geolocation : undefined,
)
const { locationStatus, locationError, userPosition } = location
let previousOverflow: string | null = null

const selected = computed(() => new Set(props.selectedIds))
const selectedTheaters = computed(() =>
  (props.index?.theaters ?? []).filter((theater) =>
    selected.value.has(theater.id),
  ),
)

const cityMatches = computed<DirectoryCity[]>(() => {
  const cities = props.index?.cities ?? []
  const query = normalizeDirectoryText(cityQuery.value)
  if (!query) return cities.filter((city) => city.count >= 4).slice(0, 8)
  if (/^\d{2,5}$/.test(query))
    return cities
      .filter((city) => city.postalCodes.some((code) => code.startsWith(query)))
      .slice(0, 8)
  return cities
    .filter(
      (city) =>
        city.search.startsWith(query) || city.search.includes(` ${query}`),
    )
    .slice(0, 8)
})

const distanceRows = computed(() =>
  center.value && props.index
    ? sortTheatersByDistance(props.index.theaters, center.value).filter(
        (row) => row.distanceKm !== null,
      )
    : [],
)
const unlocatedCityRows = computed(() => {
  const slug = chosenCity.value?.slug
  if (!slug || !props.index) return []
  const located = new Set(distanceRows.value.map((row) => row.theater.id))
  return props.index.theaters
    .filter((theater) => theater.city_slug === slug && !located.has(theater.id))
    .sort((left, right) => left.name.localeCompare(right.name, 'fr-FR'))
    .map((theater) => ({ theater, distanceKm: null, isNearest: false }))
})
const radiusOptions = computed(() =>
  center.value
    ? PICKER_RADII.map((value) => ({
        value,
        count:
          distanceRows.value.filter(
            (row) => (row.distanceKm ?? Number.POSITIVE_INFINITY) <= value,
          ).length + unlocatedCityRows.value.length,
      }))
    : [],
)
const candidateRows = computed(() =>
  [
    ...distanceRows.value.filter(
      (row) => (row.distanceKm ?? Number.POSITIVE_INFINITY) <= radius.value,
    ),
    ...unlocatedCityRows.value,
  ].slice(0, MAX_ROWS),
)
const pickPlace = computed(
  () => center.value?.label ?? chosenCity.value?.name ?? '',
)
const nearestOutside = computed(() =>
  candidateRows.value.length === 0 ? distanceRows.value[0] : undefined,
)

const title = computed(() =>
  step.value === 'manage'
    ? `Mes cinémas (${selectedTheaters.value.length})`
    : step.value === 'pick'
      ? 'Cochez vos salles'
      : 'Où allez-vous au cinéma ?',
)
const kicker = computed(() =>
  step.value === 'manage'
    ? 'Ma sélection'
    : step.value === 'pick'
      ? 'Étape 2 sur 2'
      : 'Étape 1 sur 2',
)

function chainLabel(theater: Theater) {
  return cinemaChainLabel(publicChain(theater.provider))
}

function lockScroll() {
  const root = document.documentElement
  previousOverflow = root.style.overflow
  root.style.overflow = 'hidden'
}

function unlockScroll() {
  if (previousOverflow === null) return
  document.documentElement.style.overflow = previousOverflow
  previousOverflow = null
}

watch(
  () => props.open,
  async (mode) => {
    if (!import.meta.client) return
    if (!mode) {
      location.setNearbyMode(false)
      if (dialog.value?.open) dialog.value.close()
      unlockScroll()
      return
    }
    step.value = mode
    cityQuery.value = ''
    picked.value = []
    await nextTick()
    if (!props.open || !dialog.value) return
    if (!dialog.value.open) {
      dialog.value.showModal()
      lockScroll()
    }
  },
)

watch(userPosition, (position) => {
  if (!position || step.value !== 'where') return
  chosenCity.value = null
  choose({ ...position, label: 'votre position' })
})

function choose(point: (GeographicPoint & { label: string }) | null) {
  center.value = point
  radius.value = 10
  picked.value = []
  step.value = 'pick'
}

function chooseCity(city: DirectoryCity) {
  chosenCity.value = city
  choose(
    city.latitude === null || city.longitude === null
      ? null
      : {
          latitude: city.latitude,
          longitude: city.longitude,
          label: city.name,
        },
  )
}

function usePosition() {
  if (locationStatus.value === 'idle') location.setNearbyMode(true)
  else location.retry()
}

function togglePicked(id: string) {
  picked.value = picked.value.includes(id)
    ? picked.value.filter((value) => value !== id)
    : [...picked.value, id]
}

function confirm() {
  if (picked.value.length === 0 || props.disabled) return
  emit('add', [...picked.value])
}

function startAdding() {
  step.value = 'where'
  cityQuery.value = ''
  picked.value = []
}

function onBackdrop(event: MouseEvent) {
  if (event.target === dialog.value) emit('close')
}

onBeforeUnmount(() => {
  location.dispose()
  unlockScroll()
})
</script>

<template>
  <dialog
    ref="dialog"
    aria-labelledby="cinema-picker-title"
    class="fixed inset-0 m-0 h-dvh max-h-none w-full max-w-none overflow-hidden border-0 bg-transparent p-0 text-ink backdrop:bg-black/60 open:flex open:items-end open:justify-end lg:open:items-stretch"
    @cancel.prevent="emit('close')"
    @click="onBackdrop"
  >
    <div
      class="flex h-[calc(100dvh-1rem)] min-h-0 w-full flex-col overflow-hidden border-2 border-b-0 border-ink bg-[#f8f7f2] shadow-[0_-8px_0_#27272a] lg:h-full lg:w-[32rem] lg:border-y-0 lg:border-r-0 lg:shadow-[-8px_0_0_#27272a]"
    >
      <header
        class="flex shrink-0 items-center gap-3 border-b-2 border-ink bg-surface p-4 sm:px-6"
      >
        <div class="min-w-0 flex-1">
          <p
            class="font-mono text-[0.62rem] font-black tracking-[0.14em] text-muted uppercase"
          >
            {{ kicker }}
          </p>
          <h2
            id="cinema-picker-title"
            class="mt-1 text-xl leading-tight font-black tracking-[-0.035em]"
          >
            {{ title }}
          </h2>
        </div>
        <button
          type="button"
          class="editorial-button-outline size-11 shrink-0 p-0"
          aria-label="Fermer"
          @click="emit('close')"
        >
          <X :size="20" aria-hidden="true" />
        </button>
      </header>

      <div
        v-if="step === 'where'"
        class="min-h-0 flex-1 overflow-y-auto overscroll-contain px-4 py-5 sm:px-6"
      >
        <p class="text-[0.95rem] leading-relaxed font-semibold">
          Indiquez où vous allez au cinéma : nous vous proposons les salles
          autour.
        </p>
        <button
          type="button"
          class="editorial-button mt-4 w-full"
          :disabled="locationStatus === 'requesting'"
          @click="usePosition"
        >
          <LoaderCircle
            v-if="locationStatus === 'requesting'"
            :size="18"
            class="animate-spin motion-reduce:animate-none"
            aria-hidden="true"
          />
          <LocateFixed v-else :size="18" aria-hidden="true" />
          {{
            locationStatus === 'requesting' ? 'Localisation…' : 'Utiliser ma position'
          }}
        </button>
        <p
          v-if="locationError"
          class="editorial-alert mt-3 p-3 font-bold"
          role="alert"
        >
          {{ locationError }}
        </p>
        <p
          class="my-5 text-center font-mono text-[0.62rem] font-black tracking-[0.14em] text-muted uppercase"
        >
          ou
        </p>
        <label for="cinema-picker-city" class="block text-sm font-extrabold">
          Ville ou code postal
        </label>
        <input
          id="cinema-picker-city"
          v-model="cityQuery"
          type="search"
          autocomplete="off"
          placeholder="Ex. Nantes, 69003"
          class="editorial-field mt-1.5 text-base font-bold"
        >
        <ul
          v-if="cityMatches.length"
          class="mt-3 border-2 border-ink bg-surface"
          aria-label="Villes proposées"
        >
          <li
            v-for="city in cityMatches"
            :key="city.slug"
            class="border-t-2 border-line first:border-t-0"
          >
            <button
              type="button"
              class="flex min-h-11 w-full items-center justify-between gap-3 px-4 py-2.5 text-left hover:bg-[#fff3c4]"
              @click="chooseCity(city)"
            >
              <span class="min-w-0">
                <strong class="font-extrabold">{{ city.name }}</strong>
                <span
                  v-if="city.departmentName"
                  class="ml-2 text-sm font-semibold text-muted"
                  >{{
                    city.departmentName
                  }}</span
                >
              </span>
              <span
                class="shrink-0 font-mono text-[0.65rem] font-bold text-muted uppercase"
              >
                {{ city.count }}
                cinéma{{ city.count > 1 ? 's' : '' }}
              </span>
            </button>
          </li>
        </ul>
        <p v-else class="mt-3 text-sm font-bold">
          Aucune ville trouvée. Essayez un code postal ou une ville voisine.
        </p>
      </div>

      <template v-else-if="step === 'pick'">
        <div
          class="shrink-0 border-b-2 border-ink bg-surface px-4 pt-4 sm:px-6"
        >
          <p
            class="flex flex-wrap items-center gap-x-2 text-sm font-bold"
            :class="radiusOptions.length ? '' : 'pb-3'"
          >
            <span
              >{{ center ? 'Autour de' : 'À' }}
              <strong>{{ pickPlace }}</strong></span
            >
            <button
              type="button"
              class="inline-flex min-h-11 items-center gap-1 underline decoration-2 underline-offset-4 hover:text-primary"
              @click="startAdding"
            >
              <ArrowLeft :size="14" aria-hidden="true" />
              Changer
            </button>
          </p>
          <div
            v-if="radiusOptions.length"
            role="group"
            aria-label="Rayon de recherche"
            class="mt-1 mb-4 flex flex-wrap gap-1.5"
          >
            <button
              v-for="option in radiusOptions"
              :key="option.value"
              type="button"
              class="inline-flex min-h-10 items-center gap-2 border-2 border-ink px-3 text-sm font-extrabold"
              :class="option.value === radius ? 'bg-ink text-white' : 'bg-surface hover:bg-[#fff3c4]'"
              :aria-pressed="option.value === radius"
              @click="radius = option.value"
            >
              {{ option.value }} km
              <span class="font-mono text-[0.65rem] opacity-75">{{
                option.count
              }}</span>
            </button>
          </div>
        </div>
        <div
          class="min-h-0 flex-1 overflow-y-auto overscroll-contain bg-surface"
        >
          <ul v-if="candidateRows.length" aria-label="Cinémas proposés">
            <li
              v-for="row in candidateRows"
              :key="row.theater.id"
              class="border-t-2 border-line first:border-t-0"
            >
              <label
                class="flex cursor-pointer items-center gap-3 px-4 py-3 hover:bg-subtle sm:px-6"
                :class="selected.has(row.theater.id) ? 'cursor-default' : ''"
              >
                <input
                  type="checkbox"
                  class="size-5 shrink-0 accent-[var(--color-accent)]"
                  :checked="selected.has(row.theater.id) || picked.includes(row.theater.id)"
                  :disabled="selected.has(row.theater.id)"
                  @change="togglePicked(row.theater.id)"
                >
                <span class="min-w-0 flex-1">
                  <span class="block font-black [overflow-wrap:anywhere]">
                    <TheaterName
                      :name="theaterDisplayName(row.theater)"
                      :provider="row.theater.provider"
                    />
                  </span>
                  <span class="mt-0.5 block text-xs font-semibold text-muted">
                    {{ chainLabel(row.theater) }}
                    ·
                    {{ row.theater.postal_code }} {{ row.theater.city
                    }}<template v-if="selected.has(row.theater.id)">
                      · déjà dans mes cinémas</template
                    >
                  </span>
                </span>
                <span class="shrink-0 font-mono text-xs font-black">{{
                  formatTheaterDistance(row.distanceKm)
                }}</span>
              </label>
            </li>
          </ul>
          <div v-else class="p-4 sm:px-6">
            <p class="font-bold">Aucune salle dans ce rayon.</p>
            <p
              v-if="nearestOutside"
              class="mt-1 text-sm font-semibold text-muted"
            >
              La plus proche, {{ theaterDisplayName(nearestOutside.theater) }},
              est à {{ formatTheaterDistance(nearestOutside.distanceKm) }}.
            </p>
          </div>
        </div>
        <footer
          class="shrink-0 border-t-2 border-ink bg-[#f1efe8] px-4 pt-4 pb-[calc(1rem+env(safe-area-inset-bottom))] sm:px-6"
        >
          <button
            type="button"
            class="editorial-button w-full"
            :disabled="picked.length === 0 || disabled"
            @click="confirm"
          >
            {{
              picked.length
                ? `Ajouter ${picked.length} cinéma${picked.length > 1 ? 's' : ''}`
                : 'Cochez au moins une salle'
            }}
          </button>
        </footer>
      </template>

      <template v-else>
        <div
          class="min-h-0 flex-1 overflow-y-auto overscroll-contain bg-surface"
        >
          <ul v-if="selectedTheaters.length" aria-label="Mes cinémas">
            <li
              v-for="theater in selectedTheaters"
              :key="theater.id"
              class="flex items-center gap-3 border-t-2 border-line px-4 py-3 first:border-t-0 sm:px-6"
            >
              <span class="min-w-0 flex-1">
                <span class="block font-black [overflow-wrap:anywhere]">
                  <TheaterName
                    :name="theaterDisplayName(theater)"
                    :provider="theater.provider"
                  />
                </span>
                <span class="mt-0.5 block text-xs font-semibold text-muted">
                  {{ chainLabel(theater) }}
                  · {{ theater.postal_code }}
                  {{ theater.city }}
                </span>
              </span>
              <button
                type="button"
                class="editorial-button-outline shrink-0"
                :disabled="disabled"
                :aria-label="`Retirer ${theaterDisplayName(theater)} de mes cinémas`"
                @click="emit('remove', theater.id)"
              >
                Retirer
              </button>
            </li>
          </ul>
          <p v-else class="p-4 font-bold sm:px-6">
            Aucun cinéma sélectionné pour l’instant.
          </p>
        </div>
        <footer
          class="shrink-0 border-t-2 border-ink bg-[#f1efe8] px-4 pt-4 pb-[calc(1rem+env(safe-area-inset-bottom))] sm:px-6"
        >
          <button
            type="button"
            class="editorial-button w-full"
            @click="startAdding"
          >
            Ajouter des cinémas près d’un lieu
          </button>
        </footer>
      </template>
    </div>
  </dialog>
</template>
