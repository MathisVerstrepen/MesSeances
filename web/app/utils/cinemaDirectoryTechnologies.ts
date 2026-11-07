import type { Theater } from '../types/api'
import { formatOptions, type FormatBrand } from './formats.ts'

export const cinemaTechnologyOptions = formatOptions.flatMap((option) =>
  option.brand ? [{ value: option.brand, label: option.label }] : [],
)

export function normalizeCinemaTechnologies(
  values: readonly string[],
): FormatBrand[] {
  const selected = new Set(values.map((value) => value.trim().toUpperCase()))
  return cinemaTechnologyOptions
    .map((option) => option.value)
    .filter((value) => selected.has(value))
}

export function cinemaDirectoryTechnologies(theaters: readonly Theater[]) {
  const available = new Set(
    theaters.flatMap((theater) => theater.available_formats ?? []),
  )
  return cinemaTechnologyOptions.filter((option) => available.has(option.value))
}
