<script setup lang="ts">
import { ChevronRight, Settings } from '@lucide/vue'
import { accountDestination } from '~/utils/accountState'

definePageMeta({ middleware: 'account-auth' })
const account = useAccountSession()
const complete = computed(() => account.session.value?.state === 'complete')
useHead({ title: 'Mon compte - MesSeances' })
</script>

<template>
  <AccountShell title="Mon compte" hide-explore hide-logout account-area>
    <nav v-if="complete" aria-label="Rubriques du compte">
      <ul>
        <li>
          <NuxtLink
            to="/compte/parametres"
            :prefetch="false"
            class="flex min-h-12 items-center gap-3 py-4 text-lg font-semibold hover:text-primary"
          >
            <Settings :size="20" class="shrink-0" aria-hidden="true" />
            Paramètres
            <ChevronRight
              :size="20"
              class="ml-auto shrink-0"
              aria-hidden="true"
            />
          </NuxtLink>
        </li>
      </ul>
    </nav>
    <div v-else class="space-y-4">
      <p class="text-sm">
        Votre session n’est plus active ou votre inscription reste à terminer.
      </p>
      <NuxtLink
        :to="account.session.value ? accountDestination(account.session.value) : '/connexion'"
        :prefetch="false"
        class="account-link"
        >Reprendre la connexion</NuxtLink
      >
    </div>
  </AccountShell>
</template>
