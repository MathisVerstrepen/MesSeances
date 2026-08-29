import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

test('keeps share controls below the app header and share popups above it', async () => {
  const [shareButton, appHeader] = await Promise.all([
    readFile(new URL('../app/components/ShareButton.vue', import.meta.url), 'utf8'),
    readFile(new URL('../app/components/AppHeader.vue', import.meta.url), 'utf8')
  ])

  const controlZIndex = Number(shareButton.match(/\.share-control \{[^}]*z-index: (\d+);/s)?.[1])
  const popupZIndex = Number(shareButton.match(/\.share-popup \{[^}]*z-index: (\d+);/s)?.[1])
  const headerZIndex = Number(appHeader.match(/class="[^"]*\bz-(\d+)\b[^"]*"/)?.[1])

  assert.ok(controlZIndex < headerZIndex)
  assert.ok(popupZIndex > headerZIndex)
})
