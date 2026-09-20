import type { QueryFormat, QueryLanguage, ShowtimeFormat, ShowtimeLanguage } from '../types/api'
import { formatLabel, formatOptions } from './formats.ts'

export interface ShowtimeFilterOption<Value extends string> {
  readonly value: Value
  readonly label: string
}

export const queryLanguageOptions = [
  { value: 'ALL', label: 'Toutes les langues' },
  { value: 'ORIGINAL', label: 'Version originale (VOF & VOSTFR)' },
  { value: 'VOF', label: 'VOF' },
  { value: 'VOSTFR', label: 'VOSTFR' },
  { value: 'VF', label: 'VF' }
] as const satisfies readonly ShowtimeFilterOption<QueryLanguage>[]

export const showtimeLanguageOptions = [
  { value: 'ALL', label: 'Toutes les langues' },
  { value: 'VOSTFR', label: 'VOSTFR' },
  { value: 'VF', label: 'VF' },
  { value: 'VO', label: 'VO' },
  { value: 'VF_SME', label: 'VF SME' },
  { value: 'VFSTF', label: 'VFSTF' }
] as const satisfies readonly ShowtimeFilterOption<'ALL' | ShowtimeLanguage>[]

export const queryLanguageValues = queryLanguageOptions.map((option) => option.value)
export const showtimeLanguageValues = ['VOSTFR', 'VF', 'VO', 'VF_SME', 'VFSTF'] as const satisfies readonly ShowtimeLanguage[]
export type FilmLanguageFilter = 'ALL' | 'ORIGINAL' | 'VOF' | ShowtimeLanguage
export const filmLanguageValues = ['ORIGINAL', 'VOF', ...showtimeLanguageValues] as const
export const queryFormatOptions = formatOptions
export const queryFormatValues = queryFormatOptions.map((option) => option.value)

export function languageLabel(language: string, originalLanguage?: string | null): string {
  const value = language.toUpperCase()
  if (value === 'VF' && originalLanguage === 'fr') return 'VOF'
  return showtimeLanguageOptions.find((option) => option.value === value)?.label
    ?? queryLanguageOptions.find((option) => option.value === value)?.label
    ?? language
}

export function availableLanguageOptions(available: readonly ShowtimeLanguage[]): readonly ShowtimeFilterOption<'ALL' | ShowtimeLanguage>[] {
  const values = new Set(available)
  return showtimeLanguageOptions.filter((option) => option.value === 'ALL' || values.has(option.value))
}

export function availableFilmLanguageOptions(available: readonly ShowtimeLanguage[]): readonly ShowtimeFilterOption<FilmLanguageFilter>[] {
  return [
    queryLanguageOptions[0],
    queryLanguageOptions[1],
    queryLanguageOptions[2],
    ...availableLanguageOptions(available).filter((option) => option.value !== 'ALL')
  ]
}

export function matchesFilmLanguageFilter(language: string, requested: FilmLanguageFilter, originalLanguage?: string | null): boolean {
  if (requested === 'ALL') return true
  if (requested !== 'ORIGINAL' && requested !== 'VOF') return language === requested
  return (requested === 'ORIGINAL' && (language === 'VO' || language === 'VOSTFR'))
    || (originalLanguage === 'fr' && (language === 'VF' || language === 'VF_SME' || language === 'VFSTF'))
}

export function availableFormatOptions(available: readonly ShowtimeFormat[]): readonly (typeof formatOptions)[number][] {
  const values = new Set(available)
  return formatOptions.filter((option) => {
    if (option.value === 'ALL') return true
    return values.has(option.value)
  })
}

export function showtimeFilterSummary(language: FilmLanguageFilter, format: QueryFormat, originalLanguage?: string | null): string {
  const labels: string[] = []
  if (language !== 'ALL') labels.push(languageLabel(language, originalLanguage))
  if (format !== 'ALL') labels.push(formatLabel(format))
  return labels.length ? labels.join(' · ') : `${languageLabel('ALL')} · ${formatLabel('ALL')}`
}
