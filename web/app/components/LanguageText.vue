<script setup lang="ts">
import { splitLanguageTerms } from '~/utils/languageGlossary'

const props = withDefaults(
  defineProps<{
    text: string
    // Disable keyboard focus when rendered inside a link or button.
    focusable?: boolean
  }>(),
  { focusable: true },
)

const segments = computed(() => splitLanguageTerms(props.text))
</script>

<template>
  <span
    ><template v-for="(segment, index) in segments" :key="index"
      ><LanguageTerm
        v-if="segment.kind === 'term'"
        :term="segment.text"
        :definition="segment.definition"
        :focusable="focusable"
      /><template v-else>{{ segment.text }}</template></template
    ></span
  >
</template>
