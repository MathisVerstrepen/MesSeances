import type { QueryFormat, QueryLanguage, ShowtimeFormat, ShowtimeLanguage } from '../types/api'
import { formatLabel, formatOptions } from './formats.ts'

export interface ShowtimeFilterOption<Value extends string> {
  readonly value: Value
  readonly label: string
}

export const queryLanguageOptions = [
  { value: 'ALL', label: 'Toutes les langues' },
  { value: 'VOSTFR', label: 'VOSTFR' },
  { value: 'VF', label: 'VF' }
] as const satisfies readonly ShowtimeFilterOption<QueryLanguage>[]

export const showtimeLanguageOptions = [
  ...queryLanguageOptions,
  { value: 'VO', label: 'VO' },
  { value: 'VF_SME', label: 'VF SME' }
] as const satisfies readonly ShowtimeFilterOption<'ALL' | ShowtimeLanguage>[]

export const queryLanguageValues = queryLanguageOptions.map((option) => option.value)
export const showtimeLanguageValues = ['VOSTFR', 'VF', 'VO', 'VF_SME'] as const satisfies readonly ShowtimeLanguage[]
export const queryFormatOptions = formatOptions
export const queryFormatValues = queryFormatOptions.map((option) => option.value)

export function languageLabel(language: string): string {
  return showtimeLanguageOptions.find((option) => option.value === language.toUpperCase())?.label ?? language
}

export function availableLanguageOptions(available: readonly ShowtimeLanguage[]): readonly ShowtimeFilterOption<'ALL' | ShowtimeLanguage>[] {
  const values = new Set(available)
  return showtimeLanguageOptions.filter((option) => option.value === 'ALL' || values.has(option.value))
}

export function availableFormatOptions(available: readonly ShowtimeFormat[]): readonly (typeof formatOptions)[number][] {
  const values = new Set(available)
  return formatOptions.filter((option) => {
    if (option.value === 'ALL') return true
    return values.has(option.value)
  })
}

export function showtimeFilterSummary(language: 'ALL' | ShowtimeLanguage, format: QueryFormat): string {
  const labels: string[] = []
  if (language !== 'ALL') labels.push(languageLabel(language))
  if (format !== 'ALL') labels.push(formatLabel(format))
  return labels.length ? labels.join(' · ') : `${languageLabel('ALL')} · ${formatLabel('ALL')}`
}
