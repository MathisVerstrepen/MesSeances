import assert from 'node:assert/strict'
import { readFile, readdir } from 'node:fs/promises'
import test from 'node:test'
import { compileTemplate, parse } from '@vue/compiler-sfc'

const app = new URL('../app/', import.meta.url)

test('only admin homepage owns logout UI and API wiring; subpages keep auth and navigation', async () => {
  const pages = await readdir(new URL('pages/admin/', app))
  for (const name of pages.filter((name) => name.endsWith('.vue'))) {
    const source = await readFile(new URL(`pages/admin/${name}`, app), 'utf8')
    const { descriptor } = parse(source)
    const template = descriptor.template!.content
    if (name === 'index.vue') {
      const buttons = [
        ...template.matchAll(/<button\b[^>]*>[\s\S]*?<\/button>/g),
      ]
      const logoutButtons = buttons.filter((button) =>
        button[0].includes('Se déconnecter'),
      )
      assert.equal(logoutButtons.length, 1)
      assert.match(logoutButtons[0]![0], /@click="logout"/)
      assert.match(logoutButtons[0]![0], /:disabled="loggingOut"/)
      assert.match(logoutButtons[0]![0], /v-if="loggingOut"/)
      assert.match(
        logoutButtons[0]![0],
        /loggingOut \? 'Déconnexion…' : 'Se déconnecter'/,
      )
      const script = descriptor.scriptSetup!.content
      assert.match(script, /const loggingOut = ref\(false\)/)
      assert.match(script, /if \(loggingOut\.value\) return/)
      assert.match(
        script,
        /loggingOut\.value = true\s+errorMessage\.value = ''/,
      )
      assert.match(
        script,
        /await api\.adminLogout\(\)\s+await navigateTo\('\/admin\/login'\)/,
      )
      assert.match(
        script,
        /catch \(error\) \{\s+errorMessage\.value = getFrenchAdminApiError\(error\)/,
      )
      assert.match(script, /finally \{\s+loggingOut\.value = false/)
      assert.match(template, /<div\s+v-if="errorMessage"[^>]*role="alert"/)
      assert.match(template, /\{\{ errorMessage \}\}/)
    } else {
      assert.doesNotMatch(
        source,
        /Se déconnecter|Déconnexion|adminLogout|\blogout\b|\bLogOut\b|\bloggingOut\b|\blogoutError\b/,
        name,
      )
      if (name !== 'login.vue') {
        assert.match(template, /<NuxtLink\s+to="\/admin"/, name)
      }
    }
    if (name !== 'login.vue') {
      assert.match(
        source,
        /definePageMeta\(\{ middleware: 'admin-auth' \}\)/,
        name,
      )
    }
  }
})

test('every admin page and component template compiles', async () => {
  const pages = await readdir(new URL('pages/admin/', app))
  const components = await readdir(new URL('components/admin/', app))
  for (const [directory, names] of [
    ['pages/admin/', pages],
    ['components/admin/', components],
  ] as const) {
    for (const name of names.filter((name) => name.endsWith('.vue'))) {
      const source = await readFile(new URL(`${directory}${name}`, app), 'utf8')
      const { descriptor, errors } = parse(source)
      assert.deepEqual(errors, [], name)
      assert.ok(descriptor.template, name)
      const template = descriptor.template.content
      assert.deepEqual(
        compileTemplate({ source: template, filename: name, id: name }).errors,
        [],
        name,
      )
      if (directory === 'pages/admin/') {
        assert.match(template, /<h1[\s>]/, name)
        if (name !== 'login.vue') {
          assert.match(
            source,
            /definePageMeta\(\{ middleware: 'admin-auth' \}\)/,
            name,
          )
        }
      }
    }
  }
})

test('admin loading and empty states reuse the public editorial state component', async () => {
  for (const name of [
    'movies',
    'accounts',
    'cinemas',
    'sync',
    'sync-schedules',
    'theater-locations',
    'tmdb-matches',
    'upcoming-movies',
  ]) {
    const source = await readFile(
      new URL(`pages/admin/${name}.vue`, app),
      'utf8',
    )
    assert.match(source, /<EditorialStatePanel/, name)
  }
  const picker = await readFile(
    new URL('components/admin/AdminMoviePosterPicker.vue', app),
    'utf8',
  )
  assert.match(
    picker,
    /<EditorialStatePanel\s+v-if="status === 'loading'"\s+semantic="status"/,
  )
  assert.match(picker, /v-else-if="status === 'error'"\s+semantic="alert"/)
  assert.match(
    picker,
    /v-else-if="status === 'ready' && !posters.length"\s+semantic="status"/,
  )
  assert.match(picker, /@cancel.prevent="closeModal\(\)"/)
  assert.match(picker, /trigger\.focus\(\{ preventScroll: true \}\)/)
})
