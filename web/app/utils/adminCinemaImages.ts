import type { AdminTheaterImageResult, Provider } from '../types/api'

export const cinemaImageFileLimit = 5 * 1024 * 1024
const responseLimit = 4096

export class AdminCinemaImageError extends Error {
  status: number
  code: string

  constructor(status = 0, code = '') {
    super('Cinema image request failed')
    this.status = status
    this.code = code
  }
}

export function adminCinemaImagePath(provider: Provider, id: string): string {
  return `/api/v1/admin/theaters/${encodeURIComponent(provider)}/${encodeURIComponent(id)}/image`
}

export function cinemaImageFileError(file: File): string {
  if (file.size > cinemaImageFileLimit)
    return 'Choisissez une image de 5 Mio maximum.'
  if (!file.size) return 'Ce fichier est vide. Choisissez une autre image.'
  if (!['image/jpeg', 'image/png', 'image/webp'].includes(file.type))
    return 'Choisissez une image JPEG, PNG ou WebP non animée.'
  return ''
}

export function cinemaImageURLError(value: string): string {
  try {
    const url = new URL(value)
    if (
      new TextEncoder().encode(value).length > 2048 ||
      // oxlint-disable-next-line no-control-regex -- Reject URL controls before WHATWG parsing can silently remove them.
      /[\s\\\u0000-\u001f\u007f]/.test(value) ||
      url.protocol !== 'https:' ||
      url.username ||
      url.password ||
      url.hash ||
      (url.port && url.port !== '443') ||
      !url.hostname.includes('.') ||
      url.hostname.endsWith('.') ||
      !/^[a-z0-9.-]+$/i.test(url.hostname) ||
      /^[\d.]+$/.test(url.hostname)
    )
      throw new Error()
    return ''
  } catch {
    return 'Saisissez un lien HTTPS direct vers une image, sans identifiants ni fragment.'
  }
}

function adminCinemaPreviewTarget(value: string, origin: string): URL | null {
  try {
    // Reject ambiguous input before WHATWG parsing can normalize it.
    // oxlint-disable-next-line no-control-regex -- Credentialed preview URLs must not contain stripped controls.
    if (/[?#\s\\\u0000-\u001f\u007f]/.test(value)) return null
    const url = new URL(value, origin)
    if (
      !['http:', 'https:'].includes(url.protocol) ||
      url.username ||
      url.password ||
      url.search ||
      url.hash
    )
      return null
    return url
  } catch {
    return null
  }
}

// Only this cinema's canonical, current admin revision on the configured API may become a preview.
export function cinemaImagePreviewURL(
  base: string,
  path: string,
  provider: Provider,
  id: string,
  revision: number,
  origin: string,
): string {
  if (
    !Number.isSafeInteger(revision) ||
    revision < 1 ||
    path !== `${adminCinemaImagePath(provider, id)}/${revision}`
  )
    return ''
  const api = adminCinemaPreviewTarget(base, origin)
  const url = adminCinemaPreviewTarget(
    `${base.replace(/\/$/, '')}${path}`,
    origin,
  )
  return api && url && url.origin === api.origin ? url.href : ''
}

export function cinemaImageErrorMessage(
  status: number | undefined,
  code: string | undefined,
): string {
  if (status === 401) return 'Session expirée. Connectez-vous à nouveau.'
  if (code === 'cinema_image_conflict')
    return 'Une autre modification a changé cette image. Actualisez la liste, vérifiez l’image puis réessayez.'
  if (code === 'invalid_image_url')
    return 'Ce lien n’est pas autorisé. Utilisez un lien HTTPS public direct ou un fichier.'
  if (code === 'cinema_image_too_large')
    return 'L’image dépasse la taille autorisée. Choisissez un fichier plus léger, de 5 Mio maximum.'
  if (code === 'cinema_image_unsupported')
    return 'Format non accepté. Choisissez une image JPEG, PNG ou WebP non animée.'
  if (code === 'cinema_image_invalid')
    return 'L’image est endommagée ou ses dimensions sont invalides. Choisissez une autre image.'
  if (code === 'cinema_image_download_failed')
    return 'Le téléchargement a échoué. Vérifiez que le lien mène directement à une image accessible, ou utilisez un fichier.'
  if (code === 'cinema_image_import_unavailable')
    return 'Import par lien indisponible. Utilisez un fichier.'
  if (code === 'cinema_image_busy')
    return 'Une image est déjà en cours de traitement. Patientez un instant, puis réessayez.'
  if (code === 'theater_not_found')
    return 'Ce cinéma n’est plus disponible. Actualisez la liste.'
  if (code === 'cinema_image_not_found')
    return 'Image indisponible. Actualisez la liste pour vérifier sa version.'
  if (code === 'origin_forbidden')
    return 'Enregistrement refusé depuis cette adresse. Revenez à l’administration du site et réessayez.'
  if (code === 'invalid_request')
    return 'La demande est invalide. Vérifiez votre sélection et actualisez la liste avant de réessayer.'
  if (status === 503)
    return 'Le service des images est indisponible. Réessayez plus tard.'
  return 'Impossible de joindre le service des images. Vérifiez votre connexion puis réessayez.'
}

function responseError(status: number, text: string): AdminCinemaImageError {
  let code = ''
  if (text.length <= responseLimit) {
    try {
      const value = JSON.parse(text)?.error?.code
      // oxlint-disable-next-line anti-slop/no-runtime-typeof -- Untrusted response boundary, retain code only, never server copy.
      if (typeof value === 'string' && /^[a-z_]{1,64}$/.test(value))
        code = value
    } catch {
      /* Never retain raw response text. */
    }
  }
  return new AdminCinemaImageError(status, code)
}

export interface CinemaImageDraft {
  source: 'file' | 'url'
  file: File | null
  url: string
  candidate: string
}

export function newCinemaImageDraft(): CinemaImageDraft {
  return { source: 'file', file: null, url: '', candidate: '' }
}

export function clearCinemaImageDraft(draft: CinemaImageDraft): void {
  if (draft.candidate) URL.revokeObjectURL(draft.candidate)
  draft.file = null
  draft.url = ''
  draft.candidate = ''
}

export function selectCinemaImageFile(
  draft: CinemaImageDraft,
  file: File | null,
): string {
  if (!file) {
    clearCinemaImageDraft(draft)
    return ''
  }
  const error = cinemaImageFileError(file)
  if (error) return error
  clearCinemaImageDraft(draft)
  draft.file = file
  draft.candidate = URL.createObjectURL(file)
  return ''
}

export async function fetchAdminCinemaImage(
  base: string,
  url: string,
  signal: AbortSignal,
): Promise<Blob> {
  if (signal.aborted) throw new AdminCinemaImageError()
  const api = adminCinemaPreviewTarget(base, window.location.origin)
  const target = adminCinemaPreviewTarget(url, window.location.origin)
  if (!api || !target || target.origin !== api.origin)
    throw new AdminCinemaImageError(404, 'cinema_image_not_found')
  const requestSignal = AbortSignal.any([signal, AbortSignal.timeout(25000)])
  const response = await fetch(target.href, {
    credentials: 'include',
    cache: 'no-store',
    redirect: 'error',
    signal: requestSignal,
    referrerPolicy: 'no-referrer',
  })
  const limit = response.ok ? 1024 * 1024 : responseLimit
  if (
    Number(response.headers.get('Content-Length')) > limit ||
    (response.ok && response.headers.get('Content-Type') !== 'image/webp')
  ) {
    await response.body?.cancel()
    throw new AdminCinemaImageError(
      response.ok ? 404 : response.status,
      response.ok ? 'cinema_image_not_found' : '',
    )
  }
  const reader = response.body?.getReader()
  if (!reader) throw new AdminCinemaImageError()
  const chunks: Uint8Array<ArrayBuffer>[] = []
  let size = 0
  try {
    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      size += value.byteLength
      if (size > limit)
        throw new AdminCinemaImageError(
          response.ok ? 404 : response.status,
          response.ok ? 'cinema_image_not_found' : '',
        )
      chunks.push(new Uint8Array(value))
    }
  } finally {
    await reader.cancel()
    reader.releaseLock()
  }
  const blob = new Blob(chunks, { type: 'image/webp' })
  if (!response.ok) throw responseError(response.status, await blob.text())
  if (requestSignal.aborted) throw new AdminCinemaImageError()
  return blob
}

export function uploadAdminCinemaImage(
  base: string,
  provider: Provider,
  id: string,
  file: File,
  revision: number,
  signal: AbortSignal,
  progress: (percent: number | null) => void,
): Promise<AdminTheaterImageResult> {
  return new Promise((resolve, reject) => {
    if (
      signal.aborted ||
      cinemaImageFileError(file) ||
      !Number.isSafeInteger(revision) ||
      revision < 0
    )
      return reject(new AdminCinemaImageError(400, 'invalid_request'))
    const path = adminCinemaImagePath(provider, id)
    const target = `${base.replace(/\/$/, '')}${path}`
    const xhr = new XMLHttpRequest()
    const abort = () => xhr.abort()
    const finish = (
      error?: AdminCinemaImageError,
      value?: AdminTheaterImageResult,
    ) => {
      signal.removeEventListener('abort', abort)
      xhr.onload =
        xhr.onerror =
        xhr.ontimeout =
        xhr.onabort =
        xhr.onprogress =
          null
      xhr.upload.onprogress = xhr.upload.onload = null
      if (error) reject(error)
      else resolve(value!)
    }
    xhr.open('POST', target)
    xhr.withCredentials = true
    xhr.timeout = 25000
    xhr.upload.onprogress = (event) =>
      progress(
        event.lengthComputable && event.total > 0
          ? Math.min(
              100,
              Math.max(0, Math.floor((event.loaded / event.total) * 100)),
            )
          : null,
      )
    xhr.upload.onload = () => progress(100)
    xhr.onprogress = (event) => {
      if (event.loaded > responseLimit) {
        finish(responseError(xhr.status, ''))
        xhr.abort()
      }
    }
    xhr.onerror =
      xhr.ontimeout =
      xhr.onabort =
        () => finish(new AdminCinemaImageError())
    xhr.onload = () => {
      if (
        signal.aborted ||
        xhr.responseURL !== new URL(target, window.location.origin).href
      )
        return finish(new AdminCinemaImageError())
      if (xhr.status !== 200)
        return finish(responseError(xhr.status, xhr.responseText))
      try {
        if (xhr.responseText.length > responseLimit) throw new Error()
        const value = JSON.parse(xhr.responseText)
        if (
          !Number.isSafeInteger(value.image_revision) ||
          value.image_revision <= revision ||
          !value.image ||
          value.image.url !== `${path}/${value.image_revision}` ||
          ![value.image.width, value.image.height].every(
            (n: number) => Number.isInteger(n) && n >= 1 && n <= 1600,
          ) ||
          !Number.isInteger(value.image.size_bytes) ||
          value.image.size_bytes < 1 ||
          value.image.size_bytes > 1024 * 1024
        )
          throw new Error()
        finish(undefined, {
          image_revision: value.image_revision,
          image: value.image,
        })
      } catch {
        finish(new AdminCinemaImageError())
      }
    }
    signal.addEventListener('abort', abort, { once: true })
    const body = new FormData()
    body.append('expected_revision', String(revision))
    body.append('image', file)
    try {
      xhr.send(body)
    } catch {
      finish(new AdminCinemaImageError())
    }
  })
}
