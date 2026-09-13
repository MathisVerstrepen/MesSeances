import { ref } from 'vue'
import { isValidGeographicPoint, type GeographicPoint } from './theaterDistance.ts'

const UNAVAILABLE = 'Position indisponible. Vérifiez que la localisation est activée, puis réessayez.'

// Browser access is injected and deferred until the mounted page activates nearby mode.
export function createCinemaDirectoryLocation(getGeolocation: () => Pick<Geolocation, 'getCurrentPosition'> | undefined) {
  const locationStatus = ref<'idle' | 'requesting' | 'active' | 'failed'>('idle')
  const locationError = ref('')
  const userPosition = ref<GeographicPoint | null>(null)
  const locationAccuracyMeters = ref<number | null>(null)
  let nearby = false
  let disposed = false
  let requestId = 0

  function clear() {
    requestId++
    userPosition.value = null
    locationAccuracyMeters.value = null
    locationStatus.value = 'idle'
    locationError.value = ''
  }

  function retry() {
    if (disposed || !nearby || locationStatus.value === 'requesting') return
    clear()
    const id = requestId
    const isCurrent = () => !disposed && nearby && id === requestId && locationStatus.value === 'requesting'
    function fail(message: string) {
      if (!isCurrent()) return
      locationStatus.value = 'failed'
      locationError.value = message
    }

    locationStatus.value = 'requesting'
    try {
      const geolocation = getGeolocation()
      if (!geolocation) {
        fail('La localisation n’est pas disponible dans ce navigateur. Continuez avec la liste par ville.')
        return
      }
      geolocation.getCurrentPosition((position) => {
        if (!isCurrent()) return
        const point = { latitude: position.coords.latitude, longitude: position.coords.longitude }
        if (!isValidGeographicPoint(point)) {
          fail(UNAVAILABLE)
          return
        }
        userPosition.value = point
        locationAccuracyMeters.value = Number.isFinite(position.coords.accuracy) && position.coords.accuracy >= 0
          ? position.coords.accuracy
          : null
        locationStatus.value = 'active'
      }, (error) => {
        fail(error.code === 1
          ? 'Localisation refusée. Autorisez l’accès à votre position dans les réglages du navigateur, puis réessayez.'
          : error.code === 3 ? 'La localisation a pris trop de temps. Réessayez.' : UNAVAILABLE)
      }, {
        enableHighAccuracy: false,
        timeout: 8000,
        maximumAge: 600000
      })
    } catch {
      fail(UNAVAILABLE)
    }
  }

  function setNearbyMode(enabled: boolean) {
    if (disposed || nearby === enabled) return
    nearby = enabled
    if (enabled) retry()
    else clear()
  }

  function dispose() {
    disposed = true
    clear()
  }

  return { locationStatus, locationError, userPosition, locationAccuracyMeters, setNearbyMode, retry, dispose }
}
