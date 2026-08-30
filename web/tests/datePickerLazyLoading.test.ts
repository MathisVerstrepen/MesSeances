import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const filmsSource = await readFile(new URL('../app/pages/films.vue', import.meta.url), 'utf8')
const searchSource = await readFile(new URL('../app/pages/recherche.vue', import.meta.url), 'utf8')
const showtimeSource = await readFile(new URL('../app/components/ShowtimeDatePicker.vue', import.meta.url), 'utf8')
const deferredSource = await readFile(new URL('../app/components/DeferredVueDatePicker.vue', import.meta.url), 'utf8')
const loaderSource = await readFile(new URL('../app/utils/vueDatePickerLoader.ts', import.meta.url), 'utf8')

const routeAndSharedSources = [filmsSource, searchSource, showtimeSource]

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
  assert.match(searchSource, /<DeferredVueDatePicker[\s\S]*?<template #trigger>/)
  assert.match(showtimeSource, /<DeferredVueDatePicker[\s\S]*?<template #trigger>/)
})

test('custom picker CSS targets Vuepic 14 classes and outranks late package defaults', () => {
  for (const source of routeAndSharedSources) assert.doesNotMatch(source, /dp__/)

  for (const source of [searchSource, showtimeSource]) {
    assert.match(source, /:global\(\.dp--menu\.editorial-calendar-menu\)/)
    assert.match(source, /\.editorial-calendar-menu \.dp--calendar-header-item/)
    assert.match(source, /\.editorial-calendar-menu \.dp--month-year-select/)
    assert.match(source, /\.editorial-calendar-menu \.dp--active/)
    assert.match(source, /\.editorial-calendar-menu \.dp--today/)
  }

  assert.match(filmsSource, /\.catalog-datepicker :deep\(\.dp--input\)/)
  assert.match(filmsSource, /\.catalog-datepicker :deep\(\.dp--input-focus\)/)
  assert.match(filmsSource, /:global\(\.dp--menu\.catalog-calendar-menu\)/)
  assert.match(filmsSource, /\.catalog-calendar-menu \.dp--range-between/)
  assert.match(filmsSource, /\.catalog-calendar-menu \.dp--range-border-start/)
  assert.match(filmsSource, /\.catalog-calendar-menu \.dp--range-border-end/)
})
