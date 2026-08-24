<script setup lang="ts">
import { CalendarDays } from '@lucide/vue'
import { VueDatePicker } from '@vuepic/vue-datepicker'
import { fr } from 'date-fns/locale/fr'
import '@vuepic/vue-datepicker/dist/main.css'
import { formatDateLabel, formatLongDate } from '~/utils/date'

const props = defineProps<{
  selectedDate: string
  availableDates: string[]
  today: string
}>()

const emit = defineEmits<{
  select: [date: string]
}>()

const isCalendarOpen = ref(false)

function dateFromCalendarDate(value: string): Date | null {
  const [year, month, day] = value.split('-').map(Number)
  if (!year || !month || !day) return null
  return new Date(year, month - 1, day, 12)
}

function calendarDateFromDate(value: Date): string {
  return [value.getFullYear(), String(value.getMonth() + 1).padStart(2, '0'), String(value.getDate()).padStart(2, '0')].join('-')
}

const allowedDateValues = computed(() => props.availableDates.map(dateFromCalendarDate).filter((value): value is Date => value !== null))
const pickerDate = computed<Date | null>({
  get: () => dateFromCalendarDate(props.selectedDate),
  set: (value) => {
    if (!value) return
    const selectedDate = calendarDateFromDate(value)
    if (props.availableDates.includes(selectedDate)) emit('select', selectedDate)
  }
})
const tomorrowDate = computed(() => {
  const today = dateFromCalendarDate(props.today)
  if (!today) return ''
  today.setDate(today.getDate() + 1)
  return calendarDateFromDate(today)
})
const calendarAriaLabels = {
  menu: 'Calendrier des dates disponibles',
  input: 'Choisir une autre date',
  calendarIcon: 'Ouvrir le calendrier',
  prevMonth: 'Mois précédent',
  nextMonth: 'Mois suivant',
  prevYear: 'Année précédente',
  nextYear: 'Année suivante',
  openMonthsOverlay: 'Choisir un mois',
  openYearsOverlay: 'Choisir une année',
  monthPicker: (overlay: boolean) => overlay ? 'Fermer le choix du mois' : 'Ouvrir le choix du mois',
  yearPicker: (overlay: boolean) => overlay ? 'Fermer le choix de l’année' : 'Ouvrir le choix de l’année',
  day: ({ value }: { value: Date }) => `Choisir ${formatLongDate(calendarDateFromDate(value))}`
}
</script>

<template>
  <div class="flex w-full min-w-0 items-center gap-2 sm:flex-1 sm:gap-3">
    <VueDatePicker
      v-model="pickerDate"
      class="editorial-datepicker shrink-0"
      :allowed-dates="allowedDateValues"
      :aria-labels="calendarAriaLabels"
      :locale="fr"
      :time-config="{ enableTimePicker: false }"
      :transitions="false"
      :floating="{ arrow: false, offset: 6 }"
      :ui="{ menu: 'editorial-calendar-menu' }"
      teleport="body"
      auto-apply
      arrow-navigation
      prevent-min-max-navigation
      @open="isCalendarOpen = true"
      @closed="isCalendarOpen = false"
    >
      <template #trigger>
        <button
          type="button"
          class="calendar-trigger grid size-9 shrink-0 place-items-center border-2 border-ink bg-[#ffcf3f] hover:bg-highlight sm:size-10"
          :class="selectedDate !== today && selectedDate !== tomorrowDate ? 'date-button--active' : ''"
          :aria-label="`Choisir une autre date. Date actuelle : ${formatLongDate(selectedDate)}`"
          :aria-expanded="isCalendarOpen"
        >
          <CalendarDays :size="18" aria-hidden="true" />
        </button>
      </template>
    </VueDatePicker>
    <div class="min-w-0 flex-1 overflow-hidden sm:overflow-x-auto [scrollbar-width:thin]">
      <div class="grid min-w-0 grid-cols-2 sm:flex sm:min-w-max" aria-label="Choisir une date">
        <button
          v-for="option in availableDates"
          :key="option"
          type="button"
          class="date-button h-9 w-full border-2 border-r-0 border-ink bg-surface px-2 font-mono text-[10px] font-black uppercase capitalize tracking-[0.04em] last:border-r-2 sm:h-10 sm:w-auto sm:px-4 sm:tracking-[0.06em]"
          :class="[
            selectedDate === option ? 'date-button--active' : 'hover:bg-[#e8e6de]',
            option !== today && option !== tomorrowDate ? 'hidden sm:block' : '',
            option === tomorrowDate ? 'max-sm:border-r-2' : ''
          ]"
          :aria-pressed="selectedDate === option"
          @click="emit('select', option)"
        >
          {{ formatDateLabel(option, today) }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.date-button--active {
  background: #27272a;
  color: #fff;
  box-shadow: inset 0 -3px 0 var(--color-highlight);
}

.date-button:focus-visible,
.calendar-trigger:focus-visible {
  z-index: 1;
  outline: 3px solid #1f6f78;
  outline-offset: 2px;
}

.editorial-datepicker {
  min-width: 0;
}

:global(.editorial-calendar-menu) {
  --dp-font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  --dp-border-radius: 0;
  --dp-cell-border-radius: 0;
  --dp-background-color: #f8f7f2;
  --dp-text-color: #27272a;
  --dp-hover-color: #e8e6de;
  --dp-hover-text-color: #27272a;
  --dp-hover-icon-color: #27272a;
  --dp-primary-color: #27272a;
  --dp-primary-text-color: #fff;
  --dp-secondary-color: #71717a;
  --dp-border-color: #27272a;
  --dp-menu-border-color: #27272a;
  --dp-border-color-hover: #27272a;
  --dp-border-color-focus: #27272a;
  --dp-disabled-color: #e8e6de;
  --dp-disabled-color-text: #71717a;
  --dp-icon-color: #27272a;
  --dp-menu-min-width: 19rem;
  --dp-font-size: 0.78rem;
  --dp-common-transition: none;
  --dp-animation-duration: 0s;
  border-width: 2px;
  box-shadow: 6px 6px 0 #27272a;
}

:global(.editorial-calendar-menu .dp__calendar_header_item),
:global(.editorial-calendar-menu .dp__month_year_select) {
  font-size: 0.65rem;
  font-weight: 900;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

:global(.editorial-calendar-menu .dp__active_date) {
  box-shadow: inset 0 -3px 0 var(--color-highlight);
}

:global(.editorial-calendar-menu .dp__today) {
  border: 2px solid #991b1b;
}
</style>
