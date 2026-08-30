<script setup lang="ts">
import { CalendarDays } from '@lucide/vue'
import { addCalendarDays, formatDateLabel } from '~/utils/date'

const props = defineProps<{
  selectedDate: string
  availableDates: string[]
  today: string
}>()

const emit = defineEmits<{
  select: [date: string]
}>()

const tomorrowDate = computed(() => addCalendarDays(props.today, 1))
</script>

<template>
  <div class="flex w-full min-w-0 items-center gap-2 sm:flex-1 sm:gap-3">
    <ShowtimeDatePicker :selected-date="selectedDate" :allowed-dates="availableDates" @select="emit('select', $event)">
      <template #trigger="{ isOpen, triggerLabel }">
        <button
          type="button"
          class="calendar-trigger grid size-9 shrink-0 place-items-center border-2 border-ink bg-[#ffcf3f] hover:bg-highlight focus-visible:z-[1] focus-visible:outline-[3px] focus-visible:outline-offset-2 focus-visible:outline-accent sm:size-10"
          :class="selectedDate !== today && selectedDate !== tomorrowDate ? 'bg-ink text-surface shadow-[inset_0_-3px_0_var(--color-highlight)]' : ''"
          :aria-label="triggerLabel"
          :aria-expanded="isOpen"
        >
          <CalendarDays :size="18" aria-hidden="true" />
        </button>
      </template>
    </ShowtimeDatePicker>
    <div class="min-w-0 flex-1 overflow-hidden sm:overflow-x-auto [scrollbar-width:thin]">
      <div class="grid min-w-0 grid-cols-2 sm:flex sm:min-w-max" aria-label="Choisir une date">
        <button
          v-for="option in availableDates"
          :key="option"
          type="button"
          class="date-button h-9 w-full border-2 border-r-0 border-ink px-2 font-mono text-[10px] font-black uppercase capitalize tracking-[0.04em] last:border-r-2 focus-visible:z-[1] focus-visible:outline-[3px] focus-visible:outline-offset-2 focus-visible:outline-accent sm:h-10 sm:w-auto sm:px-4 sm:tracking-[0.06em]"
          :class="[
            selectedDate === option ? 'bg-ink text-surface shadow-[inset_0_-3px_0_var(--color-highlight)]' : 'bg-surface hover:bg-[#e8e6de]',
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
