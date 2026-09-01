<script setup lang="ts">
import { CalendarDays } from '@lucide/vue'
import { addCalendarDays, formatDateLabel } from '~/utils/date'

type DateTabsMode = 'today-tomorrow' | 'all'
type CalendarPosition = 'start' | 'end'

const props = withDefaults(defineProps<{
  selectedDate: string
  availableDates: string[]
  today: string
  disabled?: boolean
  centered?: boolean
  mobileTabs?: DateTabsMode
  desktopTabs?: DateTabsMode
  calendarPosition?: CalendarPosition
  stretchTabs?: boolean
}>(), {
  mobileTabs: 'today-tomorrow',
  desktopTabs: 'all',
  calendarPosition: 'start',
  stretchTabs: false
})

const emit = defineEmits<{
  select: [date: string]
  pickerOpen: []
  pickerClosed: []
  menuMounted: [menu: HTMLElement]
  menuUnmounted: [menu: HTMLElement]
}>()

const tomorrowDate = computed(() => addCalendarDays(props.today, 1))
const calendarTrigger = ref<HTMLButtonElement | null>(null)
const todayAndTomorrowDates = computed(() => [props.today, tomorrowDate.value])
const layoutOrder = computed<Array<'calendar' | 'tabs'>>(() => props.calendarPosition === 'end' ? ['tabs', 'calendar'] : ['calendar', 'tabs'])

function datesForMode(mode: DateTabsMode) {
  return mode === 'all' ? props.availableDates : todayAndTomorrowDates.value
}

const dateGroups = computed(() => [
  {
    key: 'mobile',
    mode: props.mobileTabs,
    dates: datesForMode(props.mobileTabs)
  },
  {
    key: 'desktop',
    mode: props.desktopTabs,
    dates: datesForMode(props.desktopTabs)
  }
].map(group => ({
  ...group,
  rovingDate: group.dates.includes(props.selectedDate) ? props.selectedDate : group.dates[0]
})))

function selectAdjacentDate(event: KeyboardEvent, index: number, dates: string[]) {
  if (dates.length === 0) return

  let nextIndex: number | undefined
  if (event.key === 'ArrowRight') nextIndex = (index + 1) % dates.length
  else if (event.key === 'ArrowLeft') nextIndex = (index - 1 + dates.length) % dates.length
  else if (event.key === 'Home') nextIndex = 0
  else if (event.key === 'End') nextIndex = dates.length - 1
  if (nextIndex === undefined) return

  const nextDate = dates[nextIndex]
  if (!nextDate) return
  event.preventDefault()
  emit('select', nextDate)
  const currentTarget = event.currentTarget
  if (!(currentTarget instanceof HTMLElement)) return
  const tabs = currentTarget.parentElement?.querySelectorAll<HTMLButtonElement>('[role="tab"]')
  nextTick(() => tabs?.[nextIndex]?.focus())
}

defineExpose({
  getTriggerElement: () => calendarTrigger.value
})
</script>

<template>
  <div class="flex w-full min-w-0 items-center gap-2 sm:flex-1 sm:gap-3">
    <template v-for="item in layoutOrder" :key="item">
      <ShowtimeDatePicker
        v-if="item === 'calendar'"
        :selected-date="selectedDate"
        :allowed-dates="availableDates"
        :disabled="disabled"
        :centered="centered"
        @select="emit('select', $event)"
        @open="emit('pickerOpen')"
        @closed="emit('pickerClosed')"
        @menu-mounted="emit('menuMounted', $event)"
        @menu-unmounted="emit('menuUnmounted', $event)"
      >
        <template #trigger="{ isOpen, triggerLabel, disabled: pickerDisabled }">
          <button
            ref="calendarTrigger"
            type="button"
            class="calendar-trigger grid size-9 shrink-0 place-items-center border-2 border-ink bg-[#ffcf3f] hover:bg-highlight focus-visible:z-[1] focus-visible:outline-[3px] focus-visible:outline-offset-2 focus-visible:outline-accent sm:size-10"
            :class="selectedDate !== today && selectedDate !== tomorrowDate ? 'bg-ink text-surface shadow-[inset_0_-3px_0_var(--color-highlight)]' : ''"
            :disabled="pickerDisabled"
            :aria-label="triggerLabel"
            :aria-expanded="isOpen"
          >
            <CalendarDays :size="18" aria-hidden="true" />
          </button>
        </template>
      </ShowtimeDatePicker>
      <div
        v-else
        class="min-w-0 flex-1 [scrollbar-width:thin]"
        :class="[
          mobileTabs === 'all' ? 'overflow-x-auto' : 'overflow-hidden',
          desktopTabs === 'all' ? 'sm:overflow-x-auto' : 'sm:overflow-hidden'
        ]"
      >
        <div
          v-for="group in dateGroups"
          :key="group.key"
          :class="group.key === 'mobile'
            ? [group.mode === 'all' ? 'flex min-w-max' : 'grid min-w-0 grid-cols-2', 'sm:hidden']
            : stretchTabs && group.mode === 'today-tomorrow'
              ? 'hidden min-w-0 grid-cols-2 sm:grid'
              : 'hidden min-w-max sm:flex'"
          role="tablist"
          aria-label="Choisir une date"
        >
          <button
            v-for="(option, index) in group.dates"
            :key="option"
            type="button"
            role="tab"
            class="date-button h-9 shrink-0 border-2 border-r-0 border-ink px-2 font-mono text-[10px] font-black uppercase capitalize tracking-[0.04em] last:border-r-2 focus-visible:z-[1] focus-visible:outline-[3px] focus-visible:outline-offset-2 focus-visible:outline-accent sm:h-10 sm:px-4 sm:tracking-[0.06em]"
            :class="[
              selectedDate === option ? 'bg-ink text-surface shadow-[inset_0_-3px_0_var(--color-highlight)]' : 'bg-surface hover:bg-[#e8e6de]',
              group.mode === 'today-tomorrow' && (group.key === 'mobile' || stretchTabs) ? 'w-full' : 'w-auto'
            ]"
            :aria-pressed="selectedDate === option"
            :aria-selected="selectedDate === option"
            :tabindex="group.rovingDate === option ? 0 : -1"
            @click="emit('select', option)"
            @keydown="selectAdjacentDate($event, index, group.dates)"
          >
            {{ formatDateLabel(option, today) }}
          </button>
        </div>
      </div>
    </template>
  </div>
</template>
