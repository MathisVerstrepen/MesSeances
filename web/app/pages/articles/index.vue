<script setup lang="ts">
import { ArrowUpRight } from '@lucide/vue'
import imaxLogo from '~/assets/imgs/imax_logo_large.webp?no-inline'
import { serializeJsonLd } from '~/utils/jsonLd'
import { absoluteSiteUrl } from '~/utils/siteUrl'

const config = useRuntimeConfig()
const canonicalUrl = absoluteSiteUrl(config.public.siteUrl, '/articles')
const title = 'Articles cinéma - MesSeances'
const description =
  'Formats, salles et expériences de cinéma : nos articles pour comprendre les différences et mieux choisir votre séance.'

useSeoMeta({
  title,
  description,
  robots: 'index,follow',
  ogTitle: title,
  ogDescription: description,
  ogUrl: canonicalUrl,
  ogType: 'website',
})
useHead({
  link: [{ rel: 'canonical', href: canonicalUrl }],
  script: [
    {
      key: 'articles-jsonld',
      type: 'application/ld+json',
      innerHTML: serializeJsonLd({
        '@context': 'https://schema.org',
        '@graph': [
          {
            '@type': 'CollectionPage',
            name: 'Articles cinéma',
            description,
            url: canonicalUrl,
            inLanguage: 'fr-FR',
          },
          {
            '@type': 'ItemList',
            itemListElement: [
              {
                '@type': 'ListItem',
                position: 1,
                url: absoluteSiteUrl(config.public.siteUrl, '/articles/imax'),
              },
            ],
          },
          {
            '@type': 'BreadcrumbList',
            itemListElement: [
              {
                '@type': 'ListItem',
                position: 1,
                name: 'Accueil',
                item: absoluteSiteUrl(config.public.siteUrl, '/'),
              },
              {
                '@type': 'ListItem',
                position: 2,
                name: 'Articles',
                item: canonicalUrl,
              },
            ],
          },
        ],
      }),
    },
  ],
})
</script>

<template>
  <main
    class="bg-[#f8f7f2] text-ink [font-family:'Noto_Sans_Variable',sans-serif]"
  >
    <div class="mx-auto max-w-6xl px-4 py-6 sm:px-6 lg:px-10">
      <Breadcrumbs
        :items="[{ label: 'Accueil', to: '/' }, { label: 'Articles' }]"
      />
      <header class="pb-7 pt-3 sm:pb-9 sm:pt-5">
        <h1 class="text-4xl font-black tracking-tight sm:text-5xl">Articles</h1>
        <p
          class="mt-3 max-w-2xl text-base leading-relaxed text-muted sm:text-lg"
        >
          Des repères pour mieux comprendre le cinéma et choisir votre séance.
        </p>
      </header>

      <article class="mb-10 border-y border-ink/20 sm:mb-16">
        <NuxtLink
          to="/articles/imax"
          class="article-entry group outline-offset-4 focus-visible:outline-2 focus-visible:outline-accent"
          aria-labelledby="imax-article-title"
        >
          <div class="article-logo bg-accent-soft">
            <img
              :src="imaxLogo"
              width="2048"
              height="388"
              alt="IMAX"
              class="h-auto w-full"
            >
          </div>
          <div class="min-w-0 flex-1">
            <p
              class="font-mono text-[10px] font-bold uppercase tracking-wider text-accent"
            >
              Formats &amp; projection
            </p>
            <h2
              id="imax-article-title"
              class="mt-2 text-xl font-black leading-tight tracking-tight group-hover:text-accent sm:text-2xl"
            >
              IMAX : le guide complet
            </h2>
            <p
              class="mt-2 max-w-2xl text-sm leading-relaxed text-muted sm:text-base"
            >
              Formats d’image, laser ou pellicule, salles en France : les
              repères pour choisir votre séance IMAX.
            </p>
            <span
              class="mt-2 inline-flex min-h-10 items-center gap-2 font-mono text-xs font-bold group-hover:text-accent"
            >
              Lire l’article <ArrowUpRight :size="16" aria-hidden="true" />
            </span>
          </div>
        </NuxtLink>
      </article>
    </div>
  </main>
</template>

<style scoped>
.article-entry {
  display: flex;
  align-items: center;
  gap: 28px;
  padding-block: 24px;
}
.article-logo {
  display: flex;
  flex: 0 0 160px;
  align-items: center;
  justify-content: center;
  width: 160px;
  height: 104px;
  padding: 24px;
}
@media (max-width: 639px) {
  .article-entry {
    align-items: flex-start;
    gap: 16px;
    padding-block: 20px;
  }
  .article-logo {
    flex-basis: 64px;
    width: 64px;
    height: 64px;
    padding: 10px;
  }
}
</style>
