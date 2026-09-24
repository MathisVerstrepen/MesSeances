import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const sources = await Promise.all(
  [
    '../app/pages/cinemas.vue',
    '../app/components/CinemaTheaterMap.client.vue',
    '../app/pages/recherche.vue',
    '../app/pages/film/[slug].vue',
    '../app/components/SharedTheaterNotice.vue',
    '../app/composables/useSharedTheaterRestoration.ts',
    '../app/pages/index.vue',
    '../app/pages/confidentialite.vue',
    '../app/pages/films/index.vue',
    '../app/pages/planning.vue',
    '../app/pages/cinema/[slug].vue',
    '../app/pages/ville/[slug]/cinemas.vue',
  ].map((path) => readFile(new URL(path, import.meta.url), 'utf8')),
)

test('uses Mes cinémas for visible preference headings and actions', () => {
  const [cinemas, map, search, film, notice, restoration, home, privacy] =
    sources
  assert.match(cinemas!, /Mes<br\s*\/?><span>cinémas/)
  assert.match(cinemas!, /aria-label="Sélection de mes cinémas"/)
  assert.match(map!, />\s*Mes cinémas\s*<\/span\s*>/)
  assert.match(map!, /'Retirer de mes cinémas' : 'Ajouter à mes cinémas'/)
  assert.match(search!, />\s*Gérer mes cinémas\s*<\/NuxtLink\s*>/)
  assert.match(film!, />\s*Modifier mes cinémas\s*<\/NuxtLink\s*>/)
  assert.match(notice!, /Utiliser mes cinémas/)
  assert.match(restoration!, /Vos cinémas n’ont pas pu être restaurés/)
  assert.match(home!, /Gardez vos cinémas au centre/)
  assert.match(privacy!, /Stockage local « Mes cinémas »/)
})

test('removes user-facing favorite terminology without renaming internal contracts', () => {
  const renderedCopy = sources.join('\n')
  assert.doesNotMatch(
    renderedCopy,
    /cinémas favoris|mes favoris|vos favoris|des favoris|aux favoris|>Favori</i,
  )
  assert.match(renderedCopy, /favoriteTheaterIds/)
  assert.match(renderedCopy, /toggle-favorite/)
  assert.match(renderedCopy, /messeances\.favoriteTheaterIds\.v1/)
})

const privacyCopy = sources[7]!.replace(/<[^>]+>/g, '').replace(/\s+/g, ' ')

test('privacy distinguishes local selection from personal account preferences', () => {
  assert.match(privacyCopy, /messeances\.favoriteTheaterIds\.v1/)
  assert.match(
    privacyCopy,
    /sélection locale reste distincte de celle du compte/,
  )
  assert.match(
    privacyCopy,
    /enregistrés sur le serveur et associés à ce compte/,
  )
  assert.match(privacyCopy, /Ce sont des données personnelles/)
  assert.match(
    privacyCopy,
    /sélection du compte prévaut sur celle de chaque appareil/,
  )
  assert.match(privacyCopy, /sans remplacer la sélection locale du navigateur/)
  assert.doesNotMatch(
    privacyCopy,
    /restent locaux|sans synchronisation avec le compte|ne constitue donc pas en lui-même un traitement de données personnelles/,
  )
})

test('privacy explains bounded import and non-instant cross-device updates', () => {
  assert.match(
    privacyCopy,
    /Si le compte n’a encore aucune sélection enregistrée/,
  )
  assert.match(
    privacyCopy,
    /cinémas encore disponibles dans le catalogue parmi votre sélection locale enregistrée sont importés une seule fois/,
  )
  assert.match(privacyCopy, /Sans sélection locale enregistrée valide/)
  assert.match(
    privacyCopy,
    /aucun import n’a lieu et le compte attend un enregistrement explicite/,
  )
  assert.match(
    privacyCopy,
    /au retour sur le site, à l’actualisation, lorsque la page redevient active ou lorsque la connexion réseau revient/,
  )
  assert.match(privacyCopy, /onglets du même navigateur sont également avertis/)
  assert.match(privacyCopy, /ne s’agit pas d’une synchronisation instantanée/)
  assert.match(
    privacyCopy,
    /changements non enregistrés doivent être réappliqués/,
  )
})

test('privacy covers preference retention and distinguishes storage exemption from account legal basis', () => {
  assert.match(
    privacyCopy,
    /stockage des préférences associées au compte et leur synchronisation reposent sur l’exécution du contrat de service/,
  )
  assert.match(privacyCopy, /point b\), du RGPD/)
  assert.match(
    privacyCopy,
    /exemption de consentement au stockage local ne dispense pas de respecter le RGPD/,
  )
  assert.match(
    privacyCopy,
    /déconnexion ou la suppression du compte ne supprime pas la sélection locale/,
  )
  assert.match(
    privacyCopy,
    /supprime cette sélection locale, sans supprimer celle du compte/,
  )
  assert.match(
    privacyCopy,
    /Préférences de cinémas du compte Jusqu’à leur remplacement ou la suppression du compte dans la base active/,
  )
  assert.match(
    privacyCopy,
    /durée de conservation des sauvegardes et leur effacement restent à valider par l’exploitant ; aucune suppression immédiate de ces copies n’est promise/,
  )
  assert.match(
    privacyCopy,
    /compte, moyens de connexion, sessions, préférences de cinémas, liens et messages en attente associés sont supprimés/,
  )
})

test('privacy tracker summary reflects separate stores without announcing production activation', () => {
  const trackers = privacyCopy.split('Résumé des traceurs et stockages')[1]!
  assert.match(
    trackers,
    /sélection locale enregistrée valide peut initialiser un compte sans sélection/,
  )
  assert.match(
    trackers,
    /modifications du compte ne remplacent pas ce stockage/,
  )
  assert.match(
    trackers,
    /Sélection de cinémas du compte Conservée sur le serveur/,
  )
  assert.match(trackers, /n’est pas recopiée dans le stockage local/)
  assert.match(privacyCopy, /Lorsque les comptes sont disponibles/)
  assert.match(
    privacyCopy,
    /cette notice n’annonce pas leur activation en production/,
  )
})

test('keeps one contextual share control per shareable route and places it last in action groups', () => {
  for (const [index, source] of sources.entries()) {
    const count = source.match(/<ShareButton/g)?.length ?? 0
    const expected = [0, 0, 1, 1, 0, 0, 0, 0, 1, 1, 1, 1][index]
    assert.equal(count, expected)
  }
  for (const source of [
    sources[2]!,
    sources[3]!,
    sources[8]!,
    sources[9]!,
    sources[10]!,
    sources[11]!,
  ]) {
    assert.match(source, /<ShareButton[^>]*class="[^"]*shrink-0/)
  }
})
