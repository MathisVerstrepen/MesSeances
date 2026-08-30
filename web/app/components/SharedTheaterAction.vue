<script setup lang="ts">
const { pending, errorMessage, restoreSavedTheaters } = useSharedTheaterRestoration()
</script>

<template>
  <slot :pending="pending" :error-message="errorMessage" :restore-saved-theaters="restoreSavedTheaters" />
</template>
