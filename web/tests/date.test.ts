import assert from 'node:assert/strict'
import test from 'node:test'
import { addCalendarDays, calendarDateFromDate, dateFromCalendarDate, formatShortCalendarDate, isCalendarDate } from '../app/utils/date.ts'

test('validates real calendar dates', () => {
  assert.equal(isCalendarDate('2026-08-26'), true)
  assert.equal(isCalendarDate('2026-02-29'), false)
  assert.equal(isCalendarDate('2026-2-09'), false)
  assert.equal(isCalendarDate('invalid'), false)
})

test('converts calendar dates at local noon and rejects invalid input', () => {
  const date = dateFromCalendarDate('2026-08-26')
  assert.ok(date)
  assert.equal(date.getFullYear(), 2026)
  assert.equal(date.getMonth(), 7)
  assert.equal(date.getDate(), 26)
  assert.equal(date.getHours(), 12)
  assert.equal(dateFromCalendarDate('2026-02-29'), null)
  assert.equal(calendarDateFromDate(new Date(Number.NaN)), '')
})

test('converts local dates to calendar strings', () => {
  assert.equal(calendarDateFromDate(new Date(2026, 7, 26, 12)), '2026-08-26')
})

test('adds UTC calendar days and preserves invalid-input behavior', () => {
  assert.equal(addCalendarDays('2026-08-31', 1), '2026-09-01')
  assert.equal(addCalendarDays('invalid', 1), '')
})

test('formats valid calendar dates as visible dd-MM-yy values', () => {
  assert.equal(formatShortCalendarDate('2026-08-26'), '26-08-26')
  assert.equal(formatShortCalendarDate('invalid'), 'invalid')
})
