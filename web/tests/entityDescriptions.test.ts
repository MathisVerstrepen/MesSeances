import assert from 'node:assert/strict'
import test from 'node:test'
import {
  cinemaDescription,
  cityDescription,
  cityShowtimesDescription,
} from '../app/utils/entityDescriptions.ts'

test('builds French city descriptions with stored counts and correct plurals', () => {
  assert.equal(
    cityDescription('Lille', 1, 1),
    'À Lille, 1 cinéma programme actuellement 1 film.',
  )
  assert.equal(
    cityDescription('Roubaix', 2, 0),
    'À Roubaix, 2 cinémas programment actuellement 0 films.',
  )
})

test('builds cinema descriptions from provider, location, and available-date count', () => {
  assert.equal(
    cinemaDescription({
      name: 'Capitole Studios',
      provider: 'cinewest',
      city: 'Le Pontet',
      address: '',
      postalCode: '84130',
      availableDateCount: 2,
    }),
    'Capitole Studios est un cinéma Cinewest à 84130 Le Pontet. Sa programmation compte 2 dates disponibles.',
  )
  assert.equal(
    cinemaDescription({
      name: 'Katorza',
      provider: 'cineville',
      city: 'Quimper',
      address: '',
      postalCode: '29000',
      availableDateCount: 2,
    }),
    'Katorza est un cinéma Cinéville à 29000 Quimper. Sa programmation compte 2 dates disponibles.',
  )
  assert.equal(
    cinemaDescription({
      name: 'Megarama Bordeaux',
      provider: 'megarama',
      city: 'Bordeaux',
      address: '',
      postalCode: '',
      availableDateCount: 0,
    }),
    'Megarama Bordeaux est un cinéma Megarama à Bordeaux. Sa programmation compte 0 dates disponibles.',
  )
  assert.equal(
    cinemaDescription({
      name: 'UGC Lille',
      provider: 'ugc',
      city: 'Lille',
      address: '40 rue de Béthune',
      postalCode: '59000',
      availableDateCount: 2,
    }),
    'UGC Lille est un cinéma UGC à 40 rue de Béthune, 59000 Lille. Sa programmation compte 2 dates disponibles.',
  )

  assert.equal(
    cinemaDescription({
      name: 'Kinepolis Lomme',
      provider: 'kinepolis',
      city: 'Lomme',
      address: '',
      postalCode: '',
      availableDateCount: 1,
    }),
    'Kinepolis Lomme est un cinéma Kinepolis à Lomme. Sa programmation compte 1 date disponible.',
  )

  assert.equal(
    cinemaDescription({
      name: 'Pathé Bellecour',
      provider: 'pathe',
      city: 'Lyon',
      address: '79 rue de la République',
      postalCode: '69002',
      availableDateCount: 3,
    }),
    'Pathé Bellecour est un cinéma Pathé à 79 rue de la République, 69002 Lyon. Sa programmation compte 3 dates disponibles.',
  )

  assert.equal(
    cinemaDescription({
      name: 'CGR Bordeaux Le Français',
      provider: 'cgr',
      city: 'Bordeaux',
      address: '9 rue Montesquieu',
      postalCode: '33000',
      availableDateCount: 4,
    }),
    'CGR Bordeaux Le Français est un cinéma CGR à 9 rue Montesquieu, 33000 Bordeaux. Sa programmation compte 4 dates disponibles.',
  )
})

test('cinema descriptions drop postal codes and cities already in the address', () => {
  assert.equal(
    cinemaDescription({
      name: 'Le Majestic',
      provider: 'ugc',
      city: 'LILLE',
      address: '54 rue de Béthune 59000 LILLE',
      postalCode: '59000',
      availableDateCount: 4,
    }),
    'Le Majestic est un cinéma UGC à 54 rue de Béthune 59000 LILLE. Sa programmation compte 4 dates disponibles.',
  )
})

test('cinema and city descriptions lead with the films on screen', () => {
  assert.equal(
    cinemaDescription({
      name: 'Le Majestic',
      provider: 'ugc',
      city: 'Lille',
      address: '54 rue de Béthune',
      postalCode: '59000',
      availableDateCount: 4,
      movieTitles: [
        'Un bon petit soldat',
        'Club Kid',
        ' Club Kid ',
        'Malika',
        '15/18',
      ],
    }),
    'Le Majestic est un cinéma UGC à 54 rue de Béthune, 59000 Lille. À l’affiche : Un bon petit soldat, Club Kid, Malika et 1 autre film.',
  )
  assert.match(
    cinemaDescription({
      name: 'Cinéma',
      provider: 'cgr',
      city: 'Agen',
      availableDateCount: 1,
      movieTitles: ['Gabin', 'Moulin'],
    }),
    /À l’affiche : Gabin et Moulin\.$/,
  )
  assert.equal(
    cityShowtimesDescription('Lille', 3, [
      'Hunger Games',
      'Billie',
      'Jupiter',
      'Camembert',
      'Moulin',
    ]),
    'Séances de cinéma à Lille : horaires du jour dans 3 cinémas. À l’affiche : Hunger Games, Billie, Jupiter et 2 autres films. Versions VF et VOSTFR, réservation.',
  )
  assert.equal(
    cityShowtimesDescription('Agen', 1, []),
    'Séances de cinéma à Agen : horaires du jour dans 1 cinéma.',
  )
})
