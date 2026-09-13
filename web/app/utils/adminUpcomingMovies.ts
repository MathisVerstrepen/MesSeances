import type { LocationQuery } from 'vue-router'
import type { AdminUpcomingMovie, AdminUpcomingMoviesQuery, FrenchReleaseRow, UpcomingReviewDecision, UpcomingReviewFilter, UpcomingReviewReason } from '../types/api.ts'
import { positiveSafeInteger, singularQueryValue } from './routeQuery.ts'

export const UPCOMING_REVIEW_PAGE_SIZE = 50
export const UPCOMING_REVIEW_SEARCH_DELAY = 350
export const upcomingReviewFilters = [
  { value: 'needs_review', label: 'À examiner' },
  { value: 'pending_assessment', label: 'En attente d’évaluation' },
  { value: 'approved', label: 'Approuvés' },
  { value: 'excluded', label: 'Exclus' },
  { value: 'all', label: 'Tous' }
] satisfies Array<{ value: UpcomingReviewFilter; label: string }>

export const upcomingReviewReasonLabels = {
  limited_only: 'Sortie limitée uniquement',
  non_theatrical_before_or_same_day: 'Diffusion hors cinéma antérieure ou le même jour',
  broadcaster_theatrical_note: 'Diffuseur cité dans une sortie cinéma',
  single_screening_note: 'Séance ponctuelle mentionnée'
} satisfies Record<UpcomingReviewReason, string>
export const upcomingReviewDecisionLabels = {
  unreviewed: 'Non examiné', approved: 'Approuvé', excluded: 'Exclu'
} satisfies Record<UpcomingReviewDecision, string>
export const frenchReleaseTypeLabels = {
  1: 'Première', 2: 'Cinéma limité', 3: 'Cinéma national', 4: 'Numérique', 5: 'Support physique', 6: 'Télévision'
} satisfies Record<FrenchReleaseRow['type'], string>

export interface UpcomingReviewRouteState {
  filter: UpcomingReviewFilter
  q: string
  page: number
}

export function normalizeUpcomingReviewSearch(value: string): string {
  return Array.from(value.trim()).slice(0, 1024).join('')
}

export function parseUpcomingReviewRoute(query: LocationQuery): UpcomingReviewRouteState {
  const filter = upcomingReviewFilters.find(option => option.value === singularQueryValue(query.filter))?.value ?? 'needs_review'
  const page = positiveSafeInteger(singularQueryValue(query.page)) ?? 1
  return {
    filter,
    q: normalizeUpcomingReviewSearch(singularQueryValue(query.q) ?? ''),
    page: page <= Math.floor(2_147_483_647 / UPCOMING_REVIEW_PAGE_SIZE) + 1 ? page : 1
  }
}

export function upcomingReviewRouteQuery(state: UpcomingReviewRouteState, current: LocationQuery = {}): LocationQuery {
  const query = { ...current }
  for (const key of ['filter', 'q', 'page']) delete query[key]
  if (state.filter !== 'needs_review') query.filter = state.filter
  if (state.q) query.q = state.q
  if (state.page > 1) query.page = String(state.page)
  return query
}

export function upcomingReviewApiQuery(state: UpcomingReviewRouteState): AdminUpcomingMoviesQuery {
  return { filter: state.filter, search: state.q || undefined, limit: UPCOMING_REVIEW_PAGE_SIZE, offset: (state.page - 1) * UPCOMING_REVIEW_PAGE_SIZE }
}

export function upcomingReviewVisibility(movie: AdminUpcomingMovie): string {
  if (movie.decision === 'excluded') return 'Exclu'
  return movie.publicly_visible ? 'Visible' : 'Hors de la liste'
}

export function formatUpcomingAssessmentTime(value: string): string {
  return new Intl.DateTimeFormat('fr-FR', { timeZone: 'Europe/Paris', dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}
