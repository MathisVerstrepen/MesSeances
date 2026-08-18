<template>
  <div class="flex min-h-screen flex-col">
    <AppHeader />
    <div class="min-w-0 flex-1">
      <NuxtPage />
    </div>
    <footer class="border-t border-line bg-surface">
      <div class="mx-auto flex max-w-[1440px] justify-center px-4 py-4 sm:px-6 lg:px-10">
        <NuxtLink to="/credits" class="text-sm text-muted transition hover:text-ink">
          Crédits
        </NuxtLink>
      </div>
    </footer>
  </div>
</template>
