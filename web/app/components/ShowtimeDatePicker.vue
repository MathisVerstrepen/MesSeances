<script setup lang="ts">
import { fr } from 'date-fns/locale/fr'
import { calendarDateFromDate, dateFromCalendarDate, formatLongDate } from '~/utils/date'

const props = withDefaults(defineProps<{
  selectedDate: string
  allowedDates: string[]
  disabled?: boolean
  centered?: boolean
  menuClass?: string
}>(), {
  disabled: false,
  centered: false,
  menuClass: 'editorial-calendar-menu'
})

const emit = defineEmits<{
  select: [date: string]
  open: []
  closed: []
  menuMounted: [menu: HTMLElement]
  menuUnmounted: [menu: HTMLElement]
}>()

const isOpen = ref(false)

const allowedDateValues = computed(() => props.allowedDates.map(dateFromCalendarDate).filter((value): value is Date => value !== null))
const pickerDate = computed<Date | null>({
  get: () => props.disabled ? null : dateFromCalendarDate(props.selectedDate),
  set: (value) => {
    if (!value) return
    const selectedDate = calendarDateFromDate(value)
    if (props.allowedDates.includes(selectedDate)) emit('select', selectedDate)
  }
})
const triggerLabel = computed(() => props.disabled
  ? 'Choisir une autre date. Aucune date disponible.'
  : `Choisir une autre date. Date actuelle : ${formatLongDate(props.selectedDate)}`)
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

function handleOpen() {
  isOpen.value = true
  emit('open')
}

function handleClosed() {
  isOpen.value = false
  emit('closed')
}
</script>

<template>
  <DeferredVueDatePicker
    v-model="pickerDate"
    class="min-w-0 shrink-0"
    :allowed-dates="allowedDateValues"
    :aria-labels="calendarAriaLabels"
    :disabled="disabled"
    :locale="fr"
    :time-config="{ enableTimePicker: false }"
    :transitions="false"
    :floating="{ arrow: false, offset: 6 }"
    :ui="{ menu: menuClass }"
    :centered="centered"
    teleport="body"
    auto-apply
    arrow-navigation
    prevent-min-max-navigation
    @open="handleOpen"
    @closed="handleClosed"
    @menu-mounted="emit('menuMounted', $event)"
    @menu-unmounted="emit('menuUnmounted', $event)"
  >
    <template #trigger>
      <slot name="trigger" :is-open="isOpen" :trigger-label="triggerLabel" :disabled="disabled" />
    </template>
  </DeferredVueDatePicker>
</template>

<style scoped>
:global(.dp--menu.editorial-calendar-menu) {
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

:global(.editorial-calendar-menu .dp--calendar-header-item),
:global(.editorial-calendar-menu .dp--month-year-select) {
  font-size: 0.65rem;
  font-weight: 900;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

:global(.editorial-calendar-menu .dp--active) {
  box-shadow: inset 0 -3px 0 var(--color-highlight);
}

:global(.editorial-calendar-menu .dp--today) {
  border: 2px solid #991b1b;
}
</style>
