import assert from 'node:assert/strict'
import test from 'node:test'
import {
  base64UrlToBytes,
  bytesToBase64Url,
  pushDeviceLabel,
  pushSupport,
  sameServerKey,
} from '../app/utils/webPush.ts'

const env = {
  serviceWorker: true,
  pushManager: true,
  notification: true,
  userAgent: '',
  maxTouchPoints: 0,
  standalone: false,
}

test('push support detects iOS home screen requirement', () => {
  assert.equal(pushSupport(env), 'supported')
  const safari = {
    ...env,
    pushManager: false,
    notification: false,
    userAgent:
      'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Version/18.0 Mobile/15E148 Safari/604.1',
  }
  assert.equal(pushSupport(safari), 'install-required')
  assert.equal(pushSupport({ ...safari, standalone: true }), 'unsupported')
  assert.equal(
    pushSupport({
      ...safari,
      userAgent: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Safari/605',
      maxTouchPoints: 5,
    }),
    'install-required',
  )
  assert.equal(pushSupport({ ...env, pushManager: false }), 'unsupported')
})

test('device labels stay coarse', () => {
  for (const [ua, label] of [
    [
      'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/131 Safari/537.36',
      'Chrome · Linux',
    ],
    [
      'Mozilla/5.0 (Windows NT 10.0) AppleWebKit/537.36 Chrome/131 Safari/537.36 Edg/131',
      'Edge · Windows',
    ],
    [
      'Mozilla/5.0 (Android 14; Mobile; rv:132.0) Gecko/132.0 Firefox/132.0',
      'Firefox · Android',
    ],
    [
      'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) Version/18.0 Mobile Safari/604.1',
      'Safari · iPhone',
    ],
    [
      'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Version/18.0 Safari/605.1.15',
      'Safari · macOS',
    ],
    ['curl/8', 'Navigateur'],
  ] as const)
    assert.equal(pushDeviceLabel(ua), label)
  assert.equal(
    pushDeviceLabel('Mozilla/5.0 (Macintosh) Version/18.0 Safari/605', 5),
    'Safari · iPad',
  )
})

test('application server keys round-trip through base64url', () => {
  const key =
    'BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4'
  const bytes = base64UrlToBytes(key)
  assert.equal(bytes.length, 65)
  assert.equal(bytesToBase64Url(bytes.buffer), key)
  assert.ok(sameServerKey(bytes.buffer, key))
  assert.ok(!sameServerKey(bytes.buffer, key.replace('B', 'C')))
  assert.ok(!sameServerKey(null, key))
  assert.equal(bytesToBase64Url(null), '')
})
