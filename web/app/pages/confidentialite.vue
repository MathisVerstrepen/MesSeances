<script setup lang="ts">
const config = useRuntimeConfig()
const canonicalUrl = absoluteSiteUrl(config.public.siteUrl, '/confidentialite')

useSeoMeta({
  title: 'Confidentialité - MesSeances',
  description: 'Découvrez comment MesSeances utilise le stockage local, les journaux techniques, les images distantes et la mesure d’audience.',
  robots: 'index,follow'
})
useHead({ link: [{ rel: 'canonical', href: canonicalUrl }] })
</script>

<template>
  <LegalPageLayout eyebrow="Données personnelles · Brouillon" title="Confidentialité">
    <section class="legal-section" aria-labelledby="controller-heading">
      <h2 id="controller-heading">Responsable du traitement</h2>
      <dl class="legal-list">
        <div>
          <dt>Identité</dt>
          <dd>Mathis Verstrepen</dd>
        </div>
        <div>
          <dt>Contact pour les données personnelles</dt>
          <dd><a href="mailto:contact@diikstra.fr">contact@diikstra.fr</a></dd>
        </div>
      </dl>
    </section>

    <section class="legal-section" aria-labelledby="data-heading">
      <h2 id="data-heading">Données et mécanismes utilisés</h2>

      <h3>Préférences de cinémas</h3>
      <p>Le navigateur conserve les identifiants des cinémas sélectionnés dans le stockage local, sous la clé <code>messeances.favoriteTheaterIds.v1</code>. Cette préférence personnalise les séances affichées. Les identifiants peuvent être transmis à l’API MesSeances comme filtres lors de la consultation des pages. Ils ne servent pas à créer un compte utilisateur.</p>
      <p>La sélection peut être modifiée depuis la page <NuxtLink to="/cinemas">Cinémas</NuxtLink>. L’effacement des données du site depuis le navigateur supprime la préférence enregistrée et rétablit ensuite la sélection par défaut.</p>

      <h3>Géolocalisation et carte interactive</h3>
      <p>La géolocalisation ne démarre qu’après un clic sur le bouton <strong>Utiliser ma position</strong> et l’autorisation explicite accordée dans le navigateur. L’API de géolocalisation du navigateur fournit alors la latitude, la longitude et une estimation de la précision, sans demande de haute précision. Elle peut réutiliser une position mise en cache datant d’au plus 10 minutes.</p>
      <p>La position et sa précision restent uniquement en mémoire dans la page, afin de calculer localement les cinémas proches et d’afficher la position sur la carte. MesSeances ne les transmet pas à son API et ne les enregistre ni dans un cookie, ni dans le stockage local ou de session, ni dans l’URL.</p>
      <p>Lorsque la carte est affichée, le navigateur demande automatiquement à OpenFreeMap le style et les ressources cartographiques nécessaires. Ces requêtes peuvent communiquer les données techniques usuelles du navigateur, notamment l’adresse IP et les en-têtes, ainsi que la zone de carte demandée. Le style OpenFreeMap peut sélectionner dynamiquement les services chargés de fournir les tuiles, glyphes et sprites. À l’inverse, la latitude et la longitude ne sont transmises à OpenStreetMap par le lien vers la position que si l’utilisateur choisit explicitement d’ouvrir ce lien.</p>

      <h3>Session d’administration</h3>
      <p>L’espace réservé à l’administration utilise le cookie <code>messeances_admin_session</code> après authentification. Ce cookie est limité aux routes d’administration de l’API, marqué <code>HttpOnly</code> et <code>SameSite=Strict</code>, et expire après 12 heures. Il est nécessaire au maintien et à la sécurisation de la session d’administration.</p>

      <h3>Mesure d’audience Umami</h3>
      <p>MesSeances utilise en permanence une instance Umami auto-hébergée pour produire des statistiques de fréquentation. Le traceur fonctionne sans cookie et sans suivi entre sites. Il est intégré dans sa configuration par défaut, avec l’identifiant du site pour seul paramètre : aucun événement personnalisé, tag, identifiant distinct ou mécanisme d’identification des utilisateurs n’est configuré.</p>
      <p>Umami enregistre par défaut l’identifiant du site, le nom d’hôte, le chemin visité et sa chaîne de requête, le titre de la page, le site référent, le navigateur, le système d’exploitation, le type d’appareil, les dimensions de l’écran, la langue du navigateur, le pays, la région et la ville déduits de l’adresse IP, les paramètres UTM et les identifiants de clic publicitaire présents dans l’URL, des identifiants techniques d’événement et de session, ainsi que les horodatages associés. L’adresse IP sert à déterminer la localisation, mais n’est pas enregistrée.</p>

      <h3>Journaux techniques</h3>
      <p>Les journaux applicatifs de MesSeances enregistrent un horodatage, un niveau de gravité, un composant et un événement prédéfini. Pour une requête HTTP, ils contiennent la méthode normalisée, le motif de route plutôt que l’URL complète ou sa chaîne de requête, le statut de la réponse et la durée de traitement. Une erreur applicative est limitée à un code générique.</p>
      <p>Les opérations de mise à jour des séances et de synchronisation peuvent aussi enregistrer l’étape, le résultat ou le motif, la durée et des compteurs agrégés. En cas d’échec d’un fournisseur, le diagnostic est limité à la catégorie d’échec, l’opération, le statut HTTP, le numéro de tentative et la limite de tentatives.</p>
      <p>Ces journaux applicatifs excluent l’adresse IP du visiteur, l’agent utilisateur, l’URL complète et sa chaîne de requête, les corps des requêtes et réponses, les identifiants de connexion, mots de passe et jetons, les détails des proxys, ainsi que la valeur d’une panique récupérée et sa pile d’appels. Aucun journal de requêtes distinct du reverse proxy ou de l’hôte n’est conservé.</p>

      <h3>Affiches et images distantes</h3>
      <p>Les affiches et arrière-plans peuvent être chargés directement depuis les domaines de TMDB (<code>image.tmdb.org</code>), UGC, Kinepolis (<code>cdn.kinepolis.fr</code>), Pathé et ACSTA pour certaines images CGR. Lors de ce chargement, le fournisseur distant reçoit nécessairement la requête technique du navigateur, notamment l’adresse IP et les en-têtes transmis par celui-ci.</p>
    </section>

    <section class="legal-section" aria-labelledby="purposes-heading">
      <h2 id="purposes-heading">Finalités et bases légales envisagées</h2>
      <ul>
        <li><strong>Personnaliser les séances :</strong> mémoriser et appliquer les cinémas choisis. L’écriture et la lecture de cette préférence dans le stockage local sont exemptées de consentement au titre de l’<a href="https://www.legifrance.gouv.fr/loda/article_lc/LEGIARTI000037813978">article 82 de la loi « Informatique et Libertés »</a>, car elles sont strictement nécessaires à une fonctionnalité expressément demandée. Les identifiants de cinémas ne permettent pas, à eux seuls, d’identifier un utilisateur : leur stockage ne constitue donc pas en lui-même un traitement de données personnelles auquel attribuer une base légale au titre du RGPD. Si leur transmission à l’API constitue un traitement de données personnelles, celui-ci repose sur l’intérêt légitime de l’éditeur, conformément à l’<a href="https://www.cnil.fr/fr/reglement-europeen-protection-donnees/chapitre2#Article6">article 6, paragraphe 1, point f), du RGPD</a>, à seule fin de renvoyer les résultats personnalisés expressément demandés.</li>
        <li><strong>Trouver les cinémas proches et afficher la position :</strong> utiliser la position dans le navigateur pour calculer les distances, trier les cinémas et placer un repère sur la carte. Le traitement de la position repose sur le consentement, conformément à l’<a href="https://www.cnil.fr/fr/reglement-europeen-protection-donnees/chapitre2#Article6">article 6, paragraphe 1, point a), du RGPD</a>. Ce consentement est exprimé par le clic sur le bouton <strong>Utiliser ma position</strong>, sous réserve de l’autorisation du navigateur, que celui-ci peut demander ou avoir déjà mémorisée. Il peut être retiré pour l’avenir dans les réglages d’autorisation du navigateur. Le retour au mode d’affichage par ville cesse immédiatement l’utilisation locale de la position et l’efface de la page.</li>
        <li><strong>Administrer et sécuriser le service :</strong> contrôler l’accès à l’espace restreint, prévenir les accès non autorisés et les abus, et détecter, diagnostiquer puis corriger les incidents techniques. Ces traitements reposent sur l’intérêt légitime de l’éditeur à assurer la sécurité, la disponibilité et le bon fonctionnement du service, conformément à l’<a href="https://www.cnil.fr/fr/reglement-europeen-protection-donnees/chapitre2#Article6">article 6, paragraphe 1, point f), du RGPD</a>.</li>
        <li><strong>Afficher les contenus demandés :</strong> charger auprès des fournisseurs distants les affiches et arrière-plans nécessaires à la consultation des films et des séances, ainsi que le style et les ressources de la carte lorsque celle-ci est affichée. La transmission technique requise repose sur l’intérêt légitime de l’éditeur à présenter ces contenus dans le service demandé, conformément à l’article 6, paragraphe 1, point f), du RGPD.</li>
        <li><strong>Mesurer la fréquentation :</strong> produire des statistiques pour comprendre l’usage du site et améliorer son contenu. Ces traitements reposent sur l’intérêt légitime de l’éditeur au titre de l’article 6, paragraphe 1, point f), du RGPD et, pour les opérations relevant de l’article 82, une exemption de consentement uniquement si la configuration déployée respecte de manière démontrable les <a href="https://www.cnil.fr/fr/cookies-solutions-pour-les-outils-de-mesure-daudience">critères publiés par la CNIL</a>, notamment ceux de son <a href="https://www.cnil.fr/sites/default/files/2025-07/outil_d_auto-evaluation_mesure_d_audience.pdf">outil d’auto-évaluation 2025</a>.</li>
      </ul>
    </section>

    <section class="legal-section" aria-labelledby="recipients-heading">
      <h2 id="recipients-heading">Destinataires</h2>
      <p>Les données sont destinées à l’éditeur et, strictement dans la mesure nécessaire à leurs prestations, aux destinataires suivants :</p>
      <ul>
        <li><strong>Hetzner Online GmbH</strong>, qui héberge le site, l’API, l’instance Umami auto-hébergée et leurs bases de données sur la même infrastructure de production. Umami n’est donc pas fourni comme service SaaS par un destinataire distinct ;</li>
        <li><strong>TMDB, UGC, Kinepolis, Pathé et ACSTA</strong>, uniquement lorsque le navigateur charge automatiquement une affiche ou une image distante : chacun reçoit alors les données techniques de la requête nécessaires à la livraison de l’image concernée ;</li>
        <li><strong>OpenFreeMap et les services cartographiques sélectionnés par son style</strong>, lorsque la carte est affichée : ils reçoivent automatiquement les données techniques nécessaires à la livraison du style et des ressources correspondant à la zone demandée ;</li>
        <li><strong>OpenStreetMap</strong>, uniquement si l’utilisateur ouvre le lien vers sa position : la latitude et la longitude sont alors incluses dans l’adresse ouverte.</li>
      </ul>
      <p>Les données ne sont ni vendues ni transmises à des tiers pour leur propre publicité ou prospection commerciale.</p>
    </section>

    <section class="legal-section" aria-labelledby="retention-heading">
      <h2 id="retention-heading">Durées de conservation</h2>
      <dl class="legal-list">
        <div>
          <dt>Préférences locales</dt>
          <dd>Jusqu’à leur remplacement par une nouvelle sélection ou l’effacement des données du site dans le navigateur.</dd>
        </div>
        <div>
          <dt>Position du visiteur</dt>
          <dd>Uniquement en mémoire jusqu’au retour au mode d’affichage par ville ou au départ de la page. MesSeances ne conserve pas cette position. Le navigateur peut réutiliser une position mise en cache datant d’au plus 10 minutes ; sa conservation effective relève du navigateur.</dd>
        </div>
        <div>
          <dt>Session d’administration</dt>
          <dd>12 heures au maximum, ou jusqu’à la déconnexion.</dd>
        </div>
        <div>
          <dt>Journaux techniques</dt>
          <dd>Les journaux opérationnels des conteneurs sont acheminés vers <code>journald</code> sur le VPS dédié ; après installation du paramétrage prévu sur l’hôte, leur conservation est limitée à 30 jours au maximum. Les diagnostics persistés des synchronisations terminées sont supprimés après 30 jours ; les synchronisations en cours sont exclues de cette suppression. Aucun journal de requêtes distinct du reverse proxy ou de l’hôte n’est conservé.</dd>
        </div>
        <div>
          <dt>Données Umami</dt>
          <dd>Les données de mesure d’audience sont conservées 25 mois au maximum. Une purge automatisée s’exécute immédiatement lors du déploiement, puis chaque jour.</dd>
        </div>
      </dl>
    </section>

    <section class="legal-section" aria-labelledby="transfers-heading">
      <h2 id="transfers-heading">Transferts hors Union européenne</h2>
      <p>Le site, l’API, l’instance Umami auto-hébergée et leurs bases de données sont hébergés et stockés dans l’Union européenne, sur un serveur Hetzner situé en Finlande. Lorsque le navigateur charge directement des images auprès de TMDB, UGC, Kinepolis, Pathé ou ACSTA, ces fournisseurs peuvent traiter les requêtes techniques selon leurs propres infrastructures. Il en va de même pour les requêtes cartographiques adressées automatiquement à OpenFreeMap et aux services sélectionnés dynamiquement par son style, ainsi que pour la requête adressée à OpenStreetMap si l’utilisateur ouvre le lien vers sa position. MesSeances ne peut donc pas garantir l’absence de transfert hors de l’Union européenne pour ces requêtes externes.</p>
    </section>

    <section class="legal-section" aria-labelledby="rights-heading">
      <h2 id="rights-heading">Vos droits</h2>
      <p>Selon le traitement concerné et les conditions prévues par la réglementation, vous pouvez demander l’accès, la rectification ou l’effacement de vos données, la limitation du traitement, vous opposer au traitement, demander la portabilité des données ou retirer votre consentement lorsqu’il constitue la base légale.</p>
      <p>Pour exercer ces droits, contactez : <a href="mailto:contact@diikstra.fr">contact@diikstra.fr</a>. Une preuve d’identité peut être demandée uniquement lorsque cela est nécessaire pour vérifier l’identité du demandeur.</p>
      <p>Vous pouvez également adresser une réclamation à la <a href="https://www.cnil.fr/fr/plaintes" target="_blank" rel="noopener noreferrer">Commission nationale de l’informatique et des libertés (CNIL)<span class="sr-only">, ouverture dans un nouvel onglet</span></a>.</p>
    </section>

    <section class="legal-section" aria-labelledby="trackers-heading">
      <h2 id="trackers-heading">Résumé des traceurs et stockages</h2>
      <dl class="legal-list">
        <div>
          <dt>Stockage local des cinémas favoris</dt>
          <dd>Présent pour mémoriser la sélection de l’utilisateur. Qualification et régime applicables à confirmer avant publication.</dd>
        </div>
        <div>
          <dt>Cookie de session d’administration</dt>
          <dd>Présent uniquement après connexion à l’espace restreint, pour une durée maximale de 12 heures.</dd>
        </div>
        <div>
          <dt>Umami</dt>
          <dd>Actif en permanence, sans cookie ni suivi entre sites. Les données collectées sont détaillées ci-dessus.</dd>
        </div>
      </dl>
    </section>

    <section class="legal-section" aria-labelledby="updates-heading">
      <h2 id="updates-heading">Mise à jour de la politique</h2>
      <p>Date d’entrée en vigueur : 27 août 2026. Cette politique sera mise à jour si les traitements, prestataires ou obligations applicables évoluent.</p>
    </section>
  </LegalPageLayout>
</template>
