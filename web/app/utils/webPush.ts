export type PushSupport = 'supported' | 'unsupported' | 'install-required'

export interface PushEnvironment {
  serviceWorker: boolean
  pushManager: boolean
  notification: boolean
  userAgent: string
  maxTouchPoints: number
  standalone: boolean
}

function isAppleMobile(userAgent: string, maxTouchPoints: number) {
  return (
    /iPhone|iPad|iPod/.test(userAgent) ||
    (/Macintosh/.test(userAgent) && maxTouchPoints > 1)
  )
}

// iOS and iPadOS only expose Web Push to web apps added to the home screen.
export function pushSupport(env: PushEnvironment): PushSupport {
  if (env.serviceWorker && env.pushManager && env.notification)
    return 'supported'
  if (isAppleMobile(env.userAgent, env.maxTouchPoints) && !env.standalone)
    return 'install-required'
  return 'unsupported'
}

export function pushDeviceLabel(userAgent: string, maxTouchPoints = 0): string {
  const browser = /Edg\//.test(userAgent)
    ? 'Edge'
    : /OPR\/|Opera/.test(userAgent)
      ? 'Opera'
      : /SamsungBrowser\//.test(userAgent)
        ? 'Samsung Internet'
        : /Firefox\/|FxiOS\//.test(userAgent)
          ? 'Firefox'
          : /Chrome\/|CriOS\//.test(userAgent)
            ? 'Chrome'
            : /Safari\//.test(userAgent)
              ? 'Safari'
              : 'Navigateur'
  const system = /iPhone/.test(userAgent)
    ? 'iPhone'
    : /iPad/.test(userAgent) ||
        (/Macintosh/.test(userAgent) && maxTouchPoints > 1)
      ? 'iPad'
      : /Android/.test(userAgent)
        ? 'Android'
        : /Windows/.test(userAgent)
          ? 'Windows'
          : /Mac OS X|Macintosh/.test(userAgent)
            ? 'macOS'
            : /CrOS/.test(userAgent)
              ? 'ChromeOS'
              : /Linux/.test(userAgent)
                ? 'Linux'
                : ''
  return system ? `${browser} · ${system}` : browser
}

export function base64UrlToBytes(value: string): Uint8Array<ArrayBuffer> {
  const padded = value.replace(/-/g, '+').replace(/_/g, '/')
  const binary = atob(padded + '='.repeat((4 - (padded.length % 4)) % 4))
  const bytes = new Uint8Array(new ArrayBuffer(binary.length))
  for (let index = 0; index < binary.length; index++)
    bytes[index] = binary.charCodeAt(index)
  return bytes
}

export function bytesToBase64Url(buffer: ArrayBuffer | null): string {
  if (!buffer) return ''
  let binary = ''
  for (const byte of new Uint8Array(buffer)) binary += String.fromCharCode(byte)
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

export function sameServerKey(
  current: ArrayBuffer | null | undefined,
  publicKey: string,
): boolean {
  return !!current && bytesToBase64Url(current) === publicKey
}
