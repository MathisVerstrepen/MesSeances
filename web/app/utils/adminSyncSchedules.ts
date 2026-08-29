import type {
  AdminSaveSyncScheduleRequest,
  AdminSyncJob,
  AdminSyncScheduleItem,
  AdminSyncScheduleKind,
  AdminSyncScheduleTarget,
  AdminSyncWeekday,
  Provider
} from '../types/api.ts'

export interface AdminSyncScheduleDraft {
  enabled: boolean
  kind: AdminSyncScheduleKind
  time: string
  weekdays: AdminSyncWeekday[]
  expression: string
}

export interface AdminSyncScheduleDraftErrors {
  time?: string
  weekdays?: string
  expression?: string
}

export interface AdminSyncScheduleDraftValidation {
  valid: boolean
  errors: AdminSyncScheduleDraftErrors
}

const TIME_PATTERN = /^(?:[01]\d|2[0-3]):[0-5]\d$/
const WEEKDAY_ORDER: readonly AdminSyncWeekday[] = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun']

export function blankAdminSyncScheduleDraft(): AdminSyncScheduleDraft {
  return { enabled: false, kind: 'daily', time: '', weekdays: [], expression: '' }
}

export function adminSyncScheduleDraftFromItem(item: AdminSyncScheduleItem): AdminSyncScheduleDraft {
  if (item.schedule.kind === 'daily') {
    return { enabled: item.enabled, kind: 'daily', time: item.schedule.time, weekdays: [], expression: '' }
  }
  if (item.schedule.kind === 'weekly') {
    return { enabled: item.enabled, kind: 'weekly', time: item.schedule.time, weekdays: [...item.schedule.weekdays], expression: '' }
  }
  return { enabled: item.enabled, kind: 'cron', time: '', weekdays: [], expression: item.schedule.expression }
}

export function adminSyncScheduleDraftFingerprint(draft: AdminSyncScheduleDraft): string {
  return JSON.stringify(buildAdminSyncScheduleRequest(draft))
}

export function isAdminSyncScheduleTargetAvailable(
  target: AdminSyncScheduleTarget,
  availableTargets: readonly AdminSyncScheduleTarget[]
): boolean {
  return availableTargets.includes(target)
}

export function validateAdminSyncScheduleDraft(draft: AdminSyncScheduleDraft): AdminSyncScheduleDraftValidation {
  const errors: AdminSyncScheduleDraftErrors = {}

  if (draft.kind === 'daily' || draft.kind === 'weekly') {
    if (!draft.time) errors.time = 'Indiquez une heure.'
    else if (!TIME_PATTERN.test(draft.time)) errors.time = 'Utilisez une heure valide au format HH:MM.'
  }

  if (draft.kind === 'weekly' && draft.weekdays.length === 0) {
    errors.weekdays = 'Sélectionnez au moins un jour.'
  }

  if (draft.kind === 'cron') {
    const expression = normalizeCronExpression(draft.expression)
    if (!expression) errors.expression = 'Indiquez une expression cron.'
    else if (new TextEncoder().encode(expression).length > 255) errors.expression = 'L’expression cron ne peut pas dépasser 255 octets.'
    else if (expression.split(' ').length !== 5) errors.expression = 'L’expression cron doit contenir exactement cinq champs.'
  }

  return { valid: Object.keys(errors).length === 0, errors }
}

export function buildAdminSyncScheduleRequest(draft: AdminSyncScheduleDraft): AdminSaveSyncScheduleRequest {
  if (draft.kind === 'daily') {
    return { enabled: draft.enabled, schedule: { kind: 'daily', time: draft.time } }
  }
  if (draft.kind === 'weekly') {
    const selected = new Set(draft.weekdays)
    return {
      enabled: draft.enabled,
      schedule: { kind: 'weekly', time: draft.time, weekdays: WEEKDAY_ORDER.filter((weekday) => selected.has(weekday)) }
    }
  }
  return { enabled: draft.enabled, schedule: { kind: 'cron', expression: normalizeCronExpression(draft.expression) } }
}

export function selectLatestProviderRun(
  provider: Provider,
  job: AdminSyncJob | null,
  runs: readonly AdminSyncJob[]
): AdminSyncJob | null {
  const seen = new Set<string>()
  let latest: AdminSyncJob | null = null
  let latestStartedAt = Number.NEGATIVE_INFINITY

  for (const run of job ? [job, ...runs] : runs) {
    if (seen.has(run.id)) continue
    seen.add(run.id)
    if (run.state === 'running') continue
    if (run.target !== provider && run.target !== 'all') continue
    if (run.providers[provider].state === 'not_requested') continue

    const startedAt = new Date(run.started_at).getTime()
    const comparableStartedAt = Number.isFinite(startedAt) ? startedAt : Number.NEGATIVE_INFINITY
    if (latest === null || comparableStartedAt > latestStartedAt) {
      latest = run
      latestStartedAt = comparableStartedAt
    }
  }

  return latest
}

export function adminSyncRunDurationMilliseconds(run: AdminSyncJob, now: number): number {
  const startedAt = new Date(run.started_at).getTime()
  const finishedAt = run.finished_at === null ? now : new Date(run.finished_at).getTime()
  if (!Number.isFinite(startedAt) || !Number.isFinite(finishedAt)) return 0
  return Math.max(0, finishedAt - startedAt)
}

function normalizeCronExpression(expression: string): string {
  return expression.trim().replace(/\s+/g, ' ')
}
