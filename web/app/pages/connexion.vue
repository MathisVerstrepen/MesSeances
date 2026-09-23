<script setup lang="ts">
definePageMeta({ middleware: 'account-auth' })
const route = useRoute()
const account = useAccountSession()
const providerError = computed(() => route.query.error === 'google_failed')
useHead({ title: 'Connexion - MesSeances' })
</script>

<template>
  <AccountShell title="Connexion" compact>
    <p v-if="providerError" role="alert" class="account-alert mb-5">
      La confirmation Google n’a pas abouti ou a expiré. Recommencez depuis
      votre compte ou utilisez votre moyen de connexion habituel.
    </p>
    <a
      v-if="account.session.value?.account"
      href="/compte"
      class="account-primary"
      >Revenir à mon compte</a
    >
    <AccountCredentialsForm v-else />
  </AccountShell>
</template>
