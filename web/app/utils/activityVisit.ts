export function activityEventID(value: string | null): string | null {
  if (value === null || !/^(0|[1-9][0-9]{0,18})$/.test(value)) return null
  return BigInt(value) <= 9223372036854775807n ? value : null
}

export function latestActivityEventID(items: readonly { event_id: string }[]) {
  return items.reduce(
    (latest, item) =>
      activityEventID(item.event_id) && BigInt(item.event_id) > BigInt(latest)
        ? item.event_id
        : latest,
    '0',
  )
}

// Store only the feed identity and decimal event ID, never account information.
export function visitGuestActivity(
  storage: Pick<Storage, 'getItem' | 'setItem'>,
  scope: string,
  latest: string,
) {
  const key = `messeances:activity-visit:v1:${scope}`
  const previous = activityEventID(storage.getItem(key)) ?? '0'
  storage.setItem(key, BigInt(previous) > BigInt(latest) ? previous : latest)
  return previous
}
