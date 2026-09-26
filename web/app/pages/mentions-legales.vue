<script setup lang="ts">
const config = useRuntimeConfig()
const canonicalUrl = absoluteSiteUrl(config.public.siteUrl, '/mentions-legales')

useSeoMeta({
  title: 'Mentions légales - MesSeances',
  description:
    'Consultez les mentions légales de MesSeances, site personnel non professionnel consacré aux séances de cinéma.',
  robots: 'index,follow',
})
useHead({ link: [{ rel: 'canonical', href: canonicalUrl }] })
</script>

<template>
  <LegalPageLayout eyebrow="Cadre légal" title="Mentions légales">
    <section class="legal-section" aria-labelledby="publisher-heading">
      <h2 id="publisher-heading">Édition du site</h2>
      <p>
        MesSeances est édité à titre non professionnel par Mathis Verstrepen,
        dans le cadre d’un projet personnel sans activité commerciale.
      </p>
    </section>

    <section class="legal-section" aria-labelledby="hosting-heading">
      <h2 id="hosting-heading">Hébergement</h2>
      <dl class="legal-list">
        <div>
          <dt>Nom légal exact de l’hébergeur</dt>
          <dd>Hetzner Online GmbH</dd>
        </div>
        <div>
          <dt>Adresse postale complète de l’hébergeur</dt>
          <dd>Industriestr. 25, 91710 Gunzenhausen, Allemagne</dd>
        </div>
        <div>
          <dt>Numéro de téléphone de l’hébergeur</dt>
          <dd>+49 (0)9831 505-0</dd>
        </div>
        <div>
          <dt>Localisation de l’infrastructure du serveur</dt>
          <dd>Centre de données HEL1-DC5, à Helsinki, en Finlande</dd>
        </div>
      </dl>
    </section>

    <section class="legal-section" aria-labelledby="purpose-heading">
      <h2 id="purpose-heading">Objet du site</h2>
      <p>
        MesSeances facilite la consultation et la comparaison d’horaires de
        cinéma. Les informations affichées sont fournies à titre indicatif.
        Elles peuvent évoluer ou comporter des erreurs. Seules les informations
        publiées par les cinémas et exploitants concernés font foi.
      </p>
      <p>
        Lorsque la fonctionnalité est disponible, vous pouvez créer un compte
        privé par email et mot de passe ou avec Google, gérer vos moyens de
        connexion et ajouter une photo facultative dans les paramètres. Il n’y a
        ni profil public ni annuaire ; les préférences de cinémas restent dans
        votre navigateur, sans synchronisation avec le compte. La
        <NuxtLink to="/confidentialite">politique de confidentialité</NuxtLink>
        détaille les données utilisées, les cookies nécessaires, leur
        conservation et vos droits.
      </p>
    </section>

    <section class="legal-section" aria-labelledby="property-heading">
      <h2 id="property-heading">Propriété intellectuelle</h2>
      <p>
        La structure du site, son interface et ses contenus originaux sont
        protégés par les règles applicables à la propriété intellectuelle. Toute
        reproduction ou réutilisation non autorisée de ces éléments est
        interdite, sauf exception prévue par la loi.
      </p>
      <p>
        Les noms, marques, logos, affiches, visuels et données appartenant à des
        tiers restent la propriété de leurs titulaires respectifs. Leur présence
        sur MesSeances n’implique aucune affiliation ni approbation.
      </p>
    </section>

    <section class="legal-section" aria-labelledby="sources-heading">
      <h2 id="sources-heading">Sources et crédits</h2>
      <p>
        MesSeances agrège des informations publiques provenant d’exploitants de
        cinéma et utilise des visuels et métadonnées fournis notamment par The
        Movie Database (TMDB). Les attributions et liens vers les sources sont
        regroupés sur la page
        <NuxtLink to="/credits">Crédits &amp; sources</NuxtLink>.
      </p>
    </section>

    <section class="legal-section" aria-labelledby="liability-heading">
      <h2 id="liability-heading">Responsabilité</h2>
      <p>
        L’éditeur s’efforce de présenter des informations accessibles et à jour,
        sans garantir leur exhaustivité, leur disponibilité permanente ni
        l’absence d’erreur. L’utilisateur doit vérifier les horaires, tarifs,
        formats et conditions d’accès directement auprès du cinéma avant tout
        déplacement ou achat.
      </p>
      <p>
        MesSeances contient des liens vers des services tiers. L’éditeur ne
        contrôle pas leurs contenus, leur disponibilité ou leurs pratiques et ne
        peut en être tenu responsable.
      </p>
    </section>

    <section class="legal-section" aria-labelledby="contact-heading">
      <h2 id="contact-heading">Contact</h2>
      <p>
        Pour toute question relative au site ou à son contenu :
        <a href="mailto:contact@diikstra.fr">contact@diikstra.fr</a>
      </p>
    </section>

    <section class="legal-section" aria-labelledby="updates-heading">
      <h2 id="updates-heading">Mise à jour des mentions légales</h2>
      <p>Dernière mise à jour : 24 septembre 2026.</p>
    </section>
  </LegalPageLayout>
</template>
