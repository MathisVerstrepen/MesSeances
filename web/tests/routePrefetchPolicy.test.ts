import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const nuxtConfigSource = await readFile(new URL('../nuxt.config.ts', import.meta.url), 'utf8')

test('NuxtLink defaults prefetch routes on interaction instead of visibility', () => {
  assert.match(
    nuxtConfigSource,
    /nuxtLink:\s*{\s*prefetchOn:\s*{\s*visibility:\s*false,\s*interaction:\s*true\s*}\s*}/
  )
})
