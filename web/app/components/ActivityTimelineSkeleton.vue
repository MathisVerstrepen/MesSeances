<template>
  <div aria-hidden="true" class="relative max-w-5xl motion-safe:animate-pulse">
    <span
      class="absolute top-2 bottom-0 left-[5px] w-0.5 bg-ink/20 lg:left-[167px]"
    />
    <div
      v-for="day in 2"
      :key="day"
      class="relative pb-10 pl-7 lg:grid lg:grid-cols-[168px_minmax(0,1fr)] lg:gap-10 lg:pl-0"
    >
      <span class="absolute top-2 left-0 size-3 bg-ink/20 lg:left-[162px]" />
      <div class="mb-6 space-y-3 lg:pr-6">
        <div class="h-6 w-48 bg-ink/10 lg:h-12 lg:w-16" />
        <div class="hidden h-4 w-24 bg-ink/10 lg:block" />
      </div>
      <div class="space-y-8">
        <div
          v-for="row in day === 1 ? 2 : 1"
          :key="row"
          class="flex gap-4 sm:gap-6"
        >
          <div class="aspect-[2/3] w-18 shrink-0 bg-ink/10 sm:w-24 lg:w-28" />
          <div class="min-w-0 flex-1 space-y-3">
            <div class="h-7 w-3/4 max-w-96 bg-ink/10" />
            <div class="h-4 w-1/2 max-w-64 bg-ink/10" />
            <div class="h-4 w-2/3 max-w-72 bg-ink/10" />
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
