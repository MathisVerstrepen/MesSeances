<script setup lang="ts">
definePageMeta({ middleware: 'account-auth' })
const { session } = useAccountSession()
useHead({ title: 'Créer un compte - MesSeances' })
</script>

<template>
  <AccountShell title="Créer un compte">
    <div v-if="session?.state === 'complete'" class="space-y-4">
      <p>Vous êtes déjà connecté.</p>
      <a href="/connexion" class="account-primary">Mon compte</a>
    </div>
    <AccountCredentialsForm v-else register />
  </AccountShell>
</template>
