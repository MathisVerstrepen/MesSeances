import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const filmsSource = await readFile(new URL('../app/pages/films.vue', import.meta.url), 'utf8')
const searchSource = await readFile(new URL('../app/pages/recherche.vue', import.meta.url), 'utf8')
const showtimeSource = await readFile(new URL('../app/components/ShowtimeDatePicker.vue', import.meta.url), 'utf8')
const dateBarSource = await readFile(new URL('../app/components/ShowtimeDateBar.vue', import.meta.url), 'utf8')
const filmSource = await readFile(new URL('../app/pages/film/[slug].vue', import.meta.url), 'utf8')
const cinemaSource = await readFile(new URL('../app/pages/cinema/[slug].vue', import.meta.url), 'utf8')
const planningSource = await readFile(new URL('../app/pages/planning.vue', import.meta.url), 'utf8')
const deferredSource = await readFile(new URL('../app/components/DeferredVueDatePicker.vue', import.meta.url), 'utf8')
const loaderSource = await readFile(new URL('../app/utils/vueDatePickerLoader.ts', import.meta.url), 'utf8')

const routeAndSharedSources = [filmsSource, searchSource, showtimeSource, dateBarSource, filmSource, cinemaSource, planningSource]

test('route and shared date controls have no static Vuepic JavaScript or CSS imports', () => {
  for (const source of routeAndSharedSources) {
    assert.doesNotMatch(source, /import\s+(?:[^('].*?\s+from\s+)?['"]@vuepic\/vue-datepicker/)
    assert.doesNotMatch(source, /import\s+['"]@vuepic\/vue-datepicker\/dist\/main\.css['"]/)
  }
})

test('Vuepic JavaScript and CSS share one deferred loader boundary', () => {
  assert.match(loaderSource, /import\('@vuepic\/vue-datepicker'\)/)
  assert.match(loaderSource, /import\('@vuepic\/vue-datepicker\/dist\/main\.css'\)/)
  assert.match(loaderSource, /Promise\.all\(/)
})

test('films only mounts deferred pickers inside custom and range modes', () => {
  assert.match(filmsSource, /v-if="draftFilters\.dateMode === 'custom'"[\s\S]*?<DeferredVueDatePicker[\s\S]*?\bactive\b/)
  assert.match(filmsSource, /v-else-if="draftFilters\.dateMode === 'range'"[\s\S]*?<DeferredVueDatePicker[\s\S]*?\bactive\b/)
  assert.equal((filmsSource.match(/<DeferredVueDatePicker/g) ?? []).length, 2)
})

test('custom-trigger pickers render their trigger before activation then load and open', () => {
  assert.match(deferredSource, /v-else-if="\$slots\.trigger"/)
  assert.match(deferredSource, /@click="activate"/)
  assert.match(deferredSource, /await ensureLoaded\(\)[\s\S]*?await nextTick\(\)[\s\S]*?datePicker\.value\?\.openMenu\(\)/)
  assert.match(deferredSource, /inputRoot\.querySelector<HTMLElement>\('button'\)\?\.focus\(\{ preventScroll: true \}\)/)
  assert.match(searchSource, /<ShowtimeDateBar/)
  assert.match(dateBarSource, /<ShowtimeDatePicker[\s\S]*?<template #trigger/)
  assert.match(showtimeSource, /<DeferredVueDatePicker[\s\S]*?<template #trigger>/)
})

test('single-date showtime surfaces share the canonical date bar', () => {
  assert.equal((filmSource.match(/<ShowtimeDateBar/g) ?? []).length, 2)
  assert.equal((cinemaSource.match(/<ShowtimeDateBar/g) ?? []).length, 1)
  assert.equal((planningSource.match(/<ShowtimeDateBar/g) ?? []).length, 1)
  assert.equal((searchSource.match(/<ShowtimeDateBar/g) ?? []).length, 1)
  for (const source of [filmSource, searchSource]) assert.doesNotMatch(source, /<ShowtimeDatePicker/)
  assert.match(dateBarSource, /role="tablist"/)
  assert.match(dateBarSource, /event\.key === 'ArrowRight'/)
  assert.match(dateBarSource, /selectAdjacentDate\(event: KeyboardEvent, index: number, dates: string\[\]\)/)
  assert.match(dateBarSource, /@keydown="selectAdjacentDate\(\$event, index, group\.dates\)"/)
  assert.match(dateBarSource, /getTriggerElement/)
  assert.match(showtimeSource, /centered\?: boolean/)
  assert.match(showtimeSource, /menuMounted: \[menu: HTMLElement\]/)
})

test('date bar limits navigation and roving tabindex to each explicit responsive tab mode', () => {
  assert.match(dateBarSource, /type DateTabsMode = 'today-tomorrow' \| 'all'/)
  assert.match(dateBarSource, /mobileTabs: 'today-tomorrow'/)
  assert.match(dateBarSource, /desktopTabs: 'all'/)
  assert.match(dateBarSource, /datesForMode\(props\.mobileTabs\)/)
  assert.match(dateBarSource, /datesForMode\(props\.desktopTabs\)/)
  assert.match(dateBarSource, /group\.dates\.includes\(props\.selectedDate\) \? props\.selectedDate : group\.dates\[0\]/)
  assert.match(dateBarSource, /mobileTabs === 'all' \? 'overflow-x-auto' : 'overflow-hidden'/)
  assert.match(dateBarSource, /desktopTabs === 'all' \? 'sm:overflow-x-auto' : 'sm:overflow-hidden'/)
  assert.match(dateBarSource, /:allowed-dates="availableDates"/)
})

test('today and tomorrow tabs stay selectable independently from screening availability', () => {
  assert.match(dateBarSource, /const tomorrowDate = computed\(\(\) => addCalendarDays\(props\.today, 1\)\)/)
  assert.match(dateBarSource, /const todayAndTomorrowDates = computed\(\(\) => \[props\.today, tomorrowDate\.value\]\)/)
  assert.match(dateBarSource, /return mode === 'all' \? props\.availableDates : todayAndTomorrowDates\.value/)
  assert.doesNotMatch(dateBarSource, /props\.availableDates\.filter\(date => date === props\.today \|\| date === tomorrowDate\.value\)/)
  assert.doesNotMatch(dateBarSource, /:disabled="[^"]*(?:option|group\.dates)/)
  assert.doesNotMatch(dateBarSource, /aria-disabled/)
  assert.match(dateBarSource, /:aria-pressed="selectedDate === option"/)
  assert.match(dateBarSource, /:aria-selected="selectedDate === option"/)
  assert.match(dateBarSource, /:tabindex="group\.rovingDate === option \? 0 : -1"/)
  assert.match(dateBarSource, /@click="emit\('select', option\)"/)
})

test('date bar preserves two-tab keyboard wrapping and post-selection focus', () => {
  assert.match(dateBarSource, /event\.key === 'ArrowRight'\) nextIndex = \(index \+ 1\) % dates\.length/)
  assert.match(dateBarSource, /event\.key === 'ArrowLeft'\) nextIndex = \(index - 1 \+ dates\.length\) % dates\.length/)
  assert.match(dateBarSource, /event\.key === 'Home'\) nextIndex = 0/)
  assert.match(dateBarSource, /event\.key === 'End'\) nextIndex = dates\.length - 1/)
  assert.match(dateBarSource, /emit\('select', nextDate\)[\s\S]*?nextTick\(\(\) => tabs\?\.\[nextIndex\]\?\.focus\(\)\)/)
})

test('search accepts unavailable quick dates without broadening calendar-date availability', () => {
  assert.match(searchSource, /addCalendarDays, createServiceTimeOptions, formatLongDate, todayInParis/)
  assert.match(searchSource, /const quickDateOptions = computed\(\(\) => \[todayDate\.value, addCalendarDays\(todayDate\.value, 1\)\]\)/)
  assert.match(searchSource, /\(!availableDateOptions\.value\.includes\(date\) && !quickDateOptions\.value\.includes\(date\)\)/)
  assert.match(searchSource, /const hasValidSelectedDate = computed\(\(\) => Boolean\(calendarDate\(form\.date\)\)\)/)
  assert.match(searchSource, /:disabled="pending \|\| isLoading \|\| !isInitialized \|\| activeTheaterIds\.length === 0 \|\| !hasValidSelectedDate"/)
  assert.match(searchSource, /const query = submittedQuery\(search, preserveSelection\)[\s\S]*?await router\.replace\(\{ query \}\)/)
  assert.match(searchSource, /v-else-if="results\?\.length === 0"[\s\S]*?Aucune séance ne tient entièrement dans ce créneau\./)
  assert.match(searchSource, /:disabled="!hasAvailableDates"[\s\S]*?desktop-tabs="today-tomorrow"/)
})

test('cinema keeps route-selected quick tabs visible with empty availability', () => {
  assert.match(cinemaSource, /const selectedDate = computed\(\(\) => requestedDate\.value \?\? todayInParis\(\)\)/)
  assert.match(cinemaSource, /<ShowtimeDateBar :selected-date="selectedDate" :available-dates="availableDates" :today="todayInParis\(\)" :disabled="availableDates\.length === 0" @select="selectDate" \/>/)
  assert.doesNotMatch(cinemaSource, /<ShowtimeDateBar v-if="availableDates\.length"/)
  assert.match(cinemaSource, /date: date === todayInParis\(\) \? undefined : date/)
  assert.match(cinemaSource, /v-else-if="normalizedResults\.length === 0"[\s\S]*?Aucune séance à cette date/)
})

test('planning empty states and film all-date controls remain isolated', () => {
  assert.match(planningSource, /date\.value = queryDate && queryDate >= today\.value \? queryDate : today\.value/)
  assert.match(planningSource, /date: selectedDate === today\.value \? undefined : selectedDate/)
  assert.match(planningSource, /date: date\.value,[\s\S]*?api\.timeline|api\.timeline\(\{[\s\S]*?date: date\.value/)
  assert.match(planningSource, /Aucune séance pour cette date et cette langue\./)
  assert.match(filmSource, /<ShowtimeDateBar[^>]*mobile-tabs="all"/)
  assert.match(filmSource, /<ShowtimeDateBar v-if="hasAvailableDates"[^>]*:available-dates="availableDates"/)
  assert.equal((filmSource.match(/mobile-tabs="all"/g) ?? []).length, 1)
})

test('search compacts desktop date tabs while film expands its mobile date tabs', () => {
  assert.match(searchSource, /<ShowtimeDateBar[\s\S]*?desktop-tabs="today-tomorrow"[\s\S]*?calendar-position="end"[\s\S]*?stretch-tabs[\s\S]*?@select="form\.date = \$event"/)
  assert.match(filmSource, /<ShowtimeDateBar[^>]*mobile-tabs="all"[^>]*@select="selectMobileDate"/)
  assert.equal((filmSource.match(/mobile-tabs="all"/g) ?? []).length, 1)
  assert.match(filmSource, /<ShowtimeDateBar[^>]*@select="updateFilmQuery\(\{ date: \$event === fallbackDate\(\) \? undefined : \$event \}\)"/)
})

test('search date tabs share full row width before a fixed calendar trigger', () => {
  assert.match(dateBarSource, /type CalendarPosition = 'start' \| 'end'/)
  assert.match(dateBarSource, /calendarPosition: 'start'/)
  assert.match(dateBarSource, /stretchTabs: false/)
  assert.match(dateBarSource, /layoutOrder = computed<Array<'calendar' \| 'tabs'>>\(\(\) => props\.calendarPosition === 'end' \? \['tabs', 'calendar'\] : \['calendar', 'tabs'\]\)/)
  assert.match(dateBarSource, /<template v-for="item in layoutOrder" :key="item">/)
  assert.match(dateBarSource, /v-if="item === 'calendar'"/)
  assert.match(dateBarSource, /stretchTabs && group\.mode === 'today-tomorrow'/)
  assert.match(dateBarSource, /'hidden min-w-0 grid-cols-2 sm:grid'/)
  assert.match(dateBarSource, /group\.mode === 'today-tomorrow' && \(group\.key === 'mobile' \|\| stretchTabs\) \? 'w-full' : 'w-auto'/)
  assert.match(dateBarSource, /class="calendar-trigger grid size-9 shrink-0[^"]*sm:size-10"/)
})

test('film mobile date selection updates the route without closing its panel', () => {
  const selectMobileDateBody = filmSource.match(/function selectMobileDate\(date: string\) \{([\s\S]*?)\n\}/)?.[1]
  assert.ok(selectMobileDateBody)
  assert.match(selectMobileDateBody, /updateFilmQuery\(\{ date: date === fallbackDate\(\) \? undefined : date \}\)/)
  assert.doesNotMatch(selectMobileDateBody, /openMobilePanel|closeMobilePanel|mobileDateTrigger|\.focus\(/)
  assert.match(filmSource, /@click="toggleMobilePanel\('date'\)"/)
  assert.match(filmSource, /@keydown\.esc\.stop="closeMobilePanel\(\$event\)"/)
})

test('custom picker CSS targets Vuepic 14 classes and outranks late package defaults', () => {
  for (const source of routeAndSharedSources) assert.doesNotMatch(source, /dp__/)

  assert.doesNotMatch(searchSource, /:global\(\.dp--menu\.editorial-calendar-menu\)/)
  assert.match(showtimeSource, /:global\(\.dp--menu\.editorial-calendar-menu\)/)
  assert.match(showtimeSource, /\.editorial-calendar-menu \.dp--calendar-header-item/)
  assert.match(showtimeSource, /\.editorial-calendar-menu \.dp--month-year-select/)
  assert.match(showtimeSource, /\.editorial-calendar-menu \.dp--active/)
  assert.match(showtimeSource, /\.editorial-calendar-menu \.dp--today/)

  assert.match(filmsSource, /\.catalog-datepicker :deep\(\.dp--input\)/)
  assert.match(filmsSource, /\.catalog-datepicker :deep\(\.dp--input-focus\)/)
  assert.match(filmsSource, /:global\(\.dp--menu\.catalog-calendar-menu\)/)
  assert.match(filmsSource, /\.catalog-calendar-menu \.dp--range-between/)
  assert.match(filmsSource, /\.catalog-calendar-menu \.dp--range-border-start/)
  assert.match(filmsSource, /\.catalog-calendar-menu \.dp--range-border-end/)
})
