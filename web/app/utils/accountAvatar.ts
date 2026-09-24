import { AccountApiError } from './accountState'
import type { AccountAvatarResult } from '../types/account'

export const avatarFileLimit = 5 * 1024 * 1024
const imageLimit = 512 * 1024
const avatarPath = '/api/v1/account/avatar'

export function validAvatarURL(
  value: string | null | undefined,
): value is string {
  // oxlint-disable-next-line anti-slop/no-runtime-typeof -- JSON and account DTOs are untrusted at this URL boundary; do not coerce them.
  if (typeof value !== 'string') return false
  const match = /^\/api\/v1\/account\/avatar\/([1-9][0-9]{0,18})$/.exec(value)
  return !!match && BigInt(match[1]!) <= 9223372036854775807n
}

export function avatarFileError(file: File): string {
  if (file.size > avatarFileLimit)
    return 'Choisissez une photo de 5 Mio maximum.'
  if (
    !/\.(jpe?g|png|webp)$/i.test(file.name) ||
    (file.type &&
      !['image/jpeg', 'image/png', 'image/webp'].includes(file.type))
  )
    return 'Choisissez une photo JPEG, PNG ou WebP non animée.'
  if (!file.size) return 'Ce fichier est vide. Choisissez une autre photo.'
  return ''
}

function responseError(status: number, text: string, retry: string | null) {
  let code = ''
  if (text.length <= 4096) {
    try {
      const value = JSON.parse(text)?.error?.code
      if (
        [
          'avatar_changed',
          'avatar_too_large',
          'avatar_unsupported',
          'avatar_invalid',
          'avatar_busy',
          'avatar_not_found',
          'authentication_required',
          'onboarding_required',
          'accounts_disabled',
        ].includes(value)
      )
        code = value
    } catch {
      /* Never retain an untrusted response. */
    }
  }
  const seconds = Number(retry)
  return new AccountApiError(
    status,
    code,
    Number.isFinite(seconds) ? Math.min(86400, Math.max(0, seconds)) : 0,
  )
}

// The URL is an owner-relative revision, never an identity or a cache key.
export async function fetchAccountAvatar(
  path: string,
  signal: AbortSignal,
): Promise<Blob> {
  if (!validAvatarURL(path)) throw new AccountApiError(404, 'avatar_not_found')
  try {
    const response = await fetch(path, {
      credentials: 'same-origin',
      cache: 'no-store',
      redirect: 'error',
      signal,
    })
    const limit = response.ok ? imageLimit : 4096
    if (Number(response.headers.get('Content-Length')) > limit) {
      await response.body?.cancel()
      throw new AccountApiError(
        response.ok ? 404 : response.status,
        'avatar_not_found',
      )
    }
    if (response.ok && response.headers.get('Content-Type') !== 'image/webp') {
      await response.body?.cancel()
      throw new AccountApiError(404, 'avatar_not_found')
    }
    const reader = response.body?.getReader()
    if (!reader) throw new AccountApiError()
    const chunks: Uint8Array<ArrayBuffer>[] = []
    let size = 0
    try {
      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        size += value.byteLength
        if (size > limit)
          throw new AccountApiError(response.ok ? 404 : response.status)
        chunks.push(new Uint8Array(value))
      }
    } finally {
      await reader.cancel()
      reader.releaseLock()
    }
    const blob = new Blob(chunks, { type: 'image/webp' })
    if (!response.ok)
      throw responseError(
        response.status,
        await blob.text(),
        response.headers.get('Retry-After'),
      )
    // Transport framing only; the server validates the complete static image.
    const signature = new Uint8Array(await blob.slice(0, 12).arrayBuffer())
    if (
      signal.aborted ||
      blob.size < 20 ||
      ![82, 73, 70, 70].every((byte, index) => signature[index] === byte) ||
      ![87, 69, 66, 80].every((byte, index) => signature[index + 8] === byte) ||
      new DataView(signature.buffer).getUint32(4, true) + 8 !== blob.size
    )
      throw new AccountApiError(404, 'avatar_not_found')
    return blob
  } catch (error) {
    throw error instanceof AccountApiError ? error : new AccountApiError()
  }
}

// XHR is limited to this fixed same-origin upload for native byte progress.
export function uploadAccountAvatar(
  file: File,
  signal: AbortSignal,
  progress: (percent: number | null) => void,
): Promise<AccountAvatarResult> {
  return new Promise((resolve, reject) => {
    if (signal.aborted) return reject(new AccountApiError())
    const xhr = new XMLHttpRequest()
    const abort = () => xhr.abort()
    const finish = (error?: AccountApiError, value?: AccountAvatarResult) => {
      signal.removeEventListener('abort', abort)
      xhr.onload = xhr.onerror = xhr.ontimeout = xhr.onabort = null
      xhr.onprogress = null
      xhr.upload.onprogress = xhr.upload.onload = null
      if (error) reject(error)
      else resolve(value!)
    }
    xhr.open('POST', avatarPath)
    xhr.timeout = 15000
    xhr.setRequestHeader('X-Messeances-CSRF', '1')
    xhr.setRequestHeader('Cache-Control', 'no-store')
    xhr.upload.onprogress = (event) =>
      progress(
        event.lengthComputable
          ? Math.min(100, Math.round((event.loaded / event.total) * 100))
          : null,
      )
    xhr.upload.onload = () => progress(100)
    xhr.onprogress = (event) => {
      if (event.loaded > 4096) xhr.abort()
    }
    xhr.onerror =
      xhr.ontimeout =
      xhr.onabort =
        () => finish(new AccountApiError())
    xhr.onload = () => {
      if (xhr.responseURL !== new URL(avatarPath, window.location.origin).href)
        return finish(new AccountApiError())
      if (xhr.status !== 200)
        return finish(
          responseError(
            xhr.status,
            xhr.responseText,
            xhr.getResponseHeader('Retry-After'),
          ),
        )
      try {
        if (xhr.responseText.length > 4096) throw new Error()
        const value = JSON.parse(xhr.responseText)
        if (!validAvatarURL(value?.avatar_url)) throw new Error()
        finish(undefined, { avatar_url: value.avatar_url })
      } catch {
        finish(new AccountApiError())
      }
    }
    signal.addEventListener('abort', abort, { once: true })
    const body = new FormData()
    body.append('avatar', file)
    xhr.send(body)
  })
}
