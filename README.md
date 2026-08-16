# MovieFlow

MovieFlow fournit une API Go et une interface Nuxt en français pour visualiser une journée de séances et rechercher un film dans un créneau strict.

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

PostgreSQL contient exactement le dernier instantané complet de tous les cinémas UGC France découverts. Sans paramètre `theaters`, la timeline reste limitée à Lille et Villeneuve d’Ascq. Des identifiants explicites permettent de consulter tout cinéma français présent. La recherche `city=Lille` couvre cette même zone. Les créneaux conservent des bornes strictes, y compris après minuit et avec le délai publicitaire demandé. Les horaires HTTP sont sérialisés en UTC et les séances fournissent leur URL officielle de réservation UGC.
