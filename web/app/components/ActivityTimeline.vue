<script setup lang="ts">
import type { TheaterActivityItem } from '~/types/api'
import type { AccountActivityItem } from '~/types/cinemaFollows'
import {
  activityDateParts,
  activityFullDate,
  activityHistoryDate,
  activityShortDate,
  activityShowtimesTarget,
  activityTypeLabel,
} from '~/utils/cinemaActivity'
import { cinemaMovieTarget } from '~/utils/cinemaMovieTarget'

const props = defineProps<{
  groups: {
    day: string
    items: (TheaterActivityItem | AccountActivityItem)[]
  }[]
  label: string
  dateHeadingLevel: 2 | 3
  theaterId?: string
}>()

function eventTheaterId(item: TheaterActivityItem | AccountActivityItem) {
  return 'theater' in item ? item.theater.id : props.theaterId!
}
</script>

<template>
  <ol :aria-label="label" class="relative isolate max-w-5xl">
    <li
      v-for="(group, index) in groups"
      :key="group.day"
      :data-activity-day="group.day"
      class="relative pb-10 pl-7 last:pb-2 sm:pb-12 lg:grid lg:grid-cols-[168px_minmax(0,1fr)] lg:gap-10 lg:pl-0"
    >
      <span
        aria-hidden="true"
        class="pointer-events-none absolute bottom-0 left-[5px] w-0.5 bg-ink lg:left-[167px]"
        :class="index === 0 ? 'top-2 lg:top-3' : 'top-0'"
      />
      <span
        aria-hidden="true"
        class="pointer-events-none absolute top-2 left-0 size-3 border-2 border-ink lg:top-3 lg:left-[162px]"
        :class="index === 0 ? 'bg-[#facc15]' : 'bg-canvas'"
      />
      <span
        aria-hidden="true"
        class="pointer-events-none absolute top-[13px] left-3 h-0.5 w-2 bg-ink lg:top-[17px] lg:left-[174px] lg:w-6"
      />
      <component
        :is="`h${dateHeadingLevel}`"
        class="mb-6 min-w-0 [font-family:'Noto_Sans_Variable',sans-serif] text-lg font-black leading-tight tracking-[-0.035em] lg:mb-0 lg:pr-6"
      >
        <time :datetime="group.day">
          <span class="lg:hidden">{{ activityFullDate(group.day) }}</span>
          <span class="hidden lg:block">
            <span class="sr-only">{{ activityHistoryDate(group.day) }}</span>
            <span aria-hidden="true" class="block">
              <span class="block text-5xl leading-none tracking-[-0.065em]">{{
                activityDateParts(group.day).day
              }}</span>
              <span class="mt-2 block text-base">{{
                activityDateParts(group.day).month
              }}</span>
              <span
                class="mt-1 block text-sm font-medium tracking-normal text-zinc-600"
                >{{
                  activityDateParts(group.day).year
                }}</span
              >
            </span>
          </span>
        </time>
      </component>
      <ul class="min-w-0 space-y-7 sm:space-y-8">
        <li
          v-for="item in group.items"
          :key="item.event_id"
          :data-event-id="item.event_id"
          class="flex min-w-0 items-start gap-4 sm:gap-6"
        >
          <NuxtLink
            :to="cinemaMovieTarget(item.movie.slug, eventTheaterId(item))"
            :aria-label="item.movie.title"
            class="block w-18 shrink-0 focus-visible:outline-solid focus-visible:outline-3 focus-visible:outline-offset-4 focus-visible:outline-ink sm:w-24 lg:w-28"
          >
            <PosterImage
              :src="item.movie.poster_url"
              alt=""
              sizes="(min-width: 1024px) 112px, (min-width: 640px) 96px, 72px"
              :reset-key="item.event_id"
              fallback-variant="icon-only"
              fallback-marker="activity"
              :fallback-icon-size="30"
              class="aspect-[2/3] bg-ink/5"
              image-class="size-full object-cover"
              fallback-class="text-zinc-600"
            />
          </NuxtLink>
          <div class="min-w-0 flex-1">
            <NuxtLink
              v-if="'theater' in item"
              :to="`/cinema/${item.theater.slug}?view=activity`"
              :aria-label="item.theater.name"
              class="flex w-fit max-w-full items-start text-sm font-semibold leading-5 text-zinc-600 underline underline-offset-4 hover:text-primary focus-visible:outline-solid focus-visible:outline-3 focus-visible:outline-offset-4 focus-visible:outline-ink"
            >
              <TheaterName
                :name="item.theater.name"
                :provider="item.theater.provider"
                decorative
                class="min-w-0 break-words [overflow-wrap:anywhere]"
              />
            </NuxtLink>
            <component
              :is="`h${dateHeadingLevel + 1}`"
              class="break-words [font-family:'Noto_Sans_Variable',sans-serif] text-[1.375rem] font-black leading-tight tracking-[-0.045em] [overflow-wrap:anywhere] sm:text-[1.75rem]"
              :class="'theater' in item ? 'my-1' : 'mb-1'"
            >
              <NuxtLink
                :to="cinemaMovieTarget(item.movie.slug, eventTheaterId(item))"
                class="block w-fit max-w-full hover:underline underline-offset-4 focus-visible:outline-solid focus-visible:outline-3 focus-visible:outline-offset-4 focus-visible:outline-ink"
                >{{
                  item.movie.title
                }}</NuxtLink
              >
            </component>
            <p class="leading-5">
              <span
                class="inline-block border-l-2 pl-2 align-top text-xs font-bold leading-4"
                :class="item.type === 'return_to_program' ? 'border-accent text-accent' : 'border-ink text-ink'"
                >{{
                  activityTypeLabel(item.type)
                }}</span
              >
            </p>
            <p class="mt-2 text-sm leading-5 text-zinc-600">
              Première séance annoncée ·
              <time :datetime="item.first_screening_date">{{
                activityShortDate(item.first_screening_date)
              }}</time>
            </p>
            <p
              v-if="item.type === 'return_to_program' && item.previous_program_end_date"
              class="mt-0.5 text-sm leading-5 text-zinc-600"
            >
              Programmation précédente · jusqu’au
              <time :datetime="item.previous_program_end_date">{{
                activityShortDate(item.previous_program_end_date)
              }}</time>
            </p>
            <NuxtLink
              v-if="activityShowtimesTarget(item, eventTheaterId(item))"
              :to="activityShowtimesTarget(item, eventTheaterId(item))!"
              class="inline-flex min-h-11 items-center text-sm font-bold underline underline-offset-4 hover:text-primary focus-visible:outline-solid focus-visible:outline-3 focus-visible:outline-offset-4 focus-visible:outline-ink"
              >Voir les séances</NuxtLink
            >
          </div>
        </li>
      </ul>
    </li>
  </ol>
</template>
