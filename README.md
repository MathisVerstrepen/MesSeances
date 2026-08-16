# MovieFlow

MovieFlow fournit une API Go et une interface Nuxt en français pour consulter les séances UGC, gérer ses cinémas favoris et trouver un film compatible avec un créneau strict.

## Fonctionnalités v1

- `/` — planning de la journée cinéma (08:00–02:00), affiché par cinéma ou par film, avec choix de la date, de la langue, du format et du zoom ;
- `/recherche` — recherche d’une séance entièrement comprise dans un créneau, avec délai publicitaire optionnel de 20 minutes ;
- `/films` — catalogue paginé des films présents dans l’instantané courant, avec recherche par titre ;
- `/film/:slug` — séances d’un film dans les cinémas favoris, regroupées par cinéma ;
- `/cinemas` — sélection des cinémas favoris, recherche par nom ou ville et sélection par ville.

Les liens de réservation ouvrent la page officielle UGC dans un nouvel onglet lorsqu’une URL est disponible. Sinon, l’interface indique que la réservation est indisponible.

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

```sh
cd api
DATABASE_URL='postgres://movieflow:movieflow@localhost:5432/movieflow?sslmode=disable' \
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

Après une synchronisation complète réussie, lancer l’API :

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

- `DATABASE_URL` : connexion PostgreSQL obligatoire pour l’API et une synchronisation complète ; inutilisée en mode diagnostic.
- `PORT` : port de l’API, `8080` par défaut.
- `WEB_ORIGIN` : origine autorisée par CORS, `http://localhost:3000` par défaut.
- `NUXT_PUBLIC_API_BASE` : URL de base utilisée par Nuxt, `http://localhost:8080` par défaut.

## API v1

Toutes les routes sont en lecture seule et renvoient du JSON. Les dates utilisent `YYYY-MM-DD`, les heures de requête `HH:MM`, les listes de cinémas des identifiants séparés par des virgules et les horodatages de réponse UTC.

| Route | Paramètres implémentés | Réponse |
|---|---|---|
| `GET /api/v1/timeline` | `date` requis ; `theaters` optionnel ; `language=ALL|VOSTFR|VF` (`ALL` par défaut) | `date`, `timezone`, `window_start_time`, `window_end_time`, `theaters[]` |
| `GET /api/v1/theaters` | `city`, `chain` optionnels ; seule la chaîne `ugc` est disponible | liste de cinémas |
| `GET /api/v1/movies` | `currently_screened=true|false`, `search`, `page` (défaut `1`), `page_size` (défaut `24`, maximum `100`) | `items`, `page`, `page_size`, `total` |
| `GET /api/v1/movies/{slug}/showtimes` | `date` requis ; `city` ou `theaters`, mutuellement exclusifs | `movie`, `date`, `theaters[]` |
| `GET /api/v1/search/slot` | `city` ou `theaters` requis, mutuellement exclusifs ; `date`, `start_after`, `finish_before` requis ; `buffer_ads` de `0` à `120` (`20` par défaut) ; `language=ALL|VOSTFR|VF` | liste de résultats compatibles |

Sans `theaters`, la timeline utilise la zone par défaut Lille–Villeneuve-d’Ascq. Sans `city` ni `theaters`, les séances d’une fiche film couvrent tous les cinémas de l’instantané.

Champs exposés :

- cinéma : `id`, `slug`, `name`, `address`, `city`, `postal_code`, `available_dates`, `accepted_passes` ;
- film de catalogue : `slug`, `title`, `runtime_minutes`, `poster_url` ;
- séance : `id`, `movie`, `start_time`, `end_time`, `language`, `format`, `room`, `booking_url` ;
- résultat de créneau : `showtime`, `theater`, `effective_end_time`, `buffer_ads_minutes`, `slack_before_minutes`, `slack_after_minutes`.

La timeline ajoute `start_offset_minutes` et `duration_minutes` aux séances et expose, pour chaque cinéma, `id`, `slug`, `name`, `city`, `accepted_passes` et `showtimes`. Une fiche film expose, pour chaque cinéma, `id`, `slug`, `name`, `city` et `showtimes`.

## Vérifications

Ces commandes n’effectuent aucune synchronisation réseau UGC :

```sh
docker compose config
cd api && go test ./...
npm --prefix web run typecheck
```

Les tests d’intégration PostgreSQL utilisent une structure isolée et temporaire :

```sh
cd api
TEST_DATABASE_URL='postgres://movieflow:movieflow@localhost:5432/movieflow?sslmode=disable' \
  go test ./internal/schedule -run '^TestPostgresStoreIntegration$' -count=1
```

## Comportement des données

Les pages publiques UGC récupérées par `sync-ugc` sont la source des cinémas, films, séances et liens de réservation. PostgreSQL contient exactement le dernier instantané national complet validé ; l’API sert uniquement sa dernière version complète chargée en mémoire. L’interface Nuxt ne complète pas ces données depuis une autre source.

Le catalogue ne constitue pas une base éditoriale de films : il est déduit des séances de l’instantané courant. `currently_screened=false` renvoie donc un catalogue vide. Les seules métadonnées de film disponibles sont le titre, la durée et une affiche optionnelle ; aucun synopsis, genre, distribution, équipe, bande-annonce, classification ou autre enrichissement n’est fourni. Les informations de cinéma se limitent au nom, à l’adresse textuelle, à la ville, au code postal, aux dates disponibles et à l’indication UGC Illimité ; aucune coordonnée géographique ni liste exhaustive de services ou de tarifs n’est disponible.

La recherche `city=Lille` couvre Lille et Villeneuve-d’Ascq. Les créneaux conservent des bornes strictes, y compris après minuit et avec le délai publicitaire demandé. Le filtre `VF` inclut aussi les séances `VF_SME` ; les autres valeurs de séance possibles sont `VOSTFR` et `VO`.
