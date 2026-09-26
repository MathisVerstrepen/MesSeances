import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { compile, createSSRApp, ref } from 'vue'
import { Pencil, UserRound } from '@lucide/vue'

const source = await readFile(
  new URL('../app/components/AccountAvatar.vue', import.meta.url),
  'utf8',
)
const { descriptor } = parse(source)
const render = compile(descriptor.template!.content)

// Render the real template; transport and picker semantics have their own tests.
async function avatarMarkup(
  options: {
    url?: string | null
    disabled?: boolean
    selected?: File | null
    pending?: boolean
    uploading?: boolean
    progress?: number | null
    error?: string
    previewError?: string
    image?: string
  } = {},
) {
  const app = createSSRApp({
    render,
    setup: () => ({
      url: null,
      disabled: false,
      selectionDisabled: options.disabled ?? false,
      selected: null,
      pending: false,
      uploading: false,
      progress: 0,
      error: '',
      ...options,
      preview: {
        image: ref(
          options.image ?? (options.url ? 'blob:synthetic-avatar' : ''),
        ),
        loading: ref(false),
        error: ref(options.previewError ?? ''),
        failed: () => {},
        refresh: () => {},
      },
      openPicker: () => {},
      choose: () => {},
      save: () => {},
      cancel: () => {},
    }),
  })
  app.component('UserRound', UserRound)
  app.component('Pencil', Pencil)
  return renderToString(app)
}

const buttons = (html: string) =>
  [...html.matchAll(/<button\b[^>]*>[\s\S]*?<\/button>/g)].map(
    (match) => match[0],
  )

test('avatar pairs a compact preview with wrapping actions and full-width visible constraints', async () => {
  const html = await avatarMarkup({ url: '/api/v1/account/avatar/1' })
  assert.match(
    html,
    /class="grid grid-cols-\[auto_minmax\(0,1fr\)\] items-center gap-x-3 gap-y-2"/,
  )
  assert.match(
    html,
    /class="group relative flex size-16 min-h-11 min-w-11 shrink-0/,
  )
  assert.match(
    html,
    /<img[^>]*alt="Photo de profil"[^>]*width="64" height="64"/,
  )
  assert.match(html, /class="flex flex-wrap items-center gap-2"/)
  const help = html.match(/<p id="avatar-help"[^>]*>[\s\S]*?<\/p>/)?.[0]
  assert.ok(help)
  assert.match(help, /class="col-span-2 text-sm text-ink\/70"/)
  assert.match(help, /<span class="whitespace-nowrap">JPEG, PNG, WebP<\/span>/)
  assert.match(help, /<span class="whitespace-nowrap">non animés<\/span>/)
  assert.match(help, /<span class="whitespace-nowrap">5 Mio max\.<\/span>/)
  assert.doesNotMatch(help, /hidden|sr-only|truncate/)
})

test('avatar exposes exactly one native focus-restoration trigger in either photo state', async () => {
  for (const url of [null, '/api/v1/account/avatar/1']) {
    const html = await avatarMarkup({ url })
    const controls = buttons(html)
    assert.equal(controls.length, url ? 2 : 1)
    assert.match(
      controls[0]!,
      url
        ? /aria-label="Changer la photo"/
        : /aria-label="Ajouter une photo"[^>]*> Ajouter <\/button>/,
    )
    assert.equal((html.match(/id="trigger-avatar"/g) ?? []).length, 1)
    assert.match(controls[0]!, /^<button id="trigger-avatar" type="button"/)
    assert.doesNotMatch(html, />\s*Changer\s*</)
    assert.match(controls[0]!, /aria-describedby="avatar-help"/)
    if (url)
      assert.match(
        controls[1]!,
        /aria-label="Supprimer la photo"[^>]*> Supprimer <\/button>/,
      )
    for (const control of controls) {
      assert.match(control, /min-h-11/)
      assert.doesNotMatch(control, /\bdisabled(?: |>)/)
    }
    assert.match(
      html,
      /<input[^>]*type="file"[^>]*accept="image\/jpeg,image\/png,image\/webp,\.jpg,\.jpeg,\.png,\.webp"[^>]*aria-label="Choisir une photo"/,
    )
  }
})

test('existing photo is an editable image or placeholder with pointer, keyboard and touch affordances', async () => {
  for (const image of ['blob:synthetic-avatar', '']) {
    const html = await avatarMarkup({ url: '/api/v1/account/avatar/1', image })
    const [edit, remove] = buttons(html)
    assert.ok(edit)
    assert.ok(remove)
    assert.match(edit, /size-16 min-h-11 min-w-11/)
    assert.match(
      edit,
      /focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink/,
    )
    assert.match(
      edit,
      /<span aria-hidden="true" class="pointer-events-none[^"]*size-6[^"]*opacity-0/,
    )
    for (const variant of [
      'group-hover:opacity-100',
      'group-focus-visible:opacity-100',
      '[@media(hover:none)]:opacity-100',
      '[@media(pointer:coarse)]:opacity-100',
    ])
      assert.ok(edit.includes(variant), variant)
    assert.match(edit, /lucide-pencil/)
    if (image) assert.match(edit, /<img src="blob:synthetic-avatar"/)
    else {
      assert.match(edit, /lucide-user-round/)
      assert.doesNotMatch(edit, /<img/)
    }
    assert.match(remove, /aria-label="Supprimer la photo"/)
    assert.doesNotMatch(edit, /Supprimer/)
    assert.doesNotMatch(remove, /lucide-pencil|trigger-avatar/)
  }
  // SSR omits event handlers; retain the existing picker/delete binding contract.
  assert.match(
    descriptor.template!.content,
    /aria-label="Changer la photo"\s+aria-describedby="avatar-help"\s+@click="openPicker"/,
  )
  assert.match(
    descriptor.template!.content,
    /aria-label="Supprimer la photo"\s+@click="save\(true\)"/,
  )
})

test('blocked photo and empty states disable all native controls without losing focus target IDs', async () => {
  for (const url of [null, '/api/v1/account/avatar/1']) {
    const html = await avatarMarkup({ url, disabled: true })
    assert.equal((html.match(/id="trigger-avatar"/g) ?? []).length, 1)
    for (const control of buttons(html)) {
      assert.match(control, /\bdisabled(?: |>)/)
      assert.match(control, /min-h-11/)
    }
    assert.match(html, /<input[^>]* disabled/)
    if (!url) {
      assert.doesNotMatch(html, /lucide-pencil|Supprimer la photo/)
      assert.match(html, /lucide-user-round/)
    }
  }
})

test('selected file, disabled actions, upload progress and errors remain visible', async () => {
  const html = await avatarMarkup({
    url: '/api/v1/account/avatar/1',
    selected: new File(['synthetic'], 'portrait.png'),
    disabled: true,
    pending: true,
    uploading: true,
    progress: 42,
    error: 'Envoi impossible.',
    previewError: 'Photo indisponible.',
  })
  assert.match(html, /aria-busy="true"/)
  assert.match(html, /portrait\.png/)
  assert.match(html, /<input[^>]* disabled/)
  const controls = buttons(html)
  assert.equal(controls.length, 5)
  for (const control of controls) {
    assert.match(control, /min-h-11/)
    assert.match(control, /\bdisabled(?: |>)/)
  }
  for (const label of ['Enregistrer', 'Annuler', 'Recharger la photo'])
    assert.ok(controls.some((control) => control.includes(label)))
  assert.match(
    html,
    /<progress[^>]*value="42"[^>]*max="100"[^>]*aria-label="Envoi de la photo"/,
  )
  assert.match(html, /Envoi de la photo : 42 %/)
  assert.match(html, /role="alert"[^>]*>Envoi impossible\.<\/p>/)
  assert.match(html, /role="alert"[^>]*>Photo indisponible\.<\/p>/)
})

test('delete and processing statuses retain their existing feedback', async () => {
  const deleting = await avatarMarkup({ pending: true })
  assert.match(deleting, /role="status"/)
  assert.match(deleting, /Suppression…/)
  assert.doesNotMatch(deleting, /<progress/)
  const processing = await avatarMarkup({
    pending: true,
    uploading: true,
    progress: 100,
  })
  assert.match(processing, /Traitement…/)
  assert.doesNotMatch(processing, /<progress/)
})
