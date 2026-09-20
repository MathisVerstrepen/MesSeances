import type { LocationQuery } from 'vue-router'
import { createServiceTimeOptions, formatLongDate } from './date.ts'
import { formatLabel } from './formats.ts'
import {
  calendarDate,
  enumQueryValue,
  singularQueryValue,
} from './routeQuery.ts'
import {
  languageLabel,
  queryFormatValues,
  queryLanguageValues,
} from './showtimeFilters.ts'

export const DEFAULT_SEARCH_DESCRIPTION =
  'Trouvez les séances qui tiennent entièrement dans votre créneau horaire.'

const theaterIdPattern = /^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/
const validTimes = new Set(
  createServiceTimeOptions().map((option) => option.value),
)

function formatSearchTime(value: string): string {
  return value.replace(':', 'h')
}

export function buildSearchMetaDescription(query: LocationQuery): string {
  const theaterValue = singularQueryValue(query.theaters)
  const date = calendarDate(singularQueryValue(query.date))
  const startAfter = singularQueryValue(query.start_after)
  const finishBefore = singularQueryValue(query.finish_before)
  const theaterIds = theaterValue?.split(',') ?? []

  if (
    theaterIds.length === 0 ||
    theaterIds.some((id) => !theaterIdPattern.test(id)) ||
    new Set(theaterIds).size !== theaterIds.length ||
    !date ||
    !startAfter ||
    !finishBefore ||
    !validTimes.has(startAfter) ||
    !validTimes.has(finishBefore)
  )
    return DEFAULT_SEARCH_DESCRIPTION

  const theaterCount = theaterIds.length
  let description = `Séances dans ${theaterCount} cinéma${theaterCount === 1 ? '' : 's'} le ${formatLongDate(date)}, de ${formatSearchTime(startAfter)} à ${formatSearchTime(finishBefore)}.`
  const language = enumQueryValue(
    singularQueryValue(query.language),
    queryLanguageValues,
  )
  const format = enumQueryValue(
    singularQueryValue(query.format),
    queryFormatValues,
  )
  const filters = [
    language && language !== 'ALL' ? languageLabel(language) : null,
    format && format !== 'ALL' ? formatLabel(format) : null,
  ].filter((value): value is string => value !== null)

  if (filters.length > 0) description += ` Filtres : ${filters.join(', ')}.`
  return description
}
