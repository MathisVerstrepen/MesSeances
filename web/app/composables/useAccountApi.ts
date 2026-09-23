import { FetchError, ofetch } from 'ofetch'
import type {
  AccountAction,
  AccountDetails,
  AccountContinuation,
  GoogleStart,
  AccountSession,
} from '~/types/account'
import { AccountApiError } from '~/utils/accountState'

export function useAccountApi() {
  const config = useRuntimeConfig()
  const revalidating = useState('account-revalidating', () => false)
  const incomingCookie = import.meta.server
    ? (useRequestHeaders(['cookie']).cookie ?? '')
    : ''

  async function request<T>(
    path: string,
    body?: Record<string, string>,
    method: 'GET' | 'POST' | 'DELETE' = body ? 'POST' : 'GET',
  ): Promise<T> {
    if (import.meta.server && body) throw new AccountApiError(403)
    // Guard programmatic callers too, not only disabled buttons. Never retry writes.
    if (body && revalidating.value)
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
