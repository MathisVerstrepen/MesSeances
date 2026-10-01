import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import ts from 'typescript'
import { computed, effectScope, nextTick, reactive, ref, watch } from 'vue'
import type { Ref } from 'vue'
import { posterImageSources } from '../app/utils/safeImageUrl.ts'

const source = await readFile(
  new URL('../app/components/FilmPoster.vue', import.meta.url),
  'utf8',
)
const page = await readFile(
  new URL('../app/pages/film/[slug].vue', import.meta.url),
  'utf8',
)
const parsed = ts.createSourceFile(
  'FilmPoster.ts',
  source.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)![1]!,
  ts.ScriptTarget.Latest,
  true,
)
const compiled = ts.transpileModule(
  parsed.statements
    .filter((statement) => !ts.isImportDeclaration(statement))
    .map((statement) => statement.getFullText(parsed))
    .join('\n'),
  { compilerOptions: { target: ts.ScriptTarget.ESNext } },
).outputText
const POSTER = 'https://image.tmdb.org/t/p/w500/poster.jpg'

interface Poster {
  trigger: Ref<unknown>
  dialog: Ref<unknown>
  closeButton: Ref<unknown>
  canEnlarge: Ref<boolean>
  isOpen: Ref<boolean>
  openModal: () => Promise<void>
  closeModal: () => void
  detectLoadedThumbnail: () => void
  handleImageError: () => void
}

function harness(src: string | null = POSTER) {
  const scope = effectScope()
  const props = reactive({ src, movieTitle: 'Film', resetKey: 'film' })
  const image = { complete: false, naturalWidth: 0 }
  const style = { overflow: 'scroll' }
  const calls = { show: 0, close: 0, triggerFocus: 0, closeFocus: 0 }
  const mounted: Array<() => void | Promise<void>> = []
  const unmounted: Array<() => void> = []
  const bindings = {
    defineProps: () => props,
    ref,
    computed,
    nextTick,
    watch: (...args: Parameters<typeof watch>) =>
      scope.run(() => watch(...args)),
    posterImageSources,
    useId: () => 'poster-dialog',
    onMounted: (callback: () => void | Promise<void>) => mounted.push(callback),
    onBeforeUnmount: (callback: () => void) => unmounted.push(callback),
    document: { documentElement: { style } },
  }
  // Execute actual component setup and Vue watchers; mock only browser/Nuxt IO.
  // SAFETY: The wrapper explicitly returns the listed refs and methods from the component setup.
  const poster = new Function(
    ...Object.keys(bindings),
    `${compiled}\nreturn { trigger, dialog, closeButton, canEnlarge, isOpen, openModal, closeModal, detectLoadedThumbnail, handleImageError };`,
  )(...Object.values(bindings)) as Poster
  poster.trigger.value = {
    isConnected: true,
    querySelector: () => image,
    focus: () => calls.triggerFocus++,
  }
  poster.dialog.value = {
    showModal: () => calls.show++,
    close: () => calls.close++,
  }
  poster.closeButton.value = { focus: () => calls.closeFocus++ }
  return {
    poster,
    props,
    image,
    style,
    calls,
    mount: async () => {
      for (const callback of mounted) await callback()
    },
    dispose: () => {
      for (const callback of unmounted) callback()
      scope.stop()
    },
  }
}

test('film page preserves poster layout and delegates only poster interaction', () => {
  assert.match(page, /<FilmPoster\s+:src="schedule\.movie\.poster_url"/u)
  assert.match(page, /:movie-title="schedule\.movie\.title"/u)
  assert.match(page, /:reset-key="slug"/u)
  assert.match(page, /aspect-\[2\/3\] w-40/u)
  assert.match(source, /<PosterImage/u)
  assert.match(
    source,
    /sizes="\(min-width: 1024px\) 220px, \(min-width: 640px\) 180px, 160px"/u,
  )
  assert.match(
    source,
    /fallback-class="gap-2 px-3 text-center text-xs font-bold text-muted"/u,
  )
})

test('thumbnail must load successfully before mouse or keyboard can enlarge it', async (t) => {
  const h = harness()
  t.after(h.dispose)
  await h.poster.openModal()
  assert.equal(h.poster.isOpen.value, false)
  h.image.complete = true
  h.image.naturalWidth = 500
  h.poster.detectLoadedThumbnail()
  assert.equal(h.poster.canEnlarge.value, true)
  await h.poster.openModal()
  await h.poster.openModal()
  assert.equal(h.poster.isOpen.value, true)
  assert.equal(h.calls.show, 1)
  assert.equal(h.calls.closeFocus, 1)
  assert.equal(h.style.overflow, 'hidden')
  h.poster.closeModal()
  await nextTick()
  assert.equal(h.poster.isOpen.value, false)
  assert.equal(h.calls.close, 1)
  assert.equal(h.calls.triggerFocus, 1)
  assert.equal(h.style.overflow, 'scroll')
  assert.match(source, /<button\s+ref="trigger"\s+type="button"/u)
  assert.match(source, /:disabled="!canEnlarge"/u)
  assert.match(source, /@load="detectLoadedThumbnail"/u)
})

test('cached thumbnails enable enlargement, but missing, unsafe and failed images do not', async (t) => {
  for (const src of [POSTER, null, 'https://untrusted.test/poster.jpg']) {
    const h = harness(src)
    t.after(h.dispose)
    h.image.complete = true
    h.image.naturalWidth = 500
    await h.mount()
    assert.equal(h.poster.canEnlarge.value, src === POSTER)
    h.image.naturalWidth = 0
    h.poster.detectLoadedThumbnail()
    await h.poster.openModal()
    assert.equal(h.poster.canEnlarge.value, false)
    assert.equal(h.poster.isOpen.value, false)
  }
})

test('image errors dismiss enlargement and prevent reopening a failed poster', async (t) => {
  const h = harness()
  t.after(h.dispose)
  h.image.complete = true
  h.image.naturalWidth = 500
  await h.mount()
  await h.poster.openModal()
  h.poster.handleImageError()
  await h.poster.openModal()
  assert.equal(h.poster.canEnlarge.value, false)
  assert.equal(h.poster.isOpen.value, false)
  assert.equal(h.style.overflow, 'scroll')
  assert.equal(source.match(/@error="handleImageError"/gu)?.length, 2)
})

test('film/source changes and unmount close modal and release scroll lock without stale focus', async (t) => {
  for (const change of ['src', 'resetKey', 'movieTitle', 'unmount'] as const) {
    const h = harness()
    t.after(h.dispose)
    h.image.complete = true
    h.image.naturalWidth = 500
    await h.mount()
    await h.poster.openModal()
    h.image.complete = false
    if (change === 'unmount') h.dispose()
    else h.props[change] = 'changed'
    await nextTick()
    assert.equal(h.poster.isOpen.value, false, change)
    assert.equal(h.style.overflow, 'scroll', change)
    assert.equal(h.calls.triggerFocus, 0, change)
  }
})

test('closing before next render never opens a stale dialog or locks scroll', async (t) => {
  const h = harness()
  t.after(h.dispose)
  h.image.complete = true
  h.image.naturalWidth = 500
  await h.mount()
  const opening = h.poster.openModal()
  h.poster.closeModal()
  await opening
  assert.equal(h.calls.show, 0)
  assert.equal(h.style.overflow, 'scroll')
})

test('native modal contains focus, supports Escape/outside dismissal and keeps poster clicks open', () => {
  assert.match(source, /<dialog\s+v-if="isOpen && imageSources\.src"/u)
  assert.match(source, /dialog\.value\.showModal\(\)/u)
  assert.match(source, /:aria-label="`Affiche de \$\{movieTitle\}`"/u)
  assert.match(source, /@cancel\.prevent="closeModal\(\)"/u)
  assert.equal(source.match(/@click\.self="closeModal\(\)"/gu)?.length, 2)
  assert.match(source, /@keydown\.tab\.prevent="closeButton\?\.focus/u)
  assert.match(source, /aria-label="Fermer l’affiche"/u)
  const enlargedImage = source.match(/<img\s[\s\S]*?>/u)?.[0]
  assert.ok(enlargedImage)
  assert.doesNotMatch(enlargedImage, /@click/u)
  assert.match(enlargedImage, /object-contain/u)
  assert.match(enlargedImage, /max-h-\[calc\(100dvh_-_6rem\)\]/u)
  assert.match(enlargedImage, /max-w-full/u)
  assert.match(enlargedImage, /:src="imageSources\.src"/u)
  assert.match(source, /posterImageSources\(props\.src\)/u)
})
