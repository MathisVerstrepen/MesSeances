export function hasAdminSyncLog(log: readonly string[] | undefined): boolean {
  return Boolean(log?.length)
}

export function joinAdminSyncLog(log: readonly string[] | undefined): string {
  return log?.join('\n') ?? ''
}
