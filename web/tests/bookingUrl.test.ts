import assert from 'node:assert/strict'
import test from 'node:test'
import { safeBookingUrl } from '../app/utils/bookingUrl.ts'

test('accepts the captured CGR booking host and route', () => {
  assert.deepEqual(safeBookingUrl('https://achat.cgrcinemas.fr/cgr-abbeville-la-sucrerie/r/123456', 'cgr'), {
    provider: 'cgr',
    url: 'https://achat.cgrcinemas.fr/cgr-abbeville-la-sucrerie/r/123456'
  })
})

test('rejects unsafe or mismatched CGR booking URLs', () => {
  const invalidUrls = [
    'http://achat.cgrcinemas.fr/cgr-lille/r/123',
    'https://user@achat.cgrcinemas.fr/cgr-lille/r/123',
    'https://achat.cgrcinemas.fr:443/cgr-lille/r/123',
    'https://www.cgrcinemas.fr/cgr-lille/r/123',
    'https://cgrcinemas.fr/cgr-lille/r/123',
    'https://tickets.cgrcinemas.fr/cgr-lille/r/123',
    'https://achat.cgrcinemas.fr.evil.test/cgr-lille/r/123',
    'https://ACHAT.CGRCINEMAS.FR/cgr-lille/r/123',
    'https://achat.cgrcinemas.fr/',
    'https://achat.cgrcinemas.fr//r/123',
    'https://achat.cgrcinemas.fr/cgr-lille//r/123',
    'https://achat.cgrcinemas.fr/cgr-lille/r/0',
    'https://achat.cgrcinemas.fr/cgr-lille/r/0123',
    'https://achat.cgrcinemas.fr/cgr-lille/r/not-numeric',
    'https://achat.cgrcinemas.fr/CGR-Lille/r/123',
    'https://achat.cgrcinemas.fr/cgr_lille/r/123',
    'https://achat.cgrcinemas.fr/cgr-lille/../secret/r/123',
    'https://achat.cgrcinemas.fr/cgr-lille/%2e%2e/r/123',
    'https://achat.cgrcinemas.fr/cgr-lille\\r\\123',
    'https://achat.cgrcinemas.fr/cgr-lille/r/123?source=programme',
    'https://achat.cgrcinemas.fr/cgr-lille/r/123#details'
  ]

  for (const url of invalidUrls) assert.equal(safeBookingUrl(url, 'cgr'), null, url)
  assert.equal(safeBookingUrl('https://achat.cgrcinemas.fr/cgr-lille/r/123', 'ugc'), null)
})
