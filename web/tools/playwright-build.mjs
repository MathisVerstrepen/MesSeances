// One production Nuxt build shared by every Playwright worker's fixture server.
// Isolated from the developer's .nuxt/.output; reads no deployment dotenv.
import { existsSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { buildNuxt, loadNuxt } from '@nuxt/kit'

const root = new URL('..', import.meta.url)
export const fixtureBuildDir = fileURLToPath(
  new URL('node_modules/.cache/playwright-build/', root),
)
export const fixtureOutputDir = `${fixtureBuildDir}output`
export const fixtureServerEntry = `${fixtureOutputDir}/server/index.mjs`
const lockPath = `${fixtureBuildDir.replace(/\/$/, '')}.lock`

function alive(pid) {
  try {
    process.kill(pid, 0)
    return true
  } catch (error) {
    return error.code === 'EPERM'
  }
}

// Concurrent suites in one worktree would overwrite each other's build.
export function acquireBuildLock() {
  try {
    writeFileSync(lockPath, String(process.pid), { flag: 'wx', mode: 0o600 })
    return
  } catch (error) {
    if (error.code !== 'EEXIST') throw error
  }
  const owner = Number(readFileSync(lockPath, 'utf8'))
  if (Number.isInteger(owner) && owner !== process.pid && alive(owner))
    throw new Error(
      `Another Playwright run (pid ${owner}) owns ${lockPath}; wait for it to finish.`,
    )
  writeFileSync(lockPath, String(process.pid), { mode: 0o600 })
}

export function releaseBuildLock() {
  try {
    if (Number(readFileSync(lockPath, 'utf8')) === process.pid)
      rmSync(lockPath, { force: true })
  } catch {
    // Already released.
  }
}

export async function buildFixture() {
  const nuxt = await loadNuxt({
    cwd: fileURLToPath(root),
    dev: false,
    dotenv: false,
    overrides: {
      buildDir: `${fixtureBuildDir}nuxt`,
      // Warnings and errors only; the per-file output listing is noise here.
      logLevel: 'warn',
      typescript: { typeCheck: false },
      plugins: [
        fileURLToPath(new URL('../e2e/readiness.client.ts', import.meta.url)),
      ],
      sourcemap: { server: false, client: false },
      hooks: {
        'vite:extendConfig'(config) {
          // Nuxt supplies aliases; don't discover the developer's .nuxt tsconfig.
          config.oxc = { ...config.oxc, tsconfig: false }
          config.logLevel = 'warn'
        },
      },
      nitro: {
        // In-process listener: the fixture server owns HTTP, /api and teardown.
        preset: 'node-listener',
        serveStatic: true,
        output: { dir: fixtureOutputDir },
      },
      // Per-worker origins and API ports are applied through NUXT_* at runtime.
      runtimeConfig: {
        apiBase: 'http://127.0.0.1:9',
        internalApiSharedSecret: '',
        public: {
          apiBase: '',
          siteUrl: 'http://127.0.0.1:9',
          umamiScriptUrl: '',
          umamiWebsiteId: '',
        },
      },
    },
  })
  try {
    await buildNuxt(nuxt)
  } finally {
    await nuxt.close()
  }
  if (!existsSync(fixtureServerEntry))
    throw new Error('Playwright fixture build produced no server entry')
}

// Explicit opt-in for focused local loops; a stale build is the caller's choice.
export function reusableBuild() {
  return (
    process.env.PLAYWRIGHT_REUSE_BUILD === '1' && existsSync(fixtureServerEntry)
  )
}
