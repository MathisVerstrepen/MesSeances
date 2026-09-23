import type { AccountSession } from '../types/account'
import type { LocationQueryValue } from 'vue-router'

export function accountDestination(session: AccountSession): string {
  if (!session.enabled || session.state === 'anonymous') return '/connexion'
  if (session.state === 'pending_email') return '/verification'
  if (session.state === 'pending_username') return '/finaliser'
  return '/compte'
}

export function accountGoogleCallbackMessage(
  error: LocationQueryValue | LocationQueryValue[] | undefined,
): string {
  if (error === 'google_email_in_use')
    return 'Un compte utilise déjà cette adresse email. Connectez-vous avec votre moyen habituel, puis associez Google depuis votre compte.'
  if (error === 'google_failed')
    return 'La confirmation Google n’a pas abouti ou a expiré. Recommencez depuis votre compte ou utilisez votre moyen de connexion habituel.'
  return ''
}

export function normalizeAccountEmail(value: string): string {
  return value.trim().replace(/[A-Z]/g, (letter) => letter.toLowerCase())
}

export function accountWriteUncertain(cause: unknown): boolean {
  return (
    cause instanceof AccountApiError &&
    (cause.status === 0 || cause.status >= 500)
  )
}

export function normalizeAccountUsername(value: string): string {
  return value.replace(/[A-Z]/g, (letter) => letter.toLowerCase())
}

export function validAccountUsername(value: string): boolean {
  return /^[a-z][a-z0-9_]{2,29}$/.test(normalizeAccountUsername(value))
}

export function passwordCriteria(value: string) {
  const length = [...value].length
  return {
    minimum: length >= 10,
    maximum: length <= 128 && new TextEncoder().encode(value).length <= 512,
  }
}

export class AccountApiError extends Error {
  readonly status: number
  readonly code: string
  readonly retryAfter: number

  constructor(status = 0, code = '', retryAfter = 0) {
    super('Account request failed')
    this.status = status
    this.code = code
    this.retryAfter = retryAfter
  }
}

export function accountErrorMessage(cause: unknown): string {
  if (!(cause instanceof AccountApiError))
    return 'Action interrompue. Réessayez après avoir vérifié votre connexion.'
  if (cause.status === 429)
    return 'Trop de tentatives. Patientez un instant avant de réessayer.'
  if (cause.code === 'accounts_disabled')
    return 'Les comptes ne sont pas encore disponibles. Vous pouvez continuer à explorer les séances.'
  if (cause.status === 503)
    return 'Le service de comptes est indisponible. Réessayez dans quelques instants.'
  if (cause.code === 'verification_browser_required')
    return 'Rouvrez ce lien dans le navigateur où vous avez commencé votre inscription. Si vous n’y avez plus accès, recommencez votre inscription.'
  if (cause.status === 401)
    return 'Connexion refusée ou session expirée. Vérifiez votre email et votre mot de passe, puis reconnectez-vous.'
  if (cause.code === 'email_unavailable')
    return 'Cet email est indisponible. Votre adresse actuelle reste inchangée. Choisissez une autre adresse.'
  if (cause.code === 'invalid_link')
    return 'Ce lien est invalide ou expiré. Demandez un nouveau lien puis ouvrez le dernier email reçu.'
  if (cause.code === 'recent_auth_required')
    return 'La confirmation d’identité a expiré. Recommencez la vérification depuis votre compte.'
  if (cause.code === 'identity_unavailable')
    return 'Ce compte Google ne peut pas être associé. Utilisez un autre compte Google ou conservez votre connexion actuelle.'
  if (cause.code === 'last_login_method')
    return 'Google est votre seul moyen de connexion. Ajoutez un mot de passe avant de le dissocier.'
  if (cause.status === 409)
    return 'Ce nom d’utilisateur est indisponible. Choisissez-en un autre.'
  if (cause.status === 400 && cause.code === 'common_password')
    return 'Ce mot de passe est trop courant. Choisissez un mot de passe plus difficile à deviner.'
  if (cause.status === 400)
    return 'Les informations ou le lien ne sont pas valides. Vérifiez les champs ou demandez un nouveau lien.'
  if (cause.status === 403)
    return 'Cette action nécessite une nouvelle connexion ou une vérification. Reconnectez-vous pour continuer.'
  return 'Le service ne répond pas. Vérifiez votre connexion, puis réessayez.'
}
