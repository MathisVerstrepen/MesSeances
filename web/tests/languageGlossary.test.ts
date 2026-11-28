import assert from 'node:assert/strict'
import test from 'node:test'
import {
  languageDefinition,
  languageOptionTitle,
  splitLanguageTerms,
} from '../app/utils/languageGlossary.ts'
import {
  queryLanguageOptions,
  showtimeLanguageOptions,
  showtimeLanguageValues,
  languageLabel,
} from '../app/utils/showtimeFilters.ts'

test('every displayed language label has an explanation', () => {
  for (const value of [...showtimeLanguageValues, 'VOF'])
    assert.ok(languageDefinition(languageLabel(value)), value)
  for (const option of [...queryLanguageOptions, ...showtimeLanguageOptions]) {
    if (option.value === 'ALL') continue
    assert.ok(
      splitLanguageTerms(option.label).some((part) => part.kind === 'term'),
      option.label,
    )
  }
})

test('splitLanguageTerms keeps text and prefers the longest acronym', () => {
  assert.deepEqual(
    splitLanguageTerms('Version originale (VOF & VOSTFR)').map(
      (part) => `${part.kind}:${part.text}`,
    ),
    [
      'text:Version originale (',
      'term:VOF',
      'text: & ',
      'term:VOSTFR',
      'text:)',
    ],
  )
  assert.deepEqual(
    splitLanguageTerms('VF SME · 2D').map((part) => part.text),
    ['VF SME', ' · 2D'],
  )
  assert.deepEqual(
    splitLanguageTerms('VF_SME').map((part) => part.text),
    ['VF SME'],
  )
})

test('splitLanguageTerms ignores acronyms inside words and lowercase text', () => {
  for (const text of ['VOLTA', 'Voir en vo', 'AVF', 'Toutes les langues'])
    assert.deepEqual(splitLanguageTerms(text), [{ kind: 'text', text }])
})

test('languageOptionTitle describes every acronym of a native option', () => {
  assert.equal(languageOptionTitle('Toutes les langues'), undefined)
  assert.equal(
    languageOptionTitle('Version originale (VOF & VOSTFR)'),
    'VOF : Version originale française : film tourné en français · VOSTFR : Version originale sous-titrée en français',
  )
})
