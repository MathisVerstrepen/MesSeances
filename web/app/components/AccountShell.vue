<script setup lang="ts">
import { accountErrorMessage } from '~/utils/accountState'

defineProps<{
  title: string
  hideExplore?: boolean
  hideLogout?: boolean
  compact?: boolean
  accountArea?: boolean
}>()
const account = useAccountSession()
const route = useRoute()
const { session, status, errorMessage } = account
const signingOut = ref(false)
const signOutError = ref('')
const lifetime = useAccountLifetime()

async function logout() {
  if (signingOut.value || account.writesBlocked.value) return
  signingOut.value = true
  signOutError.value = ''
  const current = lifetime.capture(false)
  try {
    await account.logout()
    if (!current()) return
    await navigateTo('/connexion')
  } catch (error) {
    if (current()) signOutError.value = accountErrorMessage(error)
  } finally {
    if (current()) signingOut.value = false
  }
}
</script>

<template>
  <main
    :class="[
      { 'account-shell-compact': compact, 'account-shell-area': accountArea },
      accountArea
        ? 'grid min-h-svh grid-rows-[auto_1fr] bg-canvas lg:grid-cols-[15rem_minmax(0,1fr)] lg:grid-rows-1'
        : 'bg-[#f8f7f2] bg-[linear-gradient(rgba(39,39,42,0.07)_1px,transparent_1px),linear-gradient(90deg,rgba(39,39,42,0.07)_1px,transparent_1px)] bg-[size:28px_28px] px-4 py-8 sm:px-6 sm:py-14 lg:py-20',
    ]"
    class="account-shell w-full border-b-2 border-ink text-ink"
  >
    <AccountAreaNavigation v-if="accountArea" />
    <div
      class="account-shell-content w-full min-w-0"
      :class="accountArea
        ? 'px-4 py-8 sm:px-8 lg:px-12 lg:py-10'
        : 'mx-auto max-w-xl border-2 border-ink bg-canvas p-5 shadow-[6px_6px_0_#27272a] sm:p-9 sm:shadow-[8px_8px_0_#27272a]'"
    >
      <div :class="{ 'account-area-inner mx-auto max-w-[60rem]': accountArea }">
        <h1
          class="mb-8 border-b-2 border-ink pb-6 [font-family:'Noto_Sans_Variable',sans-serif] text-[clamp(2rem,6vw,3.5rem)] font-black leading-[1.05] tracking-[-0.065em] sm:mb-9 sm:pb-8"
        >
          {{ title }}
        </h1>
        <div
          v-if="status === 'idle' || status === 'loading'"
          role="status"
          class="space-y-5 motion-safe:animate-pulse"
        >
          <span class="sr-only">Vérification de la session…</span>
          <div class="h-12 border-2 border-ink/20 bg-ink/10" />
          <div class="h-12 border-2 border-ink/20 bg-ink/10" />
          <div class="h-12 w-2/3 border-2 border-ink/20 bg-ink/10" />
        </div>
        <div v-else-if="status === 'error'" class="space-y-5">
          <p role="alert" class="account-alert">
            {{ errorMessage }}
          </p>
          <button
            type="button"
            class="account-primary"
            @click="account.refresh"
          >
            Réessayer
          </button>
        </div>
        <p
          v-else-if="session && !session.enabled"
          role="status"
          class="text-sm leading-relaxed"
        >
          Les comptes ne sont pas encore disponibles. Vous pouvez continuer à
          explorer les séances.
        </p>
        <div
          v-show="status === 'ready' && session?.enabled"
          :aria-busy="account.revalidating.value"
        >
          <slot />
        </div>
        <div
          v-if="session?.account && !hideLogout"
          class="mt-8 border-t-2 border-ink pt-4"
        >
          <button
            type="button"
            class="account-link"
            :disabled="signingOut || account.writesBlocked.value"
            @click="logout"
          >
            {{ signingOut ? 'Déconnexion…' : 'Se déconnecter' }}
          </button>
          <p v-if="signOutError" role="alert" class="account-alert mt-3">
            {{ signOutError }}
          </p>
        </div>
        <NuxtLink
          v-if="!hideExplore && route.path.toLowerCase().startsWith('/compte')"
          to="/"
          class="account-link mt-6"
          >Explorer les séances</NuxtLink
        >
      </div>
    </div>
  </main>
</template>

<style scoped>
@reference "../assets/css/main.css";

.account-shell-wide .account-shell-content {
  @apply max-w-4xl;
}

.account-shell-compact {
  @apply py-6 sm:py-10 lg:py-10;
}

.account-shell-compact .account-shell-content {
  @apply max-w-[33rem] sm:p-8;
}

.account-shell-compact h1 {
  @apply mb-6 pb-4 text-[2rem] sm:mb-6 sm:pb-4 sm:text-[2.5rem];
}

.account-shell :deep(.account-label) {
  @apply mb-2 block font-mono text-[0.68rem] font-extrabold uppercase leading-relaxed tracking-[0.1em];
}

.account-shell :deep(.account-heading) {
  @apply [font-family:'Noto_Sans_Variable',sans-serif] text-2xl font-black leading-tight tracking-[-0.045em];
}

.account-shell :deep(.account-input) {
  @apply min-h-12 min-w-0 rounded-none border-2 border-ink bg-surface px-3 text-base text-ink focus:shadow-[inset_0_0_0_2px_var(--color-highlight)] disabled:cursor-not-allowed disabled:opacity-60;
}

.account-shell :deep(.account-password-input) {
  @apply pr-14;
}

.account-shell :deep(.account-password-label-row .account-label) {
  @apply mb-0;
}

.account-shell :deep(.account-navigation-link) {
  @apply inline-flex min-h-11 items-center text-sm font-normal leading-relaxed underline underline-offset-4 hover:text-primary;
}

.account-shell :deep(.account-input[aria-invalid="true"]),
.account-shell :deep(.account-input-danger) {
  @apply border-primary;
}

.account-shell :deep(.account-primary),
.account-shell :deep(.account-secondary),
.account-shell :deep(.account-danger) {
  @apply inline-flex min-h-12 items-center justify-center gap-2 rounded-none border-2 border-ink px-4 py-3 text-center font-mono text-[0.72rem] font-extrabold uppercase leading-relaxed tracking-[0.06em] transition-colors disabled:cursor-not-allowed disabled:opacity-50;
}

.account-shell :deep(.account-primary) {
  @apply bg-ink text-white enabled:hover:bg-primary;
}

.account-shell :deep(a.account-primary:hover) {
  @apply bg-primary;
}

.account-shell :deep(.account-secondary) {
  @apply bg-surface text-ink enabled:hover:bg-highlight;
}

.account-shell :deep(.account-danger) {
  @apply border-primary bg-primary-soft text-primary enabled:hover:bg-primary enabled:hover:text-white;
}

.account-shell :deep(.account-link) {
  @apply inline-flex min-h-11 items-center font-mono text-xs font-bold leading-relaxed underline decoration-2 underline-offset-4 hover:text-primary disabled:cursor-not-allowed disabled:opacity-50;
}

.account-overview :deep(.overview-link) {
  @apply min-w-11 font-sans text-sm font-semibold;
}

.account-overview :deep(.overview-secondary) {
  @apply max-w-full font-sans text-sm font-semibold normal-case tracking-normal;
}

.account-shell :deep(.account-alert) {
  @apply border-2 border-primary bg-primary-soft p-4 text-sm leading-relaxed text-primary;
}

.account-shell :deep(p[role="status"]:not(.sr-only)) {
  @apply border-l-4 border-ink bg-[#f1efe8] p-4 text-sm leading-relaxed;
}

.account-shell :deep(input:focus-visible),
.account-shell :deep(button:focus-visible),
.account-shell :deep(a:focus-visible) {
  @apply outline-3 outline-offset-3 outline-ink ring-0;
}

.account-shell :deep(.account-choice) {
  @apply flex min-h-12 items-center gap-3 border-2 border-ink bg-surface p-3 text-sm font-semibold has-checked:bg-highlight;
}

.account-shell :deep(.account-choice input) {
  @apply size-4 shrink-0 accent-ink;
}

.account-shell :deep(.account-settings > section) {
  @apply min-w-0 border-t-2 border-ink pt-7;
}

.account-shell :deep(.account-settings > section:first-child) {
  @apply border-t-0 pt-0;
}

@media (min-width: 768px) {
  .account-shell :deep(.account-settings > section) {
    @apply grid grid-cols-[minmax(0,1fr)_minmax(0,1.7fr)] gap-x-10 gap-y-5 space-y-0;
  }

  .account-shell :deep(.account-settings > section > :not(h2)) {
    @apply col-start-2 min-w-0;
  }

  .account-shell :deep(.account-settings > section > h2 + *) {
    @apply row-start-1;
  }
}
</style>
