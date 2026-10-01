<script setup lang="ts">
import { AlertTriangle, ArrowLeft, Users } from '@lucide/vue'
import {
  adminAccountMethodLabels,
  adminAccountStateLabels,
  formatAdminAccountDate,
  useAdminAccounts,
} from '~/composables/useAdminAccounts'

definePageMeta({ middleware: 'admin-auth' })

const {
  items,
  total,
  page,
  pageCount,
  loading,
  loaded,
  error,
  load,
  changePage,
  dispose,
} = useAdminAccounts(useMesSeancesApi())
const numberFormatter = new Intl.NumberFormat('fr-FR')

onMounted(() => {
  void load()
})
onBeforeRouteLeave(() => {
  dispose()
})
onBeforeUnmount(dispose)

useHead({
  title: 'Comptes - Administration - MesSeances',
  meta: [
    { name: 'robots', content: 'noindex, nofollow' },
    { name: 'referrer', content: 'no-referrer' },
  ],
})
</script>

<template>
  <main class="mx-auto max-w-5xl px-4 py-8 sm:px-6 sm:py-10 lg:px-8">
    <header class="border-b-2 border-ink pb-6">
      <NuxtLink
        to="/admin"
        class="mb-4 inline-flex min-h-11 items-center gap-2 text-sm font-semibold text-ink hover:text-accent"
      >
        <ArrowLeft :size="17" aria-hidden="true" />
        Administration
      </NuxtLink>
      <h1 class="editorial-title">Comptes</h1>
    </header>

    <section class="mt-6" aria-label="Liste des comptes" :aria-busy="loading">
      <EditorialStatePanel
        v-if="loading"
        semantic="status"
        live="polite"
        size="compact"
        shadow="small"
      >
        <p class="font-semibold text-ink">Chargement des comptes…</p>
        <div class="grid w-full max-w-xl gap-3" aria-hidden="true">
          <div
            v-for="row in 3"
            :key="row"
            class="h-8 border-b border-ink/20 bg-subtle"
          />
        </div>
      </EditorialStatePanel>

      <div
        v-else-if="error"
        class="editorial-alert flex flex-wrap items-start gap-3 p-4"
        role="alert"
      >
        <AlertTriangle :size="20" class="shrink-0" aria-hidden="true" />
        <p class="min-w-0 flex-1">{{ error }}</p>
        <button type="button" class="editorial-button-outline" @click="load()">
          Réessayer
        </button>
      </div>

      <template v-else-if="loaded">
        <p class="mb-4 font-semibold text-ink" role="status" aria-live="polite">
          {{ numberFormatter.format(total) }}
          {{ total === 1 ? 'compte' : 'comptes' }}
        </p>

        <EditorialStatePanel
          v-if="!items.length"
          semantic="status"
          size="compact"
          shadow="small"
        >
          <template #icon><Users :size="28" aria-hidden="true" /></template>
          <p class="font-semibold text-ink">Aucun compte enregistré.</p>
        </EditorialStatePanel>

        <div
          v-else
          class="overflow-x-auto border-2 border-ink bg-surface"
          tabindex="0"
          role="region"
          aria-label="Tableau des comptes, défilement horizontal"
        >
          <table class="w-full min-w-[64rem] border-collapse text-left text-sm">
            <caption class="sr-only">
              Comptes enregistrés, du plus récent au plus ancien. Dates en heure
              de Paris.
            </caption>
            <thead class="border-b-2 border-ink bg-highlight">
              <tr>
                <th scope="col" class="px-4 py-3 font-bold">E-mail</th>
                <th scope="col" class="px-4 py-3 font-bold">Pseudo</th>
                <th scope="col" class="px-4 py-3 font-bold">Inscription</th>
                <th scope="col" class="px-4 py-3 font-bold">
                  E-mail vérifié le
                </th>
                <th scope="col" class="px-4 py-3 font-bold">État</th>
                <th scope="col" class="px-4 py-3 font-bold">
                  Méthodes associées
                </th>
              </tr>
            </thead>
            <tbody class="divide-y divide-ink/20">
              <tr
                v-for="(account, index) in items"
                :key="index"
                class="align-top"
              >
                <th
                  scope="row"
                  class="max-w-72 px-4 py-4 font-semibold wrap-anywhere"
                >
                  {{ account.email }}
                </th>
                <td class="max-w-48 px-4 py-4 wrap-anywhere">
                  {{ account.username ?? 'Non choisi' }}
                </td>
                <td class="px-4 py-4">
                  <time :datetime="account.created_at">{{
                    formatAdminAccountDate(account.created_at)
                  }}</time>
                </td>
                <td class="px-4 py-4">
                  <time
                    v-if="account.email_verified_at"
                    :datetime="account.email_verified_at"
                    >{{
                      formatAdminAccountDate(account.email_verified_at)
                    }}</time
                  ><span v-else>Non vérifié</span>
                </td>
                <td class="px-4 py-4">
                  {{ adminAccountStateLabels[account.state] }}
                </td>
                <td class="px-4 py-4">
                  {{ adminAccountMethodLabels(account) }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <nav
          v-if="pageCount > 1"
          aria-label="Pagination des comptes"
          class="mt-5 grid grid-cols-2 items-center justify-between gap-3 border-t-2 border-ink pt-5 sm:flex sm:flex-wrap"
        >
          <button
            type="button"
            class="editorial-button-outline"
            :disabled="page <= 1"
            @click="changePage(page - 1)"
          >
            Précédente
          </button>
          <span
            class="order-first col-span-2 min-w-0 text-center text-sm text-ink sm:order-none"
            >Page {{ numberFormatter.format(page) }} sur
            {{ numberFormatter.format(pageCount) }}</span
          >
          <button
            type="button"
            class="editorial-button-outline"
            :disabled="page >= pageCount"
            @click="changePage(page + 1)"
          >
            Suivante
          </button>
        </nav>
      </template>
    </section>
  </main>
</template>
