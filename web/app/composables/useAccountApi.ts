import { FetchError, ofetch } from 'ofetch'
import type {
  AccountAction,
  AccountDetails,
  AccountContinuation,
  GoogleStart,
  AccountSession,
  AccountAvatarResult,
  AccountTheaterPreferences,
  SaveAccountTheaterPreferences,
} from '~/types/account'
import { AccountApiError } from '~/utils/accountState'
import type {
  AccountNotifications,
  NotificationRuleDraft,
  NotificationRules,
  NotificationStatus,
  PushDevices,
  DigestFrequency,
  NotificationDigest,
} from '~/types/notifications'
import { uploadAccountAvatar } from '~/utils/accountAvatar'
import type {
  WatchedSnapshot,
  WatchedMutation,
  SaveWatched,
  SaveWatchedPreferences,
} from '~/types/watched'
import type {
  AccountTheaterFollows,
  SaveAccountTheaterFollow,
  AccountActivityResponse,
  ActivityVisit,
  ActivityNotification,
  ActivityReading,
  ActivityRead,
  ActivityReadingSnapshot,
} from '~/types/cinemaFollows'
import type {
  AccountWatchlist,
  WatchlistNextScreenings,
  SaveWatchlist,
  SaveWatchlistSort,
  SaveWatchlistPreferences,
  WatchlistSearch,
  ImportWatchlist,
  ImportedWatchlist,
  CreateWatchlistTag,
  UpdateWatchlistTag,
  DeleteWatchlistTag,
  AssignWatchlistTag,
} from '~/types/watchlist'

export function useAccountApi() {
  const config = useRuntimeConfig()
  const revalidating = useState('account-revalidating', () => false)
  const incomingCookie = import.meta.server
    ? (useRequestHeaders(['cookie']).cookie ?? '')
    : ''

  async function request<T>(
    path: string,
    body?: Record<string, string> | string[],
    method: 'GET' | 'POST' | 'DELETE' = body ? 'POST' : 'GET',
    signal?: AbortSignal,
    readOnly = method === 'GET',
  ): Promise<T> {
    if (import.meta.server && body) throw new AccountApiError(403)
    // Guard programmatic callers too, not only disabled buttons. Never retry writes.
    if (body && !readOnly && revalidating.value)
      throw new AccountApiError(403, 'recent_auth_required')
    const headers: Record<string, string> = body
      ? { 'Content-Type': 'application/json', 'X-Messeances-CSRF': '1' }
      : {}
    if (import.meta.server) {
      const { accountRequestHeaders } = await import(
        '~~/server/utils/accountApi'
      )
      Object.assign(
        headers,
        accountRequestHeaders(incomingCookie, config.public.siteUrl),
      )
    }
    try {
      return await ofetch<T>(
        `${import.meta.server ? config.apiBase.replace(/\/$/, '') : ''}/api/v1${path}`,
        {
          method,
          body,
          headers,
          credentials: 'same-origin',
          cache: 'no-store',
          redirect: 'error',
          retry: false,
          timeout: 12000,
          signal,
        },
      )
    } catch (error: unknown) {
      // Do not keep ofetch request/options/cause: these can contain credentials.
      if (!(error instanceof FetchError)) throw new AccountApiError()
      const retryAfter = Number(error.response?.headers.get('Retry-After'))
      // Only retain a recognized error code; never persist the response body.
      const code = [
        'accounts_disabled',
        'email_unavailable',
        'common_password',
        'invalid_link',
        'verification_browser_required',
        'authentication_required',
        'recent_auth_required',
        'identity_unavailable',
        'last_login_method',
        'avatar_changed',
        'theater_selection_changed',
        'theater_follows_changed',
        'theater_follow_limit_reached',
        'notification_rule_not_found',
        'notification_rule_limit_reached',
        'push_unavailable',
        'push_device_limit_reached',
        'push_device_not_found',
        'push_device_expired',
        'push_delivery_failed',
        'avatar_too_large',
        'avatar_unsupported',
        'avatar_invalid',
        'avatar_busy',
        'avatar_not_found',
        'watchlist_changed',
        'watched_changed',
        'watched_limit_reached',
        'watched_unavailable',
        'watchlist_tag_name_taken',
        'watchlist_tag_limit_reached',
        'watchlist_tag_not_found',
        'watchlist_movie_not_saved',
        'watchlist_limit_reached',
        'watchlist_external_unavailable',
        'watchlist_unavailable',
        'movie_not_found',
        'movie_not_importable',
        'onboarding_required',
      ].includes(error.data?.error?.code)
        ? String(error.data.error.code)
        : ''
      throw new AccountApiError(
        error.status ?? 0,
        code,
        Number.isFinite(retryAfter)
          ? Math.min(86400, Math.max(0, retryAfter))
          : 0,
      )
    }
  }

  return {
    session: () => request<AccountSession>('/auth/session'),
    watchlistNextScreenings: (
      theaterIds: readonly string[],
      signal: AbortSignal,
    ) =>
      request<WatchlistNextScreenings>(
        '/account/watchlist/next-screenings',
        [...theaterIds],
        'POST',
        signal,
        true,
      ),
    login: (email: string, password: string) =>
      request<AccountSession>('/auth/login', { email, password }),
    register: (email: string, password: string) =>
      request<void>('/auth/register', { email, password }),
    requestVerification: (email: string) =>
      request<void>('/auth/verification/request', { email }),
    confirmVerification: (token: string) =>
      request<AccountSession>('/auth/verification/confirm', { token }),
    username: (username: string) =>
      request<AccountSession>('/account/username', { username }),
    logout: () => request<void>('/auth/logout', {}),
    logoutAll: () => request<void>('/auth/logout-all', {}),
    details: () => request<AccountDetails>('/account'),
    theaterFollows: (signal?: AbortSignal) =>
      request<AccountTheaterFollows>(
        '/account/theater-follows',
        undefined,
        'GET',
        signal,
      ),
    saveTheaterFollow: (
      input: SaveAccountTheaterFollow,
      signal?: AbortSignal,
    ) =>
      request<AccountTheaterFollows>(
        '/account/theater-follows',
        { ...input },
        'POST',
        signal,
      ),
    accountActivity: (cursor?: string, signal?: AbortSignal) =>
      request<AccountActivityResponse>(
        `/account/activity?limit=20${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`,
        undefined,
        'GET',
        signal,
      ),
    visitActivity: (
      input: {
        expected_username: string
        scope: string
        last_event_id: string
      },
      signal?: AbortSignal,
    ) =>
      request<ActivityVisit>('/account/activity/visit', input, 'POST', signal),
    activityNotification: (signal?: AbortSignal) =>
      request<ActivityNotification>(
        '/account/activity/notification',
        undefined,
        'GET',
        signal,
      ),
    activityReading: (ids: string[], signal?: AbortSignal) =>
      request<ActivityReadingSnapshot>(
        `/account/activity/reading${ids.length ? `?${ids.map((id) => `event_id=${encodeURIComponent(id)}`).join('&')}` : ''}`,
        undefined,
        'GET',
        signal,
      ),
    saveActivityReading: (
      input: { expected_username: string; manual_reading: 'true' | 'false' },
      signal?: AbortSignal,
    ) =>
      request<ActivityReading>(
        '/account/activity/reading',
        input,
        'POST',
        signal,
      ),
    saveActivityRead: (
      input: {
        expected_username: string
        event_id: string
        consulted: 'true' | 'false'
      },
      signal?: AbortSignal,
    ) => request<ActivityRead>('/account/activity/read', input, 'POST', signal),
    markAllActivityConsulted: (
      input: { expected_username: string; last_event_id: string },
      signal?: AbortSignal,
    ) =>
      request<ActivityVisit>(
        '/account/activity/read-all',
        input,
        'POST',
        signal,
      ),
    notificationStatus: (signal?: AbortSignal) =>
      request<NotificationStatus>(
        '/account/notifications/status',
        undefined,
        'GET',
        signal,
      ),
    notifications: (
      options: { limit?: number; before?: string } = {},
      signal?: AbortSignal,
    ) => {
      const query = new URLSearchParams()
      if (options.limit) query.set('limit', String(options.limit))
      if (options.before) query.set('before', options.before)
      const suffix = query.size ? `?${query.toString()}` : ''
      return request<AccountNotifications>(
        `/account/notifications${suffix}`,
        undefined,
        'GET',
        signal,
      )
    },
    markNotificationsRead: (
      input: { expected_username: string; ids: readonly string[] },
      signal?: AbortSignal,
    ) =>
      request<NotificationStatus>(
        '/account/notifications/read',
        {
          expected_username: input.expected_username,
          ids: input.ids.join(','),
        },
        'POST',
        signal,
      ),
    markAllNotificationsRead: (
      input: { expected_username: string; through_id: string },
      signal?: AbortSignal,
    ) =>
      request<NotificationStatus>(
        '/account/notifications/read-all',
        { ...input },
        'POST',
        signal,
      ),
    notificationRules: (signal?: AbortSignal) =>
      request<NotificationRules>(
        '/account/notification-rules',
        undefined,
        'GET',
        signal,
      ),
    saveNotificationRule: (
      input: {
        expected_username: string
        id?: string
        rule: NotificationRuleDraft
      },
      signal?: AbortSignal,
    ) => {
      const fields = {
        expected_username: input.expected_username,
        kind: input.rule.kind,
        theater_scope: input.rule.theater_scope,
        theater_ids:
          input.rule.theater_scope === 'custom'
            ? input.rule.theater_ids.join(',')
            : '',
        include_returns:
          input.rule.kind === 'cinema_activity' && input.rule.include_returns
            ? 'true'
            : 'false',
        enabled: input.rule.enabled ? 'true' : 'false',
        push: input.rule.push ? 'true' : 'false',
        email: input.rule.email ? 'true' : 'false',
      }
      return request<NotificationRules>(
        input.id
          ? '/account/notification-rules/update'
          : '/account/notification-rules',
        input.id ? { ...fields, id: input.id } : fields,
        'POST',
        signal,
      )
    },
    deleteNotificationRule: (
      input: { expected_username: string; id: string },
      signal?: AbortSignal,
    ) =>
      request<NotificationRules>(
        '/account/notification-rules/delete',
        { ...input },
        'POST',
        signal,
      ),
    notificationDigest: (signal?: AbortSignal) =>
      request<NotificationDigest>(
        '/account/notification-digest',
        undefined,
        'GET',
        signal,
      ),
    saveNotificationDigest: (
      input: { expected_username: string; frequency: DigestFrequency },
      signal?: AbortSignal,
    ) =>
      request<NotificationDigest>(
        '/account/notification-digest',
        { ...input },
        'POST',
        signal,
      ),
    pushDevices: (signal?: AbortSignal) =>
      request<PushDevices>('/account/push', undefined, 'GET', signal),
    savePushSubscription: (
      input: {
        expected_username: string
        endpoint: string
        p256dh: string
        auth: string
        label: string
      },
      signal?: AbortSignal,
    ) =>
      request<PushDevices>(
        '/account/push/subscriptions',
        { ...input },
        'POST',
        signal,
      ),
    deletePushDevice: (
      input:
        | { expected_username: string; id: string }
        | {
            expected_username: string
            endpoint: string
          },
      signal?: AbortSignal,
    ) =>
      request<PushDevices>(
        '/account/push/subscriptions/delete',
        { ...input },
        'POST',
        signal,
      ),
    testPushDevice: (
      input: { expected_username: string; id: string },
      signal?: AbortSignal,
    ) => request<void>('/account/push/test', { ...input }, 'POST', signal),
    watchlist: (signal?: AbortSignal) =>
      request<AccountWatchlist>('/account/watchlist', undefined, 'GET', signal),
    watched: (signal?: AbortSignal) =>
      request<WatchedSnapshot>('/account/watched', undefined, 'GET', signal),
    saveWatched: (input: SaveWatched, signal?: AbortSignal) =>
      request<WatchedMutation>(
        '/account/watched',
        { ...input },
        'POST',
        signal,
      ),
    saveWatchedPreferences: (
      input: SaveWatchedPreferences,
      signal?: AbortSignal,
    ) =>
      request<WatchedSnapshot>(
        '/account/watched/preferences',
        { ...input },
        'POST',
        signal,
      ),
    saveWatchlist: (input: SaveWatchlist, signal?: AbortSignal) =>
      request<AccountWatchlist>(
        '/account/watchlist',
        { ...input },
        'POST',
        signal,
      ),
    saveWatchlistSort: (input: SaveWatchlistSort, signal?: AbortSignal) =>
      request<AccountWatchlist>(
        '/account/watchlist/sort',
        { ...input },
        'POST',
        signal,
      ),
    saveWatchlistPreferences: (
      input: SaveWatchlistPreferences,
      signal?: AbortSignal,
    ) =>
      request<AccountWatchlist>(
        '/account/watchlist/preferences',
        { ...input, filter_tag_id: input.filter_tag_id ?? '' },
        'POST',
        signal,
      ),
    createWatchlistTag: (input: CreateWatchlistTag, signal?: AbortSignal) =>
      request<AccountWatchlist>(
        '/account/watchlist/tags',
        { ...input },
        'POST',
        signal,
      ),
    updateWatchlistTag: (input: UpdateWatchlistTag, signal?: AbortSignal) =>
      request<AccountWatchlist>(
        '/account/watchlist/tags/update',
        { ...input },
        'POST',
        signal,
      ),
    deleteWatchlistTag: (input: DeleteWatchlistTag, signal?: AbortSignal) =>
      request<AccountWatchlist>(
        '/account/watchlist/tags/delete',
        { ...input },
        'POST',
        signal,
      ),
    assignWatchlistTag: (input: AssignWatchlistTag, signal?: AbortSignal) =>
      request<AccountWatchlist>(
        '/account/watchlist/tags/assign',
        { ...input },
        'POST',
        signal,
      ),
    searchWatchlist: (
      input: { expected_username: string; query: string },
      signal?: AbortSignal,
    ) =>
      request<WatchlistSearch>(
        '/account/watchlist/search',
        { ...input },
        'POST',
        signal,
      ),
    importWatchlist: (input: ImportWatchlist, signal?: AbortSignal) =>
      request<ImportedWatchlist>(
        '/account/watchlist/import',
        { ...input },
        'POST',
        signal,
      ),
    theaterPreferences: (signal?: AbortSignal) =>
      request<AccountTheaterPreferences>(
        '/account/theaters',
        undefined,
        'GET',
        signal,
      ),
    saveTheaterPreferences: (
      input: SaveAccountTheaterPreferences,
      signal?: AbortSignal,
    ) =>
      request<AccountTheaterPreferences>(
        '/account/theaters',
        { ...input },
        'POST',
        signal,
      ),
    uploadAvatar: (
      file: File,
      signal: AbortSignal,
      progress: (percent: number | null) => void,
    ) => {
      if (import.meta.server || revalidating.value)
        return Promise.reject(new AccountApiError(403, 'recent_auth_required'))
      return uploadAccountAvatar(file, signal, progress)
    },
    removeAvatar: (signal: AbortSignal) =>
      request<AccountAvatarResult>('/account/avatar', {}, 'DELETE', signal),
    requestPasswordReset: (email: string) =>
      request<void>('/auth/password/reset/request', { email }),
    confirmPasswordReset: (token: string, password: string) =>
      request<void>('/auth/password/reset/confirm', { token, password }),
    reauthPassword: (
      password: string,
      action: AccountAction,
      target?: string,
    ) => {
      if (target === undefined)
        return request<{ grant: string }>('/account/reauth/password', {
          password,
          action,
        })
      return request<{ grant: string }>('/account/reauth/password', {
        password,
        action,
        target,
      })
    },
    changePassword: (password: string, grant: string) =>
      request<void>('/account/password', { password, grant }),
    requestEmailChange: (email: string, grant: string) =>
      request<void>('/account/email/request', { email, grant }),
    confirmEmailChange: (token: string, grant: string) =>
      request<void>('/account/email/confirm', { token, grant }),
    cancelEmailChange: () => request<void>('/account/email/cancel', {}),
    googleStart: (options: GoogleStart = { mode: 'login' }) =>
      request<{ authorization_url: string }>('/auth/google/start', options),
    continuation: () =>
      request<AccountContinuation>('/account/reauth/continuation'),
    requestIdentityEmail: () =>
      request<void>('/account/reauth/email/request', {}),
    confirmIdentityEmail: (
      token: string,
      action: AccountAction,
      target?: string,
    ) => {
      if (target === undefined)
        return request<{ grant: string }>('/account/reauth/email/confirm', {
          token,
          action,
        })
      return request<{ grant: string }>('/account/reauth/email/confirm', {
        token,
        action,
        target,
      })
    },
    googleUnlink: (grant: string) =>
      request<void>('/account/google/unlink', { grant }),
    deleteAccount: (grant: string, confirmation: string) =>
      request<void>('/account', { grant, confirmation }, 'DELETE'),
  }
}
