import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const sources = await Promise.all([
  '../app/pages/cinemas.vue',
  '../app/components/CinemaTheaterMap.client.vue',
  '../app/pages/recherche.vue',
  '../app/pages/film/[slug].vue',
  '../app/components/SharedTheaterNotice.vue',
  '../app/composables/useSharedTheaterRestoration.ts',
  '../app/pages/index.vue',
  '../app/pages/confidentialite.vue',
  '../app/pages/films.vue',
  '../app/pages/planning.vue',
  '../app/pages/cinema/[slug].vue',
  '../app/pages/ville/[slug]/cinemas.vue'
].map((path) => readFile(new URL(path, import.meta.url), 'utf8')))

test('uses Mes cinémas for visible preference headings and actions', () => {
  const [cinemas, map, search, film, notice, restoration, home, privacy] = sources
  assert.match(cinemas!, /Mes<br \/><span>cinémas/)
  assert.match(cinemas!, /aria-label="Sélection de mes cinémas"/)
  assert.match(map!, />Mes cinémas<\/span>/)
  assert.match(map!, /'Retirer de mes cinémas' : 'Ajouter à mes cinémas'/)
  assert.match(search!, />Gérer mes cinémas<\/NuxtLink>/)
  assert.match(film!, />Modifier mes cinémas<\/NuxtLink>/)
  assert.match(notice!, /Utiliser mes cinémas/)
  assert.match(restoration!, /Vos cinémas n’ont pas pu être restaurés/)
  assert.match(home!, /Gardez vos cinémas au centre/)
  assert.match(privacy!, /Stockage local « Mes cinémas »/)
})

test('removes user-facing favorite terminology without renaming internal contracts', () => {
  const renderedCopy = sources.join('\n')
  assert.doesNotMatch(renderedCopy, /cinémas favoris|mes favoris|vos favoris|des favoris|aux favoris|>Favori</i)
  assert.match(renderedCopy, /favoriteTheaterIds/)
  assert.match(renderedCopy, /toggle-favorite/)
  assert.match(renderedCopy, /messeances\.favoriteTheaterIds\.v1/)
})

test('keeps one contextual share control per shareable route and places it last in action groups', () => {
  for (const [index, source] of sources.entries()) {
    const count = source.match(/<ShareButton/g)?.length ?? 0
    const expected = [0, 0, 1, 1, 0, 0, 0, 0, 1, 1, 1, 1][index]
    assert.equal(count, expected)
  }
  for (const source of [sources[2]!, sources[3]!, sources[8]!, sources[9]!, sources[10]!, sources[11]!]) {
    assert.match(source, /<ShareButton[^>]*class="[^"]*shrink-0/)
  }
})
