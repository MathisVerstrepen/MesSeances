import { spawn } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import { test as base, expect, type Page } from '@playwright/test'

const serverScript = fileURLToPath(
  new URL('../tools/playwright-server.mjs', import.meta.url),
)

// Each worker owns a stateful synthetic API and Nuxt fixture on ephemeral ports,
// so scenario state never crosses parallel workers.
async function startFixtureServer(label: string) {
  const child = spawn(process.execPath, [serverScript], {
    env: {
      ...process.env,
      PLAYWRIGHT_PORT: '0',
      NUXT_TELEMETRY_DISABLED: '1',
    },
    stdio: ['ignore', 'pipe', 'pipe', 'ipc'],
  })
  child.stderr.on('data', (chunk: Buffer) => {
    process.stderr.write(`[fixture ${label}] ${chunk}`)
  })
  const origin = await new Promise<string>((resolve, reject) => {
    let output = ''
    let ready = false
    const timer = setTimeout(
      () => reject(new Error('Playwright fixture server start timed out')),
      60000,
    )
    child.once('exit', (code, signal) => {
      clearTimeout(timer)
      reject(new Error(`Playwright fixture server exited (${code ?? signal})`))
    })
    // Keep draining stdout after readiness so the child never blocks on a full pipe.
    child.stdout.on('data', (chunk: Buffer) => {
      if (ready) return
      output += chunk
      const match = /PLAYWRIGHT_READY (http:\/\/127\.0\.0\.1:\d+)/.exec(output)
      if (!match) return
      ready = true
      clearTimeout(timer)
      resolve(match[1])
    })
  })
  async function stop() {
    if (child.exitCode !== null || child.signalCode !== null) return
    const exited = new Promise((resolve) => child.once('exit', resolve))
    child.kill('SIGTERM')
    const timer = setTimeout(() => child.kill('SIGKILL'), 10000)
    await exited
    clearTimeout(timer)
  }
  return { origin, stop }
}

export const test = base.extend<{ scenario: void }, { fixtureOrigin: string }>({
  fixtureOrigin: [
    // oxlint-disable-next-line no-empty-pattern -- Playwright requires a destructured first argument.
    async ({}, use, workerInfo) => {
      const server = await startFixtureServer(String(workerInfo.parallelIndex))
      try {
        await use(server.origin)
      } finally {
        await server.stop()
      }
    },
    { scope: 'worker', timeout: 90000 },
  ],
  baseURL: async ({ fixtureOrigin }, use) => {
    await use(fixtureOrigin)
  },
  // Auto fixtures run for every importing spec, unlike module-level hooks,
  // which are registered only on the first file to import a cached module.
  scenario: [
    async ({ request }, use) => {
      const ready = await request.post('/__playwright/scenario', { data: {} })
      expect(ready.ok()).toBeTruthy()
      await use()
      const response = await request.get('/__playwright/state')
      expect(response.ok()).toBeTruthy()
      expect(
        (await response.json()).unexpected,
        'Unmocked API requests',
      ).toEqual([])
    },
    { auto: true },
  ],
  page: async ({ page }, use) => {
    const errors: string[] = []
    const external: string[] = []
    page.on('pageerror', (error) => errors.push(error.message))
    await page.context().route('**/*', (route) => {
      const url = new URL(route.request().url())
      if (url.hostname === '127.0.0.1' || url.protocol === 'data:')
        return route.continue()
      external.push(url.origin)
      return route.abort()
    })
    await use(page)
    expect(errors, 'Uncaught browser errors').toEqual([])
    expect(external, 'Attempted external requests').toEqual([])
  },
})

export { expect }

export async function openPage(page: Page, path: string) {
  const response = await page.goto(path)
  // The fixture's app:mounted hook marks hydration completion, without sleeps.
  await expect(page.locator('html')).toHaveAttribute(
    'data-playwright-ready',
    'true',
  )
  return response
}
