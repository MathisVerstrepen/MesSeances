export interface GeographicPoint {
  latitude: number
  longitude: number
}

export interface TheaterCoordinates {
  id: string
  name: string
  city: string
  latitude?: number | null
  longitude?: number | null
}

export interface TheaterDistanceRow<T extends TheaterCoordinates> {
  theater: T
  distanceKm: number | null
  isNearest: boolean
}

const EARTH_RADIUS_KM = 6371
const frenchNumberFormatter = new Intl.NumberFormat('fr-FR', {
  maximumFractionDigits: 1
})
const frenchCoordinateFormatter = new Intl.NumberFormat('fr-FR', {
  minimumFractionDigits: 4,
  maximumFractionDigits: 4,
  useGrouping: false
})
const frenchAccuracyFormatter = new Intl.NumberFormat('fr-FR', {
  maximumFractionDigits: 0
})

export function isValidGeographicPoint(point: GeographicPoint): boolean {
  return Number.isFinite(point.latitude)
    && Number.isFinite(point.longitude)
    && point.latitude >= -90
    && point.latitude <= 90
    && point.longitude >= -180
    && point.longitude <= 180
}

export function haversineDistanceKm(origin: GeographicPoint, destination: GeographicPoint): number | null {
  if (!isValidGeographicPoint(origin) || !isValidGeographicPoint(destination)) return null

  const toRadians = Math.PI / 180
  const latitudeDelta = (destination.latitude - origin.latitude) * toRadians
  const longitudeDelta = (destination.longitude - origin.longitude) * toRadians
  const originLatitude = origin.latitude * toRadians
  const destinationLatitude = destination.latitude * toRadians
  const haversine = Math.sin(latitudeDelta / 2) ** 2
    + Math.cos(originLatitude) * Math.cos(destinationLatitude) * Math.sin(longitudeDelta / 2) ** 2

  return 2 * EARTH_RADIUS_KM * Math.asin(Math.min(1, Math.sqrt(haversine)))
}

function compareFrench(left: string, right: string): number {
  return left.localeCompare(right, 'fr-FR') || (left < right ? -1 : left > right ? 1 : 0)
}

function compareTheaters(left: TheaterCoordinates, right: TheaterCoordinates): number {
  return compareFrench(left.city, right.city)
    || compareFrench(left.name, right.name)
    || compareFrench(left.id, right.id)
}

export function sortTheatersByDistance<T extends TheaterCoordinates>(
  theaters: readonly T[],
  origin: GeographicPoint
): TheaterDistanceRow<T>[] {
  const rows = theaters.map((theater) => ({
    theater,
    distanceKm: haversineDistanceKm(origin, {
      latitude: theater.latitude ?? Number.NaN,
      longitude: theater.longitude ?? Number.NaN
    }),
    isNearest: false
  }))

  rows.sort((left, right) => {
    if (left.distanceKm === null && right.distanceKm !== null) return 1
    if (left.distanceKm !== null && right.distanceKm === null) return -1
    if (left.distanceKm !== null && right.distanceKm !== null && left.distanceKm !== right.distanceKm) {
      return left.distanceKm - right.distanceKm
    }
    return compareTheaters(left.theater, right.theater)
  })

  const nearest = rows.find((row) => row.distanceKm !== null)
  if (nearest) nearest.isNearest = true
  return rows
}

export function formatTheaterDistance(distanceKm: number | null): string | null {
  if (distanceKm === null || !Number.isFinite(distanceKm) || distanceKm < 0) return null
  if (distanceKm === 0) return '0 km'
  if (distanceKm < 0.1) return '< 0,1 km'
  return `${frenchNumberFormatter.format(distanceKm)} km`
}

export function formatPositionCoordinate(coordinate: number): string {
  if (!Number.isFinite(coordinate)) return ''
  const normalizedCoordinate = Math.abs(coordinate) < 0.00005 ? 0 : coordinate
  return frenchCoordinateFormatter.format(normalizedCoordinate)
}

export function buildOpenStreetMapPositionUrl(point: GeographicPoint): string | null {
  if (!isValidGeographicPoint(point)) return null
  const latitude = formatPositionCoordinate(point.latitude).replace(',', '.')
  const longitude = formatPositionCoordinate(point.longitude).replace(',', '.')
  return `https://www.openstreetmap.org/?mlat=${latitude}&mlon=${longitude}#map=16/${latitude}/${longitude}`
}

export function formatPositionAccuracy(accuracyMeters: number | null): string {
  if (accuracyMeters === null || !Number.isFinite(accuracyMeters) || accuracyMeters < 0) {
    return 'précision indisponible'
  }
  return `précision environ ${frenchAccuracyFormatter.format(accuracyMeters)} m`
}
