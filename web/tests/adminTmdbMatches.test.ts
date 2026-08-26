import assert from 'node:assert/strict'
import test from 'node:test'
import { adminPendingMatchesForFilter } from '../app/utils/adminTmdbMatches.ts'

const matches = [
  { id: 'review', status: 'review_required' as const },
  { id: 'unmatched', status: 'unmatched' as const },
  { id: 'rejected', status: 'rejected' as const }
]

test('keeps rejected matches out of the unresolved section', () => {
  assert.deepEqual(adminPendingMatchesForFilter(matches, 'unresolved').map(match => match.id), ['review', 'unmatched'])
})

test('keeps only rejected matches in the Non-TMDB section', () => {
  assert.deepEqual(adminPendingMatchesForFilter(matches, 'rejected').map(match => match.id), ['rejected'])
})
