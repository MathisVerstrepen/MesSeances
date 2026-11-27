import type { NotificationTestResult } from '../types/notifications.ts'

function devices(count: number) {
  return `${count} ${count === 1 ? 'appareil' : 'appareils'}`
}

export function notificationTestMessage(
  result: NotificationTestResult,
  email: string,
): string {
  if (result.channel === 'email')
    return `E-mail de test envoyé${email ? ` à ${email}` : ''}. Il peut mettre quelques minutes à arriver.`
  const parts = [`Notification envoyée à ${devices(result.delivered)}.`]
  if (result.expired)
    parts.push(
      `${devices(result.expired)} ${result.expired === 1 ? 'n’accepte' : 'n’acceptent'} plus les notifications et ${result.expired === 1 ? 'a été retiré' : 'ont été retirés'}.`,
    )
  if (result.failed)
    parts.push(
      `Le service de notifications n’a pas répondu pour ${devices(result.failed)}.`,
    )
  return parts.join(' ')
}
