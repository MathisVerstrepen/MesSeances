import type { Ref } from 'vue'

// Auth-flow drafts and link tokens survive only ordinary same-identity focus.
// Unlike revision-bound grants, drafts are not proofs of session continuity.
export function useAccountFlowDraft(...values: Ref<string>[]) {
  const { session } = useAccountSession()
  const clear = useAccountSecrets(...values)
  watch(
    () => [
      session.value?.enabled,
      session.value?.state,
      !!session.value?.account,
      session.value?.account?.email,
      session.value?.account?.username,
    ],
    (current, previous) => {
      if (current.some((value, index) => value !== previous[index])) clear()
    },
    { flush: 'sync' },
  )
  return clear
}
