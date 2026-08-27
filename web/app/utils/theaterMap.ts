import type { Feature, FeatureCollection, Point } from 'geojson'
import type { Provider, Theater } from '../types/api'
import { isValidGeographicPoint } from './theaterDistance.ts'

export const THEATER_PROVIDER_COLORS = {
  ugc: '#0b5cad',
  kinepolis: '#7e22ce',
  pathe: '#d97706',
  cgr: '#c81e1e'
} satisfies Record<Provider, string>

export const THEATER_PROVIDER_LABELS = {
  ugc: 'UGC',
  kinepolis: 'Kinepolis',
  pathe: 'Pathé',
  cgr: 'CGR'
} satisfies Record<Provider, string>

export interface TheaterMapProperties {
  id: string
  provider: Provider
  favorite: boolean
}

export type TheaterMapFeature = Feature<Point, TheaterMapProperties>
export type TheaterMapFeatureCollection = FeatureCollection<Point, TheaterMapProperties>

export interface TheaterMapBounds {
  west: number
  south: number
  east: number
  north: number
  count: number
}

const EARTH_RADIUS_METERS = 6_371_000
const COINCIDENT_SPREAD_METERS = 35

function spreadCoordinate(longitude: number, latitude: number, bearing: number): [number, number] {
  const angularDistance = COINCIDENT_SPREAD_METERS / EARTH_RADIUS_METERS
  const latitudeRadians = latitude * Math.PI / 180
  const longitudeRadians = longitude * Math.PI / 180
  const bearingRadians = bearing * Math.PI / 180
  const destinationLatitude = Math.asin(
    Math.sin(latitudeRadians) * Math.cos(angularDistance)
      + Math.cos(latitudeRadians) * Math.sin(angularDistance) * Math.cos(bearingRadians)
  )
  const destinationLongitude = longitudeRadians + Math.atan2(
    Math.sin(bearingRadians) * Math.sin(angularDistance) * Math.cos(latitudeRadians),
    Math.cos(angularDistance) - Math.sin(latitudeRadians) * Math.sin(destinationLatitude)
  )

  return [
    ((destinationLongitude * 180 / Math.PI + 540) % 360) - 180,
    destinationLatitude * 180 / Math.PI
  ]
}

export function buildTheaterFeatureCollection(
  theaters: readonly Theater[],
  favoriteTheaterIds: ReadonlySet<string>
): TheaterMapFeatureCollection {
  const locatedTheaters = theaters
    .filter((theater) => isValidGeographicPoint({
      latitude: theater.latitude ?? Number.NaN,
      longitude: theater.longitude ?? Number.NaN
    }))
    .toSorted((left, right) => {
      const longitudeDifference = left.longitude! - right.longitude!
      if (longitudeDifference !== 0) return longitudeDifference
      const latitudeDifference = left.latitude! - right.latitude!
      if (latitudeDifference !== 0) return latitudeDifference
      return left.id.localeCompare(right.id, 'fr-FR') || (left.id < right.id ? -1 : left.id > right.id ? 1 : 0)
    })

  const groups = new Map<string, Theater[]>()
  for (const theater of locatedTheaters) {
    const key = `${theater.longitude},${theater.latitude}`
    const group = groups.get(key) ?? []
    group.push(theater)
    groups.set(key, group)
  }

  const features: TheaterMapFeature[] = []
  for (const group of groups.values()) {
    for (const [index, theater] of group.entries()) {
      const longitude = theater.longitude!
      const latitude = theater.latitude!
      const coordinates: [number, number] = group.length === 1
        ? [longitude, latitude]
        : spreadCoordinate(longitude, latitude, index * 360 / group.length)
      features.push({
        type: 'Feature',
        geometry: { type: 'Point', coordinates },
        properties: {
          id: theater.id,
          provider: theater.provider,
          favorite: favoriteTheaterIds.has(theater.id)
        }
      })
    }
  }

  return { type: 'FeatureCollection', features }
}

export function theaterFeatureBounds(collection: TheaterMapFeatureCollection): TheaterMapBounds | null {
  const first = collection.features[0]?.geometry.coordinates
  if (!first) return null

  let west = first[0]!
  let south = first[1]!
  let east = first[0]!
  let north = first[1]!
  for (const feature of collection.features.slice(1)) {
    const longitude = feature.geometry.coordinates[0]!
    const latitude = feature.geometry.coordinates[1]!
    west = Math.min(west, longitude)
    south = Math.min(south, latitude)
    east = Math.max(east, longitude)
    north = Math.max(north, latitude)
  }
  return { west, south, east, north, count: collection.features.length }
}
