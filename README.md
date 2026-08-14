# MovieFlow

Première version locale de MovieFlow : API Go et interface Nuxt en français pour visualiser une journée de séances et rechercher un film dans un créneau strict.

## Prérequis

- Go 1.22 ou version ultérieure
- Node.js compatible avec les dépendances résolues (Node 22.23.1 vérifié)
- npm 10.9.8 vérifié

## Installation

Installer les dépendances de chaque application depuis la racine :

```sh
cd api && go mod download
cd ../web && npm install
```

## Démarrage local

Dans un premier terminal :

```sh
cd api && go run ./cmd/api
```

Dans un second terminal :

```sh
npm --prefix web run dev
```

Ouvrir `http://localhost:3000`. L’API écoute par défaut sur `http://localhost:8080`.

## Configuration

Les valeurs peuvent être remplacées directement dans l’environnement, sans fichier `.env` obligatoire :

- `PORT` : port de l’API, `8080` par défaut.
- `WEB_ORIGIN` : origine autorisée par CORS, `http://localhost:3000` par défaut.
- `NUXT_PUBLIC_API_BASE` : URL de base utilisée par Nuxt, `http://localhost:8080` par défaut.

Exemple :

```sh
(cd api && PORT=8081 WEB_ORIGIN=http://localhost:3001 go run ./cmd/api)
NUXT_PUBLIC_API_BASE=http://localhost:8081 npm --prefix web run dev -- --port 3001
```

## Vérifications minimales

```sh
cd api && go test ./...
npm --prefix web run typecheck
npm --prefix web run build
```

Vérifications manuelles de l’API :

```sh
curl -fsS 'http://localhost:8080/api/v1/timeline?date=2026-08-15&language=ALL'
curl -fsS 'http://localhost:8080/api/v1/search/slot?city=Lille&date=2026-08-15&start_after=12:00&finish_before=15:00&buffer_ads=20&language=ALL'
```

## État des données

Cette version utilise exclusivement des fixtures en mémoire : deux cinémas de la zone de Lille et quatre séances fictives. Les dates demandées déterminent les horodatages, mais les films, horaires locaux et salles restent démonstratifs. Aucun appel en direct à UGC, stockage persistant, scraping, lien de réservation ou service de production n’est inclus.
