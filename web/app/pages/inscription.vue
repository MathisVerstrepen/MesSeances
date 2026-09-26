<script setup lang="ts">
definePageMeta({ middleware: 'account-auth' })
const { session } = useAccountSession()
useHead({ title: 'Créer un compte - MesSeances' })
</script>

<template>
  <AccountShell title="Créer un compte" compact>
    <div v-if="session?.state === 'complete'" class="space-y-4">
      <p>Vous êtes déjà connecté.</p>
      <NuxtLink to="/connexion" :prefetch="false" class="account-primary"
        >Mon compte</NuxtLink
      >
    </div>
    <AccountCredentialsForm v-else register />
  </AccountShell>
</template>
