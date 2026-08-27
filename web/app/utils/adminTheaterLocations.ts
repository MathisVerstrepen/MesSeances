export interface AdminTheaterLocationCoordinateDraft {
  latitude: string
  longitude: string
}

export interface AdminTheaterLocationCoordinateErrors {
  latitude?: string
  longitude?: string
}

export interface AdminTheaterLocationCoordinateValidation {
  coordinates: { latitude: number, longitude: number } | null
  errors: AdminTheaterLocationCoordinateErrors
}

const DECIMAL_PATTERN = /^[+-]?(?:\d+(?:[.,]\d*)?|[.,]\d+)$/

function parseCoordinate(value: string): number | null {
  const trimmed = value.trim()
  if (!trimmed || !DECIMAL_PATTERN.test(trimmed)) return null
  const parsed = Number(trimmed.replace(',', '.'))
  return Number.isFinite(parsed) ? parsed : null
}

export function parseAdminTheaterLocationCoordinates(draft: AdminTheaterLocationCoordinateDraft): AdminTheaterLocationCoordinateValidation {
  const errors: AdminTheaterLocationCoordinateErrors = {}
  const latitudeText = draft.latitude.trim()
  const longitudeText = draft.longitude.trim()
  const latitude = parseCoordinate(draft.latitude)
  const longitude = parseCoordinate(draft.longitude)

  if (!latitudeText) errors.latitude = 'Indiquez la latitude.'
  else if (latitude === null) errors.latitude = 'Utilisez un nombre décimal valide.'
  else if (latitude < -90 || latitude > 90) errors.latitude = 'La latitude doit être comprise entre -90 et 90.'

  if (!longitudeText) errors.longitude = 'Indiquez la longitude.'
  else if (longitude === null) errors.longitude = 'Utilisez un nombre décimal valide.'
  else if (longitude < -180 || longitude > 180) errors.longitude = 'La longitude doit être comprise entre -180 et 180.'

  if (Object.keys(errors).length > 0 || latitude === null || longitude === null) {
    return { coordinates: null, errors }
  }
  return { coordinates: { latitude, longitude }, errors }
}
