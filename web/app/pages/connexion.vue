<script setup lang="ts">
definePageMeta({ middleware: 'account-auth' })
const route = useRoute()
const account = useAccountSession()
const providerError = computed(() => route.query.error === 'google_failed')
useHead({ title: 'Connexion - MesSeances' })
</script>

<template>
  <AccountShell title="Connexion">
    <p
      v-if="providerError"
      role="alert"
      class="mb-5 border-l-4 border-primary pl-3 text-sm text-primary"
    >
      La confirmation Google n’a pas abouti ou a expiré. Recommencez depuis
      votre compte ou utilisez votre moyen de connexion habituel.
    </p>
    <a
      v-if="account.session.value?.account"
      href="/compte"
      class="button-primary min-h-12"
      >Revenir à mon compte</a
    >
    <AccountCredentialsForm v-else />
    <a
      href="/mot-de-passe-oublie"
      class="mt-4 inline-flex min-h-11 items-center text-sm font-semibold underline underline-offset-4"
      >Mot de passe oublié ?</a
    >
  </AccountShell>
</template>
