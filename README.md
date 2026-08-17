# MovieFlow

MovieFlow fournit une API Go et une interface Nuxt en français pour consulter les séances UGC, gérer ses cinémas favoris et trouver un film compatible avec un créneau strict.

## Fonctionnalités v1

- `/` — planning de la journée cinéma (08:00–02:00), affiché par cinéma ou par film, avec choix de la date, de la langue, du format et du zoom ;
- `/recherche` — recherche d’une séance entièrement comprise dans un créneau, avec délai publicitaire optionnel de 20 minutes ;
- `/films` — catalogue paginé des films présents dans l’instantané courant, dédupliqués par correspondance TMDB et recherchables par titre ;
- `/film/:slug` — séances UGC et Kinepolis d’un film correspondant au même identifiant TMDB, regroupées par cinéma ;
- `/cinemas` — sélection des cinémas favoris, recherche par nom ou ville et sélection par ville.
- `/admin/sync` — contrôle administrateur des synchronisations UGC et Kinepolis, ensemble ou séparément.

Les liens de réservation ouvrent la page officielle du fournisseur dans un nouvel onglet lorsqu’une URL est disponible. Sinon, l’interface indique que la réservation est indisponible.

### Cinémas favoris

Les favoris sont enregistrés uniquement dans le navigateur, sous la clé `movieflow.favoriteTheaterIds.v1` de `localStorage`. Ils ne créent aucun compte et ne sont pas synchronisés entre navigateurs ou appareils. Au premier chargement, MovieFlow sélectionne les cinémas correspondant à `city=Lille` ; si cette sélection est vide, le premier cinéma du catalogue devient le choix initial. Les identifiants absents de l’instantané courant sont supprimés et l’interface conserve toujours au moins un favori lorsque des cinémas sont disponibles.

Le planning, la fiche d’un film et la recherche par créneau utilisent ces favoris. La recherche permet d’en décocher temporairement certains sans modifier les favoris enregistrés.

## Prérequis

- Go 1.23 ou version ultérieure
- Node.js compatible avec les dépendances résolues (Node 22.23.1 vérifié)
- npm 10.9.8 vérifié
- Docker avec le module Compose

## Installation

Installer les dépendances depuis la racine :

```sh
cd api && go mod download
cd ../web && npm install
```

## PostgreSQL local

Le fichier `compose.yaml` démarre PostgreSQL 18 Alpine avec un volume persistant :

```sh
docker compose up -d --wait postgres
export DATABASE_URL='postgres://movieflow:movieflow@localhost:5432/movieflow?sslmode=disable'
```

Pour arrêter PostgreSQL sans supprimer ses données :

```sh
docker compose down
```

Ne pas utiliser `docker compose down -v` sauf volonté explicite de supprimer les données locales.

## Synchronisation des séances UGC

Une synchronisation complète exige `DATABASE_URL`. Elle ouvre PostgreSQL, applique les migrations puis récupère tous les cinémas UGC. Un instantané complet valide remplace toutes les données relationnelles dans une seule transaction et avance sa version. Tout échec avant `COMMIT` conserve l’instantané précédent ; une perte de confirmation du `COMMIT` est signalée comme un échec même si PostgreSQL a pu publier l’ancien ou le nouvel instantané complet. Aucune fusion partielle n’est effectuée.

Après confirmation du remplacement UGC, `sync-ugc` lance éventuellement un enrichissement TMDB avec `TMDB_API_READ_ACCESS_TOKEN`. Cette étape recherche chaque film UGC par titre français, vérifie le titre exact et la durée, puis accepte uniquement les correspondances de confiance élevée. Les décisions `matched`, `review_required` et `unmatched`, leurs candidats bornés et les métadonnées sélectionnées restent en PostgreSQL lors des remplacements UGC suivants. Un cache de métadonnées, dont les affiches `w500` et les fonds `w780` disponibles, est réutilisé pendant 30 jours ; les décisions ambiguës ou sans résultat sont retentées après 7 jours.

L’absence du jeton affiche `enrichment=skipped`. Une panne TMDB, une limite de débit, une réponse invalide ou une erreur de persistance après le `COMMIT` affiche un avertissement générique et `enrichment=degraded`, sans annuler l’instantané UGC ni rendre la commande en échec. Les résultats déjà enrichis restent utilisables, y compris lorsqu’ils sont périmés et que leur actualisation échoue. Le mode diagnostic n’accède jamais au jeton ni aux tables d’enrichissement.

```sh
cd api
DATABASE_URL='postgres://movieflow:movieflow@localhost:5432/movieflow?sslmode=disable' \
  TMDB_API_READ_ACCESS_TOKEN="$TMDB_API_READ_ACCESS_TOKEN" \
  go run ./cmd/sync-ugc -proxy-file /chemin/vers/proxies.txt
```

La fenêtre par défaut va de la date courante à J+7 en heure de Paris. `-from` et `-through` acceptent une fenêtre inclusive de 14 jours maximum.

Le mode diagnostic valide un seul cinéma sans lire `DATABASE_URL`, ouvrir PostgreSQL, lancer les migrations ni persister de données :

```sh
cd api
go run ./cmd/sync-ugc -proxy-file /chemin/vers/proxies.txt -cinema-id 25
```

Toutes les requêtes UGC passent obligatoirement par les proxies fournis. Le client emploie `net/http`, une seule requête simultanée et un intervalle global de deux secondes. Il effectue au plus une nouvelle tentative après une erreur de transport ou un statut 5xx. Un statut 403 ou 429, une page de blocage ou un challenge arrête immédiatement la synchronisation. Ne jamais publier le fichier de proxies, ses identifiants ou sa sortie ; les messages ne contiennent que des compteurs et des informations publiques.

## Démarrage local

Après une synchronisation complète réussie, lancer PostgreSQL, l’API et Nuxt depuis la racine :

```sh
make dev
```

L’API utilise Air `v1.61.7`, version compatible avec Go 1.23 et exécutée par `go run` sans installation globale. Le premier lancement la télécharge dans le cache Go. Air reconstruit et redémarre uniquement l’API après une modification des sources Go ; une erreur de compilation est affichée sans arrêter Nuxt ni le processus de développement. Nuxt conserve son rechargement à chaud. `Ctrl-C` arrête proprement Air, l’API et Nuxt ; PostgreSQL reste disponible dans Docker.

Pour lancer les processus séparément, démarrer l’API :

```sh
cd api
DATABASE_URL='postgres://movieflow:movieflow@localhost:5432/movieflow?sslmode=disable' go run ./cmd/api
```

Dans un second terminal :

```sh
npm --prefix web run dev
```

Ouvrir `http://localhost:3000`. L’API écoute par défaut sur `http://localhost:8080`.

Au démarrage, l’API exige `DATABASE_URL`, applique les migrations et charge un instantané national complet et valide. Une base vide ou invalide provoque un arrêt. Ensuite, l’API vérifie la version PostgreSQL et charge chaque nouvelle version complète en mémoire. Une panne de lecture ou un instantané invalide conserve la dernière version valide en mémoire. Aucun import JSON, fichier de séances ou repli synthétique n’existe.

## Configuration

Les exécutables Go chargent automatiquement le premier fichier `.env` trouvé dans le répertoire courant puis son parent. Un `.env` placé à la racine fonctionne donc depuis la racine du dépôt comme depuis `api/`. Les variables déjà définies dans l’environnement du processus restent prioritaires et les deux fichiers ne sont jamais fusionnés. Copier `.env.example` vers `.env` pour démarrer ; un fichier existant illisible ou mal formé bloque le démarrage avec une erreur de configuration générique.

- `DATABASE_URL` : connexion PostgreSQL obligatoire pour l’API et une synchronisation complète ; inutilisée en mode diagnostic.
- `TMDB_API_READ_ACCESS_TOKEN` : jeton bearer TMDB facultatif, lu uniquement depuis l’environnement après publication réussie de l’instantané UGC ; ne jamais le passer dans une URL, un argument, un fichier versionné ou une sortie de commande.
- `ADMIN_PASSWORD` : mot de passe administrateur obligatoire pour activer les API de revue TMDB. Il reste uniquement côté serveur, n’est jamais journalisé et sa rotation invalide immédiatement toutes les sessions existantes.
- `PROXY_FILE` : chemin serveur facultatif vers le fichier de proxies utilisé par les synchronisations lancées depuis l’administration. Une valeur absente ou vide désactive uniquement ces routes de synchronisation. Une valeur configurée mais illisible ou invalide bloque le démarrage de l’API avec une erreur générique, sans exposer chemin, proxies ou identifiants.
- `PORT` : port de l’API, `8080` par défaut.
- `WEB_ORIGIN` : origine autorisée par CORS, `http://localhost:3000` par défaut.
- `NUXT_PUBLIC_API_BASE` : URL de base utilisée par Nuxt, `http://localhost:8080` par défaut.

## API v1

Toutes les routes publiques sont en lecture seule et renvoient du JSON. Les dates utilisent `YYYY-MM-DD`, les heures de requête `HH:MM`, les listes de cinémas des identifiants séparés par des virgules et les horodatages de réponse UTC.

| Route | Paramètres implémentés | Réponse |
|---|---|---|
| `GET /api/v1/timeline` | `date` requis ; `theaters` optionnel ; `language=ALL|VOSTFR|VF` (`ALL` par défaut) | `date`, `timezone`, `window_start_time`, `window_end_time`, `theaters[]` |
| `GET /api/v1/theaters` | `city`, `chain` optionnels ; seule la chaîne `ugc` est disponible | liste de cinémas |
| `GET /api/v1/movies` | `currently_screened=true|false`, `search`, `page` (défaut `1`), `page_size` (défaut `24`, maximum `100`) | `items`, `page`, `page_size`, `total`, dédupliqués avant comptage et pagination |
| `GET /api/v1/movies/{slug}/showtimes` | `date` requis ; `city` ou `theaters`, mutuellement exclusifs ; slug TMDB canonique pour un film correspondant | `movie`, `date`, `theaters[]`, avec les séances de tous les fournisseurs correspondant au même identifiant TMDB |
| `GET /api/v1/search/slot` | `city` ou `theaters` requis, mutuellement exclusifs ; `date`, `start_after`, `finish_before` requis ; `buffer_ads` de `0` à `120` (`20` par défaut) ; `language=ALL|VOSTFR|VF` | liste de résultats compatibles |

### API administrateur

Les routes suivantes exigent une session administrateur côté serveur, sauf la connexion et la vérification de session. La connexion reçoit `{"password":"..."}` et crée pour 12 heures un cookie signé `HttpOnly`, `SameSite=Strict`, marqué `Secure` sous HTTPS. La rotation de `ADMIN_PASSWORD` invalide les cookies existants. Les requêtes avec corps acceptent uniquement du JSON borné à 4 Kio. Toutes les mutations exigent un en-tête `Origin` exactement égal à `WEB_ORIGIN`; CORS n’autorise les credentials que pour cette origine. Ne jamais placer mot de passe ou valeur du cookie dans `localStorage`, `sessionStorage`, URL ou journaux.

| Route | Corps / paramètres | Réponse |
|---|---|---|
| `POST /api/v1/admin/login` | JSON `password` | `authenticated: true`; échecs génériques et limitation par adresse après cinq échecs sur 15 minutes |
| `GET /api/v1/admin/session` | aucun | `authenticated: true|false` |
| `POST /api/v1/admin/logout` | corps vide | supprime la session |
| `GET /api/v1/admin/syncs` | aucun | tâche lancée par ce processus, courante ou dernière, ou `job: null` |
| `POST /api/v1/admin/syncs/{target}` | corps vide ; `target=all|ugc|kinepolis` | accepte la tâche asynchrone avec `202`; refuse un chevauchement avec `409` |
| `GET /api/v1/admin/tmdb-matches` | `limit` (défaut `50`, maximum `100`), `offset` (défaut `0`) | `items`, `limit`, `offset`; chaque item contient identité, titre et durée UGC, candidats TMDB stockés et date d’évaluation |
| `POST /api/v1/admin/tmdb-matches/{source_movie_id}/approve` | JSON `tmdb_id`, obligatoirement présent parmi les candidats stockés | récupère les détails TMDB côté serveur, publie atomiquement la correspondance et avance la révision |
| `POST /api/v1/admin/tmdb-matches/{source_movie_id}/reject` | corps vide | enregistre durablement `rejected` et avance la révision |

Une approbation ou un rejet échoue si la décision n’est plus `review_required`, si le titre ou la durée UGC a changé, ou si le candidat demandé n’était pas stocké. Deux décisions concurrentes ne peuvent pas toutes deux réussir. Un rejet reste définitif tant que l’empreinte titre/durée UGC ne change pas; une nouvelle empreinte autorise une nouvelle évaluation. Sans `ADMIN_PASSWORD`, toutes les fonctions administrateur échouent de manière fermée. Sans `TMDB_API_READ_ACCESS_TOKEN`, consultation et rejet restent disponibles, mais l’approbation échoue sans publier de données. HTTPS est requis en production.

Sans `theaters`, la timeline utilise la zone par défaut Lille–Villeneuve-d’Ascq. Sans `city` ni `theaters`, les séances d’une fiche film couvrent tous les cinémas de l’instantané.

Les synchronisations administrateur s’exécutent dans le processus API sans maintenir la requête `POST` ouverte. `all` exécute UGC puis Kinepolis et s’arrête au premier échec ; un remplacement UGC déjà validé et publié n’est pas annulé si Kinepolis échoue ensuite. Le statut expose les états de chaque fournisseur et la fenêtre Paris de la date de lancement à J+7, mais jamais la configuration proxy ni les erreurs internes. Une seule tâche peut être active par processus API. Le statut n’est ni durable ni partagé entre plusieurs réplicas : un redémarrage l’efface, et les synchronisations CLI ne sont pas affichées. Aucun historique, annulation ou planification n’est fourni.

Champs exposés :

- cinéma : `id`, `slug`, `name`, `address`, `city`, `postal_code`, `available_dates`, `accepted_passes` ;
- film de catalogue : `slug`, `title`, `runtime_minutes`, `poster_url`, `tmdb_id`, `overview`, `release_date`, `genres` ; `slug` vaut `tmdb-film-<id>` lorsque `tmdb_id` est positif, sinon il conserve le slug fournisseur ; les trois champs scalaires enrichis valent `null` sans correspondance et `genres` vaut `[]` ;
- séance : `id`, `movie`, `start_time`, `end_time`, `language`, `format`, `room`, `booking_url` ;
- résultat de créneau : `showtime`, `theater`, `effective_end_time`, `buffer_ads_minutes`, `slack_before_minutes`, `slack_after_minutes`.

La timeline ajoute `start_offset_minutes`, `duration_minutes` et `backdrop_url` aux séances et expose, pour chaque cinéma, `id`, `slug`, `name`, `city`, `accepted_passes` et `showtimes`. `backdrop_url` contient uniquement une URL TMDB canonique `w780` pour une correspondance enrichie qui dispose d’un fond ; sinon sa valeur est `null`. Ce champ reste au niveau de la séance de timeline et n’est pas ajouté au film imbriqué, au catalogue, aux fiches film, aux résultats de créneau ni aux API administrateur. Une fiche film expose, pour chaque cinéma, `id`, `slug`, `name`, `city` et `showtimes`.

## Vérifications

Ces commandes n’effectuent aucune synchronisation réseau UGC ni aucun appel TMDB réel :

```sh
docker compose config
cd api && go test ./...
npm --prefix web run typecheck
```

Les tests d’intégration PostgreSQL utilisent une structure isolée et temporaire :

```sh
cd api
TEST_DATABASE_URL='postgres://movieflow:movieflow@localhost:5432/movieflow?sslmode=disable' \
  go test ./internal/database ./internal/schedule ./internal/enrichment -run 'Integration$' -count=1
```

## Comportement des données

Les pages publiques UGC récupérées par `sync-ugc` sont la source des cinémas, films, séances et liens de réservation. PostgreSQL contient exactement le dernier instantané national complet validé ; l’API sert uniquement sa dernière version complète chargée en mémoire. L’interface Nuxt ne complète pas ces données depuis une autre source.

Le catalogue ne constitue pas une base éditoriale indépendante : il reste déduit des séances de l’instantané courant et `currently_screened=false` renvoie donc un catalogue vide. Les enregistrements UGC et Kinepolis ayant exactement le même identifiant TMDB positif forment une seule identité publique `tmdb-film-<id>` avant le calcul de `total` et la pagination. Sa fiche agrège leurs séances, mais conserve pour chacune l’identifiant, le fournisseur, le cinéma et l’URL de réservation d’origine. Les enregistrements sans correspondance validée, y compris ceux en attente de revue, restent séparés sous leur slug fournisseur même si leurs titres sont identiques. Les anciennes routes fournisseur d’un film désormais associé ne sont ni des alias ni des redirections et renvoient `404`. UGC reste autoritaire pour le titre, la durée et l’appartenance aux séances. Une correspondance TMDB peut uniquement ajouter l’identifiant TMDB, le résumé français, la date de sortie, les genres, une affiche prioritaire et un fond réservé à la timeline ; elle ne peut ni retirer un film ou une séance, ni changer le titre ou la durée UGC. Sans cache valide, l’affiche UGC reste utilisée et la timeline conserve son fond de secours. Distribution, équipe, bande-annonce, classification, note et popularité ne sont pas stockées.

Les appels TMDB sont bornés à une recherche et au plus cinq détails candidats par film, espacés d’au moins 250 ms, avec un délai HTTP de 10 secondes et un budget global de deux minutes par synchronisation. Les statuts 401, 403 et 429 arrêtent la passe. Le jeton, les corps d’erreur du fournisseur et les réponses brutes ne sont jamais persistés ni affichés. La migration 004 ajoute le fond nullable sans supprimer les métadonnées existantes, sans appeler TMDB et sans avancer la révision d’enrichissement. Elle marque les lignes de cache antérieures comme immédiatement éligibles à leur prochaine actualisation. La prochaine synchronisation normale réussie avec `TMDB_API_READ_ACCESS_TOKEN` renseigne ensuite les fonds disponibles et rétablit l’échéance habituelle de 30 jours ; une absence de jeton ou un échec laisse les anciennes métadonnées utilisables et la ligne éligible à une tentative normale ultérieure. Aucun effacement de cache ni backfill SQL ou réseau direct n’est requis. Pour suspendre les appels fournisseur, retirer `TMDB_API_READ_ACCESS_TOKEN` puis relancer normalement la synchronisation ; ne pas supprimer les tables de planning ou d’enrichissement. Après application de la migration 004, ne pas redéployer un binaire dont la liste intégrée s’arrête à 003, ne pas retirer son entrée d’historique et ne pas supprimer la colonne : déployer un binaire compatible ou plus récent. La migration ne peut pas restaurer les anciennes échéances futures de `refresh_after` sans sauvegarde.

TMDB exige son logo approuvé et la mention `This product uses the TMDB API but is not endorsed or certified by TMDB.` sur la page Crédits du site avant activation en production. L’exploitant doit également confirmer l’éligibilité du compte et de la licence TMDB.

La recherche `city=Lille` couvre Lille et Villeneuve-d’Ascq. Les créneaux conservent des bornes strictes, y compris après minuit et avec le délai publicitaire demandé. Le filtre `VF` inclut aussi les séances `VF_SME` ; les autres valeurs de séance possibles sont `VOSTFR` et `VO`.
