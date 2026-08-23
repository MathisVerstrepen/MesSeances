import assert from 'node:assert/strict'
import test from 'node:test'
import { cinemaDescription, cityDescription } from '../app/utils/entityDescriptions.ts'

test('builds French city descriptions with stored counts and correct plurals', () => {
  assert.equal(cityDescription('Lille', 1, 1), 'À Lille, 1 cinéma programme actuellement 1 film.')
  assert.equal(cityDescription('Roubaix', 2, 0), 'À Roubaix, 2 cinémas programment actuellement 0 films.')
})

test('builds cinema descriptions from provider, location, and available-date count', () => {
  assert.equal(cinemaDescription({
    name: 'UGC Lille',
    provider: 'ugc',
    city: 'Lille',
    address: '40 rue de Béthune',
    postalCode: '59000',
    availableDateCount: 2
  }), 'UGC Lille est un cinéma UGC à 40 rue de Béthune, 59000, Lille. Sa programmation compte 2 dates disponibles.')

  assert.equal(cinemaDescription({
    name: 'Kinepolis Lomme',
    provider: 'kinepolis',
    city: 'Lomme',
    address: '',
    postalCode: '',
    availableDateCount: 1
  }), 'Kinepolis Lomme est un cinéma Kinepolis à Lomme. Sa programmation compte 1 date disponible.')
})
