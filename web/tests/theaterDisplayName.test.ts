import assert from 'node:assert/strict'
import test from 'node:test'
import { theaterDisplayName } from '../app/utils/theaterDisplayName.ts'

test('brand-only cinema names include the city', () => {
  for (const city of ['Annecy', 'Boulogne sur Mer']) {
    assert.equal(theaterDisplayName({ name: 'Megarama', city }), `Megarama ${city}`)
  }
  for (const name of ['UGC', 'CGR', 'IMAX', 'Kinepolis', 'Pathé', 'Pathe']) {
    assert.equal(theaterDisplayName({ name, city: 'Paris' }), `${name} Paris`)
  }
})

test('existing cinema names remain unchanged', () => {
  for (const name of ['Megarama Le Palace Cambrai', 'Megarama Annecy', 'Le Palace', 'MegaramaX']) {
    assert.equal(theaterDisplayName({ name, city: 'Cambrai' }), name)
  }
})

test('brand matching ignores case and surrounding whitespace', () => {
  assert.equal(theaterDisplayName({ name: ' megarama ', city: ' Annecy ' }), 'megarama Annecy')
})

test('missing city does not add whitespace or alter the name', () => {
  assert.equal(theaterDisplayName({ name: 'Megarama', city: '  ' }), 'Megarama')
})
