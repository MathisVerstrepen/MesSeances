import assert from 'node:assert/strict'
import test from 'node:test'
import type { Provider, Theater } from '../app/types/api.ts'
import {
  buildTheaterFeatureCollection,
  theaterFeatureBounds,
  THEATER_PROVIDER_COLORS
} from '../app/utils/theaterMap.ts'
import { haversineDistanceKm } from '../app/utils/theaterDistance.ts'

function theater(overrides: Partial<Theater> & Pick<Theater, 'id'>): Theater {
  return {
    provider: 'ugc',
    slug: overrides.id,
    name: overrides.id,
    address: '1 rue du cinéma',
    city: 'Lille',
    city_slug: 'lille',
    postal_code: '59000',
    available_dates: [],
    accepted_passes: [],
    latitude: 50.6292,
    longitude: 3.0573,
    ...overrides
  }
}

test('defines an exhaustive provider palette', () => {
  const providers: Provider[] = ['ugc', 'kinepolis', 'pathe', 'cgr']
  assert.deepEqual(Object.keys(THEATER_PROVIDER_COLORS), providers)
  assert.deepEqual(THEATER_PROVIDER_COLORS, {
    ugc: '#0b5cad',
    kinepolis: '#7e22ce',
    pathe: '#d97706',
    cgr: '#c81e1e'
  })
})

test('excludes invalid coordinate pairs and keeps longitude-latitude order', () => {
  const collection = buildTheaterFeatureCollection([
    theater({ id: 'valid', latitude: 48.8566, longitude: 2.3522 }),
    theater({ id: 'partial', longitude: null }),
    theater({ id: 'nan', latitude: Number.NaN }),
    theater({ id: 'latitude-range', latitude: 91 }),
    theater({ id: 'longitude-range', longitude: -181 })
  ], new Set())

  assert.equal(collection.features.length, 1)
  assert.deepEqual(collection.features[0]?.geometry.coordinates, [2.3522, 48.8566])
  assert.deepEqual(collection.features[0]?.properties, { id: 'valid', provider: 'ugc', favorite: false })
})

test('updates only favorite feature properties without mutating input', () => {
  const theaters = [
    theater({ id: 'ugc' }),
    theater({ id: 'cgr', provider: 'cgr', latitude: 45.764, longitude: 4.8357 })
  ]
  const before = structuredClone(theaters)
  const collection = buildTheaterFeatureCollection(theaters, new Set(['cgr']))

  assert.deepEqual(theaters, before)
  assert.deepEqual(collection.features.map((feature) => [feature.properties.id, feature.properties.favorite]), [
    ['ugc', false],
    ['cgr', true]
  ])
  assert.deepEqual(Object.keys(collection.features[0]!.properties).sort(), ['favorite', 'id', 'provider'])
})

test('spreads coincident theaters deterministically on a 35 meter ring', () => {
  const origin = { latitude: 50.6292, longitude: 3.0573 }
  const theaters = [theater({ id: 'c' }), theater({ id: 'a' }), theater({ id: 'b' })]
  const before = structuredClone(theaters)
  const collection = buildTheaterFeatureCollection(theaters, new Set())
  const reversed = buildTheaterFeatureCollection([...theaters].reverse(), new Set())

  assert.deepEqual(theaters, before)
  assert.deepEqual(collection, reversed)
  assert.deepEqual(collection.features.map((feature) => feature.properties.id), ['a', 'b', 'c'])
  for (const feature of collection.features) {
    const [longitude, latitude] = feature.geometry.coordinates
    const distance = haversineDistanceKm(origin, { latitude, longitude })
    assert.ok(distance !== null && Math.abs(distance - 0.035) < 0.000001)
  }
})

test('keeps singleton coordinates exact and calculates empty, singleton, and multiple bounds', () => {
  const empty = buildTheaterFeatureCollection([theater({ id: 'missing', latitude: null })], new Set())
  assert.equal(theaterFeatureBounds(empty), null)

  const singleton = buildTheaterFeatureCollection([theater({ id: 'one', latitude: 43.3, longitude: 5.4 })], new Set())
  assert.deepEqual(singleton.features[0]?.geometry.coordinates, [5.4, 43.3])
  assert.deepEqual(theaterFeatureBounds(singleton), {
    west: 5.4,
    south: 43.3,
    east: 5.4,
    north: 43.3,
    count: 1
  })

  const multiple = buildTheaterFeatureCollection([
    theater({ id: 'north-west', latitude: 51, longitude: -5 }),
    theater({ id: 'south-east', latitude: 42, longitude: 9 })
  ], new Set())
  assert.deepEqual(theaterFeatureBounds(multiple), {
    west: -5,
    south: 42,
    east: 9,
    north: 51,
    count: 2
  })
})
