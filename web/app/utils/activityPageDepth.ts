// Remember only a page count in this browser-history entry. Activity payloads,
// account identities and continuation cursors must never enter browser storage.
export function activityPageDepth(
  history?: Pick<History, 'state' | 'replaceState'>,
) {
  const position = history?.state?.position
  const path = history?.state?.current
  const saved: unknown = history?.state?.activityPages
  const current = () =>
    !!history &&
    history.state?.position === position &&
    history.state?.current === path
  return {
    target:
      Number.isSafeInteger(saved) && Number(saved) > 0 ? Number(saved) : 1,
    record(pages: number) {
      if (current())
        history!.replaceState({ ...history!.state, activityPages: pages }, '')
    },
  }
}
