import assert from 'node:assert/strict'
import test from 'node:test'
import { notificationTestMessage } from '../app/utils/notificationTest.ts'

test('test send results are phrased per channel', () => {
  assert.equal(
    notificationTestMessage(
      { channel: 'email', delivered: 1, expired: 0, failed: 0 },
      'a@example.com',
    ),
    'E-mail de test envoyé à a@example.com. Il peut mettre quelques minutes à arriver.',
  )
  assert.equal(
    notificationTestMessage(
      { channel: 'push', delivered: 1, expired: 0, failed: 0 },
      '',
    ),
    'Notification envoyée à 1 appareil.',
  )
  assert.equal(
    notificationTestMessage(
      { channel: 'push', delivered: 2, expired: 1, failed: 1 },
      '',
    ),
    'Notification envoyée à 2 appareils. 1 appareil n’accepte plus les notifications et a été retiré. Le service de notifications n’a pas répondu pour 1 appareil.',
  )
  assert.equal(
    notificationTestMessage(
      { channel: 'push', delivered: 1, expired: 2, failed: 0 },
      '',
    ),
    'Notification envoyée à 1 appareil. 2 appareils n’acceptent plus les notifications et ont été retirés.',
  )
})
