import assert from 'node:assert/strict'
import test from 'node:test'
import { formatBrand, formatLabel, formatOptions, isShowtimeFormat } from '../app/utils/formats.ts'

test('exposes ICE as a text-only showtime and query format', () => {
  assert.deepEqual(formatOptions.find(option => option.value === 'ICE'), { value: 'ICE', label: 'ICE' })
  assert.equal(formatLabel('ice'), 'ICE')
  assert.equal(formatBrand('ICE'), undefined)
  assert.equal(isShowtimeFormat('ICE'), true)
})
