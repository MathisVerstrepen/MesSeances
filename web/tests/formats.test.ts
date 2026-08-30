import assert from 'node:assert/strict'
import test from 'node:test'
import { formatBrand, formatLabel, formatOptions, formatRuntime, formatShowtimeCount, isShowtimeFormat } from '../app/utils/formats.ts'

test('exposes ICE as a text-only showtime and query format', () => {
  assert.deepEqual(formatOptions.find(option => option.value === 'ICE'), { value: 'ICE', label: 'ICE' })
  assert.equal(formatLabel('ice'), 'ICE')
  assert.equal(formatBrand('ICE'), undefined)
  assert.equal(isShowtimeFormat('ICE'), true)
})

test('formats movie runtime without changing missing-duration copy', () => {
  assert.equal(formatRuntime(125), '2h 5min')
  assert.equal(formatRuntime(120), '2h')
  assert.equal(formatRuntime(45), '45min')
  assert.equal(formatRuntime(0), 'Durée non renseignée')
  assert.equal(formatRuntime(90.5), 'Durée non renseignée')
})

test('pluralizes showtime counts in French', () => {
  assert.equal(formatShowtimeCount(0), '0 séances')
  assert.equal(formatShowtimeCount(1), '1 séance')
  assert.equal(formatShowtimeCount(2), '2 séances')
})
