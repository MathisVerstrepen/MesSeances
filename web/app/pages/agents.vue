<script setup lang="ts">
import { absoluteSiteUrl } from '~/utils/siteUrl'

const config = useRuntimeConfig()
const canonicalUrl = absoluteSiteUrl(config.public.siteUrl, '/agents')
// Absolute, copyable examples; braces stay readable as URL templates.
const site = absoluteSiteUrl(config.public.siteUrl, '/').replace(/\/$/, '')

const pages = [
  {
    path: '/ville/{slug}',
    label: 'Séances d’une ville',
    description:
      'Toutes les séances du jour, cinéma par cinéma puis film par film. Paramètres date (AAAA-MM-JJ) et page (6 cinémas par page).',
  },
  {
    path: '/cinema/{slug}',
    label: 'Fiche cinéma',
    description: 'Séances d’un cinéma pour une date. Paramètre date.',
  },
  {
    path: '/film/{slug}',
    label: 'Fiche film',
    description:
      'Séances d’un film dans toute la France. Paramètres date, language, format.',
  },
  {
    path: '/recherche',
    label: 'Recherche par créneau',
    description:
      'Séances qui tiennent dans un créneau : theaters, date, start_after, finish_before (HH:MM).',
  },
]

const endpoints = [
  {
    path: '/api/v1/cities',
    description: 'Villes couvertes et leurs cinémas (slug, id, chaîne).',
  },
  {
    path: '/api/v1/cities/{slug}',
    description: 'Cinémas d’une ville et films qui y sont programmés.',
  },
  {
    path: '/api/v1/theaters',
    description: 'Annuaire des cinémas ; filtres city et chain.',
  },
  {
    path: '/api/v1/theaters/{slug}/showtimes',
    description: 'Séances d’un cinéma ; paramètre date.',
  },
  {
    path: '/api/v1/timeline',
    description:
      'Séances de plusieurs cinémas pour une date ; paramètres date et theaters.',
  },
  {
    path: '/api/v1/movies',
    description:
      'Catalogue et recherche de films ; paramètres search, sort, page, page_size.',
  },
  {
    path: '/api/v1/movies/{slug}/showtimes',
    description: 'Séances d’un film ; date obligatoire, city ou theaters.',
  },
  {
    path: '/api/v1/search/slot',
    description:
      'Séances dans un créneau ; date, start_after, finish_before, city ou theaters.',
  },
  {
    path: '/api/v1/movies/upcoming',
    description: 'Prochaines sorties en salle.',
  },
]

useSeoMeta({
  title: 'Agents IA et API - MesSeances',
  description:
    'Comment un agent IA ou un développeur peut lire les séances de MesSeances : pages par ville, versions Markdown, API JSON publique et spécification OpenAPI.',
  robots: 'index,follow',
})
useHead({ link: [{ rel: 'canonical', href: canonicalUrl }] })
</script>

<template>
  <LegalPageLayout eyebrow="Agents IA et développeurs" title="Agents IA et API">
    <section class="legal-section" aria-labelledby="agents-start-heading">
      <h2 id="agents-start-heading">Par où commencer</h2>
      <p>
        MesSeances publie les horaires des séances de cinéma en France, grandes
        chaînes et cinémas indépendants. Toutes les données sont publiques et en
        lecture seule, sans compte. Pour une question comme « les séances à
        Lille cet après-midi », ouvrez la page de la ville :
        <a :href="`${site}/ville/lille`">{{ site }}/ville/lille</a>, ou sa
        version texte
        <a :href="`${site}/ville/lille.md`">{{ site }}/ville/lille.md</a>.
      </p>
      <p>
        Le même guide existe en texte brut pour les agents :
        <a :href="`${site}/llms.txt`">{{ site }}/llms.txt</a>. La liste complète
        des villes et cinémas avec leurs slugs est dans
        <a :href="`${site}/llms-full.txt`">{{ site }}/llms-full.txt</a>.
      </p>
    </section>

    <section class="legal-section" aria-labelledby="agents-pages-heading">
      <h2 id="agents-pages-heading">Pages et versions Markdown</h2>
      <p>
        Ajoutez <code>.md</code> à l’adresse d’une page ville, cinéma ou film
        pour obtenir les mêmes séances en Markdown léger, par exemple
        <a :href="`${site}/cinema/ugc-25.md`">{{ site }}/cinema/ugc-25.md</a>.
      </p>
      <dl class="legal-list">
        <div v-for="page in pages" :key="page.path">
          <dt>{{ page.label }} : <code>{{ site }}{{ page.path }}</code></dt>
          <dd>{{ page.description }}</dd>
        </div>
      </dl>
    </section>

    <section class="legal-section" aria-labelledby="agents-api-heading">
      <h2 id="agents-api-heading">API JSON publique</h2>
      <p>
        Requêtes <code>GET</code> sans authentification, réponses en JSON. La
        spécification OpenAPI 3.1 décrit chaque paramètre et chaque réponse :
        <a :href="`${site}/api/v1/openapi.json`"
          >{{ site }}/api/v1/openapi.json</a
        >.
      </p>
      <dl class="legal-list">
        <div v-for="endpoint in endpoints" :key="endpoint.path">
          <dt><code>{{ endpoint.path }}</code></dt>
          <dd>{{ endpoint.description }}</dd>
        </div>
      </dl>
      <p>
        Les lectures coûteuses sont limitées à environ une requête par seconde
        par client, avec une rafale de 20. Au-delà, l’API répond
        <code>429</code>
        avec un en-tête <code>Retry-After</code>. Les erreurs ont la forme
        <code>{ "error": { "code": "...", "message": "..." }}</code>.
      </p>
    </section>

    <section class="legal-section" aria-labelledby="agents-conventions-heading">
      <h2 id="agents-conventions-heading">Conventions</h2>
      <ul>
        <li>
          Les dates (<code>AAAA-MM-JJ</code>) et les heures (<code>HH:MM</code>)
          sont à l’heure de Paris. Les horodatages de l’API sont en UTC.
        </li>
        <li>
          Slugs : ville en minuscules sans accents (<code>lille</code>,
          <code>paris</code>), film <code>film- { n}</code>, cinéma
          <code>{ chaîne}- { identifiant}</code>.
        </li>
        <li>
          Versions : VF version française, VOF version originale française,
          VOSTFR version originale sous-titrée en français, VO version
          originale, VF SME sous-titres pour sourds et malentendants.
        </li>
        <li>
          Chaque séance de l’API porte un <code>booking_url</code> vers le site
          de l’exploitant. MesSeances ne vend pas de billets.
        </li>
      </ul>
    </section>
  </LegalPageLayout>
</template>
