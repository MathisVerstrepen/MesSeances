import assert from 'node:assert/strict'
import { readFile, readdir } from 'node:fs/promises'
import test from 'node:test'
import { compileTemplate, parse } from '@vue/compiler-sfc'

const app = new URL('../app/', import.meta.url)
const obsoleteStyles =
  /\b(?:rounded-(?:sm|md|lg|full)|shadow-(?:sm|md|lg|xl)|border-line(?:-hover)?|(?:bg|border)-(?:amber|green|emerald|violet)-\d+)\b/
const obsoletePrimitives =
  /(?:^|\s)class="(?:[^"\n]*\s)?(?:field|button-primary|state-panel)(?=[\s"])/

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

test('admin homepage has one distinct decorative leading icon per tool entry', async () => {
  const source = await readFile(new URL('pages/admin/index.vue', app), 'utf8')
  const { descriptor } = parse(source)
  const entries = [
    ...descriptor.template!.content.matchAll(
      /<NuxtLink\b[^>]*>([\s\S]*?)<\/NuxtLink>/g,
    ),
  ]
  assert.equal(entries.length, 7)
  const icons = entries.map((entry) => {
    const leadingSpan = entry[1]!.match(/^\s*<span\b[^>]*>([\s\S]*?)<\/span>/)
    assert.ok(leadingSpan, 'each tool entry has a leading icon container')
    const leadingIcons = [
      ...leadingSpan[1]!.matchAll(/<([A-Z]\w*)\b([^>]*)\/>/g),
    ]
    assert.equal(
      leadingIcons.length,
      1,
      'each tool entry has exactly one leading icon',
    )
    const icon = leadingIcons[0]!
    assert.notEqual(
      icon[1],
      'ArrowRight',
      'navigation arrows are not entry icons',
    )
    assert.match(icon[2]!, /:size="22"/)
    assert.match(icon[2]!, /aria-hidden="true"/)
    return icon[1]
  })
  assert.equal(new Set(icons).size, 7)
  assert.deepEqual(icons, [
    'CalendarDays',
    'GitCompareArrows',
    'FilePenLine',
    'RefreshCw',
    'CalendarClock',
    'MapPin',
    'Users',
  ])
})

test('legacy style check recognizes class tokens, not Vue field identifiers', () => {
  assert.match('<input class="field mt-2" />', obsoletePrimitives)
  assert.match('<button class="mt-4 button-primary" />', obsoletePrimitives)
  assert.match('<div class="state-panel" />', obsoletePrimitives)
  assert.doesNotMatch(
    '<input :key="field" :value="field" />',
    obsoletePrimitives,
  )
  assert.doesNotMatch(
    "<div :class=\"field === 'overview' ? 'lg:col-span-2' : ''\" />",
    obsoletePrimitives,
  )
  assert.doesNotMatch('<input class="editorial-field" />', obsoletePrimitives)
})

test('every admin page and component uses public editorial primitives, without a secondary soft theme', async () => {
  const pages = await readdir(new URL('pages/admin/', app))
  const components = await readdir(new URL('components/admin/', app))
  assert.equal(pages.filter((name) => name.endsWith('.vue')).length, 9)
  assert.equal(components.filter((name) => name.endsWith('.vue')).length, 4)
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
      assert.doesNotMatch(source, obsoleteStyles, name)
      assert.doesNotMatch(template, obsoletePrimitives, name)
      assert.match(
        template,
        /border-ink|editorial-(?:title|button|field)/,
        name,
      )
      assert.equal(
        descriptor.styles.length,
        0,
        `${name}: no route-scoped theme`,
      )
      if (directory === 'pages/admin/') {
        assert.match(template, /<h1[\s>]/, name)
        assert.match(template, /editorial-title|font-black/, name)
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

test('shared fields and buttons replace obsolete recipes and also serve public booking links', async () => {
  const css = await readFile(new URL('assets/css/main.css', app), 'utf8')
  assert.doesNotMatch(css, /\.(?:field|button-primary|state-panel)\s*\{/)
  assert.match(
    css,
    /\.editorial-field\s*\{[^}]*min-h-11[^}]*border-2 border-ink/,
  )
  assert.match(css, /\.editorial-button\s*\{[^}]*bg-ink text-white[^}]*shadow-/)
  assert.match(
    css,
    /\.editorial-button-outline\s*\{[^}]*bg-surface text-ink[^}]*hover:bg-highlight/,
  )
  assert.match(
    css,
    /\.editorial-button-danger\s*\{[^}]*border-primary bg-primary-soft text-primary/,
  )
  assert.match(
    css,
    /\.editorial-title\s*\{[^}]*Noto_Sans_Variable[^}]*font-black/,
  )
  assert.match(css, /:focus-visible\s*\{[^}]*ring-2/)
  assert.match(css, /prefers-reduced-motion: reduce/)
  const booking = await readFile(
    new URL('components/BookingLink.vue', app),
    'utf8',
  )
  assert.match(booking, /unstyled \? '' : 'editorial-button'/)
  assert.match(booking, /target="_blank"\s+rel="noopener noreferrer"/)
})

test('admin loading and empty states reuse the public editorial state component', async () => {
  for (const name of [
    'movies',
    'accounts',
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
    assert.doesNotMatch(source, obsoletePrimitives, name)
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

test('AG Grid uses its theming API with public tokens and keeps dense editing and responsive scrolling', async () => {
  const source = await readFile(
    new URL('components/admin/AdminMoviesGrid.client.vue', app),
    'utf8',
  )
  assert.match(source, /themeQuartz\.withParams\(\{/)
  assert.match(source, /headerBackgroundColor: 'var\(--color-highlight\)'/)
  assert.match(source, /borderColor: 'var\(--color-ink\)'/)
  assert.match(source, /wrapperBorderRadius: 0/)
  assert.match(source, /inputBorderRadius: 0/)
  assert.match(source, /buttonBorderRadius: 0/)
  assert.match(source, /popupShadow: '4px 4px 0 var\(--color-ink\)'/)
  assert.match(source, /:row-height="42"/)
  assert.match(source, /row-model-type="infinite"/)
  assert.match(source, /:enable-cell-text-selection="true"/)
  assert.match(source, /pinned: narrow \? null : 'left'/)
  assert.match(source, /pinned: narrow \? null : 'right'/)
})
