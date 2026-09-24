<script setup lang="ts">
import { serializeJsonLd, type JsonLdDocument } from '~/utils/jsonLd'
import { absoluteSiteUrl } from '~/utils/siteUrl'
import { isAccountPage } from '~~/shared/accountPrivacy'

const config = useRuntimeConfig()
const route = useRoute()
const privateDocument = computed(
  () => isAccountPage(route.path) || /^\/admin(?:\/|$)/i.test(route.path),
)
const rootUrl = absoluteSiteUrl(config.public.siteUrl, '/')
const organizationId = `${rootUrl}#organization`
const websiteId = `${rootUrl}#website`
const globalGraph: JsonLdDocument = {
  '@context': 'https://schema.org',
  '@graph': [
    {
      '@type': 'Organization',
      '@id': organizationId,
      name: 'MesSeances',
      url: rootUrl,
    },
    {
      '@type': 'WebSite',
      '@id': websiteId,
      name: 'MesSeances',
      url: rootUrl,
      inLanguage: 'fr-FR',
      publisher: { '@id': organizationId },
    },
  ],
}
const globalJsonLd = serializeJsonLd(globalGraph)
useHead(() => ({
  meta: [
    {
      name: 'referrer',
      content: privateDocument.value ? 'no-referrer' : 'strict-origin',
    },
    ...(privateDocument.value
      ? [{ name: 'robots', content: 'noindex, nofollow' }]
      : []),
  ],
  script: [{ type: 'application/ld+json', innerHTML: globalJsonLd }],
}))
</script>

<template>
  <div class="flex min-h-screen flex-col">
    <VitePwaManifest />
    <AppHeader />
    <div class="min-w-0 flex-1">
      <NuxtPage />
    </div>
    <AppFooter />
  </div>
</template>
