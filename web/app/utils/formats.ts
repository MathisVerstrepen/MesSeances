import type { QueryFormat, ShowtimeFormat } from '~/types/api'

export type FormatBrand = 'IMAX' | '3D' | 'DOLBY' | 'SCREENX' | 'LASER_ULTRA' | '4DX'

export const formatOptions: ReadonlyArray<{ value: QueryFormat; label: string; brand?: FormatBrand }> = [
  { value: 'ALL', label: 'Tous' },
  { value: '2D', label: '2D' },
  { value: '3D', label: '3D', brand: '3D' },
  { value: 'IMAX', label: 'IMAX', brand: 'IMAX' },
  { value: 'DOLBY', label: 'Dolby', brand: 'DOLBY' },
  { value: 'SCREENX', label: 'ScreenX', brand: 'SCREENX' },
  { value: 'LASER_ULTRA', label: 'Laser ULTRA by Kinepolis', brand: 'LASER_ULTRA' },
  { value: '4DX', label: '4DX', brand: '4DX' },
  { value: 'ICE', label: 'ICE' }
]

export function formatLabel(format: string): string {
  return formatOptions.find((option) => option.value === format.toUpperCase())?.label ?? format
}

export function formatBrand(format: string): FormatBrand | undefined {
  return formatOptions.find((option) => option.value === format.toUpperCase())?.brand
}

export function isShowtimeFormat(format: string): format is ShowtimeFormat {
  return formatOptions.some((option) => option.value !== 'ALL' && option.value === format)
}
