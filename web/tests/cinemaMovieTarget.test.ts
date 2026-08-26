import assert from 'node:assert/strict'
import test from 'node:test'
import { cinemaMovieTarget } from '../app/utils/cinemaMovieTarget.ts'

test('builds a film route forced to exactly the current theater', () => {
  assert.equal(cinemaMovieTarget('film-42', 'ugc-25'), '/film/film-42?shared_theaters=ugc-25')
})

test('encodes the movie slug and rejects invalid theater identifiers', () => {
  assert.equal(cinemaMovieTarget('film spécial', 'kinepolis_42'), '/film/film%20sp%C3%A9cial?shared_theaters=kinepolis_42')
  assert.throws(() => cinemaMovieTarget('film-42', 'invalid theater'))
})
